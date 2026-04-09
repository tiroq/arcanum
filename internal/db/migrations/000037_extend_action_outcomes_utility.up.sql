-- Iteration 35: Goal-Driven Execution Layer
-- Adds utility dimension columns to agent_action_outcomes for real-world value tracking.
ALTER TABLE agent_action_outcomes
    ADD COLUMN IF NOT EXISTS income_value       DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS family_value       DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS owner_relief_value DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS risk_cost          DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS utility_score      DOUBLE PRECISION NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_agent_action_outcomes_utility_score
    ON agent_action_outcomes(utility_score);
