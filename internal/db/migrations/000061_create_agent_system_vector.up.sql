CREATE TABLE IF NOT EXISTS agent_system_vector (
    id TEXT PRIMARY KEY CHECK (id = 'current'),
    income_priority DOUBLE PRECISION NOT NULL DEFAULT 0.70,
    family_safety_priority DOUBLE PRECISION NOT NULL DEFAULT 1.00,
    infra_priority DOUBLE PRECISION NOT NULL DEFAULT 0.30,
    automation_priority DOUBLE PRECISION NOT NULL DEFAULT 0.40,
    exploration_level DOUBLE PRECISION NOT NULL DEFAULT 0.30,
    risk_tolerance DOUBLE PRECISION NOT NULL DEFAULT 0.30,
    human_review_strictness DOUBLE PRECISION NOT NULL DEFAULT 0.80,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed the default vector
INSERT INTO agent_system_vector (id) VALUES ('current') ON CONFLICT DO NOTHING;
