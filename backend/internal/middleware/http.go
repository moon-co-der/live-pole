package middleware

import (
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"live-polling/internal/services"
)

func CORS(clientURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if IsAllowedOrigin(clientURL, origin) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Voter-Id, X-Poll-Referrer")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Header("Vary", "Origin")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// IsAllowedOrigin checks a comma-separated list of exact browser origins.
// It intentionally does not accept wildcard or suffix matches.
func IsAllowedOrigin(allowedOrigins, requestOrigin string) bool {
	requestOrigin = strings.TrimSpace(requestOrigin)
	if requestOrigin == "" {
		return false
	}
	for _, allowed := range strings.Split(allowedOrigins, ",") {
		if strings.TrimRight(strings.TrimSpace(allowed), "/") == requestOrigin {
			return true
		}
	}
	return false
}

func JSONRecovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		log.Printf("panic recovered: %v\n%s", recovered, debug.Stack())
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
	})
}

func SafeLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		log.Printf("http %s %s status=%d duration=%s", c.Request.Method, c.Request.URL.Path, c.Writer.Status(), time.Since(started).Round(time.Millisecond))
	}
}

func AuthRequired(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		parts := strings.Fields(c.GetHeader("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication required."})
			return
		}
		claims, err := services.VerifyToken(parts[1], secret, time.Now())
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authentication required."})
			return
		}
		c.Set("userId", claims.UserID)
		c.Next()
	}
}
