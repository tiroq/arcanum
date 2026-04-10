-- Iteration 37: Signal Ingestion Layer
-- Creates tables for raw events, normalized signals, and derived state.

CREATE TABLE IF NOT EXISTS agent_raw_events (
    id          TEXT PRIMARY KEY,
    source      TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}',
    observed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agent_raw_events_source ON agent_raw_events (source);
CREATE INDEX IF NOT EXISTS idx_agent_raw_events_event_type ON agent_raw_events (event_type);
CREATE INDEX IF NOT EXISTS idx_agent_raw_events_observed_at ON agent_raw_events (observed_at DESC);

CREATE TABLE IF NOT EXISTS agent_signals (
    id            TEXT PRIMARY KEY,
    signal_type   TEXT NOT NULL,
    severity      TEXT NOT NULL DEFAULT 'low',
    confidence    DOUBLE PRECISION NOT NULL DEFAULT 0,
    value         DOUBLE PRECISION NOT NULL DEFAULT 0,
    source        TEXT NOT NULL DEFAULT '',
    context_tags  TEXT[] NOT NULL DEFAULT '{}',
    observed_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    raw_event_id  TEXT REFERENCES agent_raw_events(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agent_signals_signal_type ON agent_signals (signal_type);
CREATE INDEX IF NOT EXISTS idx_agent_signals_severity ON agent_signals (severity);
CREATE INDEX IF NOT EXISTS idx_agent_signals_observed_at ON agent_signals (observed_at DESC);

CREATE TABLE IF NOT EXISTS agent_derived_state (
    key        TEXT PRIMARY KEY,
    value      DOUBLE PRECISION NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
