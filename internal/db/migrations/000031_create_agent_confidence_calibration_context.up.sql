-- Iteration 26: Contextual Confidence Calibration Layer
-- Tracks prediction accuracy per context for context-aware confidence adjustment.

CREATE TABLE IF NOT EXISTS agent_confidence_calibration_context (
    id               SERIAL PRIMARY KEY,
    goal_type        TEXT,
    provider_name    TEXT,
    strategy_type    TEXT,
    sample_count     INT              NOT NULL DEFAULT 0,
    avg_predicted_confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_actual_success       DOUBLE PRECISION NOT NULL DEFAULT 0,
    calibration_error        DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_updated     TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);

-- UNIQUE constraint for context matching (NULLs are distinct in standard SQL,
-- so we use COALESCE to treat them as empty strings for uniqueness).
CREATE UNIQUE INDEX IF NOT EXISTS idx_confidence_cal_ctx_unique
    ON agent_confidence_calibration_context (
        COALESCE(goal_type, ''),
        COALESCE(provider_name, ''),
        COALESCE(strategy_type, '')
    );

CREATE INDEX IF NOT EXISTS idx_confidence_cal_ctx_goal
    ON agent_confidence_calibration_context (goal_type);
