package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nambuivu/quiz-server/internal/hub"
	"github.com/nambuivu/quiz-server/internal/models"
	"github.com/nambuivu/quiz-server/internal/quiz"
	"github.com/nambuivu/quiz-server/internal/scoring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestServer(t *testing.T) (*httptest.Server, *quiz.Service) {
	t.Helper()

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	lb := scoring.NewMemoryLeaderboard()
	scorer := scoring.New(log, lb)
	h := hub.New(log)
	quizSvc := quiz.New(log, h, scorer, nil)
	hdl := New(log, h, quizSvc, scorer, nil, nil)

	mux := http.NewServeMux()
	hdl.RegisterRoutes(mux)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return server, quizSvc
}

func wsConnect(t *testing.T, server *httptest.Server, quizID string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/quiz/" + quizID
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func readEnvelope(t *testing.T, conn *websocket.Conn) models.Envelope {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, msg, err := conn.ReadMessage()
	require.NoError(t, err)
	var env models.Envelope
	require.NoError(t, json.Unmarshal(msg, &env))
	return env
}

// readUntilType reads messages until it finds one with the expected type, discarding others.
func readUntilType(t *testing.T, conn *websocket.Conn, msgType string) models.Envelope {
	t.Helper()
	for range 20 {
		env := readEnvelope(t, conn)
		if env.Type == msgType {
			return env
		}
	}
	t.Fatalf("did not receive message type %q after 20 messages", msgType)
	return models.Envelope{}
}

func sendEnvelope(t *testing.T, conn *websocket.Conn, msgType string, payload any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	env := models.Envelope{
		Type:      msgType,
		Payload:   raw,
		Timestamp: time.Now().UnixMilli(),
	}
	data, err := json.Marshal(env)
	require.NoError(t, err)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, data))
}

func TestHealthEndpoint(t *testing.T) {
	server, _ := setupTestServer(t)

	resp, err := http.Get(server.URL + "/health")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
}

func TestJoinQuizEndpoint(t *testing.T) {
	server, _ := setupTestServer(t)

	// Valid quiz
	resp, err := http.Post(server.URL+"/api/quiz/quiz-vocab-01/join", "application/json", nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Invalid quiz
	resp2, err := http.Post(server.URL+"/api/quiz/nonexistent/join", "application/json", nil)
	require.NoError(t, err)
	defer func() { _ = resp2.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp2.StatusCode)
}

// TestWebSocketLifecycle tests the full flow: connect → join → start quiz → answer → receive leaderboard.
// AI-ASSISTED: Claude Code — WebSocket integration test for full quiz lifecycle
// Verification: tested with -race flag, verified message ordering and leaderboard correctness
func TestWebSocketLifecycle(t *testing.T) {
	server, quizSvc := setupTestServer(t)
	quizID := "quiz-vocab-01"

	// Connect two clients
	conn1 := wsConnect(t, server, quizID)
	conn2 := wsConnect(t, server, quizID)

	// Alice joins
	sendEnvelope(t, conn1, "join", models.JoinPayload{UserID: "alice", Username: "Alice"})
	env := readEnvelope(t, conn1)
	assert.Equal(t, models.TypeQuizState, env.Type)

	var quizState models.QuizStatePayload
	require.NoError(t, json.Unmarshal(env.Payload, &quizState))
	assert.Equal(t, "waiting", quizState.Status)
	assert.Contains(t, quizState.Participants, "Alice")

	// Bob joins
	sendEnvelope(t, conn2, "join", models.JoinPayload{UserID: "bob", Username: "Bob"})
	env = readUntilType(t, conn2, models.TypeQuizState)

	// Start the quiz
	require.NoError(t, quizSvc.StartQuiz(quizID))

	// Both should receive the first question (skip any user_joined notifications)
	env1 := readUntilType(t, conn1, models.TypeQuestion)
	env2 := readUntilType(t, conn2, models.TypeQuestion)
	_ = env2

	var q models.QuestionPayload
	require.NoError(t, json.Unmarshal(env1.Payload, &q))
	assert.Equal(t, "q1", q.ID)
	assert.Equal(t, 4, len(q.Options))

	// Alice answers correctly
	sendEnvelope(t, conn1, "answer", models.AnswerPayload{QuestionID: "q1", AnswerID: "a2"})

	// Alice receives score result
	scoreEnv := readUntilType(t, conn1, models.TypeScoreResult)

	var scoreResult models.ScoreResultPayload
	require.NoError(t, json.Unmarshal(scoreEnv.Payload, &scoreResult))
	assert.True(t, scoreResult.Correct)
	assert.Greater(t, scoreResult.Points, 0)

	// Both receive leaderboard update
	lbEnv1 := readUntilType(t, conn1, models.TypeLeaderboardUpdate)
	lbEnv2 := readUntilType(t, conn2, models.TypeLeaderboardUpdate)
	_ = lbEnv2

	var lbUpdate models.LeaderboardUpdatePayload
	require.NoError(t, json.Unmarshal(lbEnv1.Payload, &lbUpdate))
	require.Len(t, lbUpdate.Leaderboard, 1)
	assert.Equal(t, "alice", lbUpdate.Leaderboard[0].UserID)
	assert.Greater(t, lbUpdate.Leaderboard[0].Score, 0)
}
