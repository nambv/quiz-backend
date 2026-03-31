# Real-Time Vocabulary Quiz — EM Workflow Guide

## Challenge Summary

Build a real-time quiz feature for an English learning app. Users join quiz sessions, answer questions in real-time, and compete on a live leaderboard. The submission is evaluated on system design, implementation quality, AI collaboration, and strategic thinking.

**Your role lens**: Engineering Manager / Squad Lead — architecture & strategy, distributed system design, resilience frameworks, observability, and maintainability over raw code execution.

---

## Phase 1: System Design Document (~40% of effort)

This is your primary differentiator as an EM. Invest the most time here.

### 1.1 Architecture Diagram

**Goal**: Create a clear, professional architecture diagram showing all component interactions.

**What to include**:

- Client layer (mobile/web) connecting through a Load Balancer
- WebSocket Gateway (Go) handling persistent connections and room management
- Quiz Service managing session lifecycle, question distribution, and timer coordination
- Scoring Service validating answers, computing scores, and updating rankings
- Redis layer: Sorted Sets for leaderboard, Pub/Sub for real-time broadcasting, Streams for event decoupling
- PostgreSQL for persistent storage (quiz content, user profiles, session history)
- Monitoring stack (Prometheus + Grafana) observing all services

**EM signal**: Include a CDN/Load Balancer at the top and a monitoring layer at the bottom. This immediately signals production-readiness thinking, not just prototype-level work.

**Tools**: Use draw.io, Excalidraw, or Mermaid. Export as PNG for the design doc and keep the editable source.

**Deliverable**: `docs/architecture.png` and editable source file.

### 1.2 Component Descriptions

For each component, write three things:

1. **What it does** (1–2 sentences)
2. **Why it exists as a separate concern** (separation of responsibility, independent scaling, team ownership)
3. **Key trade-offs you considered** (what you chose, what you rejected, and why)

**Components to describe**:

| Component | Role | EM-level justification |
|-----------|------|----------------------|
| WebSocket Gateway | Manages persistent client connections, room membership, message routing | Stateless design enables horizontal scaling; sticky sessions or Redis adapter for cross-node broadcasting |
| Quiz Service | Manages quiz session lifecycle: creation, question sequencing, timer, completion | Separated from gateway to allow independent scaling of connection handling vs. business logic |
| Scoring Service | Validates answers against correct answers, computes time-weighted scores, updates leaderboard | Isolated to enforce single responsibility; scoring logic changes independently of connection or quiz management |
| Redis (Sorted Sets) | Maintains real-time leaderboard rankings with O(log N) insert and O(log N + M) range queries | Chosen over PostgreSQL materialized views for latency (sub-ms vs. tens of ms); trade-off is durability (mitigated by async persistence) |
| Redis (Pub/Sub) | Broadcasts leaderboard updates and quiz events to all connected clients in a room | Enables cross-instance communication when horizontally scaled; lightweight compared to Kafka for this use case |
| PostgreSQL | Stores quiz content, user profiles, historical session data | Source of truth for durable data; Redis serves as hot cache; eventual consistency acceptable for leaderboard history |
| Load Balancer | Distributes WebSocket connections across gateway instances | Layer 7 with WebSocket upgrade support; sticky sessions by quiz room ID for connection affinity |

### 1.3 Data Flow Narrative

Write this as a sequential story covering the full lifecycle:

**User joins a quiz**:
1. Client sends HTTP request to join quiz via `POST /api/quiz/{quizId}/join` with user credentials
2. Server validates quiz ID exists and is in "waiting" or "active" state
3. Server returns WebSocket upgrade URL with session token
4. Client establishes WebSocket connection to gateway
5. Gateway registers connection in room map (in-memory, keyed by quiz ID)
6. Gateway broadcasts `user_joined` event to all participants in the room
7. Client receives current quiz state: participant list, current question (if active), and leaderboard snapshot

