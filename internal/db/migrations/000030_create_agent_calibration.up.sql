-- Iteration 25: Self-Calibration Layer
-- Tracks predicted confidence vs actual outcomes for calibration.
-- Sliding window bounded to 500 records.

CREATE TABLE IF NOT EXISTS agent_calibration_records (
    id                   SERIAL PRIMARY KEY,
    decision_id          TEXT             NOT NULL,
    predicted_confidence DOUBLE PRECISION NOT NULL,
    actual_outcome       TEXT             NOT NULL,
    created_at           TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_calibration_records_created
    ON agent_calibration_records (created_at DESC);

-- Single-row summary table (UPSERT on id=1).
CREATE TABLE IF NOT EXISTS agent_calibration_summary (
    id                   INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    ece                  DOUBLE PRECISION NOT NULL DEFAULT 0,
    overconfidence_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    underconfidence_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    total_records        INT              NOT NULL DEFAULT 0,
    last_updated         TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);
