package middleware

import (
	"context"
	"math"
	"net"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type RateLimitStore interface {
	AllowRateLimit(context.Context, string, int, time.Duration) (bool, time.Duration, error)
}

func RateLimit(store RateLimitStore, scope string, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.Request.RemoteAddr
		if host, _, err := net.SplitHostPort(ip); err == nil {
			ip = host
		}
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