**User submits an answer**:
1. Client sends `answer` message via WebSocket: `{ type: "answer", questionId: "q1", answerId: "a2", timestamp: 1234567890 }`
2. Gateway routes message to Scoring Service
3. Scoring Service validates: (a) question belongs to active quiz, (b) user hasn't already answered this question (idempotency), (c) answer submitted within time window
4. Scoring Service computes score: base points for correctness + time bonus (faster = more points)
5. Scoring Service updates Redis Sorted Set: `ZADD quiz:{quizId}:leaderboard {newScore} {userId}`
6. Scoring Service publishes `score_updated` event to Redis Pub/Sub channel `quiz:{quizId}`
7. Gateway (subscribed to channel) receives event, builds leaderboard snapshot via `ZREVRANGE`
8. Gateway broadcasts `leaderboard_update` to all WebSocket connections in the room

**Quiz ends**:
1. Quiz Service detects final question answered or timer expires
2. Quiz Service publishes `quiz_completed` event
3. Gateway broadcasts final leaderboard and `quiz_ended` to all participants
4. Session data persisted to PostgreSQL asynchronously
5. Redis keys for the quiz set to expire (TTL) for cleanup

### 1.4 Technology Justifications

| Decision | Choice | Alternatives Considered | Why This Choice |
|----------|--------|------------------------|-----------------|
| Language | Go | Node.js, Elixir | Goroutine-per-connection is natural for WebSocket; lower memory footprint per connection than Node.js; strong stdlib for HTTP/JSON; team familiarity |
| WebSocket library | gorilla/websocket | nhooyi.io/websocket, stdlib (Go 1.26 proposal) | Most mature, battle-tested; clear API for upgrade, read/write pumps; widely documented |
| Real-time leaderboard | Redis Sorted Sets | PostgreSQL with `ORDER BY`, in-memory Go map | O(log N) inserts, O(log N + M) range queries; built-in atomic operations; Pub/Sub in same Redis instance avoids additional infrastructure |
| Message broadcasting | Redis Pub/Sub | Kafka, NATS, RabbitMQ | Lightweight, zero additional infrastructure (reuses leaderboard Redis); sufficient for single-region deployment; Kafka considered overkill for this scale |
| Persistent storage | PostgreSQL | MongoDB, MySQL | Strong consistency for quiz content and user data; JSON column support for flexible question schemas; excellent Go driver ecosystem (pgx) |
| Configuration | Environment variables + YAML | Viper, etcd | Simple, twelve-factor compliant; sufficient for single-service deployment; would adopt etcd/Consul for multi-service production |
| Logging | log/slog (stdlib) | zerolog, zap | Stdlib since Go 1.21; structured JSON output; no external dependency; sufficient performance for this use case |
| Testing | stdlib testing + testify | gomock, ginkgo | Minimal dependencies; table-driven tests are idiomatic Go; testify adds just readable assertions |

### 1.5 AI Collaboration in Design (Required)

Document how you used AI during the design phase:

**Example entries**:

> **Task**: Architecture brainstorming — component separation
> **Tool**: Claude AI (claude.ai)
> **Interaction**: "I described the challenge requirements and asked Claude to help me evaluate whether scoring should be a separate service or embedded in the gateway. We discussed trade-offs around latency (extra hop) vs. maintainability (independent deployment) vs. complexity (service mesh). Claude suggested the scoring service approach with Redis as the communication layer."
> **Verification**: Validated by checking that the architecture avoids single points of failure, confirmed Redis Sorted Set operations are indeed O(log N) via official Redis documentation, and verified that the pub/sub pattern handles cross-instance broadcasting correctly.

> **Task**: Technology selection — leaderboard data structure
> **Tool**: Claude AI
> **Interaction**: "Asked Claude to compare Redis Sorted Sets vs. PostgreSQL materialized views vs. in-memory Go maps for real-time leaderboard. Claude provided latency characteristics, consistency trade-offs, and scaling implications for each."
> **Verification**: Cross-referenced Redis Sorted Set time complexity against redis.io documentation. Confirmed materialized view refresh latency characteristics against PostgreSQL docs. Decided Redis Sorted Sets based on sub-millisecond write + read latency.

