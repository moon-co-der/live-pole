package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"live-polling/internal/middleware"
	"live-polling/internal/models"
	"live-polling/internal/realtime"
	"live-polling/internal/repository"
	"live-polling/internal/services"
)

type websocketCounts interface {
	Exists(context.Context, string) (bool, error)
	HGetAll(context.Context, string) (map[string]int64, error)
	HSetTTL(context.Context, string, map[string]string, time.Duration) error
	GetDel(context.Context, string) (string, bool, error)
}

type WebSocketHandler struct {
	polls             repository.PollStore
	votes             repository.VoteStore
	counts            websocketCounts
	hub               *realtime.Hub
	origin            string
	settings          realtime.Settings
	trustProxyHeaders bool
}

func NewWebSocketHandler(polls repository.PollStore, votes repository.VoteStore, counts websocketCounts, hub *realtime.Hub, origin string, settings realtime.Settings, trustProxyHeaders bool) *WebSocketHandler {
	return &WebSocketHandler{polls: polls, votes: votes, counts: counts, hub: hub, origin: strings.TrimRight(origin, "/"), settings: settings, trustProxyHeaders: trustProxyHeaders}
}

func (h *WebSocketHandler) Serve(c *gin.Context) {
	if !middleware.IsAllowedOrigin(h.origin, c.GetHeader("Origin")) {
		writeError(c, http.StatusForbidden, "Origin not allowed.")
		return
	}
	poll, err := h.polls.FindBySlug(c.Request.Context(), c.Param("slug"))
	if err != nil {
		writeError(c, http.StatusInternalServerError, "Unable to load this poll.")
		return
	}
	if poll == nil || poll.Status == models.PollDraft {
		writeError(c, http.StatusNotFound, "Poll not found.")
		return
	}
	if !websocket.IsWebSocketUpgrade(c.Request) {
		writeError(c, http.StatusBadRequest, "WebSocket upgrade required.")
		return
	}
	ticket := c.Query("ticket")
	if ticket == "" {
		writeError(c, http.StatusForbidden, "Vote before joining live results.")
		return
	}
	ticketValue, found, err := h.counts.GetDel(c.Request.Context(), "poll-live-ticket:"+ticket)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "Unable to authorize live results.")
		return
	}
	parts := strings.Split(ticketValue, ":")
	if !found || len(parts) != 2 || parts[0] != poll.ID.Hex() {
		writeError(c, http.StatusForbidden, "Live results access is not authorized.")
		return
	}
	voted, err := h.counts.Exists(c.Request.Context(), "voted:"+parts[0]+":"+parts[1])
	if err != nil {
		writeError(c, http.StatusInternalServerError, "Unable to authorize live results.")
		return
	}
	if !voted {
		writeError(c, http.StatusForbidden, "Vote before joining live results.")
		return
	}
	if err := services.EnsurePollCounts(c.Request.Context(), poll, h.counts, h.votes); err != nil {
		writeError(c, http.StatusInternalServerError, "Unable to load poll results.")
		return
	}
	ip := middleware.ClientIP(c, h.trustProxyHeaders)
	if !h.hub.AcquireIP(ip, h.settings.MaxPerIP) {
		writeError(c, http.StatusTooManyRequests, "Too many live connections.")
		return
	}
	upgrader := websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024, CheckOrigin: func(r *http.Request) bool { return middleware.IsAllowedOrigin(h.origin, r.Header.Get("Origin")) }}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.hub.ReleaseIP(ip)
		return
	}
	client := realtime.NewClient(conn, h.hub, poll.ID.Hex(), ip, h.settings)
	if !h.hub.Register(client) {
		h.hub.ReleaseIP(ip)
		_ = conn.Close()
		return
	}
	counts, err := h.counts.HGetAll(c.Request.Context(), "poll:"+poll.ID.Hex()+":counts")
	if err != nil {
		h.hub.Unregister(client)
		h.hub.ReleaseIP(ip)
		return
	}
	total := counts["total"]
	delete(counts, "total")
	if !client.Send(realtime.Message{Type: "snapshot", Question: poll.Question, Options: publicOptions(poll), Counts: counts, Total: total, Status: poll.Status}) {
		h.hub.Unregister(client)
		h.hub.ReleaseIP(ip)
		return
	}
	go client.WritePump()
	client.ReadPump()
	h.hub.ReleaseIP(ip)
}

func publicOptions(poll *models.Poll) []models.PublicPollOption {
	options := make([]models.PublicPollOption, 0, len(poll.Options))
	for _, option := range poll.Options {
		options = append(options, models.PublicPollOption{ID: option.ID, Text: option.Text})
	}
	return options
}
