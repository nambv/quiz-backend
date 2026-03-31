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

func TestStartQuizEndpoint(t *testing.T) {
	server, _ := setupTestServer(t)
	quizID := "quiz-vocab-01"

	// Start quiz that has no session yet → error
	resp, err := http.Post(server.URL+"/api/quiz/"+quizID+"/start", "application/json", nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// Create a session by joining via WebSocket, then start
	conn := wsConnect(t, server, quizID)
	sendEnvelope(t, conn, "join", models.JoinPayload{UserID: "alice", Username: "Alice"})
	_ = readUntilType(t, conn, models.TypeQuizState)

	resp2, err := http.Post(server.URL+"/api/quiz/"+quizID+"/start", "application/json", nil)
	require.NoError(t, err)
	defer func() { _ = resp2.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&body))
	assert.Equal(t, "started", body["status"])

	// Starting again → error (already started)
	resp3, err := http.Post(server.URL+"/api/quiz/"+quizID+"/start", "application/json", nil)
	require.NoError(t, err)
	defer func() { _ = resp3.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp3.StatusCode)
}

func TestLeaderboardEndpoint(t *testing.T) {
	server, quizSvc := setupTestServer(t)
	quizID := "quiz-vocab-01"

	// Empty leaderboard
	resp, err := http.Get(server.URL + "/api/quiz/" + quizID + "/leaderboard")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Join, start, answer, then check leaderboard via REST
	conn := wsConnect(t, server, quizID)
	sendEnvelope(t, conn, "join", models.JoinPayload{UserID: "alice", Username: "Alice"})
	_ = readUntilType(t, conn, models.TypeQuizState)
	require.NoError(t, quizSvc.StartQuiz(quizID))
	_ = readUntilType(t, conn, models.TypeQuestion)

	sendEnvelope(t, conn, "answer", models.AnswerPayload{QuestionID: "q1", AnswerID: "a2"})
	_ = readUntilType(t, conn, models.TypeScoreResult)
	_ = readUntilType(t, conn, models.TypeLeaderboardUpdate)

	resp2, err := http.Get(server.URL + "/api/quiz/" + quizID + "/leaderboard")
	require.NoError(t, err)
	defer func() { _ = resp2.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	var lbResp struct {
		Leaderboard []models.LeaderboardEntry `json:"leaderboard"`
	}
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&lbResp))
	require.Len(t, lbResp.Leaderboard, 1)
	assert.Equal(t, "alice", lbResp.Leaderboard[0].UserID)
}

func TestWebSocketWrongAnswer(t *testing.T) {
	server, quizSvc := setupTestServer(t)
	quizID := "quiz-vocab-01"

	conn := wsConnect(t, server, quizID)
	sendEnvelope(t, conn, "join", models.JoinPayload{UserID: "alice", Username: "Alice"})
	_ = readUntilType(t, conn, models.TypeQuizState)
	require.NoError(t, quizSvc.StartQuiz(quizID))
	_ = readUntilType(t, conn, models.TypeQuestion)

	// Answer incorrectly (correct is "a2")
	sendEnvelope(t, conn, "answer", models.AnswerPayload{QuestionID: "q1", AnswerID: "a1"})
	scoreEnv := readUntilType(t, conn, models.TypeScoreResult)

	var result models.ScoreResultPayload
	require.NoError(t, json.Unmarshal(scoreEnv.Payload, &result))
	assert.False(t, result.Correct)
	assert.Equal(t, 0, result.Points)
}

func TestWebSocketDuplicateJoin(t *testing.T) {
	server, _ := setupTestServer(t)
	quizID := "quiz-vocab-01"

	conn := wsConnect(t, server, quizID)
	sendEnvelope(t, conn, "join", models.JoinPayload{UserID: "alice", Username: "Alice"})
	_ = readUntilType(t, conn, models.TypeQuizState)

	// Same user joins again on a new connection
	conn2 := wsConnect(t, server, quizID)
	sendEnvelope(t, conn2, "join", models.JoinPayload{UserID: "alice", Username: "Alice"})
	env := readUntilType(t, conn2, models.TypeError)

	var errPayload models.ErrorPayload
	require.NoError(t, json.Unmarshal(env.Payload, &errPayload))
	assert.Equal(t, "ALREADY_JOINED", errPayload.Code)
}

