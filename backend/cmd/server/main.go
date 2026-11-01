package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"live-polling/internal/config"
	"live-polling/internal/handlers"
	"live-polling/internal/middleware"
	"live-polling/internal/realtime"
	"live-polling/internal/redisx"
	"live-polling/internal/repository"
	"live-polling/internal/services"
)

func main() {
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	mongoClient, err := config.ConnectMongo(cfg.MongoURI)
	if err != nil {
		log.Printf("MongoDB setup failed error_type=%q driver_error=%q", mongoErrorCategory(err), redactedMongoError(err, cfg.MongoURI))
		log.Fatal("MongoDB setup failed")
	}
	defer func() { _ = mongoClient.Disconnect(context.Background()) }()
	startupCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	if err := config.PingMongo(startupCtx, mongoClient); err != nil {
		log.Printf("MongoDB startup ping failed database=%q error_type=%q driver_error=%q", "live_polling", mongoErrorCategory(err), redactedMongoError(err, cfg.MongoURI))
		log.Print("MongoDB is not ready at startup; health checks will report it.")
	}
	if err := redisx.Ping(startupCtx, cfg.RedisURL); err != nil {
		log.Print("Redis is not ready at startup; health checks will report it.")
	}
	cancel()

	database := mongoClient.Database("live_polling")
	users := repository.NewUserRepository(database.Collection("users"))
	polls := repository.NewPollRepository(database.Collection("polls"))
	votes := repository.NewVoteRepository(database.Collection("votes"))
	views := repository.NewViewRepository(database.Collection("views"))
	var indexesReady atomic.Bool
	indexFailures := 0
	ensureIndexes := func(collection string, ensure func(context.Context) error) {
		indexCtx, indexCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer indexCancel()
		if err := ensure(indexCtx); err != nil {
			indexFailures++
			log.Printf("MongoDB index setup failed database=%q collection=%q error_type=%q driver_error=%q", database.Name(), collection, mongoErrorCategory(err), redactedMongoError(err, cfg.MongoURI))
		}
	}
	ensureIndexes("users", users.EnsureIndexes)
	ensureIndexes("polls", polls.EnsureIndexes)
	ensureIndexes("votes", votes.EnsureIndexes)
	ensureIndexes("views", views.EnsureIndexes)
	indexesReady.Store(indexFailures == 0)
	if indexFailures > 0 {
		log.Printf("MongoDB index initialization incomplete database=%q failed_collections=%d; server will start degraded", database.Name(), indexFailures)
	}
	authService := services.NewAuthService(users)
	authHandler := handlers.NewAuthHandler(authService, cfg.JWTSecret)
	redisClient := redisx.NewClient(cfg.RedisURL)
	pollService := services.NewPollService(polls, votes, views, redisClient, cfg.RedisURL)
	pollHandler := handlers.NewPollHandler(pollService)
	publicPollHandler := handlers.NewPublicPollHandler(services.NewPublicPollService(polls, redisClient, votes, views, redisClient), cfg.CookieSecure)
	analyticsHandler := handlers.NewAnalyticsHandler(services.NewAnalyticsService(repository.NewAnalyticsRepository(mongoClient.Database("live_polling"))))
	hub := realtime.NewHub()
	hubDone := make(chan struct{})
	go func() { defer close(hubDone); hub.Run() }()
	settings := realtime.Settings{PingInterval: cfg.WSPingInterval, PongWait: cfg.WSPongWait, WriteWait: cfg.WSWriteWait, MaxMessage: cfg.WSMaxMessageBytes, MaxPerIP: cfg.WSMaxConnectionsPerIP}
	wsHandler := handlers.NewWebSocketHandler(polls, votes, redisClient, hub, cfg.ClientURL, settings, cfg.TrustProxyHeaders)
	dashboardWSHandler := handlers.NewDashboardWSHandler(redisClient, hub, cfg.ClientURL, settings, cfg.TrustProxyHeaders)
	workerCtx, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()
	host, _ := os.Hostname()
	reconcileCtx, stopReconcile := context.WithCancel(context.Background())
	defer stopReconcile()
	expiryCtx, stopExpiry := context.WithCancel(context.Background())
	defer stopExpiry()
	pubsubCtx, stopPubsub := context.WithCancel(context.Background())
	var background sync.WaitGroup
	background.Add(4)
	go func() {
		defer background.Done()
		services.RunVoteStreamWorker(workerCtx, redisClient, polls, votes, fmt.Sprintf("%s-%d", host, os.Getpid()))
	}()
	go func() { defer background.Done(); runReconciliation(reconcileCtx, polls, votes, redisClient) }()
	go func() { defer background.Done(); runExpiry(expiryCtx, pollService) }()
	go func() {
		defer background.Done()
		for pubsubCtx.Err() == nil {
			err := redisClient.SubscribePattern(pubsubCtx, "poll:*", func(channel, payload string) {
				pollID := strings.TrimPrefix(channel, "poll:")
				var message realtime.Message
				var ownerEvent struct {
					OwnerID string `json:"ownerId"`
				}
				if json.Unmarshal([]byte(payload), &message) == nil && pollID != "" {
					hub.Broadcast(pollID, message, message.Type == "poll_closed")
					_ = json.Unmarshal([]byte(payload), &ownerEvent)
					if ownerEvent.OwnerID != "" && message.Type == "update" {
						hub.BroadcastDashboard(ownerEvent.OwnerID, realtime.Message{Type: "dashboard:update", Data: map[string]any{"pollId": pollID, "counts": message.Counts, "total": message.Total}})
					}
				}
			})
			if pubsubCtx.Err() == nil {
				log.Printf("Redis live poll subscription interrupted: %v", err)
				time.Sleep(time.Second)
			}
		}
	}()

	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.Use(middleware.JSONRecovery(), middleware.CORS(cfg.ClientURL), middleware.SafeLogger())
	r.GET("/api/health", handlers.NewHealthHandler(
		func(ctx context.Context) bool { return config.PingMongo(ctx, mongoClient) == nil },
		func(ctx context.Context) bool { return redisx.Ping(ctx, cfg.RedisURL) == nil },
		func(context.Context) bool { return indexesReady.Load() },
	).Check)
	r.POST("/api/auth/register", middleware.RateLimit(redisClient, "register", 10, time.Minute, cfg.TrustProxyHeaders), authHandler.Register)
	r.POST("/api/auth/login", middleware.RateLimit(redisClient, "login", 10, time.Minute, cfg.TrustProxyHeaders), authHandler.Login)
	r.GET("/api/public/polls/:slug", middleware.RateLimit(redisClient, "public-get", 60, time.Minute, cfg.TrustProxyHeaders), publicPollHandler.Get)
	r.POST("/api/public/polls/:slug/vote", middleware.RateLimit(redisClient, "vote", 10, time.Minute, cfg.TrustProxyHeaders), publicPollHandler.Vote)
	r.GET("/ws/polls/:slug", middleware.RateLimit(redisClient, "websocket", 20, time.Minute, cfg.TrustProxyHeaders), wsHandler.Serve)
	r.GET("/ws/dashboard", middleware.RateLimit(redisClient, "dashboard-websocket", 20, time.Minute, cfg.TrustProxyHeaders), dashboardWSHandler.Serve)
	ownerRoutes := r.Group("/api")
	ownerRoutes.Use(middleware.AuthRequired(cfg.JWTSecret))
	ownerRoutes.GET("/auth/me", authHandler.Me)
	ownerRoutes.POST("/polls", pollHandler.Create)
	ownerRoutes.GET("/polls", pollHandler.List)
	ownerRoutes.GET("/polls/:id", pollHandler.Get)
	ownerRoutes.PATCH("/polls/:id/close", pollHandler.Close)
	ownerRoutes.DELETE("/polls/:id", pollHandler.Delete)
	ownerRoutes.POST("/ws/ticket", dashboardWSHandler.Ticket)
	ownerRoutes.GET("/analytics/overview", analyticsHandler.Overview)
	ownerRoutes.GET("/analytics/votes-over-time", analyticsHandler.VotesOverTime)
	ownerRoutes.GET("/analytics/top-polls", analyticsHandler.TopPolls)
	ownerRoutes.GET("/polls/:id/analytics", analyticsHandler.Poll)
	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Route not found"})
	})
	r.NoMethod(func(c *gin.Context) {
		c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "Method not allowed"})
	})

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("Livepoll API listening on :%s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrors <- err
		}
	}()

	shutdownSignal, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	select {
	case <-shutdownSignal.Done():
		log.Print("shutdown signal received")
	case err := <-serverErrors:
		log.Printf("HTTP server failed: %v", err)
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShutdown()
	stopWorker()
	stopReconcile()
	stopExpiry()
	stopPubsub()
	hub.Stop()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown: %v", err)
		_ = server.Close()
	}
	select {
	case <-hubDone:
	case <-shutdownCtx.Done():
		log.Print("timed out waiting for WebSocket hub shutdown")
	}
	backgroundDone := make(chan struct{})
	go func() { background.Wait(); close(backgroundDone) }()
	select {
	case <-backgroundDone:
	case <-shutdownCtx.Done():
		log.Print("timed out waiting for background workers")
	}
	if err := mongoClient.Disconnect(shutdownCtx); err != nil {
		log.Printf("MongoDB disconnect: %v", err)
	}
}

