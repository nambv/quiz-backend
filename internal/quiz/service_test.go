package quiz

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/nambuivu/quiz-server/internal/hub"
	"github.com/nambuivu/quiz-server/internal/models"
	"github.com/nambuivu/quiz-server/internal/scoring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockQuizRepo struct {
	quizzes map[string]*models.Quiz
	err     error
}

func (m *mockQuizRepo) GetAllQuizzes(_ context.Context) (map[string]*models.Quiz, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.quizzes, nil
}

func (m *mockQuizRepo) GetQuiz(_ context.Context, id string) (*models.Quiz, error) {
	q, ok := m.quizzes[id]
	if !ok {
		return nil, m.err
	}
	return q, nil
}

type mockSessionRepo struct {
	sessionID string
	err       error
}

func (m *mockSessionRepo) CreateSession(_ context.Context, _ string) (string, error) {
	return m.sessionID, m.err
}

func (m *mockSessionRepo) UpdateSessionStatus(_ context.Context, _, _ string) error {
	return m.err
}

type mockLeaderboardRepo struct {
	err error
}

func (m *mockLeaderboardRepo) SaveSnapshot(_ context.Context, _ string, _ []models.LeaderboardEntry) error {
	return m.err
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	lb := scoring.NewMemoryLeaderboard()
	scorer := scoring.New(log, lb)
	h := hub.New(log)
	return New(log, h, scorer, nil)
}

// newTestServiceWithShortQuiz creates a service with a 2-question quiz using 1s time limits.
func newTestServiceWithShortQuiz(t *testing.T) *Service {
	t.Helper()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	lb := scoring.NewMemoryLeaderboard()
	scorer := scoring.New(log, lb)
	h := hub.New(log)
	svc := New(log, h, scorer, nil)

	svc.quizzes["short-quiz"] = &models.Quiz{
		ID:    "short-quiz",
		Title: "Short Quiz",
		Questions: []models.Question{
			{
				ID:        "sq1",
				Text:      "Question 1?",
				Options:   []models.Option{{ID: "a1", Text: "A"}, {ID: "a2", Text: "B"}},
				CorrectID: "a1",
				TimeLimit: 1,
			},
			{
				ID:        "sq2",
				Text:      "Question 2?",
				Options:   []models.Option{{ID: "a1", Text: "A"}, {ID: "a2", Text: "B"}},
				CorrectID: "a2",
				TimeLimit: 1,
			},
		},
	}
	return svc
}

func TestQuizExists(t *testing.T) {
	svc := newTestService(t)

	assert.True(t, svc.QuizExists("quiz-vocab-01"))
	assert.False(t, svc.QuizExists("nonexistent"))
	assert.False(t, svc.QuizExists(""))
}

func TestStartQuiz_NotFound(t *testing.T) {
	svc := newTestService(t)

	err := svc.StartQuiz("nonexistent")
	require.Error(t, err)
	assert.ErrorIs(t, err, errQuizNotFound)
}

func TestStartQuiz_NoSession(t *testing.T) {
	svc := newTestService(t)

	err := svc.StartQuiz("quiz-vocab-01")
	require.Error(t, err)
	assert.ErrorIs(t, err, errQuizNotFound)
}

func TestStartQuiz_AlreadyStarted(t *testing.T) {
	svc := newTestServiceWithShortQuiz(t)
	quizID := "short-quiz"

	svc.sessions[quizID] = &models.QuizSession{
		QuizID: quizID,
		Status: models.StatusWaiting,
	}
	require.NoError(t, svc.StartQuiz(quizID))

	err := svc.StartQuiz(quizID)
	require.Error(t, err)
	assert.ErrorIs(t, err, errQuizAlreadyStarted)
}

func TestGetSession(t *testing.T) {
	svc := newTestService(t)

	_, ok := svc.GetSession("quiz-vocab-01")
	assert.False(t, ok)
}

func TestGetSession_AfterStart(t *testing.T) {
	svc := newTestServiceWithShortQuiz(t)
	quizID := "short-quiz"

	svc.sessions[quizID] = &models.QuizSession{
		QuizID: quizID,
		Status: models.StatusWaiting,
	}
	require.NoError(t, svc.StartQuiz(quizID))

	sess, ok := svc.GetSession(quizID)
	require.True(t, ok)
	assert.Equal(t, models.StatusActive, sess.Status)
}

func TestAdvanceQuestion(t *testing.T) {
	svc := newTestServiceWithShortQuiz(t)
	quizID := "short-quiz"

	svc.sessions[quizID] = &models.QuizSession{
		QuizID: quizID,
		Status: models.StatusActive,
	}

	// Advance from question 0 → 1
	svc.advanceQuestion(quizID)

	svc.mu.RLock()
	sess := svc.sessions[quizID]
	assert.Equal(t, 1, sess.CurrentQuestion)
	assert.Equal(t, models.StatusActive, sess.Status)
	svc.mu.RUnlock()
}

func TestAdvanceQuestion_CompletesQuiz(t *testing.T) {
	svc := newTestServiceWithShortQuiz(t)
	quizID := "short-quiz"

	svc.sessions[quizID] = &models.QuizSession{
		QuizID:          quizID,
		Status:          models.StatusActive,
		CurrentQuestion: 1,
	}

	// Advance past last question → completes quiz
	svc.advanceQuestion(quizID)

	svc.mu.RLock()
	sess := svc.sessions[quizID]
	assert.Equal(t, models.StatusCompleted, sess.Status)
	svc.mu.RUnlock()
}

