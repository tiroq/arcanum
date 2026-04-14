-- Iteration 53: Execution Loop Engine.
-- Bounded autonomous execution tasks with step-level tracking.

CREATE TABLE IF NOT EXISTS agent_execution_tasks (
    id              TEXT PRIMARY KEY,
    opportunity_id  TEXT        NOT NULL DEFAULT '',
    goal            TEXT        NOT NULL DEFAULT '',
    status          TEXT        NOT NULL DEFAULT 'pending',
    plan            JSONB       NOT NULL DEFAULT '[]'::jsonb,
    current_step    INTEGER     NOT NULL DEFAULT 0,
    iteration_count INTEGER     NOT NULL DEFAULT 0,
    max_iterations  INTEGER     NOT NULL DEFAULT 5,
    abort_reason    TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_execution_tasks_status ON agent_execution_tasks(status);
CREATE INDEX IF NOT EXISTS idx_execution_tasks_created_at ON agent_execution_tasks(created_at DESC);

CREATE TABLE IF NOT EXISTS agent_execution_observations (
    id          BIGSERIAL PRIMARY KEY,
    step_id     TEXT        NOT NULL DEFAULT '',
    task_id     TEXT        NOT NULL DEFAULT '',
    success     BOOLEAN     NOT NULL DEFAULT FALSE,
    output      JSONB,
    error       TEXT        NOT NULL DEFAULT '',
    timestamp   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_execution_observations_task_id ON agent_execution_observations(task_id);
CREATE INDEX IF NOT EXISTS idx_execution_observations_timestamp ON agent_execution_observations(timestamp ASC);