var mongoURIErrorPattern = regexp.MustCompile(`(?i)mongodb(?:\+srv)?://[^\s"'<>]+`)

func mongoErrorCategory(err error) string {
	if err == nil {
		return "none"
	}
	message := strings.ToLower(err.Error())
	var serverErr mongo.ServerError
	if errors.As(err, &serverErr) {
		switch {
		case serverErr.HasErrorCode(13):
			return "authorization"
		case serverErr.HasErrorCode(85), serverErr.HasErrorCode(86):
			return "index conflict"
		}
	}
	if mongo.IsDuplicateKeyError(err) {
		return "duplicate key"
	}
	switch {
	case strings.Contains(message, "not authorized"), strings.Contains(message, "unauthorized"), strings.Contains(message, "code 13"), strings.Contains(message, "authentication failed"), strings.Contains(message, "bad auth"), strings.Contains(message, "code 18"):
		return "authorization"
	case strings.Contains(message, "indexoptionsconflict"), strings.Contains(message, "indexkeyspecsconflict"), strings.Contains(message, "index conflict"):
		return "index conflict"
	case strings.Contains(message, "duplicate key"), strings.Contains(message, "e11000"):
		return "duplicate key"
	case strings.Contains(message, "x509"), strings.Contains(message, "tls"), strings.Contains(message, "certificate"):
		return "TLS"
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(message, "context deadline exceeded"):
		return "context deadline"
	case isNetTimeout(err), strings.Contains(message, "timeout"), strings.Contains(message, "timed out"):
		return "timeout"
	case strings.Contains(message, "server selection"), strings.Contains(message, "connection refused"), strings.Contains(message, "connection reset"), strings.Contains(message, "network"):
		return "network"
	case strings.Contains(message, "no such host"), strings.Contains(message, "lookup "), strings.Contains(message, "dns"):
		return "network"
	default:
		return "other"
	}
}

