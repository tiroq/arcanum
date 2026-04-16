-- Goal planning evolution: plans, dependencies, subgoal strategy fields (Iteration 55)

CREATE TABLE IF NOT EXISTS agent_goal_plans (
    id               TEXT PRIMARY KEY,
    goal_id          TEXT        NOT NULL,
    version          INT         NOT NULL DEFAULT 1,
    horizon          TEXT        NOT NULL DEFAULT 'medium',
    strategy         TEXT        NOT NULL DEFAULT 'exploit_success_path',
    status           TEXT        NOT NULL DEFAULT 'draft',
    expected_utility DOUBLE PRECISION NOT NULL DEFAULT 0,
    risk_estimate    DOUBLE PRECISION NOT NULL DEFAULT 0,
    replan_count     INT         NOT NULL DEFAULT 0,
    last_replan_at   TIMESTAMPTZ NOT NULL DEFAULT '0001-01-01T00:00:00Z',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_agent_goal_plans_goal_id ON agent_goal_plans(goal_id);
CREATE INDEX IF NOT EXISTS idx_agent_goal_plans_status  ON agent_goal_plans(status);

CREATE TABLE IF NOT EXISTS agent_goal_dependencies (
    id              TEXT PRIMARY KEY,
    plan_id         TEXT        NOT NULL REFERENCES agent_goal_plans(id),
    from_subgoal_id TEXT        NOT NULL,
    to_subgoal_id   TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_agent_goal_deps_plan ON agent_goal_dependencies(plan_id);

-- Add new columns to agent_subgoals for plan linking and strategy tracking.
ALTER TABLE agent_subgoals ADD COLUMN IF NOT EXISTS plan_id       TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_subgoals ADD COLUMN IF NOT EXISTS strategy      TEXT NOT NULL DEFAULT 'exploit_success_path';
ALTER TABLE agent_subgoals ADD COLUMN IF NOT EXISTS failure_count INT  NOT NULL DEFAULT 0;
ALTER TABLE agent_subgoals ADD COLUMN IF NOT EXISTS success_count INT  NOT NULL DEFAULT 0;
