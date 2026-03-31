# Real-Time Vocabulary Quiz — System Design Document

## 1. Overview

This document describes the system design for a real-time quiz feature in an English learning application. The feature allows multiple users to join quiz sessions simultaneously, answer vocabulary questions in real-time, and compete on a live leaderboard that updates instantly as scores change.

The design prioritizes low-latency real-time communication, horizontal scalability, operational observability, and maintainable architecture suitable for team-scale development.

---

## 2. Architecture

### 2.1 Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                            CLIENTS                                      │
│         ┌──────────┐    ┌──────────┐    ┌──────────┐                   │
│         │ Mobile   │    │ Web App  │    │ Mobile   │                   │
│         │ (iOS)    │    │ (React)  │    │ (Android)│                   │
│         └────┬─────┘    └────┬─────┘    └────┬─────┘                   │
└──────────────┼───────────────┼───────────────┼──────────────────────────┘
               │               │               │
               │          HTTPS + WSS          │
               └───────────────┼───────────────┘
                               │
                    ┌──────────▼──────────┐
                    │   Load Balancer     │
                    │   (Nginx / ALB)     │
                    │   L7 + WS Upgrade   │
                    │   Sticky Sessions   │
                    └──────────┬──────────┘
                               │
              ┌────────────────┼────────────────┐
              │                │                │
     ┌────────▼──────┐ ┌──────▼────────┐ ┌─────▼───────┐
     │  Gateway #1   │ │  Gateway #2   │ │  Gateway #N  │
     │  (Go)         │ │  (Go)         │ │  (Go)        │
     │               │ │               │ │              │
     │ ┌───────────┐ │ │ ┌───────────┐ │ │┌───────────┐│
     │ │ WebSocket │ │ │ │ WebSocket │ │ ││ WebSocket ││
     │ │ Hub       │ │ │ │ Hub       │ │ ││ Hub       ││
     │ └───────────┘ │ │ └───────────┘ │ │└───────────┘│
     │ ┌───────────┐ │ │ ┌───────────┐ │ │┌───────────┐│
     │ │ Quiz      │ │ │ │ Quiz      │ │ ││ Quiz      ││
     │ │ Service   │ │ │ │ Service   │ │ ││ Service   ││
     │ └───────────┘ │ │ └───────────┘ │ │└───────────┘│
     │ ┌───────────┐ │ │ ┌───────────┐ │ │┌───────────┐│
     │ │ Scoring   │ │ │ │ Scoring   │ │ ││ Scoring   ││
     │ │ Service   │ │ │ │ Service   │ │ ││ Service   ││
     │ └───────────┘ │ │ └───────────┘ │ │└───────────┘│
     └───────┬───────┘ └───────┬───────┘ └──────┬──────┘
             │                 │                 │
             └─────────────────┼─────────────────┘
                               │
                    ┌──────────▼──────────┐
                    │      Redis          │
                    │                     │
                    │  ┌───────────────┐  │
                    │  │ Sorted Sets   │  │
                    │  │ (Leaderboard) │  │
                    │  └───────────────┘  │
                    │  ┌───────────────┐  │
                    │  │ Pub/Sub       │  │
                    │  │ (Broadcast)   │  │
                    │  └───────────────┘  │
                    │  ┌───────────────┐  │
                    │  │ Streams       │  │
                    │  │ (Event Log)   │  │
                    │  └───────────────┘  │
                    └──────────┬──────────┘
                               │
                    ┌──────────▼──────────┐
                    │    PostgreSQL       │
                    │                     │
                    │  Quiz Content       │
                    │  User Profiles      │
                    │  Session History    │
                    └─────────────────────┘

         ┌────────────────────────────────────────┐
         │          Observability Layer            │
         │                                        │
         │  Prometheus ──► Grafana ──► Alerting   │
         │  Structured Logs ──► Log Aggregator    │
         └────────────────────────────────────────┘
