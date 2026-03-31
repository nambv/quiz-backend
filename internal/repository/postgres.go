package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nambuivu/quiz-server/internal/models"
)

// Postgres implements all repository interfaces using PostgreSQL.
// AI-ASSISTED: Claude Code — PostgreSQL repository layer with pgx
// Verification: tested with docker-compose PostgreSQL, verified query correctness
type Postgres struct {
	pool *pgxpool.Pool
}

// NewPostgres creates a Postgres repository with the given connection pool.
func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

// GetAllQuizzes loads all quizzes with their questions from PostgreSQL.
func (p *Postgres) GetAllQuizzes(ctx context.Context) (map[string]*models.Quiz, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT q.id, q.title,
		       qu.id, qu.text, qu.options, qu.correct_id, qu.time_limit, qu.sort_order
		FROM quizzes q
		JOIN questions qu ON qu.quiz_id = q.id
		ORDER BY q.id, qu.sort_order
	`)
	if err != nil {
		return nil, fmt.Errorf("query quizzes: %w", err)
	}
	defer rows.Close()

	quizzes := make(map[string]*models.Quiz)
	for rows.Next() {
		var (
			quizID, title         string
			qID, text, correctID  string
			optionsJSON           []byte
			timeLimit, sortOrder  int
		)
		if err := rows.Scan(&quizID, &title, &qID, &text, &optionsJSON, &correctID, &timeLimit, &sortOrder); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		var options []models.Option
		if err := json.Unmarshal(optionsJSON, &options); err != nil {
			return nil, fmt.Errorf("unmarshal options for question %s: %w", qID, err)
		}

		quiz, ok := quizzes[quizID]
		if !ok {
			quiz = &models.Quiz{ID: quizID, Title: title}
			quizzes[quizID] = quiz
		}
		quiz.Questions = append(quiz.Questions, models.Question{
			ID:        qID,
			Text:      text,
			Options:   options,
			CorrectID: correctID,
			TimeLimit: timeLimit,
			SortOrder: sortOrder,
		})
	}

	return quizzes, rows.Err()
}

// GetQuiz loads a single quiz with its questions.
func (p *Postgres) GetQuiz(ctx context.Context, quizID string) (*models.Quiz, error) {
	var title string
	err := p.pool.QueryRow(ctx, `SELECT title FROM quizzes WHERE id = $1`, quizID).Scan(&title)
	if err != nil {
		return nil, fmt.Errorf("query quiz %s: %w", quizID, err)
	}

	rows, err := p.pool.Query(ctx, `
		SELECT id, text, options, correct_id, time_limit, sort_order
		FROM questions WHERE quiz_id = $1 ORDER BY sort_order
	`, quizID)
	if err != nil {
		return nil, fmt.Errorf("query questions: %w", err)
	}
	defer rows.Close()

	quiz := &models.Quiz{ID: quizID, Title: title}
	for rows.Next() {
		var (
			qID, text, correctID string
			optionsJSON          []byte
			timeLimit, sortOrder int
		)
		if err := rows.Scan(&qID, &text, &optionsJSON, &correctID, &timeLimit, &sortOrder); err != nil {
			return nil, fmt.Errorf("scan question: %w", err)
		}

		var options []models.Option
		if err := json.Unmarshal(optionsJSON, &options); err != nil {
			return nil, fmt.Errorf("unmarshal options: %w", err)
		}

		quiz.Questions = append(quiz.Questions, models.Question{
			ID:        qID,
			Text:      text,
			Options:   options,
			CorrectID: correctID,
			TimeLimit: timeLimit,
			SortOrder: sortOrder,
		})
	}

	return quiz, rows.Err()
}

// CreateSession inserts a new quiz session and returns its ID.
func (p *Postgres) CreateSession(ctx context.Context, quizID string) (string, error) {
	var sessionID string
	err := p.pool.QueryRow(ctx,
		`INSERT INTO quiz_sessions (quiz_id) VALUES ($1) RETURNING id`, quizID,
	).Scan(&sessionID)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return sessionID, nil
}

// UpdateSessionStatus updates the status and timestamps of a session.
func (p *Postgres) UpdateSessionStatus(ctx context.Context, sessionID, status string) error {
	var query string
	switch status {
	case "active":
		query = `UPDATE quiz_sessions SET status = $1, started_at = now() WHERE id = $2`
	case "completed":
		query = `UPDATE quiz_sessions SET status = $1, ended_at = now() WHERE id = $2`
	default:
		query = `UPDATE quiz_sessions SET status = $1 WHERE id = $2`
	}
	_, err := p.pool.Exec(ctx, query, status, sessionID)
	if err != nil {
		return fmt.Errorf("update session status: %w", err)
	}
	return nil
}

// SaveAnswer persists an answer record.
func (p *Postgres) SaveAnswer(ctx context.Context, answer *AnswerRecord) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO answers (session_id, user_id, question_id, selected_id, correct, score, time_taken_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (session_id, user_id, question_id) DO NOTHING
	`, answer.SessionID, answer.UserID, answer.QuestionID, answer.SelectedID, answer.Correct, answer.Score, answer.TimeTakenMs)
	if err != nil {
		return fmt.Errorf("save answer: %w", err)
	}
	return nil
}

// SaveSnapshot persists the final leaderboard for a session.
func (p *Postgres) SaveSnapshot(ctx context.Context, sessionID string, entries []models.LeaderboardEntry) error {
	for _, e := range entries {
		_, err := p.pool.Exec(ctx, `
			INSERT INTO leaderboard_snapshots (session_id, user_id, username, final_score, final_rank)
			VALUES ($1, $2, $3, $4, $5)
		`, sessionID, e.UserID, e.Username, e.Score, e.Rank)
		if err != nil {
			return fmt.Errorf("save leaderboard entry: %w", err)
		}
	}
	return nil
}
