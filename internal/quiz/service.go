package quiz

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nambuivu/quiz-server/internal/hub"
	"github.com/nambuivu/quiz-server/internal/metrics"
	"github.com/nambuivu/quiz-server/internal/models"
	"github.com/nambuivu/quiz-server/internal/repository"
	"github.com/nambuivu/quiz-server/internal/scoring"
)

// Service manages quiz sessions, question lifecycle, and coordinates scoring.
// Implements hub.MessageHandler.
type Service struct {
	mu           sync.RWMutex
	log          *slog.Logger
	hub          *hub.Hub
	scorer       *scoring.Service
	repo         repository.QuizRepository                 // nil if no DB
	sessionRepo  repository.SessionRepository              // nil if no DB
	answerRepo   repository.AnswerRepository               // nil if no DB
	lbRepo       repository.LeaderboardRepository          // nil if no DB
	quizzes      map[string]*models.Quiz                   // quizID → quiz definition
	sessions     map[string]*models.QuizSession            // quizID → active session
	sessionIDs   map[string]string                         // quizID → DB session UUID
	participants map[string]map[string]*models.Participant // quizID → userID → participant
	timers       map[string]*time.Timer                    // quizID → current question timer
}

// Deps holds optional dependencies for the quiz service.
type Deps struct {
	QuizRepo        repository.QuizRepository
	SessionRepo     repository.SessionRepository
	AnswerRepo      repository.AnswerRepository
	LeaderboardRepo repository.LeaderboardRepository
}

// New creates a Service and registers itself as the hub's message handler.
// If deps provides a QuizRepository, quizzes are loaded from the database.
// Otherwise, mock data is used.
func New(log *slog.Logger, h *hub.Hub, scorer *scoring.Service, deps *Deps) *Service {
	s := &Service{
		log:          log.With("component", "quiz"),
		hub:          h,
		scorer:       scorer,
		sessions:     make(map[string]*models.QuizSession),
		sessionIDs:   make(map[string]string),
		participants: make(map[string]map[string]*models.Participant),
		timers:       make(map[string]*time.Timer),
	}

	if deps != nil {
		s.repo = deps.QuizRepo
		s.sessionRepo = deps.SessionRepo
		s.answerRepo = deps.AnswerRepo
		s.lbRepo = deps.LeaderboardRepo
	}

	// Load quizzes from DB or fall back to mock data
	if s.repo != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		quizzes, err := s.repo.GetAllQuizzes(ctx)
		if err != nil {
			s.log.Error("failed to load quizzes from database, using mock data", "error", err)
			s.quizzes = MockQuizzes()
		} else {
			s.quizzes = quizzes
			s.log.Info("loaded quizzes from database", "count", len(quizzes))
		}
	} else {
		s.quizzes = MockQuizzes()
	}

	h.SetHandler(s)
	return s
}

// QuizExists checks whether a quiz definition exists.
func (s *Service) QuizExists(quizID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.quizzes[quizID]
	return ok
}

// ListQuizzes returns a summary of all available quizzes.
func (s *Service) ListQuizzes() []map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]map[string]any, 0, len(s.quizzes))
	for _, q := range s.quizzes {
		result = append(result, map[string]any{
			"id":            q.ID,
			"title":         q.Title,
			"questionCount": len(q.Questions),
		})
	}
	return result
}

// ResetQuiz clears all in-memory state for a quiz (session, participants, timers).
func (s *Service) ResetQuiz(quizID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.quizzes[quizID]; !ok {
		return errQuizNotFound
	}

	if timer, ok := s.timers[quizID]; ok {
		timer.Stop()
		delete(s.timers, quizID)
	}
	delete(s.sessions, quizID)
	delete(s.sessionIDs, quizID)
	delete(s.participants, quizID)

	s.scorer.Reset(quizID)

	s.log.Info("quiz reset", "quiz_id", quizID)
	return nil
}

