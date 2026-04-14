-- Iteration 54.5/55A: Autonomy chain closure
-- Adds linkage between actuation decisions and orchestrated tasks,
-- execution outcome tracking on orchestrated tasks,
-- and structured execution feedback for reflection/objective.

-- 1. Add actuation→task linkage and execution outcome fields to orchestrated tasks.
ALTER TABLE agent_orchestrated_tasks
    ADD COLUMN IF NOT EXISTS actuation_decision_id TEXT,
    ADD COLUMN IF NOT EXISTS execution_task_id TEXT,
    ADD COLUMN IF NOT EXISTS outcome_type TEXT DEFAULT '',
    ADD COLUMN IF NOT EXISTS last_error TEXT DEFAULT '',
    ADD COLUMN IF NOT EXISTS attempt_count INT DEFAULT 0,
    ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_orchestrated_tasks_actuation_decision
    ON agent_orchestrated_tasks(actuation_decision_id)
    WHERE actuation_decision_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_orchestrated_tasks_execution_task
    ON agent_orchestrated_tasks(execution_task_id)
    WHERE execution_task_id IS NOT NULL;

-- 2. Structured execution feedback table.
-- Stores semantic execution outcomes for consumption by reflection and objective.
CREATE TABLE IF NOT EXISTS agent_execution_feedback (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL,
    execution_task_id TEXT NOT NULL,
    outcome_type TEXT NOT NULL,           -- completed, failed, aborted, blocked
    success BOOLEAN NOT NULL DEFAULT false,
    steps_executed INT NOT NULL DEFAULT 0,
    steps_failed INT NOT NULL DEFAULT 0,
    error_summary TEXT NOT NULL DEFAULT '',
    semantic_signal TEXT NOT NULL DEFAULT '',  -- e.g. "safe_action_succeeded", "repeated_failure", "blocked_by_review"
    source_decision_type TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_execution_feedback_created
    ON agent_execution_feedback(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_execution_feedback_outcome
    ON agent_execution_feedback(outcome_type);

CREATE INDEX IF NOT EXISTS idx_execution_feedback_signal
    ON agent_execution_feedback(semantic_signal);
