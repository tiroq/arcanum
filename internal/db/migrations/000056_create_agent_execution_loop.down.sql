-- Rollback Iteration 53: Execution Loop Engine.

DROP INDEX IF EXISTS idx_execution_observations_timestamp;
DROP INDEX IF EXISTS idx_execution_observations_task_id;
DROP TABLE IF EXISTS agent_execution_observations;

DROP INDEX IF EXISTS idx_execution_tasks_created_at;
DROP INDEX IF EXISTS idx_execution_tasks_status;
DROP TABLE IF EXISTS agent_execution_tasks;