func isNetTimeout(err error) bool {
	var networkErr net.Error
	return errors.As(err, &networkErr) && networkErr.Timeout()
}

func redactedMongoError(err error, mongoURI string) string {
	message := err.Error()
	if parsed, parseErr := url.Parse(mongoURI); parseErr == nil && parsed.User != nil {
		message = strings.ReplaceAll(message, parsed.User.String(), "[REDACTED]")
		if password, ok := parsed.User.Password(); ok && password != "" {
			message = strings.ReplaceAll(message, password, "[REDACTED]")
		}
	}
	return mongoURIErrorPattern.ReplaceAllString(message, "[REDACTED_MONGODB_URI]")
}

func runReconciliation(ctx context.Context, polls repository.PollStore, votes repository.VoteStore, cache *redisx.Client) {
	reconcile := func() {
		taskCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		if _, err := services.ReconcilePollCounts(taskCtx, polls, votes, cache); err != nil {
			log.Printf("poll count reconciliation failed: %v", err)
		}
		cancel()
	}
	reconcile()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcile()
		}
	}
}

func runExpiry(ctx context.Context, pollService *services.PollService) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			taskCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			if _, err := pollService.ExpireDue(taskCtx); err != nil {
				log.Printf("poll expiry scan failed: %v", err)
			}
			cancel()
		}
	}
}
