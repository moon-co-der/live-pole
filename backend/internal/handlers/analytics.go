package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"live-polling/internal/services"
)

type AnalyticsHandler struct{ analytics *services.AnalyticsService }

func NewAnalyticsHandler(analytics *services.AnalyticsService) *AnalyticsHandler {
	return &AnalyticsHandler{analytics: analytics}
}
func (h *AnalyticsHandler) Overview(c *gin.Context) {
	owner, ok := c.Get("userId")
	if !ok {
		writeError(c, 401, "Authentication required.")
		return
	}
	data, err := h.analytics.Overview(c.Request.Context(), owner.(string))
	if err != nil {
		writeError(c, 500, "Unable to load analytics.")
		return
	}
	c.JSON(200, data)
}
func (h *AnalyticsHandler) VotesOverTime(c *gin.Context) {
	owner, ok := c.Get("userId")
	if !ok {
		writeError(c, 401, "Authentication required.")
		return
	}
	days := 7
	if raw := c.Query("days"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || (value != 7 && value != 30) {
			writeError(c, 400, "Days must be 7 or 30.")
			return
		}
		days = value
	}
	data, err := h.analytics.VotesOverTime(c.Request.Context(), owner.(string), days)
	if errors.Is(err, services.ErrInvalidPoll) {
		writeError(c, 400, "Days must be 7 or 30.")
		return
	}
	if err != nil {
		writeError(c, 500, "Unable to load vote history.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"series": data, "days": days})
}
func (h *AnalyticsHandler) TopPolls(c *gin.Context) {
	owner, ok := c.Get("userId")
	if !ok {
		writeError(c, 401, "Authentication required.")
		return
	}
	data, err := h.analytics.TopPolls(c.Request.Context(), owner.(string))
	if err != nil {
		writeError(c, 500, "Unable to load top polls.")
		return
	}
	c.JSON(200, gin.H{"polls": data})
}
func (h *AnalyticsHandler) Poll(c *gin.Context) {
	owner, ok := c.Get("userId")
	if !ok {
		writeError(c, 401, "Authentication required.")
		return
	}
	data, err := h.analytics.Poll(c.Request.Context(), owner.(string), c.Param("id"))
	if errors.Is(err, services.ErrPollNotFound) {
		writeError(c, 404, "Poll not found.")
		return
	}
	if err != nil {
		writeError(c, 500, "Unable to load poll analytics.")
		return
	}
	c.JSON(200, data)
}
