-- Reverse goal planning evolution (Iteration 55)

ALTER TABLE agent_subgoals DROP COLUMN IF EXISTS plan_id;
ALTER TABLE agent_subgoals DROP COLUMN IF EXISTS strategy;
ALTER TABLE agent_subgoals DROP COLUMN IF EXISTS failure_count;
ALTER TABLE agent_subgoals DROP COLUMN IF EXISTS success_count;

DROP TABLE IF EXISTS agent_goal_dependencies;
DROP TABLE IF EXISTS agent_goal_plans;
