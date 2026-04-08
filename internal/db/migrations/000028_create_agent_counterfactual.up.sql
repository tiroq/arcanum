-- Iteration 23: Counterfactual Simulation Layer

-- Simulation results: captures predictions for all simulated paths at decision time.
CREATE TABLE IF NOT EXISTS agent_counterfactual_simulations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    decision_id TEXT NOT NULL,
    goal_type TEXT NOT NULL,
    predictions JSONB NOT NULL DEFAULT '[]'::jsonb,
    prediction_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (decision_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_cf_simulations_goal_type
    ON agent_counterfactual_simulations (goal_type);
CREATE INDEX IF NOT EXISTS idx_agent_cf_simulations_created_at
    ON agent_counterfactual_simulations (created_at DESC);

-- Prediction outcomes: evaluated after execution, comparing predicted vs actual.
CREATE TABLE IF NOT EXISTS agent_counterfactual_prediction_outcomes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    decision_id TEXT NOT NULL,
    path_signature TEXT NOT NULL,
    goal_type TEXT NOT NULL,
    predicted_value DOUBLE PRECISION NOT NULL DEFAULT 0,
    actual_value DOUBLE PRECISION NOT NULL DEFAULT 0,
    absolute_error DOUBLE PRECISION NOT NULL DEFAULT 0,
    direction_correct BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (decision_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_cf_pred_outcomes_goal_type
    ON agent_counterfactual_prediction_outcomes (goal_type);
CREATE INDEX IF NOT EXISTS idx_agent_cf_pred_outcomes_created_at
    ON agent_counterfactual_prediction_outcomes (created_at DESC);

-- Prediction memory: accumulated prediction accuracy per path + goal.
CREATE TABLE IF NOT EXISTS agent_counterfactual_prediction_memory (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    path_signature TEXT NOT NULL,
    goal_type TEXT NOT NULL,
    total_predictions INTEGER NOT NULL DEFAULT 0,
    total_error DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_error DOUBLE PRECISION NOT NULL DEFAULT 0,
    direction_correct_count INTEGER NOT NULL DEFAULT 0,
    direction_accuracy DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_updated TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (path_signature, goal_type)
);

CREATE INDEX IF NOT EXISTS idx_agent_cf_pred_memory_goal_type
    ON agent_counterfactual_prediction_memory (goal_type);
