# Test Cases — Real-Time Vocabulary Quiz

Coverage: **82.7%** (excluding `repository/postgres` and `scoring/redis_leaderboard` which require external services).

Run all tests: `go test -race ./...`

---

## 1. Integration Tests — WebSocket Quiz Lifecycle

**File**: `internal/handler/handler_test.go`


| ID   | Test                                | Type        | Description                                                                                                        | Expected Result                                                           |
| ---- | ----------------------------------- | ----------- | ------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------- |
| I-01 | `TestWebSocketLifecycle`            | Integration | Two users connect via WebSocket, join quiz, quiz starts, Alice answers correctly — both receive leaderboard update | Alice's score > 0, leaderboard broadcast to both clients, correct ranking |
| I-02 | `TestWebSocketRejoin`               | Integration | Alice joins and quiz starts, then reconnects on a new WebSocket with `rejoin` message                              | Receives `quiz_state` with status `active` and current question payload   |
| I-03 | `TestWebSocketRejoinNotParticipant` | Integration | Bob attempts to rejoin a quiz he never joined                                                                      | Receives error with code `NOT_A_PARTICIPANT`                              |


## 2. REST API Endpoint Tests

**File**: `internal/handler/handler_test.go`


| ID   | Test                                      | Type        | Description                                           | Expected Result                             |
| ---- | ----------------------------------------- | ----------- | ----------------------------------------------------- | ------------------------------------------- |
| R-01 | `TestHealthEndpoint`                      | Unit        | `GET /health` with no Redis/PG configured             | 200 OK, `{"status": "ok"}`                  |
| R-02 | `TestJoinQuizEndpoint` (valid)            | Unit        | `POST /api/quiz/quiz-vocab-01/join`                   | 200 OK with quiz ID and WebSocket URL       |
| R-03 | `TestJoinQuizEndpoint` (invalid)          | Unit        | `POST /api/quiz/nonexistent/join`                     | 404 Not Found                               |
| R-04 | `TestStartQuizEndpoint` (no session)      | Unit        | `POST /api/quiz/{id}/start` before anyone joins       | 400 Bad Request                             |
| R-05 | `TestStartQuizEndpoint` (success)         | Integration | Join via WebSocket, then `POST /api/quiz/{id}/start`  | 200 OK, `{"status": "started"}`             |
| R-06 | `TestStartQuizEndpoint` (already started) | Integration | Start an already-started quiz                         | 400 Bad Request                             |
| R-07 | `TestLeaderboardEndpoint` (empty)         | Unit        | `GET /api/quiz/{id}/leaderboard` with no scores       | 200 OK, empty leaderboard array             |
| R-08 | `TestLeaderboardEndpoint` (after scoring) | Integration | Join, start, answer correctly, then `GET` leaderboard | 200 OK, Alice in leaderboard with score > 0 |


## 3. WebSocket Protocol Error Handling

**File**: `internal/handler/handler_test.go`


| ID   | Test                               | Type        | Description                                                        | Expected Result                                              |
| ---- | ---------------------------------- | ----------- | ------------------------------------------------------------------ | ------------------------------------------------------------ |
| W-01 | `TestWebSocketWrongAnswer`         | Integration | Alice answers with incorrect option (a1 instead of a2)             | `score_result` with `correct: false`, `points: 0`            |
| W-02 | `TestWebSocketDuplicateJoin`       | Integration | Alice joins, then second connection attempts join with same userID | Error with code `ALREADY_JOINED`                             |
| W-03 | `TestWebSocketAnswerWithoutJoin`   | Integration | Send `answer` message without sending `join` first                 | Error with code `NOT_JOINED`                                 |
| W-04 | `TestWebSocketUnknownMessageType`  | Integration | Send message with type `unknown_type`                              | Error with code `UNKNOWN_TYPE`                               |
| W-05 | `TestWebSocketInvalidQuiz`         | Integration | Attempt WebSocket connection to nonexistent quiz ID                | HTTP 404 on upgrade, connection rejected                     |
| W-06 | `TestWebSocketAnswerWrongQuestion` | Integration | Answer for question ID `q99` when active question is `q1`          | Error with code `WRONG_QUESTION`                             |
| W-07 | `TestWebSocketAnswerDuplicate`     | Integration | Answer same question twice                                         | Error with code `SCORING_ERROR` (wraps `ErrAlreadyAnswered`) |
| W-08 | `TestWebSocketAnswerBeforeStart`   | Integration | Send answer before quiz has been started (status: waiting)         | Error with code `QUIZ_NOT_ACTIVE`                            |


## 4. Scoring Service Tests

**File**: `internal/scoring/service_test.go`


