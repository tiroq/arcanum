-- Iteration 41: Time Allocation / Owner Capacity Layer

-- Single-row capacity state (UPSERT on id='current').
CREATE TABLE IF NOT EXISTS agent_capacity_state (
    id                    TEXT PRIMARY KEY CHECK (id = 'current'),
    available_hours_today DOUBLE PRECISION NOT NULL DEFAULT 0,
    available_hours_week  DOUBLE PRECISION NOT NULL DEFAULT 0,
    blocked_hours_today   DOUBLE PRECISION NOT NULL DEFAULT 0,
    owner_load_score      DOUBLE PRECISION NOT NULL DEFAULT 0,
    max_daily_work_hours  DOUBLE PRECISION NOT NULL DEFAULT 8,
    min_family_time_hours DOUBLE PRECISION NOT NULL DEFAULT 2,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Capacity evaluation decisions.
CREATE TABLE IF NOT EXISTS agent_capacity_decisions (
    id                TEXT PRIMARY KEY,
    item_type         TEXT NOT NULL,
    item_id           TEXT NOT NULL,
    estimated_effort  DOUBLE PRECISION NOT NULL DEFAULT 0,
    expected_value    DOUBLE PRECISION NOT NULL DEFAULT 0,
    value_per_hour    DOUBLE PRECISION NOT NULL DEFAULT 0,
    capacity_fit_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    recommended       BOOLEAN NOT NULL DEFAULT false,
    defer_reason      TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_capacity_decisions_created
    ON agent_capacity_decisions(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_capacity_decisions_item
    ON agent_capacity_decisions(item_type, item_id);
CREATE INDEX IF NOT EXISTS idx_capacity_decisions_recommended
    ON agent_capacity_decisions(recommended);
