-- Iteration 46 evolution: align portfolio schema with strategic revenue spec.

-- 1. Evolve agent_strategies: rename volatility → stability_score, add confidence.
ALTER TABLE agent_strategies ADD COLUMN IF NOT EXISTS stability_score DOUBLE PRECISION NOT NULL DEFAULT 0.5;
ALTER TABLE agent_strategies ADD COLUMN IF NOT EXISTS confidence DOUBLE PRECISION NOT NULL DEFAULT 0.5;

-- Migrate existing volatility data: stability_score = 1 - volatility.
UPDATE agent_strategies SET stability_score = 1.0 - COALESCE(volatility, 0) WHERE stability_score = 0.5 AND volatility IS NOT NULL AND volatility != 0;

-- Drop volatility column (data preserved in stability_score).
ALTER TABLE agent_strategies DROP COLUMN IF EXISTS volatility;

-- 2. Evolve agent_strategy_allocations: add allocation_weight.
ALTER TABLE agent_strategy_allocations ADD COLUMN IF NOT EXISTS allocation_weight DOUBLE PRECISION NOT NULL DEFAULT 0;

-- 3. Evolve agent_strategy_performance: expand with count fields, rename columns.
-- Add new count columns.
ALTER TABLE agent_strategy_performance ADD COLUMN IF NOT EXISTS opportunity_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agent_strategy_performance ADD COLUMN IF NOT EXISTS qualified_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agent_strategy_performance ADD COLUMN IF NOT EXISTS won_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agent_strategy_performance ADD COLUMN IF NOT EXISTS lost_count INTEGER NOT NULL DEFAULT 0;

-- Rename columns to match spec field names.
ALTER TABLE agent_strategy_performance RENAME COLUMN total_revenue TO total_verified_revenue;
ALTER TABLE agent_strategy_performance RENAME COLUMN total_time_spent TO total_estimated_hours;
ALTER TABLE agent_strategy_performance RENAME COLUMN roi TO roi_per_hour;

-- Drop old roi index and recreate with new column name.
DROP INDEX IF EXISTS idx_agent_strategy_performance_roi;
CREATE INDEX IF NOT EXISTS idx_agent_strategy_performance_roi_per_hour
    ON agent_strategy_performance(roi_per_hour DESC);
