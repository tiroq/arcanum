-- Iteration 36: Income Engine — Opportunities
CREATE TABLE IF NOT EXISTS agent_income_opportunities (
    id              TEXT PRIMARY KEY,
    source          TEXT NOT NULL DEFAULT 'user',
    title           TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    opportunity_type TEXT NOT NULL,
    estimated_value  DOUBLE PRECISION NOT NULL DEFAULT 0,
    estimated_effort DOUBLE PRECISION NOT NULL DEFAULT 0,
    confidence       DOUBLE PRECISION NOT NULL DEFAULT 0.5,
    status           TEXT NOT NULL DEFAULT 'open',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_income_opportunities_status
    ON agent_income_opportunities(status);
CREATE INDEX IF NOT EXISTS idx_income_opportunities_type
    ON agent_income_opportunities(opportunity_type);
CREATE INDEX IF NOT EXISTS idx_income_opportunities_created
    ON agent_income_opportunities(created_at DESC);
