package websocket

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
	"go.uber.org/zap"
)

// StatusUpdate represents a notification status change event.
type StatusUpdate struct {
	NotificationID string `json:"notification_id"`
	Status         string `json:"status"`
	Channel        string `json:"channel"`
	Timestamp      string `json:"timestamp"`
	Attempt        int    `json:"attempt,omitempty"`
	Error          string `json:"error,omitempty"`
}

// Hub manages WebSocket connections and broadcasts status updates.
type Hub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan StatusUpdate
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.RWMutex
	logger     *zap.Logger
}

// NewHub creates a new WebSocket hub.
func NewHub(logger *zap.Logger) *Hub {
	return &Hub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan StatusUpdate, 256),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		logger:     logger,
	}
}

// Run starts the hub's event loop.
func (h *Hub) Run() {
	for {
		select {
		case conn := <-h.register:
			h.mu.Lock()
			h.clients[conn] = true
			h.mu.Unlock()
			h.logger.Debug("WebSocket client connected", zap.Int("total_clients", len(h.clients)))

		case conn := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				conn.Close()
			}
			h.mu.Unlock()
			h.logger.Debug("WebSocket client disconnected", zap.Int("total_clients", len(h.clients)))

		case update := <-h.broadcast:
			data, err := json.Marshal(update)
			if err != nil {
				h.logger.Error("Failed to marshal status update", zap.Error(err))
				continue
			}

			h.mu.RLock()
			for conn := range h.clients {
				if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
					h.logger.Debug("Failed to write to WebSocket", zap.Error(err))
					h.mu.RUnlock()
					h.unregister <- conn
					h.mu.RLock()
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Register adds a new WebSocket connection.
func (h *Hub) Register(conn *websocket.Conn) {
	h.register <- conn
}

// Unregister removes a WebSocket connection.
func (h *Hub) Unregister(conn *websocket.Conn) {
	h.unregister <- conn
}

// BroadcastStatus sends a status update to all connected clients.
func (h *Hub) BroadcastStatus(notificationID, status, channel string, attempt int, errMsg string) {
	h.broadcast <- StatusUpdate{
		NotificationID: notificationID,
		Status:         status,
		Channel:        channel,
		Timestamp:      time.Now().Format(time.RFC3339),
		Attempt:        attempt,
		Error:          errMsg,
	}
}

// HandleConnection handles a WebSocket connection (read loop for keepalive).
func (h *Hub) HandleConnection(conn *websocket.Conn) {
	h.Register(conn)
	defer h.Unregister(conn)

	for {
		// Read messages to detect disconnection (keepalive)
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}
