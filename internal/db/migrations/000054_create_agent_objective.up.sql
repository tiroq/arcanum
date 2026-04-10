-- Global Objective Function + Risk Model (Iteration 50)
-- Three single-row tables for objective state, risk state, and combined summary.

CREATE TABLE IF NOT EXISTS agent_objective_state (
    id                       TEXT PRIMARY KEY CHECK (id = 'current'),
    verified_income_score    DOUBLE PRECISION NOT NULL DEFAULT 0,
    income_growth_score      DOUBLE PRECISION NOT NULL DEFAULT 0,
    owner_relief_score       DOUBLE PRECISION NOT NULL DEFAULT 0,
    family_stability_score   DOUBLE PRECISION NOT NULL DEFAULT 0,
    strategy_quality_score   DOUBLE PRECISION NOT NULL DEFAULT 0,
    execution_readiness_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    computed_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS agent_risk_state (
    id                          TEXT PRIMARY KEY CHECK (id = 'current'),
    financial_instability_risk  DOUBLE PRECISION NOT NULL DEFAULT 0,
    overload_risk               DOUBLE PRECISION NOT NULL DEFAULT 0,
    execution_risk              DOUBLE PRECISION NOT NULL DEFAULT 0,
    strategy_concentration_risk DOUBLE PRECISION NOT NULL DEFAULT 0,
    pricing_confidence_risk     DOUBLE PRECISION NOT NULL DEFAULT 0,
    computed_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS agent_objective_summary (
    id                       TEXT PRIMARY KEY CHECK (id = 'current'),
    utility_score            DOUBLE PRECISION NOT NULL DEFAULT 0,
    risk_score               DOUBLE PRECISION NOT NULL DEFAULT 0,
    net_utility              DOUBLE PRECISION NOT NULL DEFAULT 0,
    dominant_positive_factor TEXT NOT NULL DEFAULT '',
    dominant_risk_factor     TEXT NOT NULL DEFAULT '',
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
