-- Agent Foundation Layer: append-only event journal, working state, and episodic memory.

-- agent_events is the immutable event journal for the autonomous agent.
-- Every significant action in the system produces an entry here.
-- Events are correlated by correlation_id (=job_id) and form a causal
-- chain via causation_id (= event_id of the preceding event).
CREATE TABLE agent_events (
    event_id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type     TEXT NOT NULL,
    source         TEXT NOT NULL,
    timestamp      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    correlation_id UUID,
    causation_id   UUID REFERENCES agent_events(event_id),
    priority       INT NOT NULL DEFAULT 0,
    confidence     DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    payload        JSONB NOT NULL DEFAULT '{}',
    tags           TEXT[] NOT NULL DEFAULT '{}'
);

CREATE INDEX idx_agent_events_correlation_id ON agent_events(correlation_id)
    WHERE correlation_id IS NOT NULL;
CREATE INDEX idx_agent_events_timestamp ON agent_events(timestamp);

-- agent_state is a single-row working state for the agent.
-- Updated via optimistic locking (state_version) on every significant event.
CREATE TABLE agent_state (
    id                      INT PRIMARY KEY DEFAULT 1,
    state_version           BIGINT NOT NULL DEFAULT 0,
    last_event_id           UUID REFERENCES agent_events(event_id),
    active_jobs             INT NOT NULL DEFAULT 0,
    total_jobs_processed    BIGINT NOT NULL DEFAULT 0,
    total_proposals_created BIGINT NOT NULL DEFAULT 0,
    last_updated            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    snapshot                JSONB NOT NULL DEFAULT '{}'
);

-- Seed the single state row so ApplyEvent can always UPDATE without INSERT.
INSERT INTO agent_state (id) VALUES (1) ON CONFLICT DO NOTHING;

-- agent_memory_episodic stores derived significant moments from the event journal.
-- A memory entry is created only when an event's salience exceeds the threshold.
-- All memory rows reference a concrete event_id — no orphan memories.
CREATE TABLE agent_memory_episodic (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id       UUID NOT NULL REFERENCES agent_events(event_id),
    correlation_id UUID,
    summary        TEXT NOT NULL,
    salience       DOUBLE PRECISION NOT NULL DEFAULT 0.5,
    payload        JSONB NOT NULL DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_memory_correlation_id ON agent_memory_episodic(correlation_id)
    WHERE correlation_id IS NOT NULL;
CREATE INDEX idx_agent_memory_salience ON agent_memory_episodic(salience DESC);
