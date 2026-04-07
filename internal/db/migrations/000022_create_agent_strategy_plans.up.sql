CREATE TABLE agent_strategy_plans (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    goal_id           TEXT NOT NULL,
    goal_type         TEXT NOT NULL,
    strategy_type     TEXT NOT NULL,
    steps             JSONB NOT NULL DEFAULT '[]',
    expected_utility  DOUBLE PRECISION NOT NULL DEFAULT 0,
    risk_score        DOUBLE PRECISION NOT NULL DEFAULT 0,
    confidence        DOUBLE PRECISION NOT NULL DEFAULT 0,
    explanation       TEXT NOT NULL DEFAULT '',
    exploratory       BOOLEAN NOT NULL DEFAULT false,
    selected          BOOLEAN NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_strategy_plans_goal ON agent_strategy_plans(goal_id);
CREATE INDEX idx_agent_strategy_plans_selected ON agent_strategy_plans(selected) WHERE selected = true;
CREATE INDEX idx_agent_strategy_plans_created ON agent_strategy_plans(created_at DESC);
CREATE INDEX idx_agent_strategy_plans_type ON agent_strategy_plans(strategy_type);
