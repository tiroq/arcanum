-- Iteration 43: Controlled Self-Extension Sandbox

-- Component proposals
CREATE TABLE IF NOT EXISTS agent_self_proposals (
    id                   TEXT PRIMARY KEY,
    title                TEXT NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    source               TEXT NOT NULL,
    goal_alignment_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    expected_value       DOUBLE PRECISION NOT NULL DEFAULT 0,
    estimated_effort     DOUBLE PRECISION NOT NULL DEFAULT 0,
    status               TEXT NOT NULL DEFAULT 'proposed',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_self_proposals_status
    ON agent_self_proposals(status);
CREATE INDEX IF NOT EXISTS idx_self_proposals_source
    ON agent_self_proposals(source);
CREATE INDEX IF NOT EXISTS idx_self_proposals_created
    ON agent_self_proposals(created_at DESC);

-- Component specs
CREATE TABLE IF NOT EXISTS agent_self_specs (
    id                TEXT PRIMARY KEY,
    proposal_id       TEXT NOT NULL REFERENCES agent_self_proposals(id),
    input_contract    TEXT NOT NULL DEFAULT '',
    output_contract   TEXT NOT NULL DEFAULT '',
    dependencies      JSONB NOT NULL DEFAULT '[]',
    constraints       JSONB NOT NULL DEFAULT '[]',
    test_requirements JSONB NOT NULL DEFAULT '[]',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_self_specs_proposal
    ON agent_self_specs(proposal_id);

-- Sandbox runs
CREATE TABLE IF NOT EXISTS agent_self_sandbox_runs (
    id               TEXT PRIMARY KEY,
    proposal_id      TEXT NOT NULL REFERENCES agent_self_proposals(id),
    version          INTEGER NOT NULL,
    execution_result TEXT NOT NULL,
    test_results     JSONB NOT NULL DEFAULT '[]',
    logs             TEXT NOT NULL DEFAULT '',
    metrics          JSONB NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_self_sandbox_runs_proposal
    ON agent_self_sandbox_runs(proposal_id, version DESC);

-- Deployment records
CREATE TABLE IF NOT EXISTS agent_self_deployments (
    id                 TEXT PRIMARY KEY,
    proposal_id        TEXT NOT NULL REFERENCES agent_self_proposals(id),
    version            INTEGER NOT NULL,
    status             TEXT NOT NULL DEFAULT 'active',
    deployed_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    rollback_available BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_self_deployments_proposal
    ON agent_self_deployments(proposal_id, deployed_at DESC);
CREATE INDEX IF NOT EXISTS idx_self_deployments_status
    ON agent_self_deployments(status);

-- Rollback points
CREATE TABLE IF NOT EXISTS agent_self_rollback_points (
    id               TEXT PRIMARY KEY,
    deployment_id    TEXT NOT NULL REFERENCES agent_self_deployments(id),
    previous_version INTEGER NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_self_rollback_points_deployment
    ON agent_self_rollback_points(deployment_id);

-- Code artifacts (versioned, append-only)
CREATE TABLE IF NOT EXISTS agent_self_code_artifacts (
    id          TEXT PRIMARY KEY,
    proposal_id TEXT NOT NULL REFERENCES agent_self_proposals(id),
    version     INTEGER NOT NULL,
    language    TEXT NOT NULL DEFAULT 'go',
    source      TEXT NOT NULL DEFAULT 'stub',
    content     TEXT NOT NULL,
    checksum    TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_self_code_artifacts_proposal
    ON agent_self_code_artifacts(proposal_id, version DESC);

-- Approval decisions
CREATE TABLE IF NOT EXISTS agent_self_approvals (
    proposal_id   TEXT NOT NULL REFERENCES agent_self_proposals(id),
    approved_by   TEXT NOT NULL,
    approval_type TEXT NOT NULL DEFAULT 'manual',
    approved      BOOLEAN NOT NULL,
    reason        TEXT NOT NULL DEFAULT '',
    decided_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_self_approvals_proposal
    ON agent_self_approvals(proposal_id, decided_at DESC);
