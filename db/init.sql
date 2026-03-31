-- Quiz definitions (content)
CREATE TABLE IF NOT EXISTS quizzes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title       TEXT NOT NULL,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Questions within a quiz
CREATE TABLE IF NOT EXISTS questions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    quiz_id     UUID NOT NULL REFERENCES quizzes(id),
    text        TEXT NOT NULL,
    options     JSONB NOT NULL,
    correct_id  TEXT NOT NULL,
    time_limit  INT NOT NULL DEFAULT 30,
    sort_order  INT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_questions_quiz_id ON questions(quiz_id);

-- Quiz sessions (instances of a quiz being played)
CREATE TABLE IF NOT EXISTS quiz_sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    quiz_id     UUID NOT NULL REFERENCES quizzes(id),
    status      TEXT NOT NULL DEFAULT 'waiting',
    started_at  TIMESTAMPTZ,
    ended_at    TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_sessions_quiz_id ON quiz_sessions(quiz_id);
CREATE INDEX IF NOT EXISTS idx_sessions_status ON quiz_sessions(status);

-- Participant answers (historical record)
CREATE TABLE IF NOT EXISTS answers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID NOT NULL REFERENCES quiz_sessions(id),
    user_id         TEXT NOT NULL,
    question_id     UUID NOT NULL REFERENCES questions(id),
    selected_id     TEXT NOT NULL,
    correct         BOOLEAN NOT NULL,
    score           INT NOT NULL,
    time_taken_ms   INT NOT NULL,
    submitted_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(session_id, user_id, question_id)
);

CREATE INDEX IF NOT EXISTS idx_answers_session ON answers(session_id);
CREATE INDEX IF NOT EXISTS idx_answers_user ON answers(user_id);

-- Final leaderboard snapshots
CREATE TABLE IF NOT EXISTS leaderboard_snapshots (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id  UUID NOT NULL REFERENCES quiz_sessions(id),
    user_id     TEXT NOT NULL,
    username    TEXT NOT NULL,
    final_score INT NOT NULL,
    final_rank  INT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_leaderboard_session ON leaderboard_snapshots(session_id);

-- Seed quiz data
INSERT INTO quizzes (id, title, description) VALUES
    ('a1b2c3d4-e5f6-7890-abcd-ef1234567890', 'English Vocabulary Challenge', 'Test your English vocabulary knowledge')
ON CONFLICT (id) DO NOTHING;

INSERT INTO questions (id, quiz_id, text, options, correct_id, time_limit, sort_order) VALUES
    ('11111111-1111-1111-1111-111111111111', 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
     'What does ''ubiquitous'' mean?',
     '[{"id":"a1","text":"Rare"},{"id":"a2","text":"Present everywhere"},{"id":"a3","text":"Ancient"},{"id":"a4","text":"Fragile"}]',
     'a2', 30, 1),
    ('22222222-2222-2222-2222-222222222222', 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
     'What is a synonym for ''eloquent''?',
     '[{"id":"a1","text":"Clumsy"},{"id":"a2","text":"Articulate"},{"id":"a3","text":"Silent"},{"id":"a4","text":"Aggressive"}]',
     'a2', 30, 2),
    ('33333333-3333-3333-3333-333333333333', 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
     'What does ''ephemeral'' mean?',
     '[{"id":"a1","text":"Lasting forever"},{"id":"a2","text":"Very large"},{"id":"a3","text":"Short-lived"},{"id":"a4","text":"Extremely bright"}]',
     'a3', 30, 3),
    ('44444444-4444-4444-4444-444444444444', 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
     'What is the meaning of ''pragmatic''?',
     '[{"id":"a1","text":"Idealistic"},{"id":"a2","text":"Practical and realistic"},{"id":"a3","text":"Emotional"},{"id":"a4","text":"Theoretical"}]',
     'a2', 30, 4),
    ('55555555-5555-5555-5555-555555555555', 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
     'What does ''benevolent'' mean?',
     '[{"id":"a1","text":"Hostile"},{"id":"a2","text":"Indifferent"},{"id":"a3","text":"Well-meaning and kindly"},{"id":"a4","text":"Mysterious"}]',
     'a3', 30, 5)
ON CONFLICT (id) DO NOTHING;
