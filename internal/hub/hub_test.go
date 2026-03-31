package hub

import (
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/nambuivu/quiz-server/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

// newFakeClient creates a Client without a real WebSocket (for unit testing hub logic).
func newFakeClient(h *Hub, quizID, userID, username string) *Client {
	return &Client{
		hub:      h,
		send:     make(chan []byte, 256),
		QuizID:   quizID,
		UserID:   userID,
		Username: username,
	}
}

func TestRoomSize(t *testing.T) {
	h := New(testLogger())
	quizID := "quiz1"

	assert.Equal(t, 0, h.RoomSize(quizID))

	c1 := newFakeClient(h, quizID, "alice", "Alice")
	h.Register(c1)
	assert.Equal(t, 1, h.RoomSize(quizID))

	c2 := newFakeClient(h, quizID, "bob", "Bob")
	h.Register(c2)
	assert.Equal(t, 2, h.RoomSize(quizID))

	h.Unregister(c1)
	assert.Equal(t, 1, h.RoomSize(quizID))
}

func TestRoomSize_DifferentRooms(t *testing.T) {
	h := New(testLogger())

	c1 := newFakeClient(h, "quiz1", "alice", "Alice")
	c2 := newFakeClient(h, "quiz2", "bob", "Bob")
	h.Register(c1)
	h.Register(c2)

	assert.Equal(t, 1, h.RoomSize("quiz1"))
	assert.Equal(t, 1, h.RoomSize("quiz2"))
	assert.Equal(t, 0, h.RoomSize("quiz3"))
}

func TestSendToUser(t *testing.T) {
	h := New(testLogger())
	quizID := "quiz1"

	c1 := newFakeClient(h, quizID, "alice", "Alice")
	c2 := newFakeClient(h, quizID, "bob", "Bob")
	h.Register(c1)
	h.Register(c2)

	h.SendToUser(quizID, "alice", models.TypeScoreResult, models.ScoreResultPayload{
		QuestionID: "q1",
		Correct:    true,
		Points:     150,
	})

	// Alice should receive the message
	select {
	case msg := <-c1.send:
		var env models.Envelope
		require.NoError(t, json.Unmarshal(msg, &env))
		assert.Equal(t, models.TypeScoreResult, env.Type)
	case <-time.After(time.Second):
		t.Fatal("alice did not receive message")
	}

	// Bob should NOT receive the message
	select {
	case <-c2.send:
		t.Fatal("bob should not receive the message")
	case <-time.After(100 * time.Millisecond):
		// expected
	}
}

func TestSendToUser_NotFound(t *testing.T) {
	h := New(testLogger())
	quizID := "quiz1"

	c := newFakeClient(h, quizID, "alice", "Alice")
	h.Register(c)

	// Sending to non-existent user should not panic
	h.SendToUser(quizID, "unknown", models.TypeScoreResult, nil)

	select {
	case <-c.send:
		t.Fatal("alice should not receive the message")
	case <-time.After(100 * time.Millisecond):
		// expected
	}
}

func TestBroadcastToRoom(t *testing.T) {
	h := New(testLogger())
	quizID := "quiz1"

	c1 := newFakeClient(h, quizID, "alice", "Alice")
	c2 := newFakeClient(h, quizID, "bob", "Bob")
	h.Register(c1)
	h.Register(c2)

	h.BroadcastToRoom(quizID, models.TypeQuestion, models.QuestionPayload{
		ID:   "q1",
		Text: "Test?",
	})

	for _, c := range []*Client{c1, c2} {
		select {
		case msg := <-c.send:
			var env models.Envelope
			require.NoError(t, json.Unmarshal(msg, &env))
			assert.Equal(t, models.TypeQuestion, env.Type)
		case <-time.After(time.Second):
			t.Fatal("client did not receive broadcast")
		}
	}
}

func TestRegisterUnregister(t *testing.T) {
	h := New(testLogger())
	quizID := "quiz1"

	c := newFakeClient(h, quizID, "alice", "Alice")
	h.Register(c)
	assert.Equal(t, 1, h.RoomSize(quizID))

	h.Unregister(c)
	assert.Equal(t, 0, h.RoomSize(quizID))

	// Room should be cleaned up
	h.mu.RLock()
	_, exists := h.rooms[quizID]
	h.mu.RUnlock()
	assert.False(t, exists)
}

func TestUnregister_NonExistent(t *testing.T) {
	h := New(testLogger())

	c := newFakeClient(h, "quiz1", "", "")
	// Should not panic
	h.Unregister(c)
}

type mockHandler struct {
	joinCalled   bool
	rejoinCalled bool
	answerCalled bool
}

func (m *mockHandler) HandleJoin(_ *Client, _ models.JoinPayload)     { m.joinCalled = true }
func (m *mockHandler) HandleRejoin(_ *Client, _ models.RejoinPayload) { m.rejoinCalled = true }
func (m *mockHandler) HandleAnswer(_ *Client, _ models.AnswerPayload) { m.answerCalled = true }

func TestHandleMessage_Routing(t *testing.T) {
	h := New(testLogger())
	mh := &mockHandler{}
	h.SetHandler(mh)

	c := newFakeClient(h, "quiz1", "alice", "Alice")

	tests := []struct {
		name     string
		msgType  string
		payload  any
		checkFn  func() bool
	}{
		{
			name:    "join",
			msgType: models.TypeJoin,
			payload: models.JoinPayload{UserID: "alice", Username: "Alice"},
			checkFn: func() bool { return mh.joinCalled },
		},
		{
			name:    "rejoin",
			msgType: models.TypeRejoin,
			payload: models.RejoinPayload{UserID: "alice", QuizID: "quiz1"},
			checkFn: func() bool { return mh.rejoinCalled },
		},
		{
			name:    "answer",
			msgType: models.TypeAnswer,
			payload: models.AnswerPayload{QuestionID: "q1", AnswerID: "a1"},
			checkFn: func() bool { return mh.answerCalled },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			*mh = mockHandler{}
			raw, err := json.Marshal(tt.payload)
			require.NoError(t, err)
			env := &models.Envelope{
				Type:    tt.msgType,
				Payload: raw,
			}
			h.HandleMessage(c, env)
			assert.True(t, tt.checkFn())
		})
	}
}

