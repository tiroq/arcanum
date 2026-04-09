-- Iteration 35: Goal-Driven Execution Layer — rollback
DROP INDEX IF EXISTS idx_agent_action_outcomes_utility_score;

ALTER TABLE agent_action_outcomes
    DROP COLUMN IF EXISTS income_value,
    DROP COLUMN IF EXISTS family_value,
    DROP COLUMN IF EXISTS owner_relief_value,
    DROP COLUMN IF EXISTS risk_cost,
    DROP COLUMN IF EXISTS utility_score;
