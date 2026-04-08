-- Provider usage tracking for quota-aware routing.
-- Tracks per-provider request and token consumption with minute/day windows.
CREATE TABLE IF NOT EXISTS agent_provider_usage (
    provider_name TEXT PRIMARY KEY,
    requests_this_minute INTEGER NOT NULL DEFAULT 0,
    tokens_this_minute   INTEGER NOT NULL DEFAULT 0,
    requests_today       INTEGER NOT NULL DEFAULT 0,
    tokens_today         INTEGER NOT NULL DEFAULT 0,
    last_updated         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_provider_usage_updated ON agent_provider_usage (last_updated);
