-- agent_policy_state: current active policy parameter values (key-value store).
CREATE TABLE IF NOT EXISTS agent_policy_state (
    key TEXT PRIMARY KEY,
    value DOUBLE PRECISION NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed with default scoring constants from scorer.go.
INSERT INTO agent_policy_state (key, value) VALUES
    ('feedbackAvoidPenalty',    0.40),
    ('feedbackPreferBoost',    0.25),
    ('highBacklogResyncPenalty', 0.30),
    ('highRetryBoost',         0.15),
    ('safetyPreferenceBoost',  0.05),
    ('noopBasePenalty',        0.20)
ON CONFLICT (key) DO NOTHING;

-- agent_policy_changes: audit trail for all proposed and applied changes.
CREATE TABLE IF NOT EXISTS agent_policy_changes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    parameter TEXT NOT NULL,
    old_value DOUBLE PRECISION NOT NULL,
    new_value DOUBLE PRECISION NOT NULL,
    reason TEXT NOT NULL,
    evidence JSONB NOT NULL DEFAULT '{}',
    applied BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    evaluated_at TIMESTAMPTZ,
    improvement_detected BOOLEAN
);

CREATE INDEX IF NOT EXISTS idx_policy_changes_parameter ON agent_policy_changes (parameter);
CREATE INDEX IF NOT EXISTS idx_policy_changes_created ON agent_policy_changes (created_at DESC);
