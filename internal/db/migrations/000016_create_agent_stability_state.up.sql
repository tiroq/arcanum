CREATE TABLE IF NOT EXISTS agent_stability_state (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    mode                 TEXT NOT NULL DEFAULT 'normal',
    throttle_multiplier  DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    blocked_action_types JSONB NOT NULL DEFAULT '[]',
    reason               TEXT NOT NULL DEFAULT '',
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed the single current-state row.
INSERT INTO agent_stability_state (id, mode, throttle_multiplier, blocked_action_types, reason, updated_at)
VALUES (gen_random_uuid(), 'normal', 1.0, '[]', 'initial', now());