```

### 2.2 Component Descriptions

#### WebSocket Gateway (Go)

**Role**: Manages persistent WebSocket connections from clients. Handles connection lifecycle (upgrade, heartbeat, graceful close), room-based grouping by quiz ID, and bidirectional message routing between clients and internal services.

**Why separate**: The gateway is stateless regarding business logic — it only manages connections and message routing. This allows horizontal scaling by adding more gateway instances behind the load balancer. Each instance handles a subset of connections, and Redis Pub/Sub provides cross-instance communication.

**Key decisions**:
- Goroutine-per-connection model: each client gets a dedicated readPump and writePump goroutine, ensuring non-blocking I/O across all connections
- Room map protected by `sync.RWMutex` for concurrent read-heavy access patterns (reads during broadcasts far outnumber writes during join/leave)
- Ping/pong heartbeat with configurable intervals (default: 30s ping, 60s pong deadline) for connection health detection

#### Quiz Service

**Role**: Manages quiz session lifecycle — creation, state transitions (waiting → active → completed), question sequencing, and timer coordination. Serves quiz content to clients on join and advances questions based on time or completion.

**Why separate**: Quiz logic (which question to show, when to advance, session rules) changes independently from connection handling and scoring. A product team can modify quiz behavior without touching WebSocket infrastructure.

**Key decisions**:
- Quiz state machine: `waiting` (accepting joins) → `active` (questions in progress) → `completed` (final leaderboard)
- Question timer uses Go's `time.AfterFunc` for per-question deadlines, with cleanup on quiz completion to prevent goroutine leaks
- Mock data layer in implementation; interface-based repository pattern ready for PostgreSQL integration

#### Scoring Service

**Role**: Validates answer submissions, computes scores with time-weighted bonuses, enforces idempotency (one answer per user per question), and updates the leaderboard in Redis.

**Why separate**: Scoring rules are the most critical business logic — they determine fairness and user trust. Isolating scoring enables independent testing, auditing, and modification without risk to connection stability.

**Key decisions**:
- Score formula: `basePoints × (1 + timeBonus)` where `timeBonus = max(0, (timeLimit - elapsed) / timeLimit)`. This rewards both correctness and speed without making slow-but-correct answers worthless.
- Idempotency via Redis SET with NX flag: `SET quiz:{quizId}:user:{userId}:q:{questionId} 1 NX`. If the key already exists, the submission is rejected as duplicate.
- Atomic leaderboard update via Redis `ZADD` with `INCR` flag for cumulative scoring, or plain `ZADD` for replacement scoring (configurable).

#### Redis Layer

**Role**: Serves three distinct purposes via different Redis data structures.

**Sorted Sets (Leaderboard)**:
- Key pattern: `quiz:{quizId}:leaderboard`
- `ZADD` for score updates: O(log N) per insert
- `ZREVRANGEWITHSCORES 0 -1` for full leaderboard: O(log N + M) where M is participant count
- `ZREVRANK` for individual rank lookup: O(log N)
- Chosen over PostgreSQL because sub-millisecond latency is critical for real-time feel; leaderboard reads happen on every score update and must not block

**Pub/Sub (Cross-instance Broadcasting)**:
- Channel pattern: `quiz:{quizId}:events`
- When Scoring Service updates a leaderboard, it publishes an event to the quiz's channel
- All gateway instances subscribe to channels for their active quizzes
- Fire-and-forget semantics are acceptable here because leaderboard updates are idempotent — a missed update is corrected by the next one

**Streams (Event Log)** — production roadmap:
- Durable event log for answer submissions
- Consumer groups for at-least-once processing
- Enables replay on Scoring Service restart
- Not implemented in challenge scope but discussed for reliability

**Why Redis over alternatives**:
- Single infrastructure component serves leaderboard, broadcasting, and event logging
- Redis Sorted Sets are purpose-built for ranking — no custom implementation needed
- Pub/Sub is built-in and zero-configuration
- Trade-off: Redis is primarily in-memory, so durability requires AOF/RDB configuration. Acceptable because PostgreSQL is the durable source of truth.

#### PostgreSQL

**Role**: Persistent storage for quiz content (questions, correct answers, metadata), user profiles, and historical session data (final leaderboards, answer logs).

**Why PostgreSQL**:
- Strong consistency guarantees for quiz content (a question must not change mid-quiz)
- JSONB columns for flexible question schemas (multiple choice, fill-in-the-blank, matching)
- Excellent Go driver ecosystem (`pgx` for high-performance connections)
- Read replicas for horizontal read scaling of quiz content

**Interaction pattern**: PostgreSQL is the cold path. Quiz content is loaded into memory at quiz creation. Scores are written to PostgreSQL asynchronously after quiz completion. Redis is the hot path for all real-time operations.

#### Load Balancer

**Role**: Distributes incoming connections across gateway instances. Must support WebSocket upgrade (HTTP/1.1 → WebSocket handshake) and maintain connection affinity.

**Key decisions**:
- Layer 7 load balancing with WebSocket upgrade support
- Sticky sessions by quiz room ID (via consistent hashing or cookie-based affinity) to minimize cross-instance broadcasting
- Health check endpoint: `/health` on each gateway instance
- Connection draining on instance removal (graceful shutdown signal → stop accepting new connections → drain existing → terminate)

---

## 3. Data Flow

### 3.1 User Joins a Quiz

```
Client                    Load Balancer           Gateway              Redis              PostgreSQL
  │                            │                    │                   │                    │
  │  POST /quiz/{id}/join      │                    │                   │                    │
  │ ──────────────────────────►│                    │                   │                    │
  │                            │  Route to gateway  │                   │                    │
  │                            │ ──────────────────►│                   │                    │
  │                            │                    │  Validate quiz    │                    │
  │                            │                    │  exists & active  │                    │
  │                            │                    │ ──────────────────────────────────────►│
  │                            │                    │◄────────────────────────────────────── │
  │                            │                    │                   │                    │
  │  HTTP 200 + WS URL         │                    │                   │                    │
  │◄──────────────────────────────────────────────── │                   │                    │
  │                            │                    │                   │                    │
  │  WebSocket Upgrade         │                    │                   │                    │
  │ ──────────────────────────►│ ──────────────────►│                   │                    │
  │                            │                    │                   │                    │
  │                            │                    │  Register in room │                    │
  │                            │                    │  (in-memory map)  │                    │
  │                            │                    │                   │                    │
  │                            │                    │  Get leaderboard  │                    │
  │                            │                    │ ─────────────────►│                    │
  │                            │                    │◄───────────────── │                    │
  │                            │                    │                   │                    │
  │  quiz_state message        │                    │                   │                    │
  │  (participants, question,  │                    │                   │                    │
  │   leaderboard snapshot)    │                    │                   │                    │
  │◄──────────────────────────────────────────────── │                   │                    │
  │                            │                    │                   │                    │
  │                            │                    │  Broadcast        │                    │
  │                            │                    │  "user_joined"    │                    │
  │                            │                    │  to room          │                    │
  │                            │                    │                   │                    │
