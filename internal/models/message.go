package models

import "encoding/json"

// Message types for WebSocket protocol routing.
const (
	// Client → Server
	TypeJoin   = "join"
	TypeRejoin = "rejoin"
	TypeAnswer = "answer"

	// Server → Client
	TypeQuizState         = "quiz_state"
	TypeQuestion          = "question"
	TypeScoreResult       = "score_result"
	TypeLeaderboardUpdate = "leaderboard_update"
	TypeUserJoined        = "user_joined"
	TypeUserLeft          = "user_left"
	TypeQuizEnded         = "quiz_ended"
	TypeError             = "error"
)

// Envelope is the top-level WebSocket message format.
type Envelope struct {
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp int64           `json:"timestamp"`
}

// JoinPayload is sent by a client to join a quiz room.
type JoinPayload struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
}

// RejoinPayload is sent by a client to reconnect to a quiz room.
type RejoinPayload struct {
	UserID string `json:"userId"`
	QuizID string `json:"quizId"`
}

// AnswerPayload is sent by a client to submit an answer.
type AnswerPayload struct {
	QuestionID string `json:"questionId"`
	AnswerID   string `json:"answerId"`
}

// QuizStatePayload is sent to a client on join with current quiz state.
type QuizStatePayload struct {
	QuizID          string             `json:"quizId"`
	Status          string             `json:"status"`
	CurrentQuestion *QuestionPayload   `json:"currentQuestion,omitempty"`
	Participants    []string           `json:"participants"`
	Leaderboard     []LeaderboardEntry `json:"leaderboard"`
}

// QuestionPayload represents a question broadcast to participants.
type QuestionPayload struct {
	ID        string   `json:"id"`
	Text      string   `json:"text"`
	Options   []Option `json:"options"`
	TimeLimit int      `json:"timeLimit"`
	Index     int      `json:"index"`
	Total     int      `json:"total"`
}

// Option is a single answer choice for a question.
type Option struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// ScoreResultPayload is sent to the answering user after scoring.
type ScoreResultPayload struct {
	QuestionID string  `json:"questionId"`
	Correct    bool    `json:"correct"`
	Points     int     `json:"points"`
	TotalScore int     `json:"totalScore"`
	TimeBonus  float64 `json:"timeBonus"`
}

// LeaderboardUpdatePayload is broadcast to all room participants.
type LeaderboardUpdatePayload struct {
	Leaderboard []LeaderboardEntry `json:"leaderboard"`
}

// LeaderboardEntry represents one row in the leaderboard.
type LeaderboardEntry struct {
	Rank     int    `json:"rank"`
	UserID   string `json:"userId"`
	Username string `json:"username"`
	Score    int    `json:"score"`
}

// UserJoinedPayload is broadcast when a user joins the room.
type UserJoinedPayload struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
}

// UserLeftPayload is broadcast when a user leaves the room.
type UserLeftPayload struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
}

// QuizEndedPayload is broadcast when the quiz completes.
type QuizEndedPayload struct {
	FinalLeaderboard []LeaderboardEntry `json:"finalLeaderboard"`
}

// ErrorPayload is sent for error conditions.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
