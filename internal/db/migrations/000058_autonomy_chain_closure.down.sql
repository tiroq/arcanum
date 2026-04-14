-- Iteration 54.5/55A: Reverse autonomy chain closure

DROP TABLE IF EXISTS agent_execution_feedback;

ALTER TABLE agent_orchestrated_tasks
    DROP COLUMN IF EXISTS actuation_decision_id,
    DROP COLUMN IF EXISTS execution_task_id,
    DROP COLUMN IF EXISTS outcome_type,
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS attempt_count,
    DROP COLUMN IF EXISTS completed_at;
