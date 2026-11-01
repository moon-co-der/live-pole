package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"

	"live-polling/internal/models"
	"live-polling/internal/realtime"
)

func TestWebSocketRejectsWrongOriginAndUnknownPoll(t *testing.T) {
	gin.SetMode(gin.TestMode)
	polls, counts := newMemoryPolls(), newMemoryCounts()
	id := bson.ObjectID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	polls.items[id.Hex()] = &models.Poll{ID: id, Slug: "testSlug", Question: "Favorite place?", Status: models.PollActive, Options: []models.PollOption{{ID: "a", Text: "Library"}, {ID: "b", Text: "Cafe"}}}
	hub := realtime.NewHub()
	go hub.Run()
	defer hub.Stop()
	router := gin.New()
	router.GET("/ws/polls/:slug", NewWebSocketHandler(polls, &memoryVotes{}, counts, hub, "https://app.example.invalid", realtime.Settings{PingInterval: time.Hour, PongWait: 2 * time.Hour, WriteWait: time.Second, MaxMessage: 1024, MaxPerIP: 2}).Serve)
	wrongOrigin := httptest.NewRequest(http.MethodGet, "/ws/polls/testSlug", nil)
	wrongOrigin.Header.Set("Origin", "https://wrong.example")
	wrongResult := httptest.NewRecorder()
	router.ServeHTTP(wrongResult, wrongOrigin)
	if wrongResult.Code != http.StatusForbidden {
		t.Fatalf("wrong-origin status=%d", wrongResult.Code)
	}
	unknownPoll := httptest.NewRequest(http.MethodGet, "/ws/polls/missing", nil)
	unknownPoll.Header.Set("Origin", "https://app.example.invalid")
	unknownResult := httptest.NewRecorder()
	router.ServeHTTP(unknownResult, unknownPoll)
	if unknownResult.Code != http.StatusNotFound {
		t.Fatalf("unknown-poll status=%d", unknownResult.Code)
	}
	validPoll := httptest.NewRequest(http.MethodGet, "/ws/polls/testSlug", nil)
	validPoll.Header.Set("Origin", "https://app.example.invalid")
	validResult := httptest.NewRecorder()
	router.ServeHTTP(validResult, validPoll)
	if validResult.Code != http.StatusBadRequest {
		t.Fatalf("non-WebSocket valid-origin request status=%d", validResult.Code)
	}
}
