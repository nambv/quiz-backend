# Real-Time Vocabulary Quiz — Engineering Manager Challenge

## Project Context

This is a coding challenge submission for an **Engineering Manager / Squad Lead** role at Elsa. The challenge requires building a real-time quiz feature for an English learning application. The submission is evaluated with an EM lens: architecture & strategy, distributed system design, resilience frameworks, observability (SLIs/SLOs), and maintainability — not just code execution.

## Architecture Overview

### Core Components
- **WebSocket Gateway**: Handles client connections, room management, and real-time message broadcasting
- **Quiz Service**: Manages quiz sessions, question lifecycle, and session state
- **Scoring Service**: Validates answers, calculates scores, updates leaderboard
- **Leaderboard**: Redis Sorted Sets for O(log N) ranking with pub/sub for real-time broadcast
- **Persistent Store**: PostgreSQL for quiz content, user data, session history (mocked in implementation)
- **Message Broker**: Redis Streams for decoupling services (discussed in design doc, mocked in implementation)

### Tech Stack
- **Language**: Go (chosen for goroutine-per-connection concurrency model, low-latency WebSocket handling)
- **WebSocket**: `gorilla/websocket` or `nhooyi.io/websocket`
- **Cache/Leaderboard**: Redis (`go-redis/v9`) — Sorted Sets for ranking, Pub/Sub for broadcasting
- **Logging**: `log/slog` (stdlib structured logging)
- **Testing**: stdlib `testing` + `testify` for assertions
- **Container**: Docker Compose (Go server + Redis)

## Project Structure

```
quiz-server/
├── cmd/server/main.go          # Entry point, server bootstrap
├── internal/
│   ├── handler/                 # WebSocket upgrade, HTTP endpoints
│   ├── hub/                     # Connection manager, room broadcasting
│   ├── quiz/                    # Quiz session logic, question management
│   ├── scoring/                 # Score calculation, leaderboard
│   ├── models/                  # Shared types, DTOs, events
│   └── config/                  # Configuration loading
├── pkg/
│   └── middleware/              # Logging, recovery, CORS
├── docs/
│   ├── SYSTEM_DESIGN.md         # Part 1: Full system design document
│   ├── AI_COLLABORATION.md      # AI collaboration documentation
│   └── architecture.png         # Architecture diagram
├── docker-compose.yml
├── Makefile
├── go.mod
├── go.sum
└── README.md                    # Setup instructions, how to run
```

## Code Conventions

### Go Style
- Follow standard Go conventions: `gofmt`, `go vet`, `golangci-lint`
- Use `context.Context` for timeout/cancellation propagation across all service boundaries
- Prefer interfaces for testability — define interfaces where they are consumed, not where they are implemented
- Use `sync.RWMutex` for thread-safe concurrent access to shared state (e.g., room maps, leaderboard cache)
- Errors: wrap with `fmt.Errorf("operation: %w", err)` for context; use sentinel errors for expected failure modes
- Naming: use Go idioms — `New*` constructors, receiver names as short abbreviations, unexported by default

### Architecture Patterns
- Clean separation: handlers → services → repositories
- Dependency injection via constructors, no globals
- All external dependencies (Redis, DB) behind interfaces for mocking
- Graceful shutdown: listen for OS signals, drain WebSocket connections, flush pending writes

### WebSocket Protocol
- JSON message format with `type` field for routing: `join`, `answer`, `leaderboard_update`, `question`, `error`
- Server broadcasts leaderboard updates to all room participants on every score change
- Client reconnection: support session resumption via quiz ID + user ID

### Testing
- Unit tests for scoring logic, leaderboard ranking, answer validation
- Integration tests for WebSocket connection lifecycle (join → answer → leaderboard update)
- Table-driven tests preferred
- Target: meaningful coverage on core business logic (scoring, leaderboard)

## AI Collaboration Guidelines

This challenge **requires** documented AI collaboration. When AI assists with code:

1. **Mark AI-assisted sections** with comments: `// AI-ASSISTED: [tool] - [task description]`
2. **Document verification steps**: what you tested, edge cases checked, refinements made
3. **Frame AI usage as strategic partnership** (EM lens):
   - Architectural brainstorming and trade-off evaluation
   - SDLC optimization (test scaffolds, API contracts, documentation)
   - Code generation with human review and refinement
4. Keep a running log in `docs/AI_COLLABORATION.md`

## Build for the Future (EM Focus Areas)

### Scalability
- Discuss horizontal scaling: sticky sessions or Redis adapter for cross-instance pub/sub
- Quiz room partitioning across nodes (consistent hashing)
- Stateless server design — all session state in Redis

### Reliability
- Reconnection handling with exponential backoff
- Idempotent answer submissions (prevent double-scoring)
- Circuit breaker pattern for downstream service failures
- Data consistency between Redis (hot) and PostgreSQL (cold)

### Observability
- **SLIs**: leaderboard update latency, WebSocket connection success rate, scoring accuracy
- **SLOs**: p99 leaderboard update < 200ms, connection success > 99.5%
- Prometheus metrics: connection count, message throughput, scoring latency, error rates
- Structured logging with correlation IDs per quiz session
- Grafana dashboard design (discussed in design doc)

### Maintainability
- Modular structure enabling team-level ownership
- Type-safe message handling
- Clear separation of concerns for independent deployment paths

## Commands

```bash
# Run server
go run cmd/server/main.go

# Run with Docker
docker-compose up

# Run tests
go test ./...

# Run linter
golangci-lint run

# Build
go build -o bin/quiz-server cmd/server/main.go
```
