package events

import "time"

// SourceTaskDetectedEvent is published when a new source task is discovered.
type SourceTaskDetectedEvent struct {
	Version            string    `json:"version"`
	SourceTaskID       string    `json:"source_task_id"`
	SourceConnectionID string    `json:"source_connection_id"`
	ExternalID         string    `json:"external_id"`
	ChangeType         string    `json:"change_type"`
	DetectedAt         time.Time `json:"detected_at"`
}

func NewSourceTaskDetectedEvent(sourceTaskID, sourceConnectionID, externalID, changeType string, detectedAt time.Time) SourceTaskDetectedEvent {
	return SourceTaskDetectedEvent{
		Version:            "v1",
		SourceTaskID:       sourceTaskID,
		SourceConnectionID: sourceConnectionID,
		ExternalID:         externalID,
		ChangeType:         changeType,
		DetectedAt:         detectedAt,
	}
}

// SourceTaskChangedEvent is published when a source task's content changes.
type SourceTaskChangedEvent struct {
	Version      string    `json:"version"`
	SourceTaskID string    `json:"source_task_id"`
	PreviousHash string    `json:"previous_hash"`
	NewHash      string    `json:"new_hash"`
	ChangedAt    time.Time `json:"changed_at"`
}

func NewSourceTaskChangedEvent(sourceTaskID, previousHash, newHash string, changedAt time.Time) SourceTaskChangedEvent {
	return SourceTaskChangedEvent{
		Version:      "v1",
		SourceTaskID: sourceTaskID,
		PreviousHash: previousHash,
		NewHash:      newHash,
		ChangedAt:    changedAt,
	}
}

// JobCreatedEvent is published when a processing job is created.
type JobCreatedEvent struct {
	Version      string `json:"version"`
	JobID        string `json:"job_id"`
	SourceTaskID string `json:"source_task_id"`
	JobType      string `json:"job_type"`
	Priority     int    `json:"priority"`
	DedupeKey    string `json:"dedupe_key"`
}

func NewJobCreatedEvent(jobID, sourceTaskID, jobType string, priority int, dedupeKey string) JobCreatedEvent {
	return JobCreatedEvent{
		Version:      "v1",
		JobID:        jobID,
		SourceTaskID: sourceTaskID,
		JobType:      jobType,
		Priority:     priority,
		DedupeKey:    dedupeKey,
	}
}

// JobRetryEvent is published when a processing job is scheduled for retry.
type JobRetryEvent struct {
	Version      string    `json:"version"`
	JobID        string    `json:"job_id"`
	AttemptCount int       `json:"attempt_count"`
	Reason       string    `json:"reason"`
	RetryAt      time.Time `json:"retry_at"`
}

func NewJobRetryEvent(jobID string, attemptCount int, reason string, retryAt time.Time) JobRetryEvent {
	return JobRetryEvent{
		Version:      "v1",
		JobID:        jobID,
		AttemptCount: attemptCount,
		Reason:       reason,
		RetryAt:      retryAt,
	}
}

// JobDeadEvent is published when a processing job is moved to dead-letter.
type JobDeadEvent struct {
	Version string    `json:"version"`
	JobID   string    `json:"job_id"`
	Reason  string    `json:"reason"`
	DeadAt  time.Time `json:"dead_at"`
}

func NewJobDeadEvent(jobID, reason string, deadAt time.Time) JobDeadEvent {
	return JobDeadEvent{
		Version: "v1",
		JobID:   jobID,
		Reason:  reason,
		DeadAt:  deadAt,
	}
}

// ProposalCreatedEvent is published when a suggestion proposal is created.
type ProposalCreatedEvent struct {
	Version             string `json:"version"`
	ProposalID          string `json:"proposal_id"`
	SourceTaskID        string `json:"source_task_id"`
	ProposalType        string `json:"proposal_type"`
	HumanReviewRequired bool   `json:"human_review_required"`
}