func TestWebSocketAnswerWithoutJoin(t *testing.T) {
	server, _ := setupTestServer(t)
	quizID := "quiz-vocab-01"

	conn := wsConnect(t, server, quizID)
	sendEnvelope(t, conn, "answer", models.AnswerPayload{QuestionID: "q1", AnswerID: "a2"})
	env := readUntilType(t, conn, models.TypeError)

	var errPayload models.ErrorPayload
	require.NoError(t, json.Unmarshal(env.Payload, &errPayload))
	assert.Equal(t, "NOT_JOINED", errPayload.Code)
}

func TestWebSocketUnknownMessageType(t *testing.T) {
	server, _ := setupTestServer(t)
	quizID := "quiz-vocab-01"

	conn := wsConnect(t, server, quizID)
	sendEnvelope(t, conn, "join", models.JoinPayload{UserID: "alice", Username: "Alice"})
	_ = readUntilType(t, conn, models.TypeQuizState)

	sendEnvelope(t, conn, "unknown_type", nil)
	env := readUntilType(t, conn, models.TypeError)

	var errPayload models.ErrorPayload
	require.NoError(t, json.Unmarshal(env.Payload, &errPayload))
	assert.Equal(t, "UNKNOWN_TYPE", errPayload.Code)
}

func TestWebSocketInvalidQuiz(t *testing.T) {
	server, _ := setupTestServer(t)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/quiz/nonexistent"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected error connecting to nonexistent quiz")
	}
	if resp != nil {
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		_ = resp.Body.Close()
	}
}

func TestWebSocketAnswerWrongQuestion(t *testing.T) {
	server, quizSvc := setupTestServer(t)
	quizID := "quiz-vocab-01"

	conn := wsConnect(t, server, quizID)
	sendEnvelope(t, conn, "join", models.JoinPayload{UserID: "alice", Username: "Alice"})
	_ = readUntilType(t, conn, models.TypeQuizState)
	require.NoError(t, quizSvc.StartQuiz(quizID))
	_ = readUntilType(t, conn, models.TypeQuestion)

	// Answer for wrong question ID
	sendEnvelope(t, conn, "answer", models.AnswerPayload{QuestionID: "q99", AnswerID: "a2"})
	env := readUntilType(t, conn, models.TypeError)

	var errPayload models.ErrorPayload
	require.NoError(t, json.Unmarshal(env.Payload, &errPayload))
	assert.Equal(t, "WRONG_QUESTION", errPayload.Code)
}

func TestWebSocketAnswerDuplicate(t *testing.T) {
	server, quizSvc := setupTestServer(t)
	quizID := "quiz-vocab-01"

	conn := wsConnect(t, server, quizID)
	sendEnvelope(t, conn, "join", models.JoinPayload{UserID: "alice", Username: "Alice"})
	_ = readUntilType(t, conn, models.TypeQuizState)
	require.NoError(t, quizSvc.StartQuiz(quizID))
	_ = readUntilType(t, conn, models.TypeQuestion)

	sendEnvelope(t, conn, "answer", models.AnswerPayload{QuestionID: "q1", AnswerID: "a2"})
	_ = readUntilType(t, conn, models.TypeScoreResult)
	_ = readUntilType(t, conn, models.TypeLeaderboardUpdate)

	// Duplicate answer
	sendEnvelope(t, conn, "answer", models.AnswerPayload{QuestionID: "q1", AnswerID: "a2"})
	env := readUntilType(t, conn, models.TypeError)

	var errPayload models.ErrorPayload
	require.NoError(t, json.Unmarshal(env.Payload, &errPayload))
	assert.Equal(t, "SCORING_ERROR", errPayload.Code)
}

