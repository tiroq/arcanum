CREATE TABLE writeback_operations (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    proposal_id      UUID NOT NULL REFERENCES suggestion_proposals(id) ON DELETE CASCADE,
    source_task_id   UUID NOT NULL REFERENCES source_tasks(id) ON DELETE CASCADE,
    operation_type   TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'pending',
    request_payload  JSONB NOT NULL DEFAULT '{}',
    response_payload JSONB,
    verified         BOOLEAN NOT NULL DEFAULT false,
    error_code       TEXT,
    error_message    TEXT,
    executed_at      TIMESTAMPTZ,
    completed_at     TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_writeback_proposal_id ON writeback_operations(proposal_id);
CREATE INDEX idx_writeback_source_task_id ON writeback_operations(source_task_id);
CREATE INDEX idx_writeback_status ON writeback_operations(status);
