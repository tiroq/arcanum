CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE source_connections (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT NOT NULL,
    provider       TEXT NOT NULL,
    config         JSONB NOT NULL DEFAULT '{}',
    enabled        BOOLEAN NOT NULL DEFAULT true,
    last_synced_at TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_source_connections_provider ON source_connections(provider);
CREATE INDEX idx_source_connections_enabled ON source_connections(enabled) WHERE enabled = true;
