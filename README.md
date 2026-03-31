# Real-Time Vocabulary Quiz Server

A real-time quiz feature for an English learning application. Players join quiz sessions via WebSocket, answer vocabulary questions, and compete on a live leaderboard that updates instantly.

## Architecture

- **WebSocket Gateway** — persistent connections, room-based broadcasting
- **Quiz Service** — session lifecycle, question sequencing, timer coordination
- **Scoring Service** — answer validation, time-weighted scoring, idempotency
- **Leaderboard** — Redis Sorted Sets for O(log N) ranking (in-memory fallback)

See [SYSTEM_DESIGN.md](docs/SYSTEM_DESIGN.md) for full system design documentation.

## Tech Stack

- **Go 1.25+** — goroutine-per-connection WebSocket handling
- **Redis** — Sorted Sets for leaderboard, Pub/Sub for broadcasting
- **PostgreSQL** — quiz content, session history, answer persistence
- **gorilla/websocket** — WebSocket library
- **Prometheus** — metrics and observability
- **Docker Compose** — Go server + Redis + PostgreSQL

## Quick Start

### With Docker (recommended)

```bash
docker-compose up --build
```

### Without Docker

Requires Go 1.25+ and optionally Redis on localhost:6379 and PostgreSQL.

```bash
# Run the server (falls back gracefully if Redis/PostgreSQL unavailable)
go run cmd/server/main.go
```

**Environment variables:**

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_ADDR` | `:8080` | Server listen address |
| `REDIS_ADDR` | `localhost:6379` | Redis address |
| `DATABASE_URL` | *(empty)* | PostgreSQL connection string |

The server starts on `http://localhost:8080`. If Redis is unavailable, it falls back to an in-memory leaderboard. If PostgreSQL is unavailable, it uses mock quiz data.

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Health check (Redis + PostgreSQL status) |
| GET | `/metrics` | Prometheus metrics |
| GET | `/api/quizzes` | List all available quizzes |
| POST | `/api/quiz/{quizId}/join` | Join a quiz, get WebSocket URL |
| POST | `/api/quiz/{quizId}/start` | Start the quiz (transition to active) |
| POST | `/api/quiz/{quizId}/next` | Advance to next question immediately |
| POST | `/api/quiz/{quizId}/reset` | Reset quiz (clear participants, session, scores) |
| GET | `/api/quiz/{quizId}/leaderboard` | REST leaderboard fallback |
| GET | `/ws/quiz/{quizId}` | WebSocket upgrade endpoint |

## WebSocket Protocol

Connect to `/ws/quiz/{quizId}`, then send JSON messages:

```json
// Join
{"type": "join", "payload": {"userId": "u1", "username": "Alice"}, "timestamp": 1711800000000}

// Answer
{"type": "answer", "payload": {"questionId": "q1", "answerId": "a2"}, "timestamp": 1711800001000}
```

Server sends: `quiz_state`, `question`, `score_result`, `leaderboard_update`, `user_joined`, `user_left`, `quiz_ended`, `error`.

Reconnection is supported — send a `rejoin` message with `userId` to resume a session.

## Demo Quiz

When running with Docker (PostgreSQL), the quiz ID is a UUID loaded from the database. Without Docker, a mock quiz `quiz-vocab-01` is available.

**Demo flow:**
1. `GET /api/quizzes` — discover available quiz IDs
2. `POST /api/quiz/{quizId}/join` — get WebSocket URL
3. Connect via WebSocket → send `join` message
4. `POST /api/quiz/{quizId}/start` — start the quiz
5. Answer questions via WebSocket → receive scores and leaderboard updates
6. `POST /api/quiz/{quizId}/reset` — reset to play again

## Testing

**57 test cases** across 7 packages — **82.7% coverage** (excluding external-service-dependent code).

```bash
# Run all tests with race detector
go test -race ./...

# Run with verbose output
go test -race -v ./...

# Run with coverage report
go test -race -coverpkg=./internal/handler/,./internal/hub/,./internal/quiz/,./internal/scoring/,./internal/config/,./internal/metrics/,./pkg/middleware/ -coverprofile=coverage.out ./...
go tool cover -func=coverage.out

# Lint
golangci-lint run

# Build
go build -o bin/quiz-server cmd/server/main.go
```

| Package | Tests | Coverage | What's tested |
|---------|-------|----------|---------------|
| `internal/handler` | 15 | 92% | REST endpoints, full WebSocket lifecycle, error paths, rejoin |
| `internal/hub` | 13 | 87% | Room management, broadcast, routing, send-to-user, buffer overflow |
| `internal/quiz` | 17 | 82% | State machine, timer advancement, quiz completion, mock repos |
| `internal/scoring` | 11 | 92% | Time-weighted scoring, idempotency, ranking, leaderboard ops |
| `internal/config` | 2 | 100% | Defaults and env var overrides |
| `pkg/middleware` | 6 | 92% | Logging, panic recovery, CORS preflight |

See [docs/TEST_CASES.md](docs/TEST_CASES.md) for the full test case definitions.

## Project Structure

```
├── cmd/server/main.go          # Entry point, dependency wiring, graceful shutdown
├── internal/
│   ├── config/                  # Environment-based configuration
│   ├── handler/                 # HTTP + WebSocket handlers, integration tests
│   ├── hub/                     # WebSocket connection manager, room broadcasting
│   ├── metrics/                 # Prometheus metrics (SLI-aligned)
│   ├── models/                  # Shared types, message protocol
│   ├── quiz/                    # Quiz session logic, mock data
│   ├── repository/              # PostgreSQL repository layer (pgx)
│   └── scoring/                 # Score calculation, leaderboard (Redis + in-memory)
├── pkg/middleware/              # Logging, recovery, CORS
├── db/init.sql                  # PostgreSQL schema and seed data
├── docs/                        # System design, AI collaboration log
├── docker-compose.yml
├── Dockerfile
└── Makefile
```

## AI Collaboration

See [docs/AI_COLLABORATION.md](docs/AI_COLLABORATION.md) for documented AI-assisted development process.
