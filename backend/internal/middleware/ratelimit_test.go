package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type fixedRateLimit struct {
	allowed bool
	retry   time.Duration
	err     error
}

func (f fixedRateLimit) AllowRateLimit(context.Context, string, int, time.Duration) (bool, time.Duration, error) {
	return f.allowed, f.retry, f.err
}

func TestRateLimitReturnsRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RateLimit(fixedRateLimit{allowed: false, retry: 17 * time.Second}, "test", 1, time.Minute))
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d", response.Code)
	}
	if got := response.Header().Get("Retry-After"); got != "17" {
		t.Fatalf("Retry-After=%q", got)
	}
}

func TestRateLimitStoreFailureReturnsServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RateLimit(fixedRateLimit{err: context.DeadlineExceeded}, "test", 1, time.Minute))
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", response.Code)
	}
}
