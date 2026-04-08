-- Migration 000025: Add portfolio tracking columns to agent_strategy_memory.
-- Iteration 19: Strategy Portfolio + Competition Layer.

ALTER TABLE agent_strategy_memory
    ADD COLUMN IF NOT EXISTS selection_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS win_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS win_rate DOUBLE PRECISION NOT NULL DEFAULT 0;