func TestWebSocketAnswerBeforeStart(t *testing.T) {
	server, _ := setupTestServer(t)
	quizID := "quiz-vocab-01"

	conn := wsConnect(t, server, quizID)
	sendEnvelope(t, conn, "join", models.JoinPayload{UserID: "alice", Username: "Alice"})
	_ = readUntilType(t, conn, models.TypeQuizState)

	// Answer before quiz is started
	sendEnvelope(t, conn, "answer", models.AnswerPayload{QuestionID: "q1", AnswerID: "a2"})
	env := readUntilType(t, conn, models.TypeError)

	var errPayload models.ErrorPayload
	require.NoError(t, json.Unmarshal(env.Payload, &errPayload))
	assert.Equal(t, "QUIZ_NOT_ACTIVE", errPayload.Code)
}

func TestWebSocketRejoin(t *testing.T) {
	server, quizSvc := setupTestServer(t)
	quizID := "quiz-vocab-01"

	conn := wsConnect(t, server, quizID)
	sendEnvelope(t, conn, "join", models.JoinPayload{UserID: "alice", Username: "Alice"})
	_ = readUntilType(t, conn, models.TypeQuizState)
	require.NoError(t, quizSvc.StartQuiz(quizID))
	_ = readUntilType(t, conn, models.TypeQuestion)

	// Connect a new WS and rejoin as the same user
	conn2 := wsConnect(t, server, quizID)
	sendEnvelope(t, conn2, "rejoin", models.RejoinPayload{UserID: "alice", QuizID: quizID})
	env := readUntilType(t, conn2, models.TypeQuizState)

	var state models.QuizStatePayload
	require.NoError(t, json.Unmarshal(env.Payload, &state))
	assert.Equal(t, "active", state.Status)
	assert.NotNil(t, state.CurrentQuestion)
}

func TestListQuizzesEndpoint(t *testing.T) {
	server, _ := setupTestServer(t)

	resp, err := http.Get(server.URL + "/api/quizzes")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Quizzes []struct {
			ID            string `json:"id"`
			Title         string `json:"title"`
			QuestionCount int    `json:"questionCount"`
		} `json:"quizzes"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.NotEmpty(t, body.Quizzes)
	assert.Equal(t, "quiz-vocab-01", body.Quizzes[0].ID)
	assert.Equal(t, 5, body.Quizzes[0].QuestionCount)
}

func TestResetQuizEndpoint(t *testing.T) {
	server, quizSvc := setupTestServer(t)
	quizID := "quiz-vocab-01"

	// Join and start a quiz
	conn := wsConnect(t, server, quizID)
	sendEnvelope(t, conn, "join", models.JoinPayload{UserID: "alice", Username: "Alice"})
	_ = readUntilType(t, conn, models.TypeQuizState)
	require.NoError(t, quizSvc.StartQuiz(quizID))
	_ = readUntilType(t, conn, models.TypeQuestion)

	// Reset the quiz
	resp, err := http.Post(server.URL+"/api/quiz/"+quizID+"/reset", "application/json", nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "reset", body["status"])

	// A new user can now join fresh
	conn2 := wsConnect(t, server, quizID)
	sendEnvelope(t, conn2, "join", models.JoinPayload{UserID: "bob", Username: "Bob"})
	env := readUntilType(t, conn2, models.TypeQuizState)

	var state models.QuizStatePayload
	require.NoError(t, json.Unmarshal(env.Payload, &state))
	assert.Equal(t, "waiting", state.Status)
	assert.Len(t, state.Participants, 1)
}

func TestResetQuizEndpoint_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)

	resp, err := http.Post(server.URL+"/api/quiz/nonexistent/reset", "application/json", nil)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestWebSocketRejoinNotParticipant(t *testing.T) {
	server, _ := setupTestServer(t)
	quizID := "quiz-vocab-01"

	// Alice joins to create a session
	conn := wsConnect(t, server, quizID)
	sendEnvelope(t, conn, "join", models.JoinPayload{UserID: "alice", Username: "Alice"})
	_ = readUntilType(t, conn, models.TypeQuizState)

	// Bob tries to rejoin without ever joining
	conn2 := wsConnect(t, server, quizID)
	sendEnvelope(t, conn2, "rejoin", models.RejoinPayload{UserID: "bob", QuizID: quizID})
	env := readUntilType(t, conn2, models.TypeError)

	var errPayload models.ErrorPayload
	require.NoError(t, json.Unmarshal(env.Payload, &errPayload))
	assert.Equal(t, "NOT_A_PARTICIPANT", errPayload.Code)
}
