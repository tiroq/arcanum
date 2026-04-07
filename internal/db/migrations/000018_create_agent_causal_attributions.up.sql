CREATE TABLE agent_causal_attributions (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subject_type           TEXT NOT NULL,
    subject_id             UUID NOT NULL,
    hypothesis             TEXT NOT NULL,
    attribution            TEXT NOT NULL,
    confidence             DOUBLE PRECISION NOT NULL,
    evidence               JSONB NOT NULL DEFAULT '{}',
    competing_explanations JSONB NOT NULL DEFAULT '[]',
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_agent_causal_attributions_subject ON agent_causal_attributions (subject_type, subject_id);
CREATE INDEX idx_agent_causal_attributions_created ON agent_causal_attributions (created_at DESC);