```

### 3.2 User Submits an Answer

```
Client                    Gateway              Scoring Service        Redis
  │                          │                       │                  │
  │  { type: "answer",       │                       │                  │
  │    questionId, answerId, │                       │                  │
  │    timestamp }           │                       │                  │
  │ ────────────────────────►│                       │                  │
  │                          │                       │                  │
  │                          │  Validate & score     │                  │
  │                          │ ─────────────────────►│                  │
  │                          │                       │                  │
  │                          │                       │  Check idempotency│
  │                          │                       │  SET NX           │
  │                          │                       │ ────────────────►│
  │                          │                       │◄──────────────── │
  │                          │                       │                  │
  │                          │                       │  If new: compute │
  │                          │                       │  score & ZADD    │
  │                          │                       │ ────────────────►│
  │                          │                       │◄──────────────── │
  │                          │                       │                  │
  │                          │                       │  PUBLISH event   │
  │                          │                       │  to quiz channel │
  │                          │                       │ ────────────────►│
  │                          │                       │                  │
  │                          │  score_result         │                  │
  │                          │◄───────────────────── │                  │
  │                          │                       │                  │
  │  { type: "score_result", │                       │                  │
  │    correct, points,      │                       │                  │
  │    totalScore }          │                       │                  │
  │◄──────────────────────── │                       │                  │
  │                          │                       │                  │
  │                          │  Receive PUB/SUB event│                  │
  │                          │◄──────────────────────────────────────── │
  │                          │                       │                  │
  │                          │  ZREVRANGEWITHSCORES  │                  │
  │                          │ ────────────────────────────────────────►│
  │                          │◄──────────────────────────────────────── │
  │                          │                       │                  │
  │  { type: "leaderboard",  │  Broadcast to all     │                  │
  │    rankings: [...] }     │  room members         │                  │
  │◄──────────────────────── │                       │                  │
  │                          │                       │                  │
