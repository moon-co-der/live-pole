package realtime

import (
	"encoding/json"
	"log"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"live-polling/internal/models"
)

type Settings struct {
	PingInterval time.Duration
	PongWait     time.Duration
	WriteWait    time.Duration
	MaxMessage   int64
	MaxPerIP     int
}

type Message struct {
	Type     string                    `json:"type"`
	Question string                    `json:"question,omitempty"`
	Options  []models.PublicPollOption `json:"options,omitempty"`
	Counts   map[string]int64          `json:"counts,omitempty"`
	Total    int64                     `json:"total"`
	Status   models.PollStatus         `json:"status,omitempty"`
	Data     map[string]any            `json:"data,omitempty"`
}

type outbound struct {
	payload    []byte
	closeAfter bool
}

type Client struct {
	Conn    *websocket.Conn
	PollID  string
	OwnerID string
	IP      string
	Hub     *Hub

	send     chan outbound
	done     chan struct{}
	once     sync.Once
	settings Settings
}

func NewClient(conn *websocket.Conn, hub *Hub, pollID, ip string, settings Settings) *Client {
	return &Client{Conn: conn, Hub: hub, PollID: pollID, IP: ip, send: make(chan outbound, 32), done: make(chan struct{}), settings: settings}
}

func (c *Client) Send(message Message) bool {
	payload, err := json.Marshal(message)
	if err != nil {
		return false
	}
	select {
	case <-c.done:
		return false
	case c.send <- outbound{payload: payload}:
		return true
	default:
		return false
	}
}

func (c *Client) ReadPump() {
	defer c.Hub.Unregister(c)
	c.Conn.SetReadLimit(c.settings.MaxMessage)
	_ = c.Conn.SetReadDeadline(time.Now().Add(c.settings.PongWait))
	c.Conn.SetPongHandler(func(string) error { return c.Conn.SetReadDeadline(time.Now().Add(c.settings.PongWait)) })
	for {
		if _, _, err := c.Conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(c.settings.PingInterval)
	defer ticker.Stop()
	defer c.Hub.Unregister(c)
	for {
		select {
		case <-c.done:
			return
		case message := <-c.send:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(c.settings.WriteWait))
			if err := c.Conn.WriteMessage(websocket.TextMessage, message.payload); err != nil {
				return
			}
			if message.closeAfter {
				_ = c.Conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "poll closed"), time.Now().Add(c.settings.WriteWait))
				return
			}
		case <-ticker.C:
			if err := c.Conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(c.settings.WriteWait)); err != nil {
				return
			}
		}
	}
}

type registerRequest struct {
	client *Client
	ack    chan struct{}
}
type unregisterRequest struct {
	client *Client
	ack    chan struct{}
}
type broadcastRequest struct {
	pollID     string
	ownerID    string
	message    Message
	closeAfter bool
}

type Hub struct {
	register   chan registerRequest
	unregister chan unregisterRequest
	broadcast  chan broadcastRequest
	clients    map[string]map[*Client]struct{}
	dashboards map[string]map[*Client]struct{}
	stop       chan struct{}
	stopOnce   sync.Once
	ipMu       sync.Mutex
	ipCounts   map[string]int
}

func NewHub() *Hub {
	return &Hub{
		register: make(chan registerRequest, 128), unregister: make(chan unregisterRequest, 128),
		broadcast: make(chan broadcastRequest, 256), clients: make(map[string]map[*Client]struct{}), dashboards: make(map[string]map[*Client]struct{}),
		stop: make(chan struct{}), ipCounts: make(map[string]int),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case <-h.stop:
			for _, group := range h.clients {
				for client := range group {
					h.remove(client)
				}
			}
			for _, group := range h.dashboards {
				for client := range group {
					h.remove(client)
				}
			}
			return
		case request := <-h.register:
			if request.client.OwnerID != "" {
				group := h.dashboards[request.client.OwnerID]
				if group == nil {
					group = make(map[*Client]struct{})
					h.dashboards[request.client.OwnerID] = group
				}
				group[request.client] = struct{}{}
			} else {
				group := h.clients[request.client.PollID]
				if group == nil {
					group = make(map[*Client]struct{})
					h.clients[request.client.PollID] = group
				}
				group[request.client] = struct{}{}
			}
			close(request.ack)
		case request := <-h.unregister:
			h.remove(request.client)
			close(request.ack)
		case request := <-h.broadcast:
			h.forward(request)
		}
	}
}

