package models

import "time"

// Quiz status constants.
const (
	StatusWaiting   = "waiting"
	StatusActive    = "active"
	StatusCompleted = "completed"
)

// Quiz represents a quiz definition with its questions.
type Quiz struct {
	ID        string
	Title     string
	Questions []Question
}

// Question represents a single quiz question.
type Question struct {
	ID        string
	Text      string
	Options   []Option
	CorrectID string
	TimeLimit int // seconds
	SortOrder int
}

// QuizSession represents a running instance of a quiz.
type QuizSession struct {
	QuizID          string
	Status          string
	CurrentQuestion int // index into Quiz.Questions
	StartedAt       time.Time
	QuestionStartAt time.Time
}

// Participant represents a user in a quiz session.
type Participant struct {
	UserID   string
	Username string
}
