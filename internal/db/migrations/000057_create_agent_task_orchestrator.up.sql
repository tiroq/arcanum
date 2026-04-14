-- Migration 000057: Multi-Task Orchestration + Priority Queue (Iteration 54)

CREATE TABLE IF NOT EXISTS agent_orchestrated_tasks (
    id              TEXT PRIMARY KEY,
    source          TEXT NOT NULL DEFAULT 'manual',
    goal            TEXT NOT NULL,
    priority_score  DOUBLE PRECISION NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'pending',
    urgency         DOUBLE PRECISION NOT NULL DEFAULT 0,
    expected_value  DOUBLE PRECISION NOT NULL DEFAULT 0,
    risk_level      DOUBLE PRECISION NOT NULL DEFAULT 0,
    strategy_type   TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_orchestrated_tasks_status ON agent_orchestrated_tasks (status);
CREATE INDEX IF NOT EXISTS idx_orchestrated_tasks_priority ON agent_orchestrated_tasks (priority_score DESC);
CREATE INDEX IF NOT EXISTS idx_orchestrated_tasks_created ON agent_orchestrated_tasks (created_at);

CREATE TABLE IF NOT EXISTS agent_task_queue (
    task_id         TEXT PRIMARY KEY REFERENCES agent_orchestrated_tasks(id) ON DELETE CASCADE,
    priority_score  DOUBLE PRECISION NOT NULL DEFAULT 0,
    inserted_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_task_queue_priority ON agent_task_queue (priority_score DESC, inserted_at ASC);
