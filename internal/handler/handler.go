package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"


	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nambuivu/quiz-server/internal/hub"
	"github.com/nambuivu/quiz-server/internal/quiz"
	"github.com/nambuivu/quiz-server/internal/scoring"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

// Handler holds HTTP and WebSocket endpoint handlers.
type Handler struct {
	log      *slog.Logger
	hub      *hub.Hub
	quiz     *quiz.Service
	scorer   *scoring.Service
	redis    *redis.Client
	pgPool   *pgxpool.Pool
	upgrader websocket.Upgrader
}

// New creates a Handler with all dependencies.
func New(log *slog.Logger, h *hub.Hub, q *quiz.Service, s *scoring.Service, r *redis.Client, pg *pgxpool.Pool) *Handler {
	return &Handler{
		log:    log.With("component", "handler"),
		hub:    h,
		quiz:   q,
		scorer: s,
		redis:  r,
		pgPool: pg,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     func(r *http.Request) bool { return true }, // Allow all origins for demo
		},
	}
}

// RegisterRoutes registers all HTTP routes on the given mux.
// AI-ASSISTED: Claude Code — using Go 1.22+ enhanced ServeMux with method+path patterns
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", h.Health)
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /api/quizzes", h.ListQuizzes)
	mux.HandleFunc("POST /api/quiz/{quizId}/join", h.JoinQuiz)
	mux.HandleFunc("POST /api/quiz/{quizId}/start", h.StartQuiz)
	mux.HandleFunc("POST /api/quiz/{quizId}/next", h.NextQuestion)
	mux.HandleFunc("POST /api/quiz/{quizId}/reset", h.ResetQuiz)
	mux.HandleFunc("GET /api/quiz/{quizId}/leaderboard", h.Leaderboard)
	mux.HandleFunc("GET /ws/quiz/{quizId}", h.WebSocket)
}

// Health returns server health status including Redis and PostgreSQL connectivity.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	redisStatus := "connected"
	pgStatus := "connected"

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if h.redis != nil {
		if err := h.redis.Ping(ctx).Err(); err != nil {
			redisStatus = "disconnected"
			status = "degraded"
		}
	} else {
		redisStatus = "not configured (using in-memory)"
	}

	if h.pgPool != nil {
		if err := h.pgPool.Ping(ctx); err != nil {
			pgStatus = "disconnected"
			status = "degraded"
		}
	} else {
		pgStatus = "not configured (using mock data)"
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":   status,
		"redis":    redisStatus,
		"postgres": pgStatus,
	})
}

// ListQuizzes returns all available quizzes.
func (h *Handler) ListQuizzes(w http.ResponseWriter, r *http.Request) {
	quizzes := h.quiz.ListQuizzes()
	writeJSON(w, http.StatusOK, map[string]any{"quizzes": quizzes})
}

// JoinQuiz validates the quiz exists and returns the WebSocket URL.
func (h *Handler) JoinQuiz(w http.ResponseWriter, r *http.Request) {
	quizID := r.PathValue("quizId")
	if !h.quiz.QuizExists(quizID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "quiz not found"})
		return
	}

	wsURL := "ws://" + r.Host + "/ws/quiz/" + quizID
	writeJSON(w, http.StatusOK, map[string]string{
		"quizId": quizID,
		"wsUrl":  wsURL,
	})
}

// StartQuiz transitions a quiz from waiting to active.
func (h *Handler) StartQuiz(w http.ResponseWriter, r *http.Request) {
	quizID := r.PathValue("quizId")
	if err := h.quiz.StartQuiz(quizID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

// ResetQuiz clears all session state for a quiz.
func (h *Handler) ResetQuiz(w http.ResponseWriter, r *http.Request) {
	quizID := r.PathValue("quizId")
	if err := h.quiz.ResetQuiz(quizID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}

// NextQuestion advances to the next question immediately.
func (h *Handler) NextQuestion(w http.ResponseWriter, r *http.Request) {
	quizID := r.PathValue("quizId")
	h.quiz.AdvanceQuestion(quizID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "advanced"})
}

// Leaderboard returns the current leaderboard as a REST fallback.
func (h *Handler) Leaderboard(w http.ResponseWriter, r *http.Request) {
	quizID := r.PathValue("quizId")
	entries := h.scorer.GetLeaderboard(quizID)
	writeJSON(w, http.StatusOK, map[string]any{"leaderboard": entries})
}

// WebSocket upgrades an HTTP connection to WebSocket for real-time quiz participation.
func (h *Handler) WebSocket(w http.ResponseWriter, r *http.Request) {
	quizID := r.PathValue("quizId")
	if !h.quiz.QuizExists(quizID) {
		http.Error(w, "quiz not found", http.StatusNotFound)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Error("websocket upgrade failed", "error", err)
		return
	}

	client := hub.NewClient(h.hub, conn, quizID)
	go client.WritePump()
	go client.ReadPump()
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
