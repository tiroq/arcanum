-- Rollback migration 000025: Remove portfolio tracking columns.

ALTER TABLE agent_strategy_memory
    DROP COLUMN IF EXISTS selection_count,
    DROP COLUMN IF EXISTS win_count,
    DROP COLUMN IF EXISTS win_rate;
