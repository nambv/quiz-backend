package quiz

import "errors"

var (
	errQuizNotFound      = errors.New("quiz not found")
	errQuizAlreadyStarted = errors.New("quiz already started")
)