// HandleRejoin processes a reconnection from a previously connected client.
// It validates the user was in the quiz, re-registers the connection, and replays current state.
func (s *Service) HandleRejoin(c *hub.Client, p models.RejoinPayload) {
	s.mu.Lock()

	quiz, ok := s.quizzes[c.QuizID]
	if !ok {
		s.mu.Unlock()
		c.SendError("QUIZ_NOT_FOUND", "quiz does not exist")
		return
	}

	session, ok := s.sessions[c.QuizID]
	if !ok {
		s.mu.Unlock()
		c.SendError("SESSION_NOT_FOUND", "no active session for this quiz")
		return
	}

	// Validate user was previously a participant
	participant, ok := s.participants[c.QuizID][p.UserID]
	if !ok {
		s.mu.Unlock()
		c.SendError("NOT_A_PARTICIPANT", "user was not in this quiz")
		return
	}

	// Re-assign client identity
	c.UserID = p.UserID
	c.Username = participant.Username

	// Re-register in hub room
	s.hub.Register(c)

	// Build participant list
	participantNames := make([]string, 0, len(s.participants[c.QuizID]))
	for _, pt := range s.participants[c.QuizID] {
		participantNames = append(participantNames, pt.Username)
	}

	// Build current question payload
	var currentQ *models.QuestionPayload
	if session.Status == models.StatusActive && session.CurrentQuestion < len(quiz.Questions) {
		q := quiz.Questions[session.CurrentQuestion]
		currentQ = &models.QuestionPayload{
			ID:        q.ID,
			Text:      q.Text,
			Options:   q.Options,
			TimeLimit: q.TimeLimit,
			Index:     session.CurrentQuestion + 1,
			Total:     len(quiz.Questions),
		}
	}

	s.mu.Unlock()

	// Get leaderboard snapshot
	leaderboard := s.scorer.GetLeaderboard(c.QuizID)

	// Replay current quiz state to the reconnected client
	c.SendEnvelope(models.TypeQuizState, models.QuizStatePayload{
		QuizID:          c.QuizID,
		Status:          session.Status,
		CurrentQuestion: currentQ,
		Participants:    participantNames,
		Leaderboard:     leaderboard,
	})

	s.log.Info("user rejoined quiz", "user_id", p.UserID, "quiz_id", c.QuizID)
}

// HandleJoin processes a join message from a client.
func (s *Service) HandleJoin(c *hub.Client, p models.JoinPayload) {
	s.mu.Lock()

	quiz, ok := s.quizzes[c.QuizID]
	if !ok {
		s.mu.Unlock()
		c.SendError("QUIZ_NOT_FOUND", "quiz does not exist")
		return
	}

	// Initialize session if first join
	session, ok := s.sessions[c.QuizID]
	if !ok {
		session = &models.QuizSession{
			QuizID:          c.QuizID,
			Status:          models.StatusWaiting,
			CurrentQuestion: 0,
		}
		s.sessions[c.QuizID] = session

		// Create DB session if available
		if s.sessionRepo != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			sessionID, err := s.sessionRepo.CreateSession(ctx, c.QuizID)
			cancel()
			if err != nil {
				s.log.Error("failed to create DB session", "quiz_id", c.QuizID, "error", err)
			} else {
				s.sessionIDs[c.QuizID] = sessionID
			}
		}
	}

	// Reset session for replay if completed
	if session.Status == models.StatusCompleted {
		session.Status = models.StatusWaiting
		session.CurrentQuestion = 0
		s.participants[c.QuizID] = make(map[string]*models.Participant)
		s.scorer.Reset(c.QuizID)
	}

	// Initialize participants map
	if s.participants[c.QuizID] == nil {
		s.participants[c.QuizID] = make(map[string]*models.Participant)
	}

	// Check duplicate join
	if _, exists := s.participants[c.QuizID][p.UserID]; exists {
		s.mu.Unlock()
		c.SendError("ALREADY_JOINED", "user already in this quiz")
		return
	}

	// Register participant
	c.UserID = p.UserID
	c.Username = p.Username
	s.participants[c.QuizID][p.UserID] = &models.Participant{
		UserID:   p.UserID,
		Username: p.Username,
	}
	s.scorer.RegisterParticipant(c.QuizID, p.UserID, p.Username)

	// Register in hub room
	s.hub.Register(c)

	// Build participant list
	participantNames := make([]string, 0, len(s.participants[c.QuizID]))
	for _, pt := range s.participants[c.QuizID] {
		participantNames = append(participantNames, pt.Username)
	}

	// Build current question payload
	var currentQ *models.QuestionPayload
	if session.Status == models.StatusActive && session.CurrentQuestion < len(quiz.Questions) {
		q := quiz.Questions[session.CurrentQuestion]
		currentQ = &models.QuestionPayload{
			ID:        q.ID,
			Text:      q.Text,
			Options:   q.Options,
			TimeLimit: q.TimeLimit,
			Index:     session.CurrentQuestion + 1,
			Total:     len(quiz.Questions),
		}
	}

	s.mu.Unlock()

	// Get leaderboard snapshot
	leaderboard := s.scorer.GetLeaderboard(c.QuizID)

	// Send quiz state to joining client
	c.SendEnvelope(models.TypeQuizState, models.QuizStatePayload{
		QuizID:          c.QuizID,
		Status:          session.Status,
		CurrentQuestion: currentQ,
		Participants:    participantNames,
		Leaderboard:     leaderboard,
	})

	// Broadcast user joined to room
	s.hub.BroadcastToRoom(c.QuizID, models.TypeUserJoined, models.UserJoinedPayload(p))

	s.log.Info("user joined quiz", "user_id", p.UserID, "username", p.Username, "quiz_id", c.QuizID)
}

