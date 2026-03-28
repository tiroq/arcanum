CREATE TABLE source_tasks (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_connection_id UUID NOT NULL REFERENCES source_connections(id) ON DELETE CASCADE,
    external_id          TEXT NOT NULL,
    title                TEXT NOT NULL,
    description          TEXT,
    raw_payload          JSONB NOT NULL DEFAULT '{}',
    content_hash         TEXT NOT NULL,
    status               TEXT NOT NULL DEFAULT 'active',
    priority             INT NOT NULL DEFAULT 0,
    due_at               TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_source_tasks_connection_external UNIQUE (source_connection_id, external_id)
);

CREATE INDEX idx_source_tasks_status ON source_tasks(status);
CREATE INDEX idx_source_tasks_connection_id ON source_tasks(source_connection_id);
CREATE INDEX idx_source_tasks_priority ON source_tasks(priority DESC);