func TestAdvanceQuestion_NoSession(t *testing.T) {
	svc := newTestServiceWithShortQuiz(t)

	// Should not panic
	svc.advanceQuestion("nonexistent")
}

func TestAdvanceQuestion_NotActive(t *testing.T) {
	svc := newTestServiceWithShortQuiz(t)
	quizID := "short-quiz"

	svc.sessions[quizID] = &models.QuizSession{
		QuizID: quizID,
		Status: models.StatusCompleted,
	}

	svc.advanceQuestion(quizID)

	svc.mu.RLock()
	assert.Equal(t, models.StatusCompleted, svc.sessions[quizID].Status)
	svc.mu.RUnlock()
}

func TestCompleteQuiz(t *testing.T) {
	svc := newTestServiceWithShortQuiz(t)
	quizID := "short-quiz"

	svc.sessions[quizID] = &models.QuizSession{
		QuizID: quizID,
		Status: models.StatusActive,
	}

	svc.mu.Lock()
	svc.completeQuiz(quizID)
	svc.mu.Unlock()

	sess, ok := svc.GetSession(quizID)
	require.True(t, ok)
	assert.Equal(t, models.StatusCompleted, sess.Status)
}

func TestStartQuestionTimer(t *testing.T) {
	svc := newTestServiceWithShortQuiz(t)
	quizID := "short-quiz"

	svc.sessions[quizID] = &models.QuizSession{
		QuizID: quizID,
		Status: models.StatusActive,
	}

	svc.mu.Lock()
	svc.startQuestionTimer(quizID, 1)
	svc.mu.Unlock()

	// Wait for timer to fire and advance the question
	time.Sleep(1500 * time.Millisecond)

	svc.mu.RLock()
	sess := svc.sessions[quizID]
	assert.Equal(t, 1, sess.CurrentQuestion)
	svc.mu.RUnlock()
}

func TestNewWithQuizRepo(t *testing.T) {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	lb := scoring.NewMemoryLeaderboard()
	scorer := scoring.New(log, lb)
	h := hub.New(log)

	quizzes := map[string]*models.Quiz{
		"db-quiz": {
			ID:    "db-quiz",
			Title: "DB Quiz",
			Questions: []models.Question{
				{ID: "q1", Text: "Q1?", Options: []models.Option{{ID: "a1", Text: "A"}}, CorrectID: "a1", TimeLimit: 10},
			},
		},
	}
	repo := &mockQuizRepo{quizzes: quizzes}
	svc := New(log, h, scorer, &Deps{QuizRepo: repo})

	assert.True(t, svc.QuizExists("db-quiz"))
	assert.False(t, svc.QuizExists("quiz-vocab-01"))
}

func TestNewWithQuizRepo_Error(t *testing.T) {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	lb := scoring.NewMemoryLeaderboard()
	scorer := scoring.New(log, lb)
	h := hub.New(log)

	repo := &mockQuizRepo{err: assert.AnError}
	svc := New(log, h, scorer, &Deps{QuizRepo: repo})

	// Falls back to mock data
	assert.True(t, svc.QuizExists("quiz-vocab-01"))
}

func TestCompleteQuiz_WithRepos(t *testing.T) {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	lb := scoring.NewMemoryLeaderboard()
	scorer := scoring.New(log, lb)
	h := hub.New(log)

	sessionRepo := &mockSessionRepo{sessionID: "sess-123"}
	lbRepo := &mockLeaderboardRepo{}
	svc := New(log, h, scorer, &Deps{SessionRepo: sessionRepo, LeaderboardRepo: lbRepo})

	quizID := "quiz-vocab-01"
	svc.sessions[quizID] = &models.QuizSession{
		QuizID: quizID,
		Status: models.StatusActive,
	}
	svc.sessionIDs[quizID] = "sess-123"

	svc.mu.Lock()
	svc.completeQuiz(quizID)
	svc.mu.Unlock()

	// Allow async goroutine to execute
	time.Sleep(100 * time.Millisecond)

	sess, ok := svc.GetSession(quizID)
	require.True(t, ok)
	assert.Equal(t, models.StatusCompleted, sess.Status)
}

func TestStartQuiz_WithSessionRepo(t *testing.T) {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	lb := scoring.NewMemoryLeaderboard()
	scorer := scoring.New(log, lb)
	h := hub.New(log)

	sessionRepo := &mockSessionRepo{sessionID: "sess-456"}
	svc := New(log, h, scorer, &Deps{SessionRepo: sessionRepo})

	quizID := "quiz-vocab-01"
	svc.sessions[quizID] = &models.QuizSession{
		QuizID: quizID,
		Status: models.StatusWaiting,
	}
	svc.sessionIDs[quizID] = "sess-456"

	require.NoError(t, svc.StartQuiz(quizID))

	// Allow async goroutine to execute
	time.Sleep(100 * time.Millisecond)

	sess, ok := svc.GetSession(quizID)
	require.True(t, ok)
	assert.Equal(t, models.StatusActive, sess.Status)
}

func TestMockQuizzes(t *testing.T) {
	quizzes := MockQuizzes()

	require.Contains(t, quizzes, "quiz-vocab-01")
	q := quizzes["quiz-vocab-01"]
	assert.Equal(t, "English Vocabulary Challenge", q.Title)
	assert.Len(t, q.Questions, 5)

	for _, question := range q.Questions {
		assert.NotEmpty(t, question.ID)
		assert.NotEmpty(t, question.Text)
		assert.Len(t, question.Options, 4)
		assert.NotEmpty(t, question.CorrectID)
		assert.Greater(t, question.TimeLimit, 0)
	}
}
