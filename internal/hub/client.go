package hub

import (
	"encoding/json"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nambuivu/quiz-server/internal/models"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 30 * time.Second
	maxMessageSize = 4096
)

// Client represents a single WebSocket connection in a quiz room.
type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	QuizID   string
	UserID   string
	Username string
}

// NewClient creates a Client and starts its read/write pumps.
func NewClient(hub *Hub, conn *websocket.Conn, quizID string) *Client {
	return &Client{
		hub:    hub,
		conn:   conn,
		send:   make(chan []byte, 256),
		QuizID: quizID,
	}
}

// ReadPump reads messages from the WebSocket connection and routes them to the hub.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		_ = c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				c.hub.log.Warn("websocket read error", "user_id", c.UserID, "quiz_id", c.QuizID, "error", err)
			}
			return
		}

		var env models.Envelope
		if err := json.Unmarshal(message, &env); err != nil {
			c.SendError("INVALID_MESSAGE", "malformed JSON message")
			continue
		}

		c.hub.HandleMessage(c, &env)
	}
}

// WritePump writes messages from the send channel to the WebSocket connection.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Send queues a raw message for sending to the client.
func (c *Client) Send(data []byte) {
	select {
	case c.send <- data:
	default:
		c.hub.log.Warn("client send buffer full, dropping", "user_id", c.UserID, "quiz_id", c.QuizID)
	}
}

// SendEnvelope marshals and sends a typed envelope to the client.
func (c *Client) SendEnvelope(msgType string, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		c.hub.log.Error("failed to marshal payload", "type", msgType, "error", err)
		return
	}
	env := models.Envelope{
		Type:      msgType,
		Payload:   raw,
		Timestamp: time.Now().UnixMilli(),
	}
	data, err := json.Marshal(env)
	if err != nil {
		c.hub.log.Error("failed to marshal envelope", "type", msgType, "error", err)
		return
	}
	c.Send(data)
}

// SendError sends an error message to the client.
func (c *Client) SendError(code, message string) {
	c.SendEnvelope(models.TypeError, models.ErrorPayload{Code: code, Message: message})
}
