-- Iteration 36: Income Engine — Outcomes
CREATE TABLE IF NOT EXISTS agent_income_outcomes (
    id              TEXT PRIMARY KEY,
    opportunity_id  TEXT NOT NULL REFERENCES agent_income_opportunities(id),
    proposal_id     TEXT,
    outcome_status  TEXT NOT NULL,
    actual_value    DOUBLE PRECISION NOT NULL DEFAULT 0,
    owner_time_saved DOUBLE PRECISION NOT NULL DEFAULT 0,
    notes           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_income_outcomes_opportunity
    ON agent_income_outcomes(opportunity_id);
CREATE INDEX IF NOT EXISTS idx_income_outcomes_status
    ON agent_income_outcomes(outcome_status);
CREATE INDEX IF NOT EXISTS idx_income_outcomes_created
    ON agent_income_outcomes(created_at DESC);
