CREATE TABLE processing_runs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id         UUID NOT NULL REFERENCES processing_jobs(id) ON DELETE CASCADE,
    attempt_number INT NOT NULL,
    outcome        TEXT NOT NULL,
    started_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at    TIMESTAMPTZ,
    duration_ms    BIGINT,
    error_message  TEXT,
    result_payload JSONB,
    worker_id      TEXT
);

CREATE INDEX idx_processing_runs_job_id ON processing_runs(job_id);
CREATE INDEX idx_processing_runs_outcome ON processing_runs(outcome);
CREATE INDEX idx_processing_runs_started_at ON processing_runs(started_at DESC);
