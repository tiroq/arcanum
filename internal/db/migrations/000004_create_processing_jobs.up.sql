CREATE TABLE processing_jobs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_task_id UUID NOT NULL REFERENCES source_tasks(id) ON DELETE CASCADE,
    job_type       TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'queued',
    priority       INT NOT NULL DEFAULT 0,
    dedupe_key     TEXT,
    attempt_count  INT NOT NULL DEFAULT 0,
    max_attempts   INT NOT NULL DEFAULT 3,
    payload        JSONB NOT NULL DEFAULT '{}',
    leased_at      TIMESTAMPTZ,
    lease_expiry   TIMESTAMPTZ,
    scheduled_at   TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Partial unique index for deduplication: only active (non-terminal) jobs are deduplicated.
CREATE UNIQUE INDEX uq_processing_jobs_dedupe
    ON processing_jobs(dedupe_key)
    WHERE dedupe_key IS NOT NULL AND status NOT IN ('succeeded', 'dead_letter');

CREATE INDEX idx_processing_jobs_status ON processing_jobs(status);
CREATE INDEX idx_processing_jobs_source_task_id ON processing_jobs(source_task_id);
CREATE INDEX idx_processing_jobs_scheduled_at ON processing_jobs(scheduled_at) WHERE scheduled_at IS NOT NULL;
CREATE INDEX idx_processing_jobs_priority_status ON processing_jobs(priority DESC, created_at ASC) WHERE status = 'queued';
