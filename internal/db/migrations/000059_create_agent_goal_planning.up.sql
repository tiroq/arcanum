-- Goal Planning: subgoals decomposed from strategic goals + progress tracking
CREATE TABLE IF NOT EXISTS agent_subgoals (
    id              TEXT PRIMARY KEY,
    goal_id         TEXT        NOT NULL,
    title           TEXT        NOT NULL,
    description     TEXT        NOT NULL DEFAULT '',
    status          TEXT        NOT NULL DEFAULT 'not_started',
    progress_score  DOUBLE PRECISION NOT NULL DEFAULT 0,
    target_metric   TEXT        NOT NULL DEFAULT '',
    target_value    DOUBLE PRECISION NOT NULL DEFAULT 0,
    current_value   DOUBLE PRECISION NOT NULL DEFAULT 0,
    preferred_action TEXT       NOT NULL DEFAULT '',
    horizon         TEXT        NOT NULL DEFAULT 'weekly',
    priority        DOUBLE PRECISION NOT NULL DEFAULT 0,
    depends_on      TEXT        NOT NULL DEFAULT '',
    block_reason    TEXT        NOT NULL DEFAULT '',
    last_task_emitted TIMESTAMPTZ NOT NULL DEFAULT '0001-01-01T00:00:00Z',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_subgoals_goal_id ON agent_subgoals(goal_id);
CREATE INDEX IF NOT EXISTS idx_agent_subgoals_status  ON agent_subgoals(status);

CREATE TABLE IF NOT EXISTS agent_goal_progress (
    id           TEXT PRIMARY KEY,
    subgoal_id   TEXT        NOT NULL REFERENCES agent_subgoals(id),
    goal_id      TEXT        NOT NULL,
    metric_name  TEXT        NOT NULL DEFAULT '',
    metric_value DOUBLE PRECISION NOT NULL DEFAULT 0,
    progress_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
    measured_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_goal_progress_subgoal ON agent_goal_progress(subgoal_id);
CREATE INDEX IF NOT EXISTS idx_agent_goal_progress_goal    ON agent_goal_progress(goal_id);
