-- Iteration 30: Human Override + Governance Layer
-- Single-row governance state + append-only action history + replay packs.

-- agent_governance_state: single-row table storing the current governance state.
-- Uses "id = 1" convention with ON CONFLICT to enforce single-row invariant.
CREATE TABLE IF NOT EXISTS agent_governance_state (
    id              INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    mode            TEXT NOT NULL DEFAULT 'normal',
    freeze_learning BOOLEAN NOT NULL DEFAULT false,
    freeze_policy   BOOLEAN NOT NULL DEFAULT false,
    freeze_exploration BOOLEAN NOT NULL DEFAULT false,
    force_reasoning_mode TEXT NOT NULL DEFAULT '',
    force_safe_mode BOOLEAN NOT NULL DEFAULT false,
    require_human_review BOOLEAN NOT NULL DEFAULT false,
    reason          TEXT NOT NULL DEFAULT '',
    last_updated    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed default row.
INSERT INTO agent_governance_state (id) VALUES (1)
    ON CONFLICT (id) DO NOTHING;

-- agent_governance_actions: append-only history of operator actions.
CREATE TABLE IF NOT EXISTS agent_governance_actions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    action_type  TEXT NOT NULL,
    requested_by TEXT NOT NULL,
    reason       TEXT NOT NULL DEFAULT '',
    payload      JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_governance_actions_created
    ON agent_governance_actions (created_at DESC);

-- agent_replay_packs: persisted decision replay/explanation packs.
CREATE TABLE IF NOT EXISTS agent_replay_packs (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    decision_id         TEXT NOT NULL,
    goal_type           TEXT NOT NULL DEFAULT '',
    selected_mode       TEXT NOT NULL DEFAULT '',
    selected_path       TEXT NOT NULL DEFAULT '',
    confidence          DOUBLE PRECISION NOT NULL DEFAULT 0,
    signals             JSONB NOT NULL DEFAULT '{}',
    arbitration_trace   JSONB NOT NULL DEFAULT '{}',
    calibration_info    JSONB NOT NULL DEFAULT '{}',
    comparative_info    JSONB NOT NULL DEFAULT '{}',
    counterfactual_info JSONB NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_replay_packs_decision_id
    ON agent_replay_packs (decision_id);

CREATE INDEX IF NOT EXISTS idx_replay_packs_created
    ON agent_replay_packs (created_at DESC);
