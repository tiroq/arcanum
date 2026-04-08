-- Provider catalog model-level usage tracking (Iteration 32).
-- Extends provider usage to track per-provider+model consumption.
-- The existing agent_provider_usage table tracks provider-level usage;
-- this table adds model-level granularity.
CREATE TABLE IF NOT EXISTS agent_provider_model_usage (
    provider_name        TEXT NOT NULL,
    model_name           TEXT NOT NULL,
    requests_this_minute INTEGER NOT NULL DEFAULT 0,
    tokens_this_minute   INTEGER NOT NULL DEFAULT 0,
    requests_today       INTEGER NOT NULL DEFAULT 0,
    tokens_today         INTEGER NOT NULL DEFAULT 0,
    last_updated         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider_name, model_name)
);

CREATE INDEX IF NOT EXISTS idx_agent_provider_model_usage_updated
    ON agent_provider_model_usage (last_updated);
