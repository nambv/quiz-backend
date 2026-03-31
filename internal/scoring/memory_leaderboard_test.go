package scoring

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryLeaderboard_IncrAndRank(t *testing.T) {
	lb := NewMemoryLeaderboard()

	require.NoError(t, lb.IncrScore("quiz1", "alice", 150))
	require.NoError(t, lb.IncrScore("quiz1", "bob", 100))
	require.NoError(t, lb.IncrScore("quiz1", "alice", 80)) // cumulative

	rankings, err := lb.GetRankings("quiz1")
	require.NoError(t, err)
	require.Len(t, rankings, 2)

	assert.Equal(t, "alice", rankings[0].UserID)
	assert.Equal(t, 230, rankings[0].Score)
	assert.Equal(t, "bob", rankings[1].UserID)
	assert.Equal(t, 100, rankings[1].Score)
}

func TestMemoryLeaderboard_EmptyQuiz(t *testing.T) {
	lb := NewMemoryLeaderboard()

	rankings, err := lb.GetRankings("nonexistent")
	require.NoError(t, err)
	assert.Empty(t, rankings)
}

func TestMemoryLeaderboard_MultipleQuizzes(t *testing.T) {
	lb := NewMemoryLeaderboard()

	require.NoError(t, lb.IncrScore("quiz1", "alice", 100))
	require.NoError(t, lb.IncrScore("quiz2", "bob", 200))

	r1, err := lb.GetRankings("quiz1")
	require.NoError(t, err)
	require.Len(t, r1, 1)
	assert.Equal(t, "alice", r1[0].UserID)

	r2, err := lb.GetRankings("quiz2")
	require.NoError(t, err)
	require.Len(t, r2, 1)
	assert.Equal(t, "bob", r2[0].UserID)
}
