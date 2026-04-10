-- Migration 000041: Create agent_financial_state table for financial pressure layer (Iteration 38).
-- Stores a single-row financial snapshot used to compute pressure signals.

CREATE TABLE IF NOT EXISTS agent_financial_state (
    id                   TEXT PRIMARY KEY DEFAULT 'current',
    current_income_month DOUBLE PRECISION NOT NULL DEFAULT 0,
    target_income_month  DOUBLE PRECISION NOT NULL DEFAULT 0,
    monthly_expenses     DOUBLE PRECISION NOT NULL DEFAULT 0,
    cash_buffer          DOUBLE PRECISION NOT NULL DEFAULT 0,
    updated_at           TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);

-- Enforce single-row semantics: only 'current' is valid.
-- The CHECK constraint prevents additional rows.
ALTER TABLE agent_financial_state
    ADD CONSTRAINT agent_financial_state_single_row CHECK (id = 'current');
