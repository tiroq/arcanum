-- Migration 000046: Create financial truth tables (Iteration 42).

-- Raw financial events (append-only).
CREATE TABLE IF NOT EXISTS agent_financial_events (
    id              TEXT PRIMARY KEY,
    source          TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    direction       TEXT NOT NULL CHECK (direction IN ('inflow', 'outflow')),
    amount          DOUBLE PRECISION NOT NULL CHECK (amount > 0),
    currency        TEXT NOT NULL DEFAULT 'USD',
    description     TEXT NOT NULL DEFAULT '',
    external_ref    TEXT NOT NULL DEFAULT '',
    occurred_at     TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_financial_events_occurred ON agent_financial_events (occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_financial_events_external_ref ON agent_financial_events (external_ref) WHERE external_ref != '';

-- Normalized financial facts.
CREATE TABLE IF NOT EXISTS agent_financial_facts (
    id                      TEXT PRIMARY KEY,
    fact_type               TEXT NOT NULL CHECK (fact_type IN ('income', 'expense', 'refund', 'transfer')),
    amount                  DOUBLE PRECISION NOT NULL CHECK (amount > 0),
    currency                TEXT NOT NULL DEFAULT 'USD',
    verified                BOOLEAN NOT NULL DEFAULT FALSE,
    confidence              DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (confidence >= 0 AND confidence <= 1),
    source                  TEXT NOT NULL,
    event_id                TEXT NOT NULL REFERENCES agent_financial_events(id),
    linked_opportunity_id   TEXT,
    linked_outcome_id       TEXT,
    linked_proposal_id      TEXT,
    financially_verified    BOOLEAN NOT NULL DEFAULT FALSE,
    occurred_at             TIMESTAMPTZ NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_financial_facts_occurred ON agent_financial_facts (occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_financial_facts_verified ON agent_financial_facts (verified, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_financial_facts_opportunity ON agent_financial_facts (linked_opportunity_id) WHERE linked_opportunity_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_financial_facts_type_month ON agent_financial_facts (fact_type, occurred_at);

-- Attribution matches between facts and opportunities/outcomes.
CREATE TABLE IF NOT EXISTS agent_financial_matches (
    id                TEXT PRIMARY KEY,
    fact_id           TEXT NOT NULL REFERENCES agent_financial_facts(id),
    outcome_id        TEXT,
    opportunity_id    TEXT,
    match_type        TEXT NOT NULL CHECK (match_type IN ('exact', 'heuristic', 'manual')),
    match_confidence  DOUBLE PRECISION NOT NULL CHECK (match_confidence >= 0 AND match_confidence <= 1),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_financial_matches_fact ON agent_financial_matches (fact_id);
CREATE INDEX IF NOT EXISTS idx_financial_matches_opportunity ON agent_financial_matches (opportunity_id) WHERE opportunity_id IS NOT NULL;
