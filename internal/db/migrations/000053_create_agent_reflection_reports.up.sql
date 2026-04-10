CREATE TABLE IF NOT EXISTS agent_reflection_reports (
    id TEXT PRIMARY KEY,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    json_data JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_reflection_reports_created_at
    ON agent_reflection_reports (created_at DESC);
