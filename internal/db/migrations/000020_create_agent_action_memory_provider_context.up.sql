-- Provider-aware contextual action memory (Iteration 13).
-- Additive layer on top of global (agent_action_memory) and contextual (agent_action_memory_context) memory.
CREATE TABLE IF NOT EXISTS agent_action_memory_provider_context (
    id              UUID PRIMARY KEY,
    action_type     TEXT NOT NULL,
    goal_type       TEXT NOT NULL DEFAULT '',
    job_type        TEXT NOT NULL DEFAULT '',
    provider_name   TEXT NOT NULL,
    model_role      TEXT NOT NULL DEFAULT '',
    failure_bucket  TEXT NOT NULL DEFAULT '',
    backlog_bucket  TEXT NOT NULL DEFAULT '',
    total_runs      INT NOT NULL DEFAULT 0,
    success_runs    INT NOT NULL DEFAULT 0,
    failure_runs    INT NOT NULL DEFAULT 0,
    neutral_runs    INT NOT NULL DEFAULT 0,
    success_rate    DOUBLE PRECISION NOT NULL DEFAULT 0,
    failure_rate    DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_updated    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_provider_context UNIQUE (action_type, goal_type, job_type, provider_name, model_role, failure_bucket, backlog_bucket)
);

CREATE INDEX IF NOT EXISTS idx_provider_ctx_action ON agent_action_memory_provider_context (action_type);
CREATE INDEX IF NOT EXISTS idx_provider_ctx_provider ON agent_action_memory_provider_context (provider_name);
CREATE INDEX IF NOT EXISTS idx_provider_ctx_action_goal_provider ON agent_action_memory_provider_context (action_type, goal_type, provider_name);
