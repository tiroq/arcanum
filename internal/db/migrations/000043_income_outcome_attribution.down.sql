-- Iteration 39: Rollback outcome attribution layer.
DROP TABLE IF EXISTS agent_income_learning;

ALTER TABLE agent_income_outcomes
    DROP COLUMN IF EXISTS outcome_source,
    DROP COLUMN IF EXISTS verified;
