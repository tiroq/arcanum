-- Strategy memory: aggregate outcome statistics per strategy_type + goal_type.
CREATE TABLE IF NOT EXISTS agent_strategy_memory (
    id            UUID PRIMARY KEY,
    strategy_type TEXT NOT NULL,
    goal_type     TEXT NOT NULL,
    total_runs    INTEGER NOT NULL DEFAULT 0,
    success_runs  INTEGER NOT NULL DEFAULT 0,
    failure_runs  INTEGER NOT NULL DEFAULT 0,
    neutral_runs  INTEGER NOT NULL DEFAULT 0,
    success_rate  DOUBLE PRECISION NOT NULL DEFAULT 0,
    failure_rate  DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_updated  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(strategy_type, goal_type)
);

CREATE INDEX idx_agent_strategy_memory_type ON agent_strategy_memory (strategy_type);
CREATE INDEX idx_agent_strategy_memory_updated ON agent_strategy_memory (last_updated DESC);

-- Strategy outcomes: evaluated results of executed strategies.
CREATE TABLE IF NOT EXISTS agent_strategy_outcomes (
    id             UUID PRIMARY KEY,
    strategy_id    UUID NOT NULL,
    strategy_type  TEXT NOT NULL,
    goal_type      TEXT NOT NULL,
    step1_action   TEXT NOT NULL,
    step2_executed BOOLEAN NOT NULL DEFAULT false,
    final_status   TEXT NOT NULL,
    improvement    BOOLEAN NOT NULL DEFAULT false,
    evaluated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_agent_strategy_outcomes_strategy ON agent_strategy_outcomes (strategy_id);
CREATE INDEX idx_agent_strategy_outcomes_type ON agent_strategy_outcomes (strategy_type);
CREATE INDEX idx_agent_strategy_outcomes_evaluated ON agent_strategy_outcomes (evaluated_at DESC);
