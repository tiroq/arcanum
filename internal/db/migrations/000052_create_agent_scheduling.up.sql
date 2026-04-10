-- Autonomous Scheduling & Calendar Control (Iteration 48)
-- 4 tables: slots, candidates, decisions, calendar records

CREATE TABLE IF NOT EXISTS agent_schedule_slots (
    id TEXT PRIMARY KEY,
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ NOT NULL,
    slot_type TEXT NOT NULL DEFAULT 'work',
    available BOOLEAN NOT NULL DEFAULT true,
    day_of_week TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_schedule_slots_time
    ON agent_schedule_slots(start_time, end_time);

CREATE INDEX IF NOT EXISTS idx_schedule_slots_available
    ON agent_schedule_slots(available, slot_type);

CREATE TABLE IF NOT EXISTS agent_schedule_candidates (
    id TEXT PRIMARY KEY,
    item_type TEXT NOT NULL,
    item_id TEXT NOT NULL,
    estimated_effort_hours DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    urgency DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    expected_value DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    preferred_window TEXT NOT NULL DEFAULT '',
    strategy_priority DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_schedule_candidates_created
    ON agent_schedule_candidates(created_at DESC);

CREATE TABLE IF NOT EXISTS agent_schedule_decisions (
    id TEXT PRIMARY KEY,
    candidate_id TEXT NOT NULL REFERENCES agent_schedule_candidates(id),
    chosen_slot_id TEXT NOT NULL REFERENCES agent_schedule_slots(id),
    fit_score DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    requires_review BOOLEAN NOT NULL DEFAULT false,
    review_reason TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'proposed',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_schedule_decisions_status
    ON agent_schedule_decisions(status);

CREATE INDEX IF NOT EXISTS idx_schedule_decisions_created
    ON agent_schedule_decisions(created_at DESC);

CREATE TABLE IF NOT EXISTS agent_calendar_records (
    id TEXT PRIMARY KEY,
    decision_id TEXT NOT NULL REFERENCES agent_schedule_decisions(id),
    external_calendar_id TEXT NOT NULL DEFAULT '',
    event_ref TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_calendar_records_decision
    ON agent_calendar_records(decision_id);

CREATE INDEX IF NOT EXISTS idx_calendar_records_status
    ON agent_calendar_records(status);