func NewProposalCreatedEvent(proposalID, sourceTaskID, proposalType string, humanReviewRequired bool) ProposalCreatedEvent {
	return ProposalCreatedEvent{
		Version:             "v1",
		ProposalID:          proposalID,
		SourceTaskID:        sourceTaskID,
		ProposalType:        proposalType,
		HumanReviewRequired: humanReviewRequired,
	}
}

// ProposalApprovedEvent is published when a proposal is approved.
type ProposalApprovedEvent struct {
	Version      string    `json:"version"`
	ProposalID   string    `json:"proposal_id"`
	ApprovedBy   string    `json:"approved_by"`
	AutoApproved bool      `json:"auto_approved"`
	ApprovedAt   time.Time `json:"approved_at"`
}

func NewProposalApprovedEvent(proposalID, approvedBy string, autoApproved bool, approvedAt time.Time) ProposalApprovedEvent {
	return ProposalApprovedEvent{
		Version:      "v1",
		ProposalID:   proposalID,
		ApprovedBy:   approvedBy,
		AutoApproved: autoApproved,
		ApprovedAt:   approvedAt,
	}
}

// WritebackRequestedEvent is published when a writeback operation is requested.
type WritebackRequestedEvent struct {
	Version       string `json:"version"`
	WritebackID   string `json:"writeback_id"`
	ProposalID    string `json:"proposal_id"`
	SourceTaskID  string `json:"source_task_id"`
	OperationType string `json:"operation_type"`
}

func NewWritebackRequestedEvent(writebackID, proposalID, sourceTaskID, operationType string) WritebackRequestedEvent {
	return WritebackRequestedEvent{
		Version:       "v1",
		WritebackID:   writebackID,
		ProposalID:    proposalID,
		SourceTaskID:  sourceTaskID,
		OperationType: operationType,
	}
}

// WritebackCompletedEvent is published when a writeback operation completes.
type WritebackCompletedEvent struct {
	Version     string    `json:"version"`
	WritebackID string    `json:"writeback_id"`
	Verified    bool      `json:"verified"`
	CompletedAt time.Time `json:"completed_at"`
}

func NewWritebackCompletedEvent(writebackID string, verified bool, completedAt time.Time) WritebackCompletedEvent {
	return WritebackCompletedEvent{
		Version:     "v1",
		WritebackID: writebackID,
		Verified:    verified,
		CompletedAt: completedAt,
	}
}

// WritebackFailedEvent is published when a writeback operation fails.
type WritebackFailedEvent struct {
	Version      string    `json:"version"`
	WritebackID  string    `json:"writeback_id"`
	ErrorCode    string    `json:"error_code"`
	ErrorMessage string    `json:"error_message"`
	FailedAt     time.Time `json:"failed_at"`
}

func NewWritebackFailedEvent(writebackID, errorCode, errorMessage string, failedAt time.Time) WritebackFailedEvent {
	return WritebackFailedEvent{
		Version:      "v1",
		WritebackID:  writebackID,
		ErrorCode:    errorCode,
		ErrorMessage: errorMessage,
		FailedAt:     failedAt,
	}
}

// NotifyRequestedEvent is published when a notification is requested.
type NotifyRequestedEvent struct {
	Version          string `json:"version"`
	NotificationType string `json:"notification_type"`
	Recipient        string `json:"recipient"`
	PayloadJSON      string `json:"payload_json"`
}

func NewNotifyRequestedEvent(notificationType, recipient, payloadJSON string) NotifyRequestedEvent {
	return NotifyRequestedEvent{
		Version:          "v1",
		NotificationType: notificationType,
		Recipient:        recipient,
		PayloadJSON:      payloadJSON,
	}
}

// TaskResyncCommandEvent is a command published by the API to trigger re-processing
// of a source task. The orchestrator subscribes and calls queue.Enqueue.
type TaskResyncCommandEvent struct {
	Version      string `json:"version"`
	SourceTaskID string `json:"source_task_id"`
	JobType      string `json:"job_type"`
	Priority     int    `json:"priority"`
}