```

### 3.3 Quiz Completion

```
Quiz Service              Gateway              Redis              PostgreSQL
     │                       │                   │                    │
     │  Timer expires or     │                   │                    │
     │  all questions done   │                   │                    │
     │                       │                   │                    │
     │  quiz_completed event │                   │                    │
     │ ─────────────────────►│                   │                    │
     │                       │                   │                    │
     │                       │  Final leaderboard│                    │
     │                       │  ZREVRANGEWITHSCORES                   │
     │                       │ ─────────────────►│                    │
     │                       │◄───────────────── │                    │
     │                       │                   │                    │
     │                       │  Broadcast         │                    │
     │                       │  quiz_ended +      │                    │
     │                       │  final_leaderboard │                    │
     │                       │  to all clients    │                    │
     │                       │                   │                    │
     │                       │  Persist session   │                    │
     │                       │  data (async)      │                    │
     │                       │ ──────────────────────────────────────►│
     │                       │                   │                    │
     │                       │  Set TTL on Redis  │                    │
     │                       │  keys (cleanup)    │                    │
     │                       │ ─────────────────►│                    │
     │                       │                   │                    │
     │                       │  Close WebSocket   │                    │
     │                       │  connections       │                    │
     │                       │  (graceful)        │                    │
     │                       │                   │                    │
```

---

## 4. Data Models

### 4.1 WebSocket Message Protocol

All messages use a typed envelope format:

```json
{
  "type": "join | answer | question | score_result | leaderboard_update | user_joined | user_left | quiz_ended | error",
  "payload": { },
  "timestamp": 1711800000000
}
```

**Client → Server messages**:

```json
// Join quiz
{ "type": "join", "payload": { "userId": "u123", "username": "Alice" } }

// Submit answer
{ "type": "answer", "payload": { "questionId": "q1", "answerId": "a2" } }
```

**Server → Client messages**:

```json
// Quiz state on join
{
  "type": "quiz_state",
  "payload": {
    "quizId": "quiz-abc",
    "status": "active",
    "currentQuestion": {
      "id": "q1",
      "text": "What does 'ubiquitous' mean?",
      "options": [
        { "id": "a1", "text": "Rare" },
        { "id": "a2", "text": "Present everywhere" },
        { "id": "a3", "text": "Ancient" },
        { "id": "a4", "text": "Fragile" }
      ],
      "timeLimit": 15
    },
    "participants": ["Alice", "Bob"],
    "leaderboard": [
      { "rank": 1, "username": "Bob", "score": 150 },
      { "rank": 2, "username": "Alice", "score": 0 }
    ]
  }
}

// Score result (sent only to the answering user)
{
  "type": "score_result",
  "payload": {
    "questionId": "q1",
    "correct": true,
    "points": 85,
    "totalScore": 85,
    "timeBonus": 0.7
  }
}

// Leaderboard update (broadcast to all participants)
{
  "type": "leaderboard_update",
  "payload": {
    "leaderboard": [
      { "rank": 1, "username": "Bob", "score": 150 },
      { "rank": 2, "username": "Alice", "score": 85 }
    ]
  }
}

