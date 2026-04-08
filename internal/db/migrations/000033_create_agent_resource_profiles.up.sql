-- Iteration 29: Resource / Cost / Latency-Aware Optimization Layer
-- Tracks resource profiles per mode+goal_type for cost/latency-aware decision making.

CREATE TABLE IF NOT EXISTS agent_resource_profiles (
    id                   SERIAL PRIMARY KEY,
    mode                 TEXT             NOT NULL,
    goal_type            TEXT             NOT NULL DEFAULT '',
    avg_latency_ms       DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_reasoning_depth  DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_path_length      DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_token_cost       DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_execution_cost   DOUBLE PRECISION NOT NULL DEFAULT 0,
    sample_count         INT              NOT NULL DEFAULT 0,
    last_updated         TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_resource_profiles_mode_goal
    ON agent_resource_profiles (mode, goal_type);

CREATE INDEX IF NOT EXISTS idx_resource_profiles_mode
    ON agent_resource_profiles (mode);