func TestHandleMessage_NoHandler(t *testing.T) {
	h := New(testLogger())
	c := newFakeClient(h, "quiz1", "alice", "Alice")

	env := &models.Envelope{Type: models.TypeJoin}
	h.HandleMessage(c, env)

	select {
	case msg := <-c.send:
		var errEnv models.Envelope
		require.NoError(t, json.Unmarshal(msg, &errEnv))
		assert.Equal(t, models.TypeError, errEnv.Type)
	case <-time.After(time.Second):
		t.Fatal("expected error message")
	}
}

func TestHandleMessage_InvalidPayload(t *testing.T) {
	h := New(testLogger())
	mh := &mockHandler{}
	h.SetHandler(mh)
	c := newFakeClient(h, "quiz1", "alice", "Alice")

	env := &models.Envelope{
		Type:    models.TypeJoin,
		Payload: []byte(`{invalid`),
	}
	h.HandleMessage(c, env)
	assert.False(t, mh.joinCalled)

	select {
	case msg := <-c.send:
		var errEnv models.Envelope
		require.NoError(t, json.Unmarshal(msg, &errEnv))
		assert.Equal(t, models.TypeError, errEnv.Type)
	case <-time.After(time.Second):
		t.Fatal("expected error message")
	}
}

func TestSendBufferFull(t *testing.T) {
	h := New(testLogger())

	// Create client with tiny buffer
	c := &Client{
		hub:    h,
		send:   make(chan []byte, 1),
		QuizID: "quiz1",
		UserID: "alice",
	}

	// Fill the buffer
	c.Send([]byte("first"))

	// This should drop (buffer full)
	c.Send([]byte("second"))

	msg := <-c.send
	assert.Equal(t, "first", string(msg))
}