// Error
{
  "type": "error",
  "payload": {
    "code": "ALREADY_ANSWERED",
    "message": "You have already submitted an answer for this question"
  }
}
```

### 4.2 Redis Key Patterns

```
quiz:{quizId}:leaderboard              # Sorted Set — scores by user
quiz:{quizId}:session                  # Hash — quiz metadata (status, currentQuestion, startedAt)
quiz:{quizId}:user:{userId}:q:{qId}   # String (NX) — idempotency guard for answer submissions
quiz:{quizId}:participants             # Set — user IDs in the quiz
quiz:{quizId}:events                   # Pub/Sub channel — real-time event broadcasting
```

All quiz keys use `{quiz:{quizId}}` hash tag pattern for Redis Cluster compatibility (ensures all keys for a quiz land on the same shard).

TTL policy: all quiz keys expire 1 hour after quiz completion.

### 4.3 PostgreSQL Schema

```sql
-- Quiz definitions (content)
CREATE TABLE quizzes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title       TEXT NOT NULL,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Questions within a quiz
CREATE TABLE questions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    quiz_id     UUID NOT NULL REFERENCES quizzes(id),
    text        TEXT NOT NULL,
    options     JSONB NOT NULL,          -- [{ "id": "a1", "text": "..." }, ...]
    correct_id  TEXT NOT NULL,           -- Matches option id
    time_limit  INT NOT NULL DEFAULT 15, -- Seconds
    sort_order  INT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_questions_quiz_id ON questions(quiz_id);

-- Quiz sessions (instances of a quiz being played)
CREATE TABLE quiz_sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    quiz_id     UUID NOT NULL REFERENCES quizzes(id),
    status      TEXT NOT NULL DEFAULT 'waiting', -- waiting, active, completed
    started_at  TIMESTAMPTZ,
    ended_at    TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sessions_quiz_id ON quiz_sessions(quiz_id);
CREATE INDEX idx_sessions_status ON quiz_sessions(status);

-- Participant answers (historical record)
CREATE TABLE answers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID NOT NULL REFERENCES quiz_sessions(id),
    user_id         TEXT NOT NULL,
    question_id     UUID NOT NULL REFERENCES questions(id),
    selected_id     TEXT NOT NULL,
    correct         BOOLEAN NOT NULL,
    score           INT NOT NULL,
    time_taken_ms   INT NOT NULL,
    submitted_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(session_id, user_id, question_id) -- Idempotency at DB level
);

CREATE INDEX idx_answers_session ON answers(session_id);
CREATE INDEX idx_answers_user ON answers(user_id);

