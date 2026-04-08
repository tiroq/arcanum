-- Iteration 22: Comparative Path Selection Learning

-- Decision snapshots: captures all candidate paths and scores at selection time.
CREATE TABLE IF NOT EXISTS agent_path_decision_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    decision_id TEXT NOT NULL,
    goal_type TEXT NOT NULL,
    selected_path TEXT NOT NULL,
    selected_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    candidates JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (decision_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_path_decision_snapshots_goal_type
    ON agent_path_decision_snapshots (goal_type);
CREATE INDEX IF NOT EXISTS idx_agent_path_decision_snapshots_created_at
    ON agent_path_decision_snapshots (created_at DESC);

-- Comparative outcomes: evaluated after outcome is known.
CREATE TABLE IF NOT EXISTS agent_path_comparative_outcomes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    decision_id TEXT NOT NULL,
    goal_type TEXT NOT NULL,
    selected_path TEXT NOT NULL,
    selected_outcome TEXT NOT NULL,
    ranking_error BOOLEAN NOT NULL DEFAULT FALSE,
    overestimated BOOLEAN NOT NULL DEFAULT FALSE,
    underestimated BOOLEAN NOT NULL DEFAULT FALSE,
    better_alternative_exists BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (decision_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_path_comparative_outcomes_goal_type
    ON agent_path_comparative_outcomes (goal_type);
CREATE INDEX IF NOT EXISTS idx_agent_path_comparative_outcomes_created_at
    ON agent_path_comparative_outcomes (created_at DESC);

-- Comparative memory: accumulated win/loss/miss per path signature + goal type.
CREATE TABLE IF NOT EXISTS agent_path_comparative_memory (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    path_signature TEXT NOT NULL,
    goal_type TEXT NOT NULL,
    selection_count INTEGER NOT NULL DEFAULT 0,
    win_count INTEGER NOT NULL DEFAULT 0,
    loss_count INTEGER NOT NULL DEFAULT 0,
    missed_win_count INTEGER NOT NULL DEFAULT 0,
    win_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    loss_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_updated TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (path_signature, goal_type)
);

CREATE INDEX IF NOT EXISTS idx_agent_path_comparative_memory_goal_type
    ON agent_path_comparative_memory (goal_type);