// HandleAnswer processes an answer submission from a client.
func (s *Service) HandleAnswer(c *hub.Client, p models.AnswerPayload) {
	answerStart := time.Now()
	s.mu.RLock()
	session, ok := s.sessions[c.QuizID]
	if !ok || session.Status != models.StatusActive {
		s.mu.RUnlock()
		c.SendError("QUIZ_NOT_ACTIVE", "quiz is not currently active")
		return
	}

	quiz := s.quizzes[c.QuizID]
	if session.CurrentQuestion >= len(quiz.Questions) {
		s.mu.RUnlock()
		c.SendError("NO_QUESTION", "no active question")
		return
	}

	question := quiz.Questions[session.CurrentQuestion]
	if p.QuestionID != question.ID {
		s.mu.RUnlock()
		c.SendError("WRONG_QUESTION", "this question is no longer active")
		return
	}

	questionStart := session.QuestionStartAt
	timeLimit := question.TimeLimit
	s.mu.RUnlock()

	// Delegate to scoring service
	result, err := s.scorer.SubmitAnswer(c.QuizID, c.UserID, c.Username, p.QuestionID, p.AnswerID, question.CorrectID, timeLimit, questionStart)
	metrics.AnswerDuration.Observe(time.Since(answerStart).Seconds())
	if err != nil {
		metrics.ScoringErrors.WithLabelValues(err.Error()).Inc()
		c.SendError("SCORING_ERROR", err.Error())
		return
	}

	// Persist answer asynchronously
	if s.answerRepo != nil {
		s.mu.RLock()
		sessionID := s.sessionIDs[c.QuizID]
		s.mu.RUnlock()
		if sessionID != "" {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				elapsed := time.Since(questionStart)
				if err := s.answerRepo.SaveAnswer(ctx, &repository.AnswerRecord{
					SessionID:   sessionID,
					UserID:      c.UserID,
					QuestionID:  p.QuestionID,
					SelectedID:  p.AnswerID,
					Correct:     result.Correct,
					Score:       result.Points,
					TimeTakenMs: int(elapsed.Milliseconds()),
				}); err != nil {
					s.log.Error("failed to persist answer", "quiz_id", c.QuizID, "user_id", c.UserID, "error", err)
				}
			}()
		}
	}

	// Send score result to the answering user
	c.SendEnvelope(models.TypeScoreResult, result)

	// Broadcast updated leaderboard to the room
	lbStart := time.Now()
	leaderboard := s.scorer.GetLeaderboard(c.QuizID)
	s.hub.BroadcastToRoom(c.QuizID, models.TypeLeaderboardUpdate, models.LeaderboardUpdatePayload{
		Leaderboard: leaderboard,
	})
	metrics.LeaderboardUpdateDuration.Observe(time.Since(lbStart).Seconds())
}

