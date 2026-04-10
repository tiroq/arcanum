-- Iteration 39: Real Outcome Attribution Layer

-- Extend agent_income_outcomes with source and verification fields.
ALTER TABLE agent_income_outcomes
    ADD COLUMN IF NOT EXISTS outcome_source TEXT NOT NULL DEFAULT 'manual',
    ADD COLUMN IF NOT EXISTS verified       BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS idx_income_outcomes_source
    ON agent_income_outcomes(outcome_source);
CREATE INDEX IF NOT EXISTS idx_income_outcomes_verified
    ON agent_income_outcomes(verified);

-- Per-type learning records derived from real outcomes.
CREATE TABLE IF NOT EXISTS agent_income_learning (
    opportunity_type      TEXT PRIMARY KEY,
    total_outcomes        INTEGER NOT NULL DEFAULT 0,
    success_count         INTEGER NOT NULL DEFAULT 0,
    total_accuracy        DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_accuracy          DOUBLE PRECISION NOT NULL DEFAULT 0,
    success_rate          DOUBLE PRECISION NOT NULL DEFAULT 0,
    confidence_adjustment DOUBLE PRECISION NOT NULL DEFAULT 0,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
