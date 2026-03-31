package hub

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/nambuivu/quiz-server/internal/metrics"
	"github.com/nambuivu/quiz-server/internal/models"
)

// MessageHandler processes incoming WebSocket messages from clients.
// Implemented by the quiz service to handle join, answer, etc.
type MessageHandler interface {
	HandleJoin(client *Client, payload models.JoinPayload)
	HandleRejoin(client *Client, payload models.RejoinPayload)
	HandleAnswer(client *Client, payload models.AnswerPayload)
}

// Hub manages all WebSocket connections grouped by quiz room.
type Hub struct {
	mu      sync.RWMutex
	log     *slog.Logger
	rooms   map[string]map[*Client]bool // quizID → set of clients
	handler MessageHandler
}

// New creates a Hub. Call SetHandler before accepting connections.
func New(log *slog.Logger) *Hub {
	return &Hub{
		log:   log.With("component", "hub"),
		rooms: make(map[string]map[*Client]bool),
	}
}

// SetHandler sets the message handler for the hub.
func (h *Hub) SetHandler(handler MessageHandler) {
	h.handler = handler
}

// Register adds a client to its quiz room.
func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.rooms[c.QuizID] == nil {
		h.rooms[c.QuizID] = make(map[*Client]bool)
	}
	h.rooms[c.QuizID][c] = true
	metrics.WSConnectionsActive.Inc()
	metrics.WSConnectionsTotal.Inc()
	h.log.Info("client registered", "user_id", c.UserID, "quiz_id", c.QuizID)
}

// Unregister removes a client from its quiz room and closes its send channel.
func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if clients, ok := h.rooms[c.QuizID]; ok {
		if _, exists := clients[c]; exists {
			delete(clients, c)
			close(c.send)
			metrics.WSConnectionsActive.Dec()
			if len(clients) == 0 {
				delete(h.rooms, c.QuizID)
			}
		}
	}

	if c.UserID != "" {
		h.log.Info("client unregistered", "user_id", c.UserID, "quiz_id", c.QuizID)
		h.broadcastToRoom(c.QuizID, models.TypeUserLeft, models.UserLeftPayload{
			UserID:   c.UserID,
			Username: c.Username,
		})
	}
}

// BroadcastToRoom sends a typed message to all clients in a quiz room.
func (h *Hub) BroadcastToRoom(quizID, msgType string, payload any) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	h.broadcastToRoom(quizID, msgType, payload)
}

// broadcastToRoom sends without acquiring the lock (caller must hold at least RLock).
func (h *Hub) broadcastToRoom(quizID, msgType string, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		h.log.Error("failed to marshal broadcast payload", "type", msgType, "error", err)
		return
	}
	env := models.Envelope{
		Type:      msgType,
		Payload:   raw,
		Timestamp: time.Now().UnixMilli(),
	}
	data, err := json.Marshal(env)
	if err != nil {
		h.log.Error("failed to marshal broadcast envelope", "type", msgType, "error", err)
		return
	}

	metrics.MessagesBroadcast.WithLabelValues(msgType).Inc()
	for client := range h.rooms[quizID] {
		client.Send(data)
	}
}

// SendToUser sends a typed message to a specific user in a quiz room.
func (h *Hub) SendToUser(quizID, userID, msgType string, payload any) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.rooms[quizID] {
		if client.UserID == userID {
			client.SendEnvelope(msgType, payload)
			return
		}
	}
}

// RoomSize returns the number of clients in a quiz room.
func (h *Hub) RoomSize(quizID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rooms[quizID])
}

// HandleMessage routes an incoming message to the appropriate handler.
func (h *Hub) HandleMessage(c *Client, env *models.Envelope) {
	if h.handler == nil {
		c.SendError("INTERNAL_ERROR", "server not ready")
		return
	}

	metrics.MessagesReceived.WithLabelValues(env.Type).Inc()

	switch env.Type {
	case models.TypeJoin:
		var p models.JoinPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			c.SendError("INVALID_PAYLOAD", "invalid join payload")
			return
		}
		h.handler.HandleJoin(c, p)

	case models.TypeRejoin:
		var p models.RejoinPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			c.SendError("INVALID_PAYLOAD", "invalid rejoin payload")
			return
		}
		h.handler.HandleRejoin(c, p)

	case models.TypeAnswer:
		var p models.AnswerPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			c.SendError("INVALID_PAYLOAD", "invalid answer payload")
			return
		}
		if c.UserID == "" {
			c.SendError("NOT_JOINED", "you must join the quiz first")
			return
		}
		h.handler.HandleAnswer(c, p)

	default:
		c.SendError("UNKNOWN_TYPE", "unknown message type: "+env.Type)
	}
}