---

## Phase 2: Implementation (~30% of effort)

### 2.1 Component Choice

Implement the **server-side component**: WebSocket gateway + scoring engine + leaderboard — a single Go binary that demonstrates all three real-time requirements.

Mock the database layer and quiz content (hardcoded questions). Redis can be real (via Docker) or mocked with an in-memory sorted map.

### 2.2 Implementation Checklist

Work through these in order:

**Step 1 — Project scaffold** (~15 min):
- [ ] Initialize Go module: `go mod init github.com/yourname/quiz-server`
- [ ] Create directory structure per `CLAUDE.md`
- [ ] Set up `docker-compose.yml` with Go server + Redis
- [ ] Create `Makefile` with run, test, build, lint targets

**Step 2 — Models and protocol** (~20 min):
- [ ] Define WebSocket message types in `internal/models/`
- [ ] Message envelope: `{ "type": "join|answer|leaderboard_update|question|error", "payload": {} }`
- [ ] Define quiz session, question, participant, and leaderboard entry structs
- [ ] Use `encoding/json` with typed constants for message routing

**Step 3 — Hub and room management** (~45 min):
- [ ] Implement connection hub: register/unregister clients, room-based grouping
- [ ] Each client runs two goroutines: readPump (client→server) and writePump (server→client)
- [ ] Room broadcast: send message to all connections in a quiz room
- [ ] Handle disconnection: clean up room membership, notify remaining participants
- [ ] Use `sync.RWMutex` for thread-safe room map access

**Step 4 — Quiz session logic** (~30 min):
- [ ] Create mock quiz data: 5–10 vocabulary questions with correct answers
- [ ] Implement quiz lifecycle: waiting → active → completed
- [ ] Question timer: use `time.AfterFunc` or ticker to advance questions
- [ ] On join: send current quiz state (participants, current question, leaderboard)

**Step 5 — Scoring engine** (~30 min):
- [ ] Validate answer: correct question, not already answered, within time window
- [ ] Score formula: `basePoints * (1 + timeBonus)` where `timeBonus = max(0, (timeLimit - elapsed) / timeLimit)`
- [ ] Idempotent submissions: track answered questions per user per session
- [ ] Update leaderboard via Redis `ZADD` (or in-memory sorted structure)

**Step 6 — Leaderboard** (~30 min):
- [ ] Redis Sorted Set operations: `ZADD` for score update, `ZREVRANGEWITHSCORES` for ranking
- [ ] Build leaderboard snapshot: rank, username, score
- [ ] Broadcast to room on every score change
- [ ] Fallback: in-memory sorted slice if Redis is unavailable

**Step 7 — HTTP endpoints** (~15 min):
- [ ] `GET /health` — health check (Redis connectivity, uptime)
- [ ] `POST /api/quiz/{quizId}/join` — join quiz, return WebSocket URL
- [ ] `GET /api/quiz/{quizId}/leaderboard` — REST fallback for leaderboard
- [ ] `GET /ws/quiz/{quizId}` — WebSocket upgrade endpoint

**Step 8 — Error handling and graceful shutdown** (~20 min):
- [ ] OS signal handling (SIGINT, SIGTERM) → drain connections → flush pending writes
- [ ] WebSocket error recovery: ping/pong heartbeat, read deadline, write deadline
- [ ] Structured error responses over WebSocket: `{ "type": "error", "payload": { "code": "...", "message": "..." } }`
- [ ] Recovery middleware for panics

**Step 9 — Testing** (~30 min):
- [ ] Unit tests for scoring logic: correct answer, wrong answer, time bonus calculation, duplicate submission
- [ ] Unit tests for leaderboard: ranking order, score update, tie-breaking
- [ ] Integration test: WebSocket connection lifecycle (connect → join → answer → receive leaderboard → disconnect)
- [ ] Table-driven tests for edge cases

