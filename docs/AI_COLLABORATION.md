# AI Collaboration Documentation

## AI Tools Used

| Tool | Phase | Usage |
|------|-------|-------|
| Claude Code (CLI) | System Design | Architecture brainstorming, component trade-off evaluation, data flow design |
| Claude Code (CLI) | Implementation | Code generation with human review, test scaffolding, boilerplate reduction |
| Claude Code (CLI) | Build for Future | Prometheus metrics design, reconnection protocol, PostgreSQL repository layer |
| Claude Code (CLI) | Documentation | Design doc structure, README generation, this collaboration log |

## Design Phase Examples

### Example 1: Component Separation — Scoring as Separate Service

**Task**: Evaluate whether scoring should be embedded in the gateway or separated.

**Interaction**: Described the challenge requirements and asked Claude to evaluate trade-offs between embedding scoring in the gateway vs. isolating it. Discussed latency (extra function call vs. separate service hop), maintainability (scoring rule changes independently), and testability (unit testing scoring without WebSocket dependencies).

**Decision**: Kept scoring as a separate Go package (`internal/scoring/`) with a clean interface. Not a separate microservice (overkill for this scale), but structurally isolated for independent testing and future extraction.

**Verification**: Confirmed scoring service has zero dependency on WebSocket/hub packages. Unit tests run independently without any connection setup.

### Example 2: Leaderboard Data Structure Selection

**Task**: Choose between Redis Sorted Sets, PostgreSQL ORDER BY, and in-memory Go maps for real-time leaderboard.

**Interaction**: Asked Claude to compare latency characteristics, consistency guarantees, and scaling behavior. Claude provided O(log N) analysis for Sorted Sets vs. O(N log N) for sorting in application code.

**Verification**: Cross-referenced Redis Sorted Set time complexity against redis.io documentation. Confirmed ZADD with INCR flag provides atomic cumulative scoring. Implemented in-memory fallback using `slices.SortFunc` for resilience when Redis is unavailable.

### Example 3: WebSocket Message Protocol Design

**Task**: Design the message envelope format for client-server communication.

