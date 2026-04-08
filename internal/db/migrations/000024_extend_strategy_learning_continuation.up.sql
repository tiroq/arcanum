-- Iteration 18.1: extend strategy learning tables with step-level + continuation tracking.

-- Extend strategy memory with continuation tracking columns.
ALTER TABLE agent_strategy_memory
    ADD COLUMN IF NOT EXISTS step1_success_runs      INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS step2_success_runs      INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS continuation_used_runs  INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS continuation_gain_runs  INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS step1_success_rate      DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS step2_success_rate      DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS continuation_gain_rate  DOUBLE PRECISION NOT NULL DEFAULT 0;

-- Extend strategy outcomes with step-level signals.
ALTER TABLE agent_strategy_outcomes
    ADD COLUMN IF NOT EXISTS step1_status       TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS step2_status       TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS continuation_used  BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS continuation_gain  BOOLEAN NOT NULL DEFAULT false;
