-- Iteration 46: Strategic Revenue Portfolio

-- Strategies: repeatable revenue patterns
CREATE TABLE IF NOT EXISTS agent_strategies (
    id                      TEXT PRIMARY KEY,
    name                    TEXT NOT NULL,
    type                    TEXT NOT NULL,
    expected_return_per_hour DOUBLE PRECISION NOT NULL DEFAULT 0,
    volatility              DOUBLE PRECISION NOT NULL DEFAULT 0,
    time_to_first_value     DOUBLE PRECISION NOT NULL DEFAULT 0,
    status                  TEXT NOT NULL DEFAULT 'active',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_strategies_status
    ON agent_strategies(status);
CREATE INDEX IF NOT EXISTS idx_agent_strategies_type
    ON agent_strategies(type);
CREATE INDEX IF NOT EXISTS idx_agent_strategies_created
    ON agent_strategies(created_at DESC);

-- Strategy allocations: capacity assigned per strategy (one row per strategy)
CREATE TABLE IF NOT EXISTS agent_strategy_allocations (
    id             TEXT PRIMARY KEY,
    strategy_id    TEXT NOT NULL UNIQUE REFERENCES agent_strategies(id),
    allocated_hours DOUBLE PRECISION NOT NULL DEFAULT 0,
    actual_hours   DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_strategy_allocations_strategy
    ON agent_strategy_allocations(strategy_id);

-- Strategy performance: tracked metrics per strategy (one row per strategy)
CREATE TABLE IF NOT EXISTS agent_strategy_performance (
    strategy_id     TEXT PRIMARY KEY REFERENCES agent_strategies(id),
    total_revenue   DOUBLE PRECISION NOT NULL DEFAULT 0,
    total_time_spent DOUBLE PRECISION NOT NULL DEFAULT 0,
    roi             DOUBLE PRECISION NOT NULL DEFAULT 0,
    conversion_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_strategy_performance_roi
    ON agent_strategy_performance(roi DESC);
