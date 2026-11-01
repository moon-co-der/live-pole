package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

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
		log.Fatal("MongoDB setup failed")
	}
	defer func() { _ = mongoClient.Disconnect(context.Background()) }()
	startupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := config.PingMongo(startupCtx, mongoClient); err != nil {
		log.Print("MongoDB is not ready at startup; health checks will report it.")
	}
	if err := redisx.Ping(startupCtx, cfg.RedisURL); err != nil {
		log.Print("Redis is not ready at startup; health checks will report it.")
	}
	cancel()

	users := repository.NewUserRepository(mongoClient.Database("live_polling").Collection("users"))
	indexCtx, indexCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := users.EnsureIndexes(indexCtx); err != nil {
		indexCancel()
		log.Fatal("User email index setup failed")
	}
	indexCancel()
	polls := repository.NewPollRepository(mongoClient.Database("live_polling").Collection("polls"))
	pollIndexCtx, pollIndexCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := polls.EnsureIndexes(pollIndexCtx); err != nil {
		pollIndexCancel()
		log.Fatal("Poll index setup failed")
	}
	pollIndexCancel()
	votes := repository.NewVoteRepository(mongoClient.Database("live_polling").Collection("votes"))
	voteIndexCtx, voteIndexCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := votes.EnsureIndexes(voteIndexCtx); err != nil {
		voteIndexCancel()
		log.Fatal("Vote index setup failed")
	}
	voteIndexCancel()
	views := repository.NewViewRepository(mongoClient.Database("live_polling").Collection("views"))
	viewIndexCtx, viewIndexCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := views.EnsureIndexes(viewIndexCtx); err != nil {
		viewIndexCancel()
		log.Fatal("View index setup failed")
	}
	viewIndexCancel()
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