| ID   | Test                                            | Type | Description                                         | Expected Result                                     |
| ---- | ----------------------------------------------- | ---- | --------------------------------------------------- | --------------------------------------------------- |
| S-01 | `TestSubmitAnswer/correct_answer_fast`          | Unit | Correct answer in 1s with 15s limit                 | `correct: true`, points 150–200 (high time bonus)   |
| S-02 | `TestSubmitAnswer/correct_answer_slow`          | Unit | Correct answer in 14s with 15s limit                | `correct: true`, points 100–115 (low time bonus)    |
| S-03 | `TestSubmitAnswer/correct_answer_at_time_limit` | Unit | Correct answer at exactly 15s limit                 | `correct: true`, points exactly 100 (no time bonus) |
| S-04 | `TestSubmitAnswer/wrong_answer`                 | Unit | Incorrect answer                                    | `correct: false`, points 0                          |
| S-05 | `TestSubmitAnswer/time_limit_exceeded`          | Unit | Answer at 18s with 15s limit (beyond 2s tolerance)  | `ErrTimeLimitExceeded` error                        |
| S-06 | `TestSubmitAnswer_Idempotency`                  | Unit | Submit same user+quiz+question twice                | First succeeds, second returns `ErrAlreadyAnswered` |
| S-07 | `TestSubmitAnswer_DifferentQuestions`           | Unit | Same user answers q1 then q2                        | Both submissions succeed                            |
| S-08 | `TestGetLeaderboard_Ranking`                    | Unit | Alice answers fast, Bob answers slow, same question | Alice ranked #1 with higher score; Bob ranked #2    |


## 5. Memory Leaderboard Tests

**File**: `internal/scoring/memory_leaderboard_test.go`


| ID   | Test                                    | Type | Description                             | Expected Result                                      |
| ---- | --------------------------------------- | ---- | --------------------------------------- | ---------------------------------------------------- |
| L-01 | `TestMemoryLeaderboard_IncrAndRank`     | Unit | Increment scores: Alice 150+80, Bob 100 | Rankings: Alice (230), Bob (100) in descending order |
| L-02 | `TestMemoryLeaderboard_EmptyQuiz`       | Unit | Get rankings for nonexistent quiz       | Empty slice, no error                                |
| L-03 | `TestMemoryLeaderboard_MultipleQuizzes` | Unit | Scores in quiz1 and quiz2 independently | Each quiz has isolated leaderboard                   |


## 6. Quiz Service Tests

**File**: `internal/quiz/service_test.go`


| ID   | Test                                | Type | Description                                               | Expected Result                                                          |
| ---- | ----------------------------------- | ---- | --------------------------------------------------------- | ------------------------------------------------------------------------ |
| Q-01 | `TestQuizExists`                    | Unit | Check valid, invalid, and empty quiz IDs                  | `true` for "quiz-vocab-01", `false` for others                           |
| Q-02 | `TestStartQuiz_NotFound`            | Unit | Start a nonexistent quiz                                  | `errQuizNotFound`                                                        |
| Q-03 | `TestStartQuiz_NoSession`           | Unit | Start quiz that exists but has no session (nobody joined) | `errQuizNotFound`                                                        |
| Q-04 | `TestStartQuiz_AlreadyStarted`      | Unit | Start a quiz that is already active                       | `errQuizAlreadyStarted`                                                  |
| Q-05 | `TestGetSession`                    | Unit | Get session before anyone joins                           | `ok = false`                                                             |
| Q-06 | `TestGetSession_AfterStart`         | Unit | Get session after quiz starts                             | `ok = true`, status `active`                                             |
| Q-07 | `TestAdvanceQuestion`               | Unit | Advance from question 0 to 1 in a 2-question quiz         | `CurrentQuestion` becomes 1, status remains `active`                     |
| Q-08 | `TestAdvanceQuestion_CompletesQuiz` | Unit | Advance past last question                                | Status transitions to `completed`                                        |
| Q-09 | `TestAdvanceQuestion_NoSession`     | Unit | Advance on nonexistent quiz ID                            | No panic, no state change                                                |
| Q-10 | `TestAdvanceQuestion_NotActive`     | Unit | Advance on a completed quiz                               | Status remains `completed` (no-op)                                       |
| Q-11 | `TestCompleteQuiz`                  | Unit | Complete an active quiz directly                          | Status transitions to `completed`                                        |
| Q-12 | `TestStartQuestionTimer`            | Unit | Start 1s timer, wait 1.5s                                 | Question auto-advances to next                                           |
| Q-13 | `TestNewWithQuizRepo`               | Unit | Initialize service with mock DB repository                | Loads quizzes from repo, mock data not available                         |
| Q-14 | `TestNewWithQuizRepo_Error`         | Unit | Initialize with repo that returns error                   | Falls back to mock quiz data                                             |
| Q-15 | `TestCompleteQuiz_WithRepos`        | Unit | Complete quiz with session+leaderboard repos              | Status `completed`, persistence goroutines execute                       |
| Q-16 | `TestStartQuiz_WithSessionRepo`     | Unit | Start quiz with session repo configured                   | Status `active`, session status update goroutine executes                |
| Q-17 | `TestMockQuizzes`                   | Unit | Validate mock quiz content structure                      | 1 quiz, 5 questions, each with 4 options, valid correctID, timeLimit > 0 |


