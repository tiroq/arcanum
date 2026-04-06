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

// Known job types that the worker can process.
var KnownJobTypes = map[string]bool{
	"llm_rewrite":    true,
	"llm_routing":    true,
	"rules_classify": true,
	"composite":      true,
}

// IsKnownJobType reports whether the given job type is known to the system.
func IsKnownJobType(jobType string) bool {
	return KnownJobTypes[jobType]
}

// SourceConnection represents an integration source (e.g. Google Tasks, Jira).
type SourceConnection struct {
	ID           uuid.UUID       `db:"id" json:"id"`
	Name         string          `db:"name" json:"name"`
	Provider     string          `db:"provider" json:"provider"`
	Config       json.RawMessage `db:"config" json:"config"`
	Enabled      bool            `db:"enabled" json:"enabled"`
	LastSyncedAt *time.Time      `db:"last_synced_at" json:"last_synced_at,omitempty"`
	CreatedAt    time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time       `db:"updated_at" json:"updated_at"`
}

// SourceTask represents an individual task fetched from a source connection.
type SourceTask struct {
	ID                 uuid.UUID       `db:"id" json:"id"`
	SourceConnectionID uuid.UUID       `db:"source_connection_id" json:"source_connection_id"`
	ExternalID         string          `db:"external_id" json:"external_id"`
	Title              string          `db:"title" json:"title"`
	Description        *string         `db:"description" json:"description,omitempty"`
	RawPayload         json.RawMessage `db:"raw_payload" json:"raw_payload"`
	ContentHash        string          `db:"content_hash" json:"content_hash"`
	Status             string          `db:"status" json:"status"`
	Priority           int             `db:"priority" json:"priority"`
	DueAt              *time.Time      `db:"due_at" json:"due_at,omitempty"`
	CreatedAt          time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt          time.Time       `db:"updated_at" json:"updated_at"`
}

// SourceTaskSnapshot captures point-in-time snapshots of a source task for change detection.
type SourceTaskSnapshot struct {
	ID              uuid.UUID       `db:"id" json:"id"`
	SourceTaskID    uuid.UUID       `db:"source_task_id" json:"source_task_id"`
	SnapshotVersion int             `db:"snapshot_version" json:"snapshot_version"`
	ContentHash     string          `db:"content_hash" json:"content_hash"`
	RawPayload      json.RawMessage `db:"raw_payload" json:"raw_payload"`
	SnapshotTakenAt time.Time       `db:"snapshot_taken_at" json:"snapshot_taken_at"`
}

// ProcessingJob represents a unit of work to be executed by the worker service.
type ProcessingJob struct {
	ID               uuid.UUID       `db:"id" json:"id"`
	SourceTaskID     uuid.UUID       `db:"source_task_id" json:"source_task_id"`
	JobType          string          `db:"job_type" json:"job_type"`
	Status           string          `db:"status" json:"status"`
	Priority         int             `db:"priority" json:"priority"`
	DedupeKey        *string         `db:"dedupe_key" json:"dedupe_key,omitempty"`
	AttemptCount     int             `db:"attempt_count" json:"attempt_count"`
	MaxAttempts      int             `db:"max_attempts" json:"max_attempts"`
	Payload          json.RawMessage `db:"payload" json:"payload"`
	ErrorCode        *string         `db:"error_code" json:"error_code,omitempty"`
	ErrorMessage     *string         `db:"error_message" json:"error_message,omitempty"`
	LeasedAt         *time.Time      `db:"leased_at" json:"leased_at,omitempty"`
	LeaseExpiry      *time.Time      `db:"lease_expiry" json:"lease_expiry,omitempty"`
	LeasedByWorkerID *string         `db:"leased_by_worker_id" json:"leased_by_worker_id,omitempty"`
	ScheduledAt      *time.Time      `db:"scheduled_at" json:"scheduled_at,omitempty"`
	CreatedAt        time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt        time.Time       `db:"updated_at" json:"updated_at"`
}

// ProcessingRun records the result of a single execution attempt of a ProcessingJob.
type ProcessingRun struct {
	ID            uuid.UUID       `db:"id" json:"id"`
	JobID         uuid.UUID       `db:"job_id" json:"job_id"`
	AttemptNumber int             `db:"attempt_number" json:"attempt_number"`
	Outcome       string          `db:"outcome" json:"outcome"`
	StartedAt     time.Time       `db:"started_at" json:"started_at"`
	FinishedAt    *time.Time      `db:"finished_at" json:"finished_at,omitempty"`
	DurationMs    *int64          `db:"duration_ms" json:"duration_ms,omitempty"`
	ErrorMessage  *string         `db:"error_message" json:"error_message,omitempty"`
	ResultPayload json.RawMessage `db:"result_payload" json:"result_payload"`
	WorkerID      *string         `db:"worker_id" json:"worker_id,omitempty"`
}

// SuggestionProposal represents an AI-generated suggestion awaiting approval.
type SuggestionProposal struct {
	ID                  uuid.UUID       `db:"id" json:"id"`
	SourceTaskID        uuid.UUID       `db:"source_task_id" json:"source_task_id"`
	JobID               uuid.UUID       `db:"job_id" json:"job_id"`
	ProposalType        string          `db:"proposal_type" json:"proposal_type"`
	ApprovalStatus      string          `db:"approval_status" json:"approval_status"`
	HumanReviewRequired bool            `db:"human_review_required" json:"human_review_required"`
	ProposalPayload     json.RawMessage `db:"proposal_payload" json:"proposal_payload"`
	ApprovedBy          *string         `db:"approved_by" json:"approved_by,omitempty"`
	AutoApproved        bool            `db:"auto_approved" json:"auto_approved"`
	ReviewedAt          *time.Time      `db:"reviewed_at" json:"reviewed_at,omitempty"`
	CreatedAt           time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt           time.Time       `db:"updated_at" json:"updated_at"`
}

// WritebackOperation represents the execution of an approved proposal back to the source.
type WritebackOperation struct {
	ID              uuid.UUID       `db:"id" json:"id"`
	ProposalID      uuid.UUID       `db:"proposal_id" json:"proposal_id"`
	SourceTaskID    uuid.UUID       `db:"source_task_id" json:"source_task_id"`
	OperationType   string          `db:"operation_type" json:"operation_type"`
	Status          string          `db:"status" json:"status"`
	RequestPayload  json.RawMessage `db:"request_payload" json:"request_payload"`
	ResponsePayload json.RawMessage `db:"response_payload" json:"response_payload"`
	Verified        bool            `db:"verified" json:"verified"`
	ErrorCode       *string         `db:"error_code" json:"error_code,omitempty"`
	ErrorMessage    *string         `db:"error_message" json:"error_message,omitempty"`
	ExecutedAt      *time.Time      `db:"executed_at" json:"executed_at,omitempty"`
	CompletedAt     *time.Time      `db:"completed_at" json:"completed_at,omitempty"`
	CreatedAt       time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time       `db:"updated_at" json:"updated_at"`
}

// AuditEvent is an append-only record of significant platform events.
type AuditEvent struct {
	ID         uuid.UUID       `db:"id" json:"id"`
	EntityType string          `db:"entity_type" json:"entity_type"`
	EntityID   uuid.UUID       `db:"entity_id" json:"entity_id"`
	EventType  string          `db:"event_type" json:"event_type"`
	ActorType  string          `db:"actor_type" json:"actor_type"`
	ActorID    string          `db:"actor_id" json:"actor_id"`
	Payload    json.RawMessage `db:"payload" json:"payload"`
	OccurredAt time.Time       `db:"occurred_at" json:"occurred_at"`
}
