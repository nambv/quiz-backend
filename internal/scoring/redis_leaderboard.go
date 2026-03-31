package scoring

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLeaderboard implements Leaderboard using Redis Sorted Sets.
// AI-ASSISTED: Claude Code — Redis Sorted Set operations for O(log N) ranking
// Verification: tested with real Redis via docker-compose, verified ZADD INCR and ZREVRANGEWITHSCORES behavior
type RedisLeaderboard struct {
	client *redis.Client
}

// NewRedisLeaderboard creates a RedisLeaderboard connected to the given Redis client.
func NewRedisLeaderboard(client *redis.Client) *RedisLeaderboard {
	return &RedisLeaderboard{client: client}
}

func (r *RedisLeaderboard) leaderboardKey(quizID string) string {
	return "quiz:" + quizID + ":leaderboard"
}

// IncrScore atomically increments a user's score in the sorted set.
func (r *RedisLeaderboard) IncrScore(quizID, userID string, delta int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return r.client.ZIncrBy(ctx, r.leaderboardKey(quizID), float64(delta), userID).Err()
}

// ResetQuiz removes the leaderboard sorted set for a quiz.
func (r *RedisLeaderboard) ResetQuiz(quizID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return r.client.Del(ctx, r.leaderboardKey(quizID)).Err()
}

// GetRankings returns all entries sorted by score descending.
func (r *RedisLeaderboard) GetRankings(quizID string) ([]RankEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	results, err := r.client.ZRevRangeWithScores(ctx, r.leaderboardKey(quizID), 0, -1).Result()
	if err != nil {
		return nil, err
	}

	entries := make([]RankEntry, len(results))
	for i, z := range results {
		entries[i] = RankEntry{
			UserID: z.Member.(string),
			Score:  int(z.Score),
		}
	}
	return entries, nil
}
