-- Iteration 51: Closed Feedback Actuation layer.
-- Stores actuation decisions produced by the deterministic actuation engine.

CREATE TABLE IF NOT EXISTS agent_actuation_decisions (
    id              TEXT PRIMARY KEY,
    type            TEXT        NOT NULL,
    reason          TEXT        NOT NULL DEFAULT '',
    signal_source   TEXT        NOT NULL DEFAULT '',
    confidence      DOUBLE PRECISION NOT NULL DEFAULT 0,
    priority        DOUBLE PRECISION NOT NULL DEFAULT 0,
    requires_review BOOLEAN     NOT NULL DEFAULT FALSE,
    status          TEXT        NOT NULL DEFAULT 'proposed',
    target          TEXT        NOT NULL DEFAULT '',
    proposed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_actuation_decisions_status ON agent_actuation_decisions(status);
CREATE INDEX IF NOT EXISTS idx_actuation_decisions_proposed_at ON agent_actuation_decisions(proposed_at DESC);
