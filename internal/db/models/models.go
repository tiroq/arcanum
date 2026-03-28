package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ProcessingJob status constants
const (
	JobStatusQueued         = "queued"
	JobStatusLeased         = "leased"
	JobStatusRunning        = "running"
	JobStatusSucceeded      = "succeeded"
	JobStatusFailed         = "failed"
	JobStatusRetryScheduled = "retry_scheduled"
	JobStatusDeadLetter     = "dead_letter"
)

// ProcessingRun outcome constants
const (
	RunOutcomeSuccess = "success"
	RunOutcomeFailure = "failure"
	RunOutcomeError   = "error"
)

// SuggestionProposal approval status constants
const (
	ApprovalStatusPending  = "pending"
	ApprovalStatusApproved = "approved"
	ApprovalStatusRejected = "rejected"
)

// WritebackOperation status constants
const (
	WritebackStatusPending   = "pending"
	WritebackStatusExecuting = "executing"
	WritebackStatusCompleted = "completed"
	WritebackStatusFailed    = "failed"
	WritebackStatusVerified  = "verified"
)

// SourceConnection represents an integration source (e.g. Google Tasks, Jira).
type SourceConnection struct {
	ID           uuid.UUID       `db:"id"`
	Name         string          `db:"name"`
	Provider     string          `db:"provider"`
	Config       json.RawMessage `db:"config"`
	Enabled      bool            `db:"enabled"`
	LastSyncedAt *time.Time      `db:"last_synced_at"`
	CreatedAt    time.Time       `db:"created_at"`
	UpdatedAt    time.Time       `db:"updated_at"`
}

// SourceTask represents an individual task fetched from a source connection.
type SourceTask struct {
	ID                 uuid.UUID       `db:"id"`
	SourceConnectionID uuid.UUID       `db:"source_connection_id"`
	ExternalID         string          `db:"external_id"`
	Title              string          `db:"title"`
	Description        *string         `db:"description"`
	RawPayload         json.RawMessage `db:"raw_payload"`
	ContentHash        string          `db:"content_hash"`
	Status             string          `db:"status"`
	Priority           int             `db:"priority"`
	DueAt              *time.Time      `db:"due_at"`
	CreatedAt          time.Time       `db:"created_at"`
	UpdatedAt          time.Time       `db:"updated_at"`
}

// SourceTaskSnapshot captures point-in-time snapshots of a source task for change detection.
type SourceTaskSnapshot struct {
	ID              uuid.UUID       `db:"id"`
	SourceTaskID    uuid.UUID       `db:"source_task_id"`
	SnapshotVersion int             `db:"snapshot_version"`
	ContentHash     string          `db:"content_hash"`
	RawPayload      json.RawMessage `db:"raw_payload"`
	SnapshotTakenAt time.Time       `db:"snapshot_taken_at"`
}

// ProcessingJob represents a unit of work to be executed by the worker service.
type ProcessingJob struct {
	ID           uuid.UUID       `db:"id"`
	SourceTaskID uuid.UUID       `db:"source_task_id"`
	JobType      string          `db:"job_type"`
	Status       string          `db:"status"`
	Priority     int             `db:"priority"`
	DedupeKey    *string         `db:"dedupe_key"`
	AttemptCount int             `db:"attempt_count"`
	MaxAttempts  int             `db:"max_attempts"`
	Payload      json.RawMessage `db:"payload"`
	ErrorCode    *string         `db:"error_code"`
	ErrorMessage *string         `db:"error_message"`
	LeasedAt     *time.Time      `db:"leased_at"`
	LeaseExpiry  *time.Time      `db:"lease_expiry"`
	ScheduledAt  *time.Time      `db:"scheduled_at"`
	CreatedAt    time.Time       `db:"created_at"`
	UpdatedAt    time.Time       `db:"updated_at"`
}

// ProcessingRun records the result of a single execution attempt of a ProcessingJob.
type ProcessingRun struct {
	ID            uuid.UUID       `db:"id"`
	JobID         uuid.UUID       `db:"job_id"`
	AttemptNumber int             `db:"attempt_number"`
	Outcome       string          `db:"outcome"`
	StartedAt     time.Time       `db:"started_at"`
	FinishedAt    *time.Time      `db:"finished_at"`
	DurationMs    *int64          `db:"duration_ms"`
	ErrorMessage  *string         `db:"error_message"`
	ResultPayload json.RawMessage `db:"result_payload"`
	WorkerID      *string         `db:"worker_id"`
}

// SuggestionProposal represents an AI-generated suggestion awaiting approval.
type SuggestionProposal struct {
	ID                  uuid.UUID       `db:"id"`
	SourceTaskID        uuid.UUID       `db:"source_task_id"`
	JobID               uuid.UUID       `db:"job_id"`
	ProposalType        string          `db:"proposal_type"`
	ApprovalStatus      string          `db:"approval_status"`
	HumanReviewRequired bool            `db:"human_review_required"`
	ProposalPayload     json.RawMessage `db:"proposal_payload"`
	ApprovedBy          *string         `db:"approved_by"`
	AutoApproved        bool            `db:"auto_approved"`
	ReviewedAt          *time.Time      `db:"reviewed_at"`
	CreatedAt           time.Time       `db:"created_at"`
	UpdatedAt           time.Time       `db:"updated_at"`
}

// WritebackOperation represents the execution of an approved proposal back to the source.
type WritebackOperation struct {
	ID              uuid.UUID       `db:"id"`
	ProposalID      uuid.UUID       `db:"proposal_id"`
	SourceTaskID    uuid.UUID       `db:"source_task_id"`
	OperationType   string          `db:"operation_type"`
	Status          string          `db:"status"`
	RequestPayload  json.RawMessage `db:"request_payload"`
	ResponsePayload json.RawMessage `db:"response_payload"`
	Verified        bool            `db:"verified"`
	ErrorCode       *string         `db:"error_code"`
	ErrorMessage    *string         `db:"error_message"`
	ExecutedAt      *time.Time      `db:"executed_at"`
	CompletedAt     *time.Time      `db:"completed_at"`
	CreatedAt       time.Time       `db:"created_at"`
	UpdatedAt       time.Time       `db:"updated_at"`
}

// AuditEvent is an append-only record of significant platform events.
type AuditEvent struct {
	ID         uuid.UUID       `db:"id"`
	EntityType string          `db:"entity_type"`
	EntityID   uuid.UUID       `db:"entity_id"`
	EventType  string          `db:"event_type"`
	ActorType  string          `db:"actor_type"`
	ActorID    string          `db:"actor_id"`
	Payload    json.RawMessage `db:"payload"`
	OccurredAt time.Time       `db:"occurred_at"`
}
