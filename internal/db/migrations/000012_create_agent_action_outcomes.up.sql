CREATE TABLE agent_action_outcomes (
    id              UUID PRIMARY KEY,
    action_id       UUID NOT NULL,
    goal_id         TEXT NOT NULL,
    action_type     TEXT NOT NULL,
    target_type     TEXT NOT NULL,
    target_id       UUID NOT NULL,
    outcome_status  TEXT NOT NULL CHECK (outcome_status IN ('success', 'neutral', 'failure')),
    effect_detected BOOLEAN NOT NULL DEFAULT FALSE,
    improvement     BOOLEAN NOT NULL DEFAULT FALSE,
    before_state    JSONB NOT NULL DEFAULT '{}',
    after_state     JSONB NOT NULL DEFAULT '{}',
    evaluated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_action_outcomes_action_id ON agent_action_outcomes(action_id);
CREATE INDEX idx_agent_action_outcomes_target_id ON agent_action_outcomes(target_id);
