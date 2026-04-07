CREATE TABLE IF NOT EXISTS agent_planning_decisions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cycle_id        TEXT NOT NULL,
    goal_id         TEXT NOT NULL,
    goal_type       TEXT NOT NULL,
    selected_action TEXT NOT NULL,
    explanation     TEXT NOT NULL DEFAULT '',
    candidates      JSONB NOT NULL DEFAULT '[]',
    planned_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_agent_planning_decisions_cycle_id ON agent_planning_decisions (cycle_id);
CREATE INDEX idx_agent_planning_decisions_planned_at ON agent_planning_decisions (planned_at DESC);
CREATE INDEX idx_agent_planning_decisions_goal_type ON agent_planning_decisions (goal_type);
CREATE INDEX idx_agent_planning_decisions_selected_action ON agent_planning_decisions (selected_action);
