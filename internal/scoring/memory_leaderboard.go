package scoring

import (
	"slices"
	"sync"
)

// MemoryLeaderboard implements Leaderboard with an in-memory sorted map.
// Used as fallback when Redis is unavailable, and for testing.
type MemoryLeaderboard struct {
	mu     sync.RWMutex
	scores map[string]map[string]int // quizID → userID → score
}

// NewMemoryLeaderboard creates an in-memory leaderboard.
func NewMemoryLeaderboard() *MemoryLeaderboard {
	return &MemoryLeaderboard{
		scores: make(map[string]map[string]int),
	}
}

// IncrScore increments a user's score.
func (m *MemoryLeaderboard) IncrScore(quizID, userID string, delta int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.scores[quizID] == nil {
		m.scores[quizID] = make(map[string]int)
	}
	m.scores[quizID][userID] += delta
	return nil
}

// ResetQuiz removes all scores for a quiz.
func (m *MemoryLeaderboard) ResetQuiz(quizID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.scores, quizID)
	return nil
}

// GetRankings returns all entries sorted by score descending.
func (m *MemoryLeaderboard) GetRankings(quizID string) ([]RankEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userScores := m.scores[quizID]
	entries := make([]RankEntry, 0, len(userScores))
	for userID, score := range userScores {
		entries = append(entries, RankEntry{UserID: userID, Score: score})
	}

	slices.SortFunc(entries, func(a, b RankEntry) int {
		if a.Score != b.Score {
			return b.Score - a.Score // descending
		}
		return 0
	})

	return entries, nil
}
