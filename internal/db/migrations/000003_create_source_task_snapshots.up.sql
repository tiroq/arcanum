CREATE TABLE source_task_snapshots (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_task_id    UUID NOT NULL REFERENCES source_tasks(id) ON DELETE CASCADE,
    snapshot_version  INT NOT NULL,
    content_hash      TEXT NOT NULL,
    raw_payload       JSONB NOT NULL DEFAULT '{}',
    snapshot_taken_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_source_task_snapshots_version UNIQUE (source_task_id, snapshot_version)
);

CREATE INDEX idx_snapshots_source_task_id ON source_task_snapshots(source_task_id);
CREATE INDEX idx_snapshots_taken_at ON source_task_snapshots(snapshot_taken_at DESC);
