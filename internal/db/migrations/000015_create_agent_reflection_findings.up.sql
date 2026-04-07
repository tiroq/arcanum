CREATE TABLE IF NOT EXISTS agent_reflection_findings (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cycle_id        TEXT NOT NULL,
    rule            TEXT NOT NULL,
    severity        TEXT NOT NULL DEFAULT 'info',
    action_type     TEXT NOT NULL DEFAULT '',
    summary         TEXT NOT NULL,
    detail          JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_agent_reflection_findings_cycle_id ON agent_reflection_findings (cycle_id);
CREATE INDEX idx_agent_reflection_findings_created_at ON agent_reflection_findings (created_at DESC);
CREATE INDEX idx_agent_reflection_findings_rule ON agent_reflection_findings (rule);