-- Final leaderboard snapshots
CREATE TABLE leaderboard_snapshots (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id  UUID NOT NULL REFERENCES quiz_sessions(id),
    user_id     TEXT NOT NULL,
    username    TEXT NOT NULL,
    final_score INT NOT NULL,
    final_rank  INT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_leaderboard_session ON leaderboard_snapshots(session_id);
```

---

## 5. Scalability

### 5.1 Current Design Capacity

Single Go server + single Redis instance:
- WebSocket connections: ~10,000 concurrent (Go's goroutine model, ~4KB per goroutine)
- Redis operations: ~100,000 ops/sec (Sorted Set operations are sub-millisecond)
- Quiz rooms: limited by memory for room maps (~1KB per room, thousands easily)

### 5.2 Horizontal Scaling Strategy

**Phase 1 — Multiple gateways (10K–50K users)**:
- Deploy 3–5 gateway instances behind ALB with WebSocket support
- Redis Pub/Sub for cross-instance leaderboard broadcasting
- Sticky sessions by quiz ID to minimize cross-instance traffic
- Single Redis instance (still under 100K ops/sec threshold)

**Phase 2 — Redis Cluster (50K–200K users)**:
- Redis Cluster with 3+ shards
- Hash tags `{quiz:{quizId}}` ensure per-quiz data locality
- Pub/Sub works across cluster (sharded Pub/Sub in Redis 7+)
- PostgreSQL read replicas for quiz content serving

**Phase 3 — Regional deployment (200K+ users)**:
- Multi-region deployment with regional Redis instances
- Global quiz routing service to direct users to nearest region
- Cross-region leaderboard aggregation for global quizzes (async, eventual consistency)
- CDN for static quiz assets (images, audio for pronunciation quizzes)

### 5.3 Trade-offs

| Decision | Benefit | Cost |
|----------|---------|------|
| Sticky sessions | Reduces cross-instance broadcasting | Uneven load distribution if one quiz is very popular |
| Redis Pub/Sub (fire-and-forget) | Zero overhead, no persistence cost | Missed updates during network partitions (self-healing on next update) |
| In-process services (not microservices) | No network hop latency, simpler deployment | Scaling all components together even if only one is the bottleneck |
| Single Redis for leaderboard + pub/sub + events | Operational simplicity | Single point of failure (mitigated by Redis Sentinel / Cluster) |

---

## 6. Reliability

### 6.1 Failure Modes and Mitigations

| Failure | Impact | Mitigation |
|---------|--------|------------|
| Gateway instance crash | Clients on that instance disconnected | Load balancer health check removes instance; clients reconnect to another instance; quiz state preserved in Redis |
| Redis unavailable | Leaderboard reads/writes fail | Circuit breaker → fallback to in-memory sorted map; degraded consistency but quiz continues |
| PostgreSQL unavailable | Cannot load new quiz content | Quiz content cached in memory at session start; existing sessions unaffected; new session creation fails gracefully |
| Network partition between gateway and Redis | Score updates delayed | Client-side retry with exponential backoff; idempotency keys prevent double-scoring on retry |
| Client disconnection (network drop) | User misses questions | Server tracks answered questions per user; on reconnect, server replays current state |

### 6.2 Reconnection Protocol

1. Client detects disconnection (WebSocket `onclose` event)
2. Client initiates reconnection with exponential backoff: 1s → 2s → 4s → 8s → 16s (max), with ±500ms jitter
3. Client sends `rejoin` message with `userId` and `quizId`
4. Server validates user was previously in the quiz (check Redis participants set)
5. Server sends current quiz state: current question, leaderboard, user's answered questions list
6. Client resumes from current state (no duplicate answers possible due to idempotency)

### 6.3 Idempotency

Answer submissions are idempotent at two levels:

1. **Redis level**: `SET quiz:{quizId}:user:{userId}:q:{questionId} 1 NX EX 3600` — the NX flag ensures only the first submission is accepted
2. **PostgreSQL level**: `UNIQUE(session_id, user_id, question_id)` constraint on the answers table prevents duplicate records even if Redis state is lost

---

## 7. Observability

### 7.1 Structured Logging

All log entries include:

```json
{
  "level": "info",
  "msg": "answer_submitted",
  "quiz_id": "quiz-abc",
  "user_id": "u123",
  "question_id": "q1",
  "correct": true,
  "score": 85,
  "latency_ms": 12,
  "timestamp": "2026-03-30T10:15:30.123Z"
}
```

Correlation ID (`quiz_id`) enables filtering all events for a single quiz session across all services.

### 7.2 Metrics

**Prometheus metrics exposed at `/metrics`**:

| Metric | Type | Labels | Purpose |
|--------|------|--------|---------|
| `quiz_ws_connections_active` | Gauge | `quiz_id` | Current connection count per quiz |
| `quiz_ws_connections_total` | Counter | — | Total connections over lifetime |
| `quiz_messages_received_total` | Counter | `type` | Inbound message volume by type |
| `quiz_messages_broadcast_total` | Counter | `type` | Outbound broadcast volume |
| `quiz_answer_duration_seconds` | Histogram | — | Answer processing latency |
| `quiz_leaderboard_update_duration_seconds` | Histogram | — | Leaderboard read + broadcast time |
| `quiz_scoring_errors_total` | Counter | `error_type` | Scoring failures by category |
| `quiz_redis_duration_seconds` | Histogram | `operation` | Redis operation latency |

### 7.3 SLIs and SLOs

| SLI | Measurement | SLO Target |
|-----|-------------|------------|
| Leaderboard update latency | Time from answer submission to all clients receiving updated leaderboard | p99 < 200ms |
| WebSocket connection success | Successful upgrades / total attempts | > 99.5% per 24h |
| Scoring accuracy | Correctly scored answers / total submissions | 100% |
| Quiz join latency | Time from join request to quiz state received | p99 < 500ms |

### 7.4 Alerting Strategy

| Severity | Condition | Action |
|----------|-----------|--------|
| P1 — Critical | Scoring error rate > 0 for 1 minute | Immediate page to on-call; scoring bug affects fairness |
| P2 — High | Leaderboard p99 > 500ms for 5 minutes | Page to on-call; degraded real-time experience |
| P3 — Medium | WebSocket error rate > 1% for 15 minutes | Slack notification; investigate connection issues |
| P4 — Low | Redis latency p99 > 10ms for 30 minutes | Ticket creation; performance degradation building |

### 7.5 Grafana Dashboard Design

**Dashboard 1 — Operational Overview**:
- Active WebSocket connections (gauge, per instance)
- Message throughput (rate, inbound vs. outbound)
- Error rate (percentage, by error type)
- Redis operation latency (heatmap)

**Dashboard 2 — Quiz Session Health**:
- Per-quiz participant count over time
- Answer submission rate per question
- Score distribution histogram
- Quiz completion rate

**Dashboard 3 — SLO Burn Rate**:
- Error budget remaining (percentage, per SLO)
- Burn rate (current consumption rate vs. budget)
- Alerting threshold visualization

---

## 8. Security Considerations

### 8.1 Authentication and Authorization

- Quiz join requires authenticated user (JWT or session token validated at gateway)
- WebSocket connections carry the auth token in the initial handshake (query parameter or first message)
- Server validates token on upgrade and rejects unauthorized connections
- Each answer submission validated against the user's session — a user cannot submit answers for another user

### 8.2 Input Validation

- All incoming WebSocket messages validated against expected schema
- Quiz ID, question ID, and answer ID validated against known values
- Timestamp validation: answers submitted more than `timeLimit + 2s` (clock skew tolerance) after question start are rejected
- Message size limit: WebSocket frames capped at 4KB to prevent resource exhaustion

### 8.3 Rate Limiting

- Answer submissions rate-limited to 1 per question per user (enforced by idempotency)
- WebSocket message rate: max 10 messages/second per connection (prevents flooding)
- Quiz join: max 5 join attempts per user per minute (prevents enumeration)

### 8.4 Data Privacy

- Leaderboard displays usernames only, not user IDs or personal data
- Answer history accessible only to the answering user and quiz administrators
- Session data retained per data retention policy (configurable TTL)

---

## 9. Future Enhancements

Discussed but not implemented in this challenge submission:

1. **Feature flags**: Toggle quiz variants (timed vs. untimed, team vs. individual) via feature flag service
2. **A/B testing**: Experiment with scoring formulas to optimize engagement and learning outcomes
3. **Analytics pipeline**: Stream quiz events to a data warehouse for learning effectiveness analysis
4. **Rate limiting service**: Centralized rate limiting with sliding window counters
5. **WebSocket compression**: `permessage-deflate` for bandwidth reduction on mobile networks
6. **Multi-language support**: i18n for quiz content and UI, with locale-based question serving
7. **Anti-cheat**: Server-side answer timing validation, anomaly detection on response patterns
8. **Replay mode**: Post-quiz review where users can see correct answers and explanations
9. **Team mode**: Team-based quizzes with aggregated team scores and per-team leaderboards
10. **Offline support**: Progressive Web App with local quiz caching and score sync on reconnection
