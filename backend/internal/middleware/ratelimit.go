package middleware

import (
	"context"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type RateLimitStore interface {
	AllowRateLimit(context.Context, string, int, time.Duration) (bool, time.Duration, error)
}

func RateLimit(store RateLimitStore, scope string, limit int, window time.Duration, trustProxyHeaders bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := ClientIP(c, trustProxyHeaders)
		allowed, retryAfter, err := store.AllowRateLimit(c.Request.Context(), "rate:"+scope+":"+ip, limit, window)
		if err != nil {
			c.AbortWithStatusJSON(503, gin.H{"error": "Request protection is temporarily unavailable."})
			return
		}
		if !allowed {
			seconds := int(math.Ceil(retryAfter.Seconds()))
			if seconds < 1 {
				seconds = 1
			}
			c.Header("Retry-After", strconv.Itoa(seconds))
			c.AbortWithStatusJSON(429, gin.H{"error": "Too many requests. Please try again shortly."})
			return
		}
		c.Next()
	}
}

// ClientIP uses Render's X-Forwarded-For header only when proxy headers are trusted.
// Render appends the client address, so use the rightmost valid entry to avoid
// accepting a caller-supplied value prepended to the chain.
func ClientIP(c *gin.Context, trustProxyHeaders bool) string {
	if trustProxyHeaders {
		forwarded := strings.Split(c.GetHeader("X-Forwarded-For"), ",")
		for i := len(forwarded) - 1; i >= 0; i-- {
			if ip := net.ParseIP(strings.TrimSpace(forwarded[i])); ip != nil {
				return ip.String()
			}
		}
	}
	if host, _, err := net.SplitHostPort(c.Request.RemoteAddr); err == nil {
		return host
	}
	return c.Request.RemoteAddr
}
