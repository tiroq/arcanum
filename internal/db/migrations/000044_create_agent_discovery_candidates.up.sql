-- Iteration 40: Opportunity Discovery Engine

CREATE TABLE IF NOT EXISTS agent_discovery_candidates (
    id               TEXT PRIMARY KEY,
    candidate_type   TEXT NOT NULL,
    source_type      TEXT NOT NULL,
    source_refs      JSONB NOT NULL DEFAULT '[]',
    title            TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    confidence       DOUBLE PRECISION NOT NULL DEFAULT 0,
    estimated_value  DOUBLE PRECISION NOT NULL DEFAULT 0,
    estimated_effort DOUBLE PRECISION NOT NULL DEFAULT 0,
    dedupe_key       TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'new',
    evidence_count   INTEGER NOT NULL DEFAULT 1,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_discovery_candidates_dedupe
    ON agent_discovery_candidates(dedupe_key, candidate_type);
CREATE INDEX IF NOT EXISTS idx_discovery_candidates_status
    ON agent_discovery_candidates(status);
CREATE INDEX IF NOT EXISTS idx_discovery_candidates_created
    ON agent_discovery_candidates(created_at DESC);
