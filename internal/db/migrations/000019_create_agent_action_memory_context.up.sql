CREATE TABLE agent_action_memory_context (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    action_type    TEXT NOT NULL,
    goal_type      TEXT NOT NULL DEFAULT '',
    job_type       TEXT NOT NULL DEFAULT '',
    failure_bucket TEXT NOT NULL DEFAULT '',
    backlog_bucket TEXT NOT NULL DEFAULT '',
    total_runs     INT NOT NULL DEFAULT 0,
    success_runs   INT NOT NULL DEFAULT 0,
    failure_runs   INT NOT NULL DEFAULT 0,
    neutral_runs   INT NOT NULL DEFAULT 0,
    success_rate   DOUBLE PRECISION NOT NULL DEFAULT 0,
    failure_rate   DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_updated   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(action_type, goal_type, job_type, failure_bucket, backlog_bucket)
);

CREATE INDEX idx_agent_action_memory_context_action ON agent_action_memory_context (action_type);
CREATE INDEX idx_agent_action_memory_context_lookup ON agent_action_memory_context (action_type, goal_type);
