package scoring

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testLog = slog.New(slog.NewJSONHandler(os.Stdout, nil))

func TestSubmitAnswer(t *testing.T) {
	tests := []struct {
		name          string
		answerID      string
		correctID     string
		elapsed       time.Duration
		timeLimit     int
		wantCorrect   bool
		wantMinPoints int
		wantMaxPoints int
		wantErr       error
	}{
		{
			name:          "correct answer fast",
			answerID:      "a2",
			correctID:     "a2",
			elapsed:       1 * time.Second,
			timeLimit:     15,
			wantCorrect:   true,
			wantMinPoints: 150, // ~100*(1+0.93)
			wantMaxPoints: 200,
		},
		{
			name:          "correct answer slow",
			answerID:      "a2",
			correctID:     "a2",
			elapsed:       14 * time.Second,
			timeLimit:     15,
			wantCorrect:   true,
			wantMinPoints: 100, // ~100*(1+0.07)
			wantMaxPoints: 115,
		},
		{
			name:          "correct answer at time limit",
			answerID:      "a2",
			correctID:     "a2",
			elapsed:       15 * time.Second,
			timeLimit:     15,
			wantCorrect:   true,
			wantMinPoints: 100, // 100*(1+0)
			wantMaxPoints: 100,
		},
		{
			name:          "wrong answer",
			answerID:      "a1",
			correctID:     "a2",
			elapsed:       2 * time.Second,
			timeLimit:     15,
			wantCorrect:   false,
			wantMinPoints: 0,
			wantMaxPoints: 0,
		},
		{
			name:      "time limit exceeded",
			answerID:  "a2",
			correctID: "a2",
			elapsed:   18 * time.Second, // 15s limit + 2s tolerance exceeded
			timeLimit: 15,
			wantErr:   ErrTimeLimitExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lb := NewMemoryLeaderboard()
			svc := New(testLog, lb)

			questionStart := time.Now().Add(-tt.elapsed)
			result, err := svc.SubmitAnswer("quiz1", "user1", "Alice", "q1", tt.answerID, tt.correctID, tt.timeLimit, questionStart)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantCorrect, result.Correct)
			assert.GreaterOrEqual(t, result.Points, tt.wantMinPoints)
			assert.LessOrEqual(t, result.Points, tt.wantMaxPoints)
			assert.Equal(t, "q1", result.QuestionID)
		})
	}
}

func TestSubmitAnswer_Idempotency(t *testing.T) {
	lb := NewMemoryLeaderboard()
	svc := New(testLog, lb)

	questionStart := time.Now().Add(-2 * time.Second)

	// First submission succeeds
	_, err := svc.SubmitAnswer("quiz1", "user1", "Alice", "q1", "a2", "a2", 15, questionStart)
	require.NoError(t, err)

	// Duplicate submission fails
	_, err = svc.SubmitAnswer("quiz1", "user1", "Alice", "q1", "a2", "a2", 15, questionStart)
	require.ErrorIs(t, err, ErrAlreadyAnswered)
}

func TestSubmitAnswer_DifferentQuestions(t *testing.T) {
	lb := NewMemoryLeaderboard()
	svc := New(testLog, lb)

	questionStart := time.Now().Add(-2 * time.Second)

	// Same user, different questions — both succeed
	_, err := svc.SubmitAnswer("quiz1", "user1", "Alice", "q1", "a2", "a2", 15, questionStart)
	require.NoError(t, err)

	_, err = svc.SubmitAnswer("quiz1", "user1", "Alice", "q2", "a3", "a3", 15, questionStart)
	require.NoError(t, err)
}

func TestGetLeaderboard_Ranking(t *testing.T) {
	lb := NewMemoryLeaderboard()
	svc := New(testLog, lb)

	questionStart := time.Now().Add(-2 * time.Second)

	// Alice answers correctly (fast)
	_, err := svc.SubmitAnswer("quiz1", "alice", "Alice", "q1", "a2", "a2", 15, questionStart)
	require.NoError(t, err)

	// Bob answers correctly (slower)
	slowStart := time.Now().Add(-10 * time.Second)
	_, err = svc.SubmitAnswer("quiz1", "bob", "Bob", "q1", "a2", "a2", 15, slowStart)
	require.NoError(t, err)

	entries := svc.GetLeaderboard("quiz1")

	require.Len(t, entries, 2)
	// Alice should be ranked higher (answered faster)
	assert.Equal(t, 1, entries[0].Rank)
	assert.Equal(t, "alice", entries[0].UserID)
	assert.Greater(t, entries[0].Score, entries[1].Score)

	// Bob is second
	assert.Equal(t, 2, entries[1].Rank)
	assert.Equal(t, "bob", entries[1].UserID)
}
