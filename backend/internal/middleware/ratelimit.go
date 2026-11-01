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

func RateLimit(store RateLimitStore, scope string, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := ClientIP(c)
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

// ClientIP uses Render's Cloudflare-overwritten client-IP header when present.
// It falls back to the socket address for local development and non-Render hosts.
func ClientIP(c *gin.Context) string {
	if forwarded := net.ParseIP(strings.TrimSpace(c.GetHeader("CF-Connecting-IP"))); forwarded != nil {
		return forwarded.String()
	}
	if host, _, err := net.SplitHostPort(c.Request.RemoteAddr); err == nil {
		return host
	}
	return c.Request.RemoteAddr
}
