-- Rollback migration 000057: Multi-Task Orchestration + Priority Queue (Iteration 54)

DROP TABLE IF EXISTS agent_task_queue;
DROP TABLE IF EXISTS agent_orchestrated_tasks;
