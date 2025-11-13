-- +goose Up
-- Baseline schema for cloud-access-core (IAM, policy, config, audit)

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "citext";

DO $$ BEGIN
    CREATE TYPE tier_enum AS ENUM ('free', 'standard', 'premium', 'enterprise');
EXCEPTION WHEN duplicate_object THEN null;
END $$;

DO $$ BEGIN
    CREATE TYPE state_enum AS ENUM ('active', 'warning', 'frozen');
EXCEPTION WHEN duplicate_object THEN null;
END $$;

DO $$ BEGIN
    CREATE TYPE device_status_enum AS ENUM ('pending', 'approved', 'revoked', 'suspended');
EXCEPTION WHEN duplicate_object THEN null;
END $$;

DO $$ BEGIN
    CREATE TYPE platform_enum AS ENUM ('windows', 'macos', 'linux', 'unknown');
EXCEPTION WHEN duplicate_object THEN null;
END $$;

CREATE TABLE IF NOT EXISTS tenants (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name              TEXT NOT NULL UNIQUE,
    tier              tier_enum NOT NULL DEFAULT 'standard',
    default_policies  JSONB NOT NULL DEFAULT '{}'::jsonb,
    billing_meta      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at        TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS users (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      UUID NOT NULL REFERENCES tenants(id),
    email          CITEXT NOT NULL UNIQUE,
    password_hash  BYTEA NOT NULL,
    roles          TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    state          state_enum NOT NULL DEFAULT 'active',
    last_login     TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at     TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_users_tenant ON users(tenant_id);

CREATE TABLE IF NOT EXISTS policy_rules (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id),
    name         TEXT NOT NULL,
    effect       TEXT NOT NULL,
    actions      TEXT[] NOT NULL,
    resources    TEXT[] NOT NULL,
    conditions   JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS groups (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id),
    name                TEXT NOT NULL,
    roles               TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    speech_quota_seconds BIGINT NOT NULL DEFAULT 0,
    llm_token_quota     BIGINT NOT NULL DEFAULT 0,
    screenshot_quota    BIGINT NOT NULL DEFAULT 0,
    overdraft_policy    JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_by          UUID,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at          TIMESTAMPTZ,
    UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS quota_snapshots (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id),
    user_id           UUID NOT NULL REFERENCES users(id),
    speech_seconds_used BIGINT NOT NULL DEFAULT 0,
    llm_tokens_used   BIGINT NOT NULL DEFAULT 0,
    screenshots_used  BIGINT NOT NULL DEFAULT 0,
    state             state_enum NOT NULL DEFAULT 'active',
    captured_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_quota_snapshots_user ON quota_snapshots(user_id);

CREATE TABLE IF NOT EXISTS devices (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES users(id),
    tenant_id      UUID NOT NULL REFERENCES tenants(id),
    platform       platform_enum NOT NULL DEFAULT 'unknown',
    fingerprint    TEXT,
    trust_score    SMALLINT NOT NULL DEFAULT 0,
    status         device_status_enum NOT NULL DEFAULT 'pending',
    last_seen      TIMESTAMPTZ,
    config_version INT NOT NULL DEFAULT 1,
    metadata       JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at     TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_devices_user ON devices(user_id);

CREATE TABLE IF NOT EXISTS client_profiles (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id),
    profile_name      TEXT NOT NULL,
    sampling_rate     INT NOT NULL DEFAULT 48000,
    noise_gate        REAL NOT NULL DEFAULT 0.01,
    detection_toggles JSONB NOT NULL DEFAULT '{}'::jsonb,
    model_presets     JSONB NOT NULL DEFAULT '{}'::jsonb,
    endpoints         JSONB NOT NULL DEFAULT '{}'::jsonb,
    version           INT NOT NULL DEFAULT 1,
    signature         BYTEA,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, profile_name)
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id   UUID NOT NULL UNIQUE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id),
    actor_id   UUID,
    action     TEXT NOT NULL,
    result     TEXT NOT NULL,
    latency_ms INT NOT NULL DEFAULT 0,
    geo        TEXT,
    metadata   JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_tenant ON audit_logs(tenant_id);

-- Seed initial admin tenant/user
INSERT INTO tenants (id, name, tier)
VALUES ('00000000-0000-0000-0000-000000000001', 'root', 'enterprise')
ON CONFLICT (id) DO NOTHING;

INSERT INTO users (id, tenant_id, email, password_hash, roles)
VALUES (
    '00000000-0000-0000-0000-00000000000a',
    '00000000-0000-0000-0000-000000000001',
    'admin@cloud-access.local',
    convert_to('$argon2id$v=19$m=65536,t=1,p=10$TkQfjvPPNFcQINLRYVLqAg$tXYn8ijlVgtw+PzxzfjAtU0zrFn0d4Eu5Qa+il+Mk9Y', 'utf8'),
    ARRAY['admin']
)
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS client_profiles;
DROP TABLE IF EXISTS devices;
DROP TABLE IF EXISTS quota_snapshots;
DROP TABLE IF EXISTS groups;
DROP TABLE IF EXISTS policy_rules;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS tenants;
DROP TYPE IF EXISTS platform_enum;
DROP TYPE IF EXISTS device_status_enum;
DROP TYPE IF EXISTS state_enum;
DROP TYPE IF EXISTS tier_enum;