**Step 10 — AI collaboration comments** (~15 min):
- [ ] Add `// AI-ASSISTED: [tool] - [task]` comments to AI-generated sections
- [ ] Document verification steps for each section
- [ ] Update `docs/AI_COLLABORATION.md` with implementation examples

### 2.3 AI Collaboration in Implementation (Required)

For every significant AI-assisted code section, add inline documentation:

```go
// AI-ASSISTED: Claude Code — generated initial WebSocket hub structure
// Prompt: "Implement a WebSocket connection hub with room-based broadcasting in Go"
// Verification:
//   1. Reviewed goroutine lifecycle — confirmed readPump/writePump pattern prevents concurrent writes
//   2. Tested race conditions with `go test -race ./internal/hub/...`
//   3. Added write deadline and ping/pong heartbeat (missing from initial generation)
//   4. Refactored room map to use sync.RWMutex instead of sync.Mutex for better read concurrency
```

Keep a running log in `docs/AI_COLLABORATION.md`.

---

## Phase 3: Build for the Future (~15% of effort)

This section differentiates your EM thinking. Include these discussions in `docs/SYSTEM_DESIGN.md`.

### 3.1 Scalability

**Current implementation**: Single server, single Redis instance. Handles hundreds of concurrent users comfortably.

**Scaling strategy for 10K–100K concurrent users**:

1. **Horizontal gateway scaling**: Deploy multiple gateway instances behind a load balancer. Use Redis Pub/Sub as the cross-instance communication layer — when Scoring Service publishes a leaderboard update, all gateway instances receive it and broadcast to their local connections.

2. **Connection affinity**: Configure load balancer with sticky sessions by quiz room ID (consistent hashing). This ensures all participants in a quiz connect to the same gateway instance, reducing cross-instance broadcasting overhead. Fallback to Redis Pub/Sub when affinity breaks (e.g., instance restart).

3. **Quiz room partitioning**: For very large deployments, partition quiz rooms across gateway clusters. Each cluster owns a subset of rooms. A routing service maps quiz IDs to clusters.

4. **Redis scaling**: Single Redis instance handles ~100K operations/sec. For higher throughput, use Redis Cluster with hash tags to keep quiz-related keys on the same shard: `{quiz:123}:leaderboard`, `{quiz:123}:session`.

5. **Database read replicas**: PostgreSQL read replicas for quiz content serving. Write path remains single-primary for consistency.

**Trade-offs documented**:

- Sticky sessions reduce broadcasting but create hotspots if a quiz goes viral on one instance
- Redis Pub/Sub is fire-and-forget (no persistence) — acceptable for leaderboard updates (eventually consistent) but not for answer submissions (handled via direct Redis writes)
- Consistent hashing adds routing complexity but eliminates broadcast storms

### 3.2 Reliability

**Implemented**:

- Graceful shutdown with connection draining
- Idempotent answer submissions (prevent double-scoring)
- WebSocket heartbeat (ping/pong) with configurable deadlines
- Structured error responses for all failure modes

**Production roadmap (discussed in design doc)**:

