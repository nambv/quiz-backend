package repository

import (
	"context"

	"github.com/nambuivu/quiz-server/internal/models"
)

// QuizRepository loads quiz definitions and questions.
type QuizRepository interface {
	GetAllQuizzes(ctx context.Context) (map[string]*models.Quiz, error)
	GetQuiz(ctx context.Context, quizID string) (*models.Quiz, error)
}

// SessionRepository manages quiz session persistence.
type SessionRepository interface {
	CreateSession(ctx context.Context, quizID string) (sessionID string, err error)
	UpdateSessionStatus(ctx context.Context, sessionID, status string) error
}

// AnswerRepository persists answer submissions.
type AnswerRepository interface {
	SaveAnswer(ctx context.Context, answer *AnswerRecord) error
}

// LeaderboardRepository persists final leaderboard snapshots.
type LeaderboardRepository interface {
	SaveSnapshot(ctx context.Context, sessionID string, entries []models.LeaderboardEntry) error
}

// AnswerRecord represents an answer to persist.
type AnswerRecord struct {
	SessionID  string
	UserID     string
	QuestionID string
	SelectedID string
	Correct    bool
	Score      int
	TimeTakenMs int
}
