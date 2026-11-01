package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type HealthProbe func(context.Context) bool

type HealthHandler struct {
	mongoProbe HealthProbe
	redisProbe HealthProbe
}

func NewHealthHandler(mongoProbe, redisProbe HealthProbe) *HealthHandler {
	return &HealthHandler{mongoProbe: mongoProbe, redisProbe: redisProbe}
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

	status := http.StatusOK
	state := "ok"
	if mongoStatus != "up" || redisStatus != "up" {
		status = http.StatusServiceUnavailable
		state = "degraded"
	}
	c.JSON(status, gin.H{"status": state, "mongo": mongoStatus, "redis": redisStatus})
}