## 7. Hub (Connection Manager) Tests

**File**: `internal/hub/hub_test.go`


| ID   | Test                               | Type | Description                                        | Expected Result                                       |
| ---- | ---------------------------------- | ---- | -------------------------------------------------- | ----------------------------------------------------- |
| H-01 | `TestRoomSize`                     | Unit | Register/unregister clients, check room size       | Size increments on register, decrements on unregister |
| H-02 | `TestRoomSize_DifferentRooms`      | Unit | Clients in separate quiz rooms                     | Each room has independent count                       |
| H-03 | `TestSendToUser`                   | Unit | Send message to Alice in a room with Alice and Bob | Alice receives, Bob does not                          |
| H-04 | `TestSendToUser_NotFound`          | Unit | Send to user not in room                           | No clients receive, no panic                          |
| H-05 | `TestBroadcastToRoom`              | Unit | Broadcast to room with two clients                 | Both clients receive the message                      |
| H-06 | `TestRegisterUnregister`           | Unit | Register then unregister last client in room       | Room map entry cleaned up                             |
| H-07 | `TestUnregister_NonExistent`       | Unit | Unregister client that was never registered        | No panic                                              |
| H-08 | `TestHandleMessage_Routing/join`   | Unit | Route `join` message to handler                    | `HandleJoin` called                                   |
| H-09 | `TestHandleMessage_Routing/rejoin` | Unit | Route `rejoin` message to handler                  | `HandleRejoin` called                                 |
| H-10 | `TestHandleMessage_Routing/answer` | Unit | Route `answer` message to handler                  | `HandleAnswer` called                                 |
| H-11 | `TestHandleMessage_NoHandler`      | Unit | Handle message when no handler is set              | Error sent to client with type `error`                |
| H-12 | `TestHandleMessage_InvalidPayload` | Unit | Handle message with malformed JSON payload         | Error sent, handler not called                        |
| H-13 | `TestSendBufferFull`               | Unit | Send when client buffer (size 1) is full           | First message queued, second dropped                  |


## 8. Middleware Tests

**File**: `pkg/middleware/middleware_test.go`


| ID   | Test                           | Type | Description                                       | Expected Result                                    |
| ---- | ------------------------------ | ---- | ------------------------------------------------- | -------------------------------------------------- |
| M-01 | `TestLogging`                  | Unit | Request passes through logging middleware         | Status code captured, request completes            |
| M-02 | `TestRecovery_NoPanic`         | Unit | Normal request through recovery middleware        | 200 OK passed through                              |
| M-03 | `TestRecovery_WithPanic`       | Unit | Handler panics                                    | 500 Internal Server Error, panic caught            |
| M-04 | `TestCORS_Preflight`           | Unit | `OPTIONS` request                                 | 200 OK with CORS headers, inner handler NOT called |
| M-05 | `TestCORS_RegularRequest`      | Unit | `GET` request through CORS middleware             | CORS headers set, inner handler called             |
| M-06 | `TestStatusWriter_WriteHeader` | Unit | Write custom status code via statusWriter wrapper | Status captured and forwarded                      |


## 9. Config Tests

**File**: `internal/config/config_test.go`


| ID   | Test                     | Type | Description                       | Expected Result                                                   |
| ---- | ------------------------ | ---- | --------------------------------- | ----------------------------------------------------------------- |
| C-01 | `TestLoad_Defaults`      | Unit | Load config with no env vars set  | ServerAddr=`:8080`, RedisAddr=`localhost:6379`, DatabaseURL=empty |
| C-02 | `TestLoad_CustomEnvVars` | Unit | Load config with all env vars set | Values match env vars exactly                                     |


---

## Coverage Summary


| Package            | Coverage  | Notes                                                                              |
| ------------------ | --------- | ---------------------------------------------------------------------------------- |
| `internal/handler` | 92.3%     | Health endpoint Redis/PG ping paths untested (no real connections)                 |
| `internal/hub`     | 86.5%     | `SendEnvelope` marshal error paths, `WritePump` ping loop                          |
| `internal/quiz`    | 82.4%     | DB-persistence branches in `HandleJoin`/`HandleAnswer` partially covered via mocks |
| `internal/scoring` | 91.7%     | `redis_leaderboard.go` excluded (requires Redis)                                   |
| `internal/config`  | 100%      |                                                                                    |
| `pkg/middleware`   | 92.3%     | `Hijack` requires real hijackable connection                                       |
| `internal/metrics` | 100%      | `init()` runs automatically                                                        |
| **Total**          | **82.7%** | Excluding `repository/postgres` and `scoring/redis_leaderboard`                    |


## Not Tested (Require External Services)


| Package                                 | Reason                         |
| --------------------------------------- | ------------------------------ |
| `internal/repository/postgres.go`       | Requires PostgreSQL connection |
| `internal/scoring/redis_leaderboard.go` | Requires Redis connection      |
| `cmd/server/main.go`                    | Application entry point        |


