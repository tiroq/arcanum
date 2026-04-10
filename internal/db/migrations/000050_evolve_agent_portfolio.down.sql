-- Rollback Iteration 46 portfolio schema evolution.

-- Restore performance column names.
DROP INDEX IF EXISTS idx_agent_strategy_performance_roi_per_hour;
ALTER TABLE agent_strategy_performance RENAME COLUMN total_verified_revenue TO total_revenue;
ALTER TABLE agent_strategy_performance RENAME COLUMN total_estimated_hours TO total_time_spent;
ALTER TABLE agent_strategy_performance RENAME COLUMN roi_per_hour TO roi;
CREATE INDEX IF NOT EXISTS idx_agent_strategy_performance_roi ON agent_strategy_performance(roi DESC);

-- Drop count columns.
ALTER TABLE agent_strategy_performance DROP COLUMN IF EXISTS opportunity_count;
ALTER TABLE agent_strategy_performance DROP COLUMN IF EXISTS qualified_count;
ALTER TABLE agent_strategy_performance DROP COLUMN IF EXISTS won_count;
ALTER TABLE agent_strategy_performance DROP COLUMN IF EXISTS lost_count;

-- Drop allocation_weight.
ALTER TABLE agent_strategy_allocations DROP COLUMN IF EXISTS allocation_weight;

-- Restore volatility from stability_score.
ALTER TABLE agent_strategies ADD COLUMN IF NOT EXISTS volatility DOUBLE PRECISION NOT NULL DEFAULT 0;
UPDATE agent_strategies SET volatility = 1.0 - COALESCE(stability_score, 0.5);
ALTER TABLE agent_strategies DROP COLUMN IF EXISTS stability_score;
ALTER TABLE agent_strategies DROP COLUMN IF EXISTS confidence;
