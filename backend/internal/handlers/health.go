package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type HealthProbe func(context.Context) bool

type HealthHandler struct {
	mongoProbe   HealthProbe
	redisProbe   HealthProbe
	indexesProbe HealthProbe
}

func NewHealthHandler(mongoProbe, redisProbe HealthProbe, indexesProbe ...HealthProbe) *HealthHandler {
	handler := &HealthHandler{mongoProbe: mongoProbe, redisProbe: redisProbe}
	if len(indexesProbe) > 0 {
		handler.indexesProbe = indexesProbe[0]
	}
	return handler
}

func (h *HealthHandler) Check(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	mongoStatus := "down"
	if h.mongoProbe != nil && h.mongoProbe(ctx) {
		mongoStatus = "up"
	}
	redisStatus := "down"
	if h.redisProbe != nil && h.redisProbe(ctx) {
		redisStatus = "up"
	}
	indexesStatus := "up"
	if h.indexesProbe != nil && !h.indexesProbe(ctx) {
		indexesStatus = "down"
	}

	status := http.StatusOK
	state := "ok"
	if mongoStatus != "up" || redisStatus != "up" {
		status = http.StatusServiceUnavailable
		state = "degraded"
	} else if indexesStatus != "up" {
		// Keep the process available for diagnosis while exposing the failed index
		// state in the response. Render treats 5xx health checks as failed deploys.
		state = "degraded"
	}
	c.JSON(status, gin.H{"status": state, "mongo": mongoStatus, "redis": redisStatus, "indexes": indexesStatus})
}