1. **Reconnection handling**: Client-side exponential backoff with jitter. On reconnect, server replays current quiz state (current question, leaderboard snapshot, user's answered questions) so the user resumes seamlessly.

2. **Circuit breaker for Redis**: If Redis is unreachable, fall back to in-memory leaderboard with degraded consistency. Use a circuit breaker (closed → open → half-open) with configurable thresholds.

3. **Answer submission durability**: Write answers to Redis Stream before processing. If Scoring Service crashes mid-processing, a consumer group replays unacknowledged messages on restart. This provides at-least-once delivery for score updates.

4. **Data consistency**: Redis is the hot path (real-time leaderboard). PostgreSQL is the cold path (historical records). Async sync from Redis to PostgreSQL via background worker. Reconciliation job runs post-quiz to verify Redis leaderboard matches PostgreSQL totals.

5. **Disaster recovery**: Redis persistence (RDB snapshots + AOF) for leaderboard durability. Quiz session state reconstructable from PostgreSQL + Redis Stream replay.

### 3.3 Observability

**Implemented**:

- Structured logging with `log/slog`: JSON format, correlation ID per quiz session, log levels
- Request logging middleware: method, path, status, duration
- Health check endpoint: `/health` with Redis connectivity status

**Production roadmap (discussed in design doc)**:

**SLIs (Service Level Indicators)**:

| SLI | Measurement | Target |
|-----|-------------|--------|
| Leaderboard update latency | Time from answer submission to broadcast received by all clients | p50 < 50ms, p99 < 200ms |
| WebSocket connection success rate | Successful upgrades / total upgrade attempts | > 99.5% |
| Answer processing accuracy | Correctly scored answers / total answers submitted | 100% (zero tolerance) |
| Quiz join latency | Time from join request to receiving quiz state | p99 < 500ms |

**SLOs (Service Level Objectives)**:

- 99.9% of leaderboard updates delivered within 200ms
- 99.5% WebSocket connection success rate over any 24-hour window
- Zero scoring errors (enforced by idempotency + validation)

**Prometheus metrics to expose**:

```
quiz_websocket_connections_active{quiz_id}        # Gauge
quiz_websocket_connections_total                   # Counter
quiz_messages_received_total{type}                 # Counter
quiz_messages_broadcast_total{type}                # Counter
quiz_answer_processing_duration_seconds            # Histogram
quiz_leaderboard_update_duration_seconds           # Histogram
quiz_scoring_errors_total{error_type}              # Counter
quiz_redis_operation_duration_seconds{operation}   # Histogram
```

**Grafana dashboards**:

1. **Operational dashboard**: Active connections, message throughput, error rates, Redis latency
2. **Quiz session dashboard**: Per-quiz participant count, answer rate, completion rate
3. **SLO dashboard**: Burn rate alerts, error budget remaining

**Alerting rules**:

- P1: Scoring error rate > 0 (immediate page)
- P2: Leaderboard latency p99 > 500ms for 5 minutes
- P3: WebSocket error rate > 1% for 15 minutes
- P4: Redis connection failures > 3 in 5 minutes

### 3.4 Maintainability

**Implemented**:

- Clean separation: handlers → services → repositories (interfaces)
- Dependency injection via constructors — no globals, no init()
- All external dependencies behind interfaces for mocking
- Consistent error handling patterns across all layers
- Table-driven tests for comprehensive coverage

**EM perspective on team scalability**:

- **Module ownership**: The project structure maps to team ownership boundaries. A "Connections" team owns `hub/`, a "Quiz Logic" team owns `quiz/` and `scoring/`, a "Platform" team owns `config/`, `middleware/`, and infrastructure.
- **API contracts**: WebSocket message types are defined in `models/` — this is the contract between frontend and backend teams. Changes require versioning discussion.
- **Onboarding**: New engineers can understand the codebase by reading `CLAUDE.md` (conventions) → `docs/SYSTEM_DESIGN.md` (architecture) → `internal/models/` (data flow) → any single service package.
- **Code review guidelines**: PRs touching `scoring/` require test coverage for all new score calculation paths. PRs touching `hub/` require race condition testing (`go test -race`).

---

## Phase 4: AI Collaboration Documentation (Woven throughout)

### 4.1 Documentation Structure

Create `docs/AI_COLLABORATION.md` with these sections:

1. **AI tools used**: List each tool (Claude AI, Claude Code CLI, etc.) and the phases where you used them
2. **Design phase examples**: 2–3 detailed examples of AI-assisted architectural decisions with verification
3. **Implementation phase examples**: 3–5 code sections with inline `AI-ASSISTED` comments, prompts used, and verification steps
4. **Challenges and limitations**: Honest account of where AI suggestions were wrong or needed significant refinement
5. **Quality assurance process**: Your systematic approach to verifying AI output — testing, documentation cross-referencing, edge case exploration

### 4.2 EM Framing

Frame AI as a **strategic partner**, not just a code generator:

- "Used Claude for architectural brainstorming — evaluating pub/sub patterns, discussing Redis vs. PostgreSQL trade-offs for leaderboard, stress-testing the data flow for edge cases"
- "Leveraged Claude Code for SDLC acceleration — generating test scaffolds, boilerplate reduction, documentation generation"
- "AI as a rubber duck for system design — described my architecture verbally and asked Claude to identify single points of failure, scaling bottlenecks, and missing error handling"

### 4.3 Verification Process (Critical for evaluation)

For each AI-assisted output, document:

1. **Initial output review**: Read through generated code/design, identify assumptions
2. **Testing**: Run generated code, write tests for edge cases, use `go test -race` for concurrency
3. **Documentation cross-reference**: Verify claims against official docs (Redis, Go stdlib, WebSocket RFC)
4. **Refinement**: What you changed and why — this shows engineering judgment
5. **Security review**: Check for injection points, authentication gaps, resource exhaustion vectors

---

## Phase 5: Video Submission (~5–10 min)

### 5.1 Video Structure

| Segment | Duration | Content |
|---------|----------|---------|
| Introduction | 1 min | Your background as EM/Squad Lead, 10+ years mobile → backend, current role at Hello Health Group |
| Assignment overview | 1 min | Your understanding of the challenge, why you approached it with an EM lens |
| Architecture walkthrough | 2 min | Walk through the architecture diagram explaining the "why" behind each component and trade-off |
| AI collaboration story | 2 min | Concrete examples: which tools, which tasks, verification process, challenges encountered |
| Live demo | 2–3 min | Start server, open multiple browser tabs/Postman, join quiz, submit answers, show real-time leaderboard updates |
| Conclusion | 1 min | Lessons learned, what you'd add with more time (multi-region, A/B testing, feature flags, rate limiting), business impact framing |

### 5.2 EM Presentation Tips

- **Lead with "why"**: Don't just show the architecture — explain why each component exists and what trade-offs you made
- **Acknowledge trade-offs**: "I chose X over Y because Z, but Y would be better for [scenario]" — this shows mature engineering judgment
- **Business impact framing**: "This architecture supports the business goal of real-time engagement. The leaderboard creates competitive motivation, which drives session duration and vocabulary retention."
- **Team scalability**: "This structure allows a team of 3–5 engineers to work independently on different modules without merge conflicts or deployment coupling"
- **What I'd do with more time**: Feature flags for quiz variants, A/B testing framework for scoring algorithms, rate limiting per user, WebSocket compression (permessage-deflate), analytics pipeline for learning insights

### 5.3 Demo Script

1. Terminal 1: `docker-compose up` (show Redis + Go server starting)
2. Terminal 2: Show project structure briefly (`tree -L 2`)
3. Browser tab 1: Connect as "Alice" via WebSocket client (or simple HTML page)
4. Browser tab 2: Connect as "Bob" to same quiz
5. Show both receiving the quiz question
6. Alice answers correctly → show leaderboard update on both tabs
7. Bob answers correctly but slower → show updated rankings with time bonus difference
8. Show server logs with structured logging and correlation IDs
9. Show test run: `go test -race -v ./...`

---

## Delivery Checklist

Before submission, verify:

- [x] `docs/SYSTEM_DESIGN.md` — complete with all Part 1 sections
- [x] `docs/AI_COLLABORATION.md` — detailed AI usage documentation
- [x] `docs/architecture.png` — clean architecture diagram
- [x] Working Go server with WebSocket, scoring, and leaderboard
- [x] `README.md` — clear setup and run instructions
- [x] Tests passing: `go test -race ./...`
- [x] Linter clean: `golangci-lint run`
- [x] Docker Compose working: `docker-compose up`
- [x] `CLAUDE.md` — project conventions for AI-assisted development
- [ ] Video recorded (5–10 min), covers all required sections
- [x] Code comments marking AI-assisted sections with verification notes
