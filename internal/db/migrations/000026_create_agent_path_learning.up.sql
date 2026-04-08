-- Iteration 21: Path Memory + Transition Learning

-- Path memory: aggregated success/failure rates by path signature + goal type.
CREATE TABLE IF NOT EXISTS agent_path_memory (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    path_signature TEXT NOT NULL,
    goal_type TEXT NOT NULL,
    total_runs INTEGER NOT NULL DEFAULT 0,
    success_runs INTEGER NOT NULL DEFAULT 0,
    failure_runs INTEGER NOT NULL DEFAULT 0,
    neutral_runs INTEGER NOT NULL DEFAULT 0,
    success_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    failure_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_updated TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (path_signature, goal_type)
);

-- Transition memory: aggregated helpful/unhelpful rates by transition key + goal type.
CREATE TABLE IF NOT EXISTS agent_transition_memory (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    goal_type TEXT NOT NULL,
    from_action_type TEXT NOT NULL,
    to_action_type TEXT NOT NULL,
    transition_key TEXT NOT NULL,
    total_uses INTEGER NOT NULL DEFAULT 0,
    helpful_uses INTEGER NOT NULL DEFAULT 0,
    unhelpful_uses INTEGER NOT NULL DEFAULT 0,
    neutral_uses INTEGER NOT NULL DEFAULT 0,
    helpful_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    unhelpful_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_updated TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (goal_type, transition_key)
);

-- Path outcomes: individual evaluated path outcomes for audit trail.
CREATE TABLE IF NOT EXISTS agent_path_outcomes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    path_id TEXT NOT NULL,
    goal_type TEXT NOT NULL,
    path_signature TEXT NOT NULL,
    path_length INTEGER NOT NULL,
    first_step_action TEXT NOT NULL,
    first_step_status TEXT NOT NULL,
    continuation_used BOOLEAN NOT NULL DEFAULT FALSE,
    final_status TEXT NOT NULL,
    improvement BOOLEAN NOT NULL DEFAULT FALSE,
    evaluated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_path_outcomes_goal_type ON agent_path_outcomes (goal_type);
CREATE INDEX IF NOT EXISTS idx_agent_path_outcomes_path_signature ON agent_path_outcomes (path_signature);
CREATE INDEX IF NOT EXISTS idx_agent_path_outcomes_evaluated_at ON agent_path_outcomes (evaluated_at DESC);
