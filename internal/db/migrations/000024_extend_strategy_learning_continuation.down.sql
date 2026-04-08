-- Rollback Iteration 18.1 extensions.

ALTER TABLE agent_strategy_outcomes
    DROP COLUMN IF EXISTS continuation_gain,
    DROP COLUMN IF EXISTS continuation_used,
    DROP COLUMN IF EXISTS step2_status,
    DROP COLUMN IF EXISTS step1_status;

ALTER TABLE agent_strategy_memory
    DROP COLUMN IF EXISTS continuation_gain_rate,
    DROP COLUMN IF EXISTS step2_success_rate,
    DROP COLUMN IF EXISTS step1_success_rate,
    DROP COLUMN IF EXISTS continuation_gain_runs,
    DROP COLUMN IF EXISTS continuation_used_runs,
    DROP COLUMN IF EXISTS step2_success_runs,
    DROP COLUMN IF EXISTS step1_success_runs;
