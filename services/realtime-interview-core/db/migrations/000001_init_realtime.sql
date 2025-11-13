-- +goose Up
-- Baseline schema for realtime-interview-core (sessions, usage, speech metrics)

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

DO $$ BEGIN
    CREATE TYPE session_state_enum AS ENUM ('active', 'paused', 'ended');
EXCEPTION WHEN duplicate_object THEN null;
END $$;

DO $$ BEGIN
    CREATE TYPE session_mode_enum AS ENUM ('interview', 'diagnostic');
EXCEPTION WHEN duplicate_object THEN null;
END $$;

DO $$ BEGIN
    CREATE TYPE session_role_enum AS ENUM ('interviewer', 'assistant', 'observer');
EXCEPTION WHEN duplicate_object THEN null;
END $$;

CREATE TABLE IF NOT EXISTS sessions (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL REFERENCES tenants(id),
    user_id          UUID NOT NULL REFERENCES users(id),
    device_id        UUID REFERENCES devices(id),
    request_key      TEXT,
    state            session_state_enum NOT NULL DEFAULT 'active',
    mode             session_mode_enum NOT NULL DEFAULT 'interview',
    started_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at         TIMESTAMPTZ,
    last_heartbeat   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata         JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at       TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_sessions_tenant ON sessions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_sessions_state ON sessions(state);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_request_key ON sessions(tenant_id, request_key) WHERE request_key IS NOT NULL;

CREATE TABLE IF NOT EXISTS session_participants (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id  UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id),
    role        session_role_enum NOT NULL DEFAULT 'observer',
    joined_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    left_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_session_participants_session ON session_participants(session_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_session_participants_unique ON session_participants(session_id, user_id, role);

CREATE TABLE IF NOT EXISTS session_segments (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id               UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    sequence                 INT NOT NULL,
    transcript               JSONB NOT NULL DEFAULT '{}'::jsonb,
    confidence               REAL NOT NULL DEFAULT 0,
    provider_latency_millis  INT NOT NULL DEFAULT 0,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_session_segments_session ON session_segments(session_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_session_segments_sequence ON session_segments(session_id, sequence);

CREATE TABLE IF NOT EXISTS session_usage (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id     UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    tenant_id      UUID NOT NULL REFERENCES tenants(id),
    speech_seconds INT NOT NULL DEFAULT 0,
    llm_tokens     INT NOT NULL DEFAULT 0,
    error_count    INT NOT NULL DEFAULT 0,
    window_start   TIMESTAMPTZ NOT NULL,
    window_end     TIMESTAMPTZ NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_session_usage_session ON session_usage(session_id);
CREATE INDEX IF NOT EXISTS idx_session_usage_tenant_window ON session_usage(tenant_id, window_start);
CREATE UNIQUE INDEX IF NOT EXISTS idx_session_usage_window ON session_usage(session_id, window_start, window_end);

CREATE TABLE IF NOT EXISTS speech_provider_metrics (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id  UUID REFERENCES sessions(id) ON DELETE SET NULL,
    provider    TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    value       JSONB NOT NULL DEFAULT '{}'::jsonb,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_provider_metrics_session ON speech_provider_metrics(session_id);
CREATE INDEX IF NOT EXISTS idx_provider_metrics_provider ON speech_provider_metrics(provider);

-- +goose Down
DROP TABLE IF EXISTS speech_provider_metrics;
DROP TABLE IF EXISTS session_usage;
DROP TABLE IF EXISTS session_segments;
DROP TABLE IF EXISTS session_participants;
DROP TABLE IF EXISTS sessions;
DROP TYPE IF EXISTS session_role_enum;
DROP TYPE IF EXISTS session_mode_enum;
DROP TYPE IF EXISTS session_state_enum;
