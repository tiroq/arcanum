CREATE TABLE suggestion_proposals (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_task_id        UUID NOT NULL REFERENCES source_tasks(id) ON DELETE CASCADE,
    job_id                UUID NOT NULL REFERENCES processing_jobs(id) ON DELETE CASCADE,
    proposal_type         TEXT NOT NULL,
    approval_status       TEXT NOT NULL DEFAULT 'pending',
    human_review_required BOOLEAN NOT NULL DEFAULT false,
    proposal_payload      JSONB NOT NULL DEFAULT '{}',
    approved_by           TEXT,
    auto_approved         BOOLEAN NOT NULL DEFAULT false,
    reviewed_at           TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_proposals_source_task_id ON suggestion_proposals(source_task_id);
CREATE INDEX idx_proposals_job_id ON suggestion_proposals(job_id);
CREATE INDEX idx_proposals_approval_status ON suggestion_proposals(approval_status);
CREATE INDEX idx_proposals_pending ON suggestion_proposals(created_at ASC) WHERE approval_status = 'pending';