// StartQuiz transitions a quiz from waiting to active and starts the first question.
func (s *Service) StartQuiz(quizID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[quizID]
	if !ok {
		return errQuizNotFound
	}
	if session.Status == models.StatusActive {
		return errQuizAlreadyStarted
	}

	// Always reset scores when starting to ensure clean state
	s.scorer.Reset(quizID)

	quiz := s.quizzes[quizID]
	session.Status = models.StatusActive
	session.StartedAt = time.Now()
	session.CurrentQuestion = 0
	session.QuestionStartAt = time.Now()

	// Broadcast first question
	q := quiz.Questions[0]
	s.hub.BroadcastToRoom(quizID, models.TypeQuestion, models.QuestionPayload{
		ID:        q.ID,
		Text:      q.Text,
		Options:   q.Options,
		TimeLimit: q.TimeLimit,
		Index:     1,
		Total:     len(quiz.Questions),
	})

	// Start question timer
	s.startQuestionTimer(quizID, q.TimeLimit)

	// Persist session status change
	if s.sessionRepo != nil {
		if sessionID, ok := s.sessionIDs[quizID]; ok {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if err := s.sessionRepo.UpdateSessionStatus(ctx, sessionID, models.StatusActive); err != nil {
					s.log.Error("failed to update session status", "session_id", sessionID, "error", err)
				}
			}()
		}
	}

	s.log.Info("quiz started", "quiz_id", quizID, "total_questions", len(quiz.Questions))
	return nil
}

// startQuestionTimer schedules advancing to the next question after the time limit.
func (s *Service) startQuestionTimer(quizID string, timeLimitSec int) {
	if timer, ok := s.timers[quizID]; ok {
		timer.Stop()
	}
	s.timers[quizID] = time.AfterFunc(time.Duration(timeLimitSec)*time.Second, func() {
		s.advanceQuestion(quizID)
	})
}

// AdvanceQuestion moves to the next question or ends the quiz.
// Exposed so the handler layer can trigger it via a REST endpoint.
func (s *Service) AdvanceQuestion(quizID string) {
	s.advanceQuestion(quizID)
}

// advanceQuestion moves to the next question or ends the quiz.
func (s *Service) advanceQuestion(quizID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[quizID]
	if !ok || session.Status != models.StatusActive {
		return
	}

	quiz := s.quizzes[quizID]
	session.CurrentQuestion++

	if session.CurrentQuestion >= len(quiz.Questions) {
		s.completeQuiz(quizID)
		return
	}

	session.QuestionStartAt = time.Now()
	q := quiz.Questions[session.CurrentQuestion]

	s.hub.BroadcastToRoom(quizID, models.TypeQuestion, models.QuestionPayload{
		ID:        q.ID,
		Text:      q.Text,
		Options:   q.Options,
		TimeLimit: q.TimeLimit,
		Index:     session.CurrentQuestion + 1,
		Total:     len(quiz.Questions),
	})

	s.startQuestionTimer(quizID, q.TimeLimit)

	s.log.Info("question advanced", "quiz_id", quizID, "question_index", session.CurrentQuestion+1)
}

// completeQuiz ends the quiz and broadcasts final leaderboard. Caller must hold s.mu.
func (s *Service) completeQuiz(quizID string) {
	session := s.sessions[quizID]
	session.Status = models.StatusCompleted

	if timer, ok := s.timers[quizID]; ok {
		timer.Stop()
		delete(s.timers, quizID)
	}

	leaderboard := s.scorer.GetLeaderboard(quizID)
	s.hub.BroadcastToRoom(quizID, models.TypeQuizEnded, models.QuizEndedPayload{
		FinalLeaderboard: leaderboard,
	})

	// Persist session completion and leaderboard snapshot asynchronously
	sessionID := s.sessionIDs[quizID]
	if sessionID != "" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if s.sessionRepo != nil {
				if err := s.sessionRepo.UpdateSessionStatus(ctx, sessionID, models.StatusCompleted); err != nil {
					s.log.Error("failed to update session status", "session_id", sessionID, "error", err)
				}
			}
			if s.lbRepo != nil && len(leaderboard) > 0 {
				if err := s.lbRepo.SaveSnapshot(ctx, sessionID, leaderboard); err != nil {
					s.log.Error("failed to save leaderboard snapshot", "session_id", sessionID, "error", err)
				}
			}
		}()
	}

	s.log.Info("quiz completed", "quiz_id", quizID)
}

// GetSession returns the current session for a quiz, if any.
func (s *Service) GetSession(quizID string) (*models.QuizSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[quizID]
	return sess, ok
}
