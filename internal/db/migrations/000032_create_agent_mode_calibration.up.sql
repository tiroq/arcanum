-- Migration 000032: Mode-specific calibration tables (Iteration 28).
-- Tracks predicted confidence vs actual outcome by reasoning mode.

CREATE TABLE IF NOT EXISTS agent_mode_calibration_records (
    id              BIGSERIAL PRIMARY KEY,
    decision_id     TEXT        NOT NULL,
    goal_type       TEXT        NOT NULL DEFAULT '',
    mode            TEXT        NOT NULL,
    predicted_confidence DOUBLE PRECISION NOT NULL,
    actual_outcome  TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_mode_cal_records_mode_created
    ON agent_mode_calibration_records (mode, created_at DESC);

CREATE INDEX idx_mode_cal_records_goal_mode_created
    ON agent_mode_calibration_records (goal_type, mode, created_at DESC);

CREATE TABLE IF NOT EXISTS agent_mode_calibration_summary (
    mode                  TEXT PRIMARY KEY,
    ece                   DOUBLE PRECISION NOT NULL DEFAULT 0,
    overconfidence_score  DOUBLE PRECISION NOT NULL DEFAULT 0,
    underconfidence_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    total_records         INT              NOT NULL DEFAULT 0,
    last_updated          TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);
