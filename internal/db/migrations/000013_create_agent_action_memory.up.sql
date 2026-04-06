CREATE TABLE agent_action_memory (
    id           UUID PRIMARY KEY,
    action_type  TEXT NOT NULL,
    target_type  TEXT NOT NULL,
    total_runs   INT NOT NULL DEFAULT 0,
    success_runs INT NOT NULL DEFAULT 0,
    failure_runs INT NOT NULL DEFAULT 0,
    neutral_runs INT NOT NULL DEFAULT 0,
    success_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    failure_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_updated TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (action_type, target_type)
);

CREATE INDEX idx_agent_action_memory_action_type ON agent_action_memory(action_type);

CREATE TABLE agent_action_memory_targets (
    id           UUID PRIMARY KEY,
    action_type  TEXT NOT NULL,
    target_type  TEXT NOT NULL,
    target_id    UUID NOT NULL,
    total_runs   INT NOT NULL DEFAULT 0,
    success_runs INT NOT NULL DEFAULT 0,
    failure_runs INT NOT NULL DEFAULT 0,
    neutral_runs INT NOT NULL DEFAULT 0,
    success_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    failure_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_updated TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (action_type, target_type, target_id)
);

CREATE INDEX idx_agent_action_memory_targets_target ON agent_action_memory_targets(target_id);
