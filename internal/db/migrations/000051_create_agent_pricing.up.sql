-- Iteration 47: Negotiation / Pricing Intelligence

-- Pricing profiles: structured pricing view per opportunity
CREATE TABLE IF NOT EXISTS agent_pricing_profiles (
    id                      TEXT PRIMARY KEY,
    opportunity_id          TEXT NOT NULL UNIQUE,
    strategy_id             TEXT NOT NULL DEFAULT '',
    estimated_effort_hours  DOUBLE PRECISION NOT NULL DEFAULT 0,
    cost_basis              DOUBLE PRECISION NOT NULL DEFAULT 0,
    target_price            DOUBLE PRECISION NOT NULL DEFAULT 0,
    minimum_price           DOUBLE PRECISION NOT NULL DEFAULT 0,
    stretch_price           DOUBLE PRECISION NOT NULL DEFAULT 0,
    confidence              DOUBLE PRECISION NOT NULL DEFAULT 0.5,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_pricing_profiles_opportunity
    ON agent_pricing_profiles(opportunity_id);
CREATE INDEX IF NOT EXISTS idx_agent_pricing_profiles_strategy
    ON agent_pricing_profiles(strategy_id);
CREATE INDEX IF NOT EXISTS idx_agent_pricing_profiles_updated
    ON agent_pricing_profiles(updated_at DESC);

-- Negotiation records: state machine per opportunity
CREATE TABLE IF NOT EXISTS agent_negotiation_records (
    id                      TEXT PRIMARY KEY,
    opportunity_id          TEXT NOT NULL UNIQUE,
    negotiation_state       TEXT NOT NULL DEFAULT 'unpriced',
    current_offered_price   DOUBLE PRECISION NOT NULL DEFAULT 0,
    recommended_next_price  DOUBLE PRECISION NOT NULL DEFAULT 0,
    concession_count        INTEGER NOT NULL DEFAULT 0,
    requires_review         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_negotiation_records_opportunity
    ON agent_negotiation_records(opportunity_id);
CREATE INDEX IF NOT EXISTS idx_agent_negotiation_records_state
    ON agent_negotiation_records(negotiation_state);

-- Pricing outcomes: final results per opportunity
CREATE TABLE IF NOT EXISTS agent_pricing_outcomes (
    id                TEXT PRIMARY KEY,
    opportunity_id    TEXT NOT NULL,
    quoted_price      DOUBLE PRECISION NOT NULL DEFAULT 0,
    accepted_price    DOUBLE PRECISION NOT NULL DEFAULT 0,
    won               BOOLEAN NOT NULL DEFAULT FALSE,
    notes             TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_pricing_outcomes_opportunity
    ON agent_pricing_outcomes(opportunity_id);
CREATE INDEX IF NOT EXISTS idx_agent_pricing_outcomes_won
    ON agent_pricing_outcomes(won);

-- Pricing performance: aggregated metrics per strategy
CREATE TABLE IF NOT EXISTS agent_pricing_performance (
    strategy_id       TEXT PRIMARY KEY,
    avg_quoted_price  DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_accepted_price DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_discount_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    win_rate          DOUBLE PRECISION NOT NULL DEFAULT 0,
    total_outcomes    INTEGER NOT NULL DEFAULT 0,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
