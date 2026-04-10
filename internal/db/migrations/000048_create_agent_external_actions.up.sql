-- External Action Connectors (Iteration 45)
-- Two tables: actions and results.

CREATE TABLE IF NOT EXISTS agent_external_actions (
    id              TEXT PRIMARY KEY,
    opportunity_id  TEXT NOT NULL DEFAULT '',
    action_type     TEXT NOT NULL,
    payload         JSONB NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'created',
    connector_name  TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT NOT NULL,
    risk_level      TEXT NOT NULL DEFAULT 'low',
    review_reason   TEXT NOT NULL DEFAULT '',
    retry_count     INTEGER NOT NULL DEFAULT 0,
    max_retries     INTEGER NOT NULL DEFAULT 3,
    dry_run_completed BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_external_actions_idempotency
    ON agent_external_actions (idempotency_key);
CREATE INDEX IF NOT EXISTS idx_external_actions_status
    ON agent_external_actions (status);
CREATE INDEX IF NOT EXISTS idx_external_actions_created
    ON agent_external_actions (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_external_actions_opportunity
    ON agent_external_actions (opportunity_id)
    WHERE opportunity_id != '';

CREATE TABLE IF NOT EXISTS agent_external_action_results (
    id               TEXT PRIMARY KEY,
    action_id        TEXT NOT NULL REFERENCES agent_external_actions(id),
    success          BOOLEAN NOT NULL DEFAULT false,
    external_id      TEXT NOT NULL DEFAULT '',
    response_payload JSONB,
    error_message    TEXT NOT NULL DEFAULT '',
    mode             TEXT NOT NULL DEFAULT 'execute',
    duration_ms      BIGINT NOT NULL DEFAULT 0,
    executed_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_external_results_action
    ON agent_external_action_results (action_id);
CREATE INDEX IF NOT EXISTS idx_external_results_executed
    ON agent_external_action_results (executed_at DESC);
