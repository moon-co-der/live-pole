package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.mongodb.org/mongo-driver/v2/bson"

	"live-polling/internal/middleware"
	"live-polling/internal/realtime"
)

type dashboardTicketStore interface {
	SetNXValue(context.Context, string, string, time.Duration) (bool, error)
	GetDel(context.Context, string) (string, bool, error)
}

type DashboardWSHandler struct {
	store             dashboardTicketStore
	hub               *realtime.Hub
	origin            string
	settings          realtime.Settings
	trustProxyHeaders bool
}

func NewDashboardWSHandler(store dashboardTicketStore, hub *realtime.Hub, origin string, settings realtime.Settings, trustProxyHeaders bool) *DashboardWSHandler {
	return &DashboardWSHandler{store: store, hub: hub, origin: strings.TrimRight(origin, "/"), settings: settings, trustProxyHeaders: trustProxyHeaders}
}

func (h *DashboardWSHandler) Ticket(c *gin.Context) {
	userID, ok := c.Get("userId")
	if !ok {
		writeError(c, http.StatusUnauthorized, "Authentication required.")
		return
	}
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		writeError(c, http.StatusInternalServerError, "Unable to create a WebSocket ticket.")
		return
	}
	ticket := hex.EncodeToString(bytes[:])
	created, err := h.store.SetNXValue(c.Request.Context(), "ws:ticket:"+ticket, userID.(string), 30*time.Second)
	if err != nil || !created {
		writeError(c, http.StatusServiceUnavailable, "Unable to create a WebSocket ticket.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ticket": ticket, "expiresIn": 30})
}

func (h *DashboardWSHandler) Serve(c *gin.Context) {
	if c.GetHeader("Origin") != h.origin {
		writeError(c, http.StatusForbidden, "Origin not allowed.")
		return
	}
	ticket := c.Query("ticket")
	if ticket == "" || len(ticket) > 128 {
		writeError(c, http.StatusUnauthorized, "A valid one-time ticket is required.")
		return
	}
	ownerID, found, err := h.store.GetDel(c.Request.Context(), "ws:ticket:"+ticket)
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "Unable to validate the WebSocket ticket.")
		return
	}
	if !found {
		writeError(c, http.StatusUnauthorized, "The WebSocket ticket is invalid or expired.")
		return
	}
	if _, err := bson.ObjectIDFromHex(ownerID); err != nil {
		writeError(c, http.StatusUnauthorized, "The WebSocket ticket is invalid.")
		return
	}
	ip := middleware.ClientIP(c, h.trustProxyHeaders)
	if !h.hub.AcquireIP(ip, h.settings.MaxPerIP) {
		writeError(c, http.StatusTooManyRequests, "Too many live connections.")
		return
	}
	upgrader := websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024, CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == h.origin }}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.hub.ReleaseIP(ip)
		return
	}
	client := realtime.NewClient(conn, h.hub, "", ip, h.settings)
	if !h.hub.RegisterDashboard(client, ownerID) {
		h.hub.ReleaseIP(ip)
		_ = conn.Close()
		return
	}
	go client.WritePump()
	client.ReadPump()
	h.hub.ReleaseIP(ip)
}