func (h *Hub) Register(client *Client) bool {
	ack := make(chan struct{})
	select {
	case h.register <- registerRequest{client: client, ack: ack}:
	case <-h.stop:
		return false
	}
	select {
	case <-ack:
		return true
	case <-h.stop:
		return false
	}
}

func (h *Hub) RegisterDashboard(client *Client, ownerID string) bool {
	client.OwnerID = ownerID
	return h.Register(client)
}

func (h *Hub) Unregister(client *Client) {
	ack := make(chan struct{})
	select {
	case h.unregister <- unregisterRequest{client: client, ack: ack}:
	case <-h.stop:
		client.stop()
		return
	}
	select {
	case <-ack:
	case <-h.stop:
		client.stop()
	}
}

func (h *Hub) Broadcast(pollID string, message Message, closeAfter bool) {
	select {
	case h.broadcast <- broadcastRequest{pollID: pollID, message: message, closeAfter: closeAfter}:
	case <-h.stop:
	default:
		log.Printf("websocket broadcast queue is full for poll %s", pollID)
	}
}

func (h *Hub) BroadcastDashboard(ownerID string, message Message) {
	select {
	case h.broadcast <- broadcastRequest{ownerID: ownerID, message: message}:
	case <-h.stop:
	default:
		log.Printf("dashboard broadcast queue is full for owner %s", ownerID)
	}
}

func (h *Hub) forward(request broadcastRequest) {
	group := h.clients[request.pollID]
	if request.ownerID != "" {
		group = h.dashboards[request.ownerID]
	}
	if len(group) == 0 {
		return
	}
	payload, err := json.Marshal(request.message)
	if err != nil {
		return
	}
	for client := range group {
		item := outbound{payload: payload, closeAfter: request.closeAfter}
		select {
		case <-client.done:
			h.remove(client)
		case client.send <- item:
		default:
			log.Printf("dropping slow websocket client poll=%s ip=%s", client.PollID, client.IP)
			h.remove(client)
		}
	}
}

func (h *Hub) remove(client *Client) {
	group := h.clients[client.PollID]
	if client.OwnerID != "" {
		group = h.dashboards[client.OwnerID]
	}
	if _, exists := group[client]; exists {
		delete(group, client)
		if len(group) == 0 {
			if client.OwnerID != "" {
				delete(h.dashboards, client.OwnerID)
			} else {
				delete(h.clients, client.PollID)
			}
		}
		log.Printf("websocket client disconnected poll=%s owner=%s ip=%s", client.PollID, client.OwnerID, client.IP)
	}
	client.stop()
}

func (c *Client) stop() {
	c.once.Do(func() { close(c.done); _ = c.Conn.Close() })
}

func (h *Hub) AcquireIP(ip string, limit int) bool {
	h.ipMu.Lock()
	defer h.ipMu.Unlock()
	if h.ipCounts[ip] >= limit {
		return false
	}
	h.ipCounts[ip]++
	return true
}

func (h *Hub) ReleaseIP(ip string) {
	h.ipMu.Lock()
	defer h.ipMu.Unlock()
	if h.ipCounts[ip] <= 1 {
		delete(h.ipCounts, ip)
	} else {
		h.ipCounts[ip]--
	}
}

func RemoteIP(remoteAddr string) string {
	ip, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return ip
	}
	return remoteAddr
}

func (h *Hub) Stop() {
	h.stopOnce.Do(func() { close(h.stop) })
}