**Interaction**: Claude suggested typed envelope with `type` field for routing, `payload` as raw JSON for flexibility, and `timestamp` for ordering. Evaluated alternatives: protobuf (too complex for demo), separate endpoints per message type (doesn't fit WebSocket model).

**Verification**: Validated that `json.RawMessage` for payload allows lazy parsing — we only unmarshal the payload after determining the message type, avoiding wasted deserialization.

## Implementation Phase Examples

### 1. WebSocket Hub — Connection Management

**File**: `internal/hub/hub.go`, `internal/hub/client.go`

```go
// AI-ASSISTED: Claude Code — WebSocket hub with room-based broadcasting
```

**Prompt**: "Implement a WebSocket connection hub with room-based grouping, readPump/writePump per client, and thread-safe room management"

**Verification**:
1. Reviewed goroutine lifecycle — confirmed readPump/writePump pattern prevents concurrent writes to WebSocket connection
2. Tested with `go test -race ./internal/handler/...` — no race conditions detected
3. Added `sync.RWMutex` for room map — reads (broadcasts) far outnumber writes (join/leave)
4. Added send buffer overflow protection — drops messages for slow clients instead of blocking

### 2. Scoring Engine — Time-Weighted Formula

**File**: `internal/scoring/service.go`

```go
// AI-ASSISTED: Claude Code — score formula: basePoints × (1 + timeBonus)
```

**Prompt**: "Implement answer scoring with time bonus, idempotency checking, and leaderboard integration"

**Verification**:
1. Table-driven tests verify: correct fast (150-200 pts), correct slow (100-115 pts), correct at limit (100 pts), wrong (0 pts), timeout (error)
2. Idempotency test confirms duplicate submissions return `ErrAlreadyAnswered`
3. Cross-user isolation test confirms different users can answer the same question
4. Leaderboard ranking test confirms faster answers rank higher

### 3. Redis Leaderboard — Sorted Set Operations

**File**: `internal/scoring/redis_leaderboard.go`

```go
// AI-ASSISTED: Claude Code — Redis Sorted Set operations for O(log N) ranking
```

**Prompt**: "Implement leaderboard using Redis ZADD with INCR for cumulative scoring and ZREVRANGEWITHSCORES for ranking"

**Verification**:
1. Verified `ZIncrBy` provides atomic increment (no read-modify-write race)
2. Verified `ZRevRangeWithScores` returns descending order
3. Added context timeouts (2s) on all Redis operations to prevent hanging
4. In-memory fallback (`MemoryLeaderboard`) tested independently with same interface

### 4. Integration Test — Full WebSocket Lifecycle

**File**: `internal/handler/handler_test.go`

```go
// AI-ASSISTED: Claude Code — WebSocket integration test for full quiz lifecycle
```

**Prompt**: "Write integration test: connect two clients, join quiz, start, submit answer, verify score result and leaderboard broadcast"

**Verification**:
1. Test runs with `-race` flag — no race conditions
2. Uses `httptest.NewServer` for real HTTP server with WebSocket support
3. Helper `readUntilType` handles non-deterministic message ordering from concurrent broadcasts
4. Verifies end-to-end: join → quiz_state → start → question → answer → score_result → leaderboard_update

### 5. HTTP Routing — Go 1.22+ Enhanced ServeMux

**File**: `internal/handler/handler.go`

```go
// AI-ASSISTED: Claude Code — using Go 1.22+ enhanced ServeMux with method+path patterns
```

**Prompt**: "Register HTTP routes using Go 1.22+ method-aware patterns with path parameters"

**Verification**:
1. Confirmed `r.PathValue("quizId")` works correctly in handler tests
2. Method routing tested — POST endpoints reject GET requests automatically
3. No external router dependency needed (no gorilla/mux or chi)

### 6. PostgreSQL Repository Layer

**File**: `internal/repository/postgres.go`

```go
// AI-ASSISTED: Claude Code — PostgreSQL repository layer with pgx
```

**Prompt**: "Create a repository layer with pgx for loading quizzes from PostgreSQL, persisting answers, and saving leaderboard snapshots"

**Verification**:
1. Defined interfaces at consumer side (`repository.go`) — PostgreSQL is one implementation, mock data is the fallback
2. Used `pgxpool` for connection pooling — validated pool.Ping() before marking DB as available
3. `ON CONFLICT DO NOTHING` on answer inserts for DB-level idempotency (belt-and-suspenders with Redis NX)
4. Async persistence — answer saves and leaderboard snapshots don't block the real-time path
5. Tested with Docker Compose: verified seed data loads, sessions created, answers persisted

### 7. Prometheus Metrics — Observability Layer

**File**: `internal/metrics/metrics.go`

```go
// AI-ASSISTED: Claude Code — Prometheus metrics for quiz observability
```

**Prompt**: "Add Prometheus metrics matching the SLIs defined in SYSTEM_DESIGN.md: connection counts, answer latency, leaderboard update duration, scoring errors"

**Verification**:
1. Metrics match SLIs: `quiz_answer_processing_duration_seconds` histogram with buckets aligned to 200ms SLO
2. Instrumented hub (connection gauge), quiz service (answer/leaderboard timing), and message routing (counters by type)
3. `/metrics` endpoint registered via `promhttp.Handler()` — verified with `curl localhost:8080/metrics`
4. No metric in hot path blocks — all `Observe`/`Inc` calls are non-blocking

### 8. Reconnection (Rejoin) Support

**File**: `internal/quiz/service.go` — `HandleRejoin()`

**Prompt**: "Add a rejoin message type that validates the user was previously in the quiz and replays current state"

**Verification**:
1. Validates user exists in participants map — rejects unknown users with `NOT_A_PARTICIPANT` error
2. Re-registers connection in hub room without creating a duplicate participant entry
3. Replays full quiz state: current question, participant list, leaderboard snapshot
4. Idempotency preserved — previously answered questions remain tracked, no double-scoring possible

### 9. Logger Dependency Injection

**Prompt**: "Inject *slog.Logger into all services via constructors instead of using global slog calls"

**Verification**:
1. Each service adds `component` attribute: `log.With("component", "hub")` — enables filtering by service in log aggregators
2. Zero global `slog` calls in business logic — all logging goes through injected logger
3. Tests pass a real logger (not nil) to avoid nil pointer panics
4. Middleware also receives logger — consistent structured logging across HTTP and WebSocket paths

## EM Framing: AI as Strategic Partner

### Architectural Brainstorming
Used Claude Code for evaluating pub/sub patterns, discussing Redis vs. PostgreSQL trade-offs for leaderboard storage, and stress-testing the data flow for edge cases (reconnection, idempotency, timer expiration). Claude identified that fire-and-forget Pub/Sub is acceptable for leaderboard updates because they're idempotent — a missed update self-corrects on the next score change.

### SDLC Acceleration
Leveraged Claude Code for generating test scaffolds (table-driven tests with edge cases), boilerplate reduction (WebSocket pump patterns, middleware chains), and documentation generation (README, design doc structure). This freed time for the higher-value EM work: system design decisions, trade-off documentation, and observability strategy.

### Rubber Duck for System Design
Described the architecture verbally and asked Claude to identify single points of failure, scaling bottlenecks, and missing error handling. This surfaced:
- The need for `http.Hijacker` support in middleware (WebSocket upgrade failed through wrapped ResponseWriter)
- Non-deterministic message ordering in integration tests (solved with `readUntilType` helper)
- The importance of async persistence for answers and leaderboard snapshots (blocking the real-time path for DB writes would violate the 200ms SLO)

### What AI Got Wrong (and How I Fixed It)
1. **Middleware wrapping broke WebSocket** — initial middleware `statusWriter` didn't implement `http.Hijacker`, causing WebSocket upgrades to fail with 500. Required adding `Hijack()` method.
2. **Test message ordering assumptions** — initial integration test expected deterministic message order, but concurrent broadcasts created non-deterministic interleaving. Required `readUntilType` helper.
3. **Docker Go version mismatch** — Dockerfile used `golang:1.24-alpine` but `go.mod` required 1.25+. Required updating to match local Go version.

## Challenges and Limitations

### 1. Message Ordering in Tests
Initial integration test assumed deterministic message ordering, but WebSocket broadcasts from room joins created interleaving. Fixed by implementing `readUntilType` helper that skips irrelevant messages.

### 2. Leaderboard Entry Absence for Zero-Score Users
Initially expected wrong-answer users to appear in the leaderboard. Realized that since `IncrScore` is only called for positive scores, users who answer incorrectly don't appear. This is actually the correct behavior for a competitive leaderboard.

### 3. Clock Skew in Time Bonus
The time bonus calculation depends on `time.Since(questionStart)`. In tests, very fast execution could produce near-maximum time bonuses. Used range assertions (`wantMinPoints`/`wantMaxPoints`) instead of exact values to account for timing variance.

## Quality Assurance Process

For every AI-generated code section:

1. **Read and understand** — Never blindly accepted generated code; read every line and understood the design decisions
2. **Test with race detector** — `go test -race ./...` on every change to catch concurrency bugs
3. **Edge case exploration** — Added tests for: duplicate submissions, time limit exceeded, empty leaderboard, multiple quizzes, wrong answers
4. **Documentation cross-reference** — Verified Redis command behavior against redis.io docs, Go stdlib behavior against pkg.go.dev
5. **Security review** — Checked for: WebSocket message size limits (4KB), input validation on all payloads, idempotency enforcement, CORS configuration
6. **Refinement** — Multiple iterations on hub/client goroutine lifecycle, test message ordering, and error response consistency
