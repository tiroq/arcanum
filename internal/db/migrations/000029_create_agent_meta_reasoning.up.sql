-- Iteration 24: Meta-Reasoning Layer
-- Mode memory tracks per-mode, per-goal success/failure statistics.
-- Mode history records every mode selection decision for observability.

CREATE TABLE IF NOT EXISTS agent_meta_reasoning_memory (
    id           SERIAL PRIMARY KEY,
    mode         TEXT    NOT NULL,
    goal_type    TEXT    NOT NULL,
    selection_count INT  NOT NULL DEFAULT 0,
    success_count   INT  NOT NULL DEFAULT 0,
    failure_count   INT  NOT NULL DEFAULT 0,
    success_rate    DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_selected_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (mode, goal_type)
);

CREATE TABLE IF NOT EXISTS agent_meta_reasoning_history (
    id           SERIAL PRIMARY KEY,
    goal_type    TEXT    NOT NULL,
    selected_mode TEXT   NOT NULL,
    confidence   DOUBLE PRECISION NOT NULL DEFAULT 0,
    reason       TEXT   NOT NULL DEFAULT '',
    outcome      TEXT   NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
