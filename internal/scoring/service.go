package scoring

import (
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/nambuivu/quiz-server/internal/models"
)

var (
	ErrAlreadyAnswered = errors.New("already answered this question")
	ErrTimeLimitExceeded = errors.New("time limit exceeded")
)

const basePoints = 100

// Leaderboard defines the interface for leaderboard storage.
// AI-ASSISTED: Claude Code — interface defined at consumer for testability per Go conventions
type Leaderboard interface {
	IncrScore(quizID, userID string, delta int) error
	GetRankings(quizID string) ([]RankEntry, error)
	ResetQuiz(quizID string) error
}

// RankEntry is a raw leaderboard entry from the storage layer.
type RankEntry struct {
	UserID string
	Score  int
}

// Service handles answer validation, score computation, and leaderboard updates.
type Service struct {
	mu          sync.RWMutex
	log         *slog.Logger
	leaderboard Leaderboard
	// answered tracks which questions each user has answered: quizID → userID → questionID → true
	answered map[string]map[string]map[string]bool
	// usernames maps userID → username for leaderboard display
	usernames map[string]string
}

// New creates a scoring Service with the given leaderboard backend.
func New(log *slog.Logger, lb Leaderboard) *Service {
	return &Service{
		log:         log.With("component", "scoring"),
		leaderboard: lb,
		answered:    make(map[string]map[string]map[string]bool),
		usernames:   make(map[string]string),
	}
}

// SubmitAnswer validates and scores an answer submission.
// Returns the score result or an error if the submission is invalid.
// AI-ASSISTED: Claude Code — score formula: basePoints × (1 + timeBonus)
// Verification: unit tested with table-driven tests for correct/wrong/duplicate/timeout scenarios
func (s *Service) SubmitAnswer(quizID, userID, username, questionID, answerID, correctID string, timeLimitSec int, questionStart time.Time) (*models.ScoreResultPayload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Track username for leaderboard display
	s.usernames[userID] = username

	// Idempotency check
	if s.answered[quizID] == nil {
		s.answered[quizID] = make(map[string]map[string]bool)
	}
	if s.answered[quizID][userID] == nil {
		s.answered[quizID][userID] = make(map[string]bool)
	}
	if s.answered[quizID][userID][questionID] {
		return nil, ErrAlreadyAnswered
	}

	// Time validation
	elapsed := time.Since(questionStart)
	timeLimit := time.Duration(timeLimitSec) * time.Second
	// Allow 2s clock skew tolerance
	if elapsed > timeLimit+2*time.Second {
		return nil, ErrTimeLimitExceeded
	}

	// Mark as answered
	s.answered[quizID][userID][questionID] = true

	// Score computation
	correct := answerID == correctID
	points := 0
	timeBonus := 0.0

	if correct {
		timeBonus = max(0, float64(timeLimit-elapsed)/float64(timeLimit))
		points = int(float64(basePoints) * (1 + timeBonus))
	}

	// Update leaderboard
	if points > 0 {
		if err := s.leaderboard.IncrScore(quizID, userID, points); err != nil {
			s.log.Error("failed to update leaderboard", "quiz_id", quizID, "user_id", userID, "error", err)
		}
	}

	// Get total score
	totalScore := s.getUserScore(quizID, userID)

	s.log.Info("answer scored",
		"quiz_id", quizID,
		"user_id", userID,
		"question_id", questionID,
		"correct", correct,
		"points", points,
		"time_bonus", timeBonus,
	)

	return &models.ScoreResultPayload{
		QuestionID: questionID,
		Correct:    correct,
		Points:     points,
		TotalScore: totalScore,
		TimeBonus:  timeBonus,
	}, nil
}

// getUserScore returns the total score for a user. Caller must hold s.mu.
func (s *Service) getUserScore(quizID, userID string) int {
	rankings, err := s.leaderboard.GetRankings(quizID)
	if err != nil {
		return 0
	}
	for _, r := range rankings {
		if r.UserID == userID {
			return r.Score
		}
	}
	return 0
}

// Reset clears all scoring state for a quiz.
func (s *Service) Reset(quizID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.answered, quizID)
	if err := s.leaderboard.ResetQuiz(quizID); err != nil {
		s.log.Error("failed to reset leaderboard", "quiz_id", quizID, "error", err)
	}
}

// GetLeaderboard returns the current leaderboard with ranks and usernames.
func (s *Service) GetLeaderboard(quizID string) []models.LeaderboardEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rankings, err := s.leaderboard.GetRankings(quizID)
	if err != nil {
		s.log.Error("failed to get leaderboard", "quiz_id", quizID, "error", err)
		return nil
	}

	entries := make([]models.LeaderboardEntry, len(rankings))
	for i, r := range rankings {
		entries[i] = models.LeaderboardEntry{
			Rank:     i + 1,
			UserID:   r.UserID,
			Username: s.usernames[r.UserID],
			Score:    r.Score,
		}
	}
	return entries
}
