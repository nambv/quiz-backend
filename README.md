# Real-Time Vocabulary Quiz Server

A real-time quiz feature for an English learning application. Players join quiz sessions via WebSocket, answer vocabulary questions, and compete on a live leaderboard that updates instantly.

## Architecture

- **WebSocket Gateway** — persistent connections, room-based broadcasting
- **Quiz Service** — session lifecycle, question sequencing, timer coordination
- **Scoring Service** — answer validation, time-weighted scoring, idempotency
- **Leaderboard** — Redis Sorted Sets for O(log N) ranking (in-memory fallback)

See [SYSTEM_DESIGN.md](docs/SYSTEM_DESIGN.md) for full system design documentation.

## Tech Stack

- **Go** — goroutine-per-connection WebSocket handling
- **Redis** — Sorted Sets for leaderboard, Pub/Sub for broadcasting
- **gorilla/websocket** — WebSocket library
- **Docker Compose** — Go server + Redis

## Quick Start

### With Docker (recommended)

```bash
docker-compose up --build
```

### Without Docker

Requires Go 1.24+ and Redis running on localhost:6379.

```bash
# Start Redis
redis-server &

# Run the server
go run cmd/server/main.go
```

The server starts on `http://localhost:8080`. If Redis is unavailable, the server falls back to an in-memory leaderboard automatically.

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Health check with Redis status |
| POST | `/api/quiz/{quizId}/join` | Join a quiz, get WebSocket URL |
| POST | `/api/quiz/{quizId}/start` | Start the quiz (transition to active) |
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

## Demo Quiz

A pre-loaded quiz `quiz-vocab-01` with 5 vocabulary questions is available.

**Demo flow:**
1. `POST /api/quiz/quiz-vocab-01/join` — get WebSocket URL
2. Connect via WebSocket → send `join` message
3. `POST /api/quiz/quiz-vocab-01/start` — start the quiz
4. Answer questions via WebSocket → receive scores and leaderboard updates

## Testing

```bash
# Run all tests with race detector
go test -race -v ./...

# Build
go build -o bin/quiz-server cmd/server/main.go
```

## Project Structure

```
├── cmd/server/main.go          # Entry point, dependency wiring, graceful shutdown
├── internal/
│   ├── config/                  # Environment-based configuration
│   ├── handler/                 # HTTP + WebSocket handlers
│   ├── hub/                     # WebSocket connection manager, room broadcasting
│   ├── models/                  # Shared types, message protocol
│   ├── quiz/                    # Quiz session logic, mock data
│   └── scoring/                 # Score calculation, leaderboard (Redis + in-memory)
├── pkg/middleware/              # Logging, recovery, CORS
├── docs/                        # Design docs, AI collaboration log
├── docker-compose.yml
├── Dockerfile
└── Makefile
```

## AI Collaboration

See [docs/AI_COLLABORATION.md](docs/AI_COLLABORATION.md) for documented AI-assisted development process.
