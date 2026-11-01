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
	router.Use(RateLimit(fixedRateLimit{allowed: false, retry: 17 * time.Second}, "test", 1, time.Minute, false))
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
	router.Use(RateLimit(fixedRateLimit{err: context.DeadlineExceeded}, "test", 1, time.Minute, false))
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestClientIPUsesSocketUnlessProxyHeadersTrusted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.10:4567"
	request.Header.Set("X-Forwarded-For", "198.51.100.99, 203.0.113.8")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = request
	if got := ClientIP(c, false); got != "192.0.2.10" {
		t.Fatalf("untrusted ClientIP=%q", got)
	}
	if got := ClientIP(c, true); got != "203.0.113.8" {
		t.Fatalf("trusted ClientIP=%q", got)
	}
}

func TestClientIPFallsBackForInvalidProxyHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.10:4567"
	request.Header.Set("X-Forwarded-For", "not-an-ip")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = request
	if got := ClientIP(c, true); got != "192.0.2.10" {
		t.Fatalf("ClientIP=%q", got)
	}
}