func NewTaskResyncCommandEvent(sourceTaskID, jobType string, priority int) TaskResyncCommandEvent {
	return TaskResyncCommandEvent{
		Version:      "v1",
		SourceTaskID: sourceTaskID,
		JobType:      jobType,
		Priority:     priority,
	}
}

// JobRetryCommandEvent is a command published by the API to retry a failed or
// dead-lettered job. The orchestrator subscribes and calls queue.Retry.
type JobRetryCommandEvent struct {
	Version string `json:"version"`
	JobID   string `json:"job_id"`
}

func NewJobRetryCommandEvent(jobID string) JobRetryCommandEvent {
	return JobRetryCommandEvent{
		Version: "v1",
		JobID:   jobID,
	}
}

// --- Control-layer alert and result events ---

// LeaseExpiredAlertEvent is published by the control loop when expired leases are reclaimed.
type LeaseExpiredAlertEvent struct {
	Version    string    `json:"version"`
	Count      int64     `json:"count"`
	DetectedAt time.Time `json:"detected_at"`
}

func NewLeaseExpiredAlertEvent(count int64) LeaseExpiredAlertEvent {
	return LeaseExpiredAlertEvent{Version: "v1", Count: count, DetectedAt: time.Now().UTC()}
}

// RetryOverdueAlertEvent is published by the control loop when retry_scheduled jobs are requeued.
type RetryOverdueAlertEvent struct {
	Version    string    `json:"version"`
	Count      int64     `json:"count"`
	DetectedAt time.Time `json:"detected_at"`
}

func NewRetryOverdueAlertEvent(count int64) RetryOverdueAlertEvent {
	return RetryOverdueAlertEvent{Version: "v1", Count: count, DetectedAt: time.Now().UTC()}
}

// QueueBacklogAlertEvent is published by the control loop when queued job count exceeds threshold.
type QueueBacklogAlertEvent struct {
	Version    string    `json:"version"`
	Count      int64     `json:"count"`
	Threshold  int64     `json:"threshold"`
	DetectedAt time.Time `json:"detected_at"`
}

func NewQueueBacklogAlertEvent(count, threshold int64) QueueBacklogAlertEvent {
	return QueueBacklogAlertEvent{Version: "v1", Count: count, Threshold: threshold, DetectedAt: time.Now().UTC()}
}

// LeaseLostAlertEvent is published by a worker's heartbeat when lease ownership is lost mid-execution.
type LeaseLostAlertEvent struct {
	Version    string    `json:"version"`
	JobID      string    `json:"job_id"`
	WorkerID   string    `json:"worker_id"`
	DetectedAt time.Time `json:"detected_at"`
}

func NewLeaseLostAlertEvent(jobID, workerID string) LeaseLostAlertEvent {
	return LeaseLostAlertEvent{Version: "v1", JobID: jobID, WorkerID: workerID, DetectedAt: time.Now().UTC()}
}

// ReclaimCompletedEvent is published by the control loop after reclaiming expired leases.
type ReclaimCompletedEvent struct {
	Version   string `json:"version"`
	Reclaimed int64  `json:"reclaimed"`
}

func NewReclaimCompletedEvent(reclaimed int64) ReclaimCompletedEvent {
	return ReclaimCompletedEvent{Version: "v1", Reclaimed: reclaimed}
}

// RetryRequeueCompletedEvent is published by the control loop after requeuing overdue retries.
type RetryRequeueCompletedEvent struct {
	Version  string `json:"version"`
	Requeued int64  `json:"requeued"`
}

func NewRetryRequeueCompletedEvent(requeued int64) RetryRequeueCompletedEvent {
	return RetryRequeueCompletedEvent{Version: "v1", Requeued: requeued}
}
