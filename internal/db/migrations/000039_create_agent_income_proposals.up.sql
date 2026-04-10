-- Iteration 36: Income Engine — Proposals
CREATE TABLE IF NOT EXISTS agent_income_proposals (
    id              TEXT PRIMARY KEY,
    opportunity_id  TEXT NOT NULL REFERENCES agent_income_opportunities(id),
    action_type     TEXT NOT NULL,
    title           TEXT NOT NULL,
    reason          TEXT NOT NULL DEFAULT '',
    expected_value  DOUBLE PRECISION NOT NULL DEFAULT 0,
    risk_level      TEXT NOT NULL DEFAULT 'medium',
    requires_review BOOLEAN NOT NULL DEFAULT FALSE,
    status          TEXT NOT NULL DEFAULT 'pending',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_income_proposals_opportunity
    ON agent_income_proposals(opportunity_id);
CREATE INDEX IF NOT EXISTS idx_income_proposals_status
    ON agent_income_proposals(status);
CREATE INDEX IF NOT EXISTS idx_income_proposals_created
    ON agent_income_proposals(created_at DESC);
