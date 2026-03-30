package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/db/models"
)

// EnqueueParams holds parameters for creating a new job.
type EnqueueParams struct {
	SourceTaskID uuid.UUID
	JobType      string
	Priority     int
	DedupeKey    *string
	MaxAttempts  int
}

// Queue manages ProcessingJob creation and status transitions.
type Queue struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

// NewQueue creates a new Queue.
func NewQueue(db *pgxpool.Pool, logger *zap.Logger) *Queue {
	return &Queue{db: db, logger: logger}
}

// Enqueue creates a new job unless a dedupe_key with an active (non-terminal) status already exists.
// Returns nil job (no error) when deduplicated.
func (q *Queue) Enqueue(ctx context.Context, params EnqueueParams) (*models.ProcessingJob, error) {
	if params.MaxAttempts <= 0 {
		params.MaxAttempts = 3
	}

	// Check dedupe: if a non-terminal job with this key already exists, skip.
	if params.DedupeKey != nil {
		const checkDedupe = `
			SELECT id FROM processing_jobs
			WHERE dedupe_key = $1
			  AND status NOT IN ('succeeded', 'dead_letter')`
		var existingID uuid.UUID
		err := q.db.QueryRow(ctx, checkDedupe, *params.DedupeKey).Scan(&existingID)
		if err == nil {
			q.logger.Debug("job deduplicated", zap.String("dedupe_key", *params.DedupeKey))
			return nil, nil
		}
		if err != pgx.ErrNoRows {
			return nil, fmt.Errorf("check dedupe key: %w", err)
		}
		// pgx.ErrNoRows means no conflict — proceed with insert.
	}

	now := time.Now().UTC()
	job := &models.ProcessingJob{
		ID:           uuid.New(),
		SourceTaskID: params.SourceTaskID,
		JobType:      params.JobType,
		Status:       models.JobStatusQueued,
		Priority:     params.Priority,
		DedupeKey:    params.DedupeKey,
		MaxAttempts:  params.MaxAttempts,
		Payload:      []byte("{}"),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	const insert = `
		INSERT INTO processing_jobs (id, source_task_id, job_type, status, priority, dedupe_key, attempt_count, max_attempts, payload, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, 0, $7, $8, $9, $9)`

	if _, err := q.db.Exec(ctx, insert,
		job.ID, job.SourceTaskID, job.JobType, job.Status,
		job.Priority, job.DedupeKey, job.MaxAttempts, job.Payload, now,
	); err != nil {
		return nil, fmt.Errorf("insert job: %w", err)
	}

	q.logger.Info("job enqueued",
		zap.String("job_id", job.ID.String()),
		zap.String("job_type", job.JobType),
	)
	return job, nil
}

// Lease atomically leases the next available job for a worker using SKIP LOCKED.
func (q *Queue) Lease(ctx context.Context, workerID string, jobTypes []string) (*models.ProcessingJob, error) {
	now := time.Now().UTC()
	expiry := now.Add(5 * time.Minute)

	const query = `
		UPDATE processing_jobs
		SET status = $1, leased_at = $2, lease_expiry = $3, updated_at = $2
		WHERE id = (
			SELECT id FROM processing_jobs
			WHERE status = 'queued'
			  AND (scheduled_at IS NULL OR scheduled_at <= $2)
			  AND job_type = ANY($4)
			ORDER BY priority DESC, created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, source_task_id, job_type, status, priority, dedupe_key, attempt_count, max_attempts, payload, leased_at, lease_expiry, scheduled_at, created_at, updated_at`

	var job models.ProcessingJob
	err := q.db.QueryRow(ctx, query, models.JobStatusLeased, now, expiry, jobTypes).Scan(
		&job.ID, &job.SourceTaskID, &job.JobType, &job.Status,
		&job.Priority, &job.DedupeKey, &job.AttemptCount, &job.MaxAttempts,
		&job.Payload, &job.LeasedAt, &job.LeaseExpiry, &job.ScheduledAt,
		&job.CreatedAt, &job.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		// No jobs available — normal steady-state condition.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lease job: %w", err)
	}
	return &job, nil
}

// Complete marks a job as succeeded.
func (q *Queue) Complete(ctx context.Context, jobID uuid.UUID) error {
	const query = `
		UPDATE processing_jobs
		SET status = $1, updated_at = $2
		WHERE id = $3`
	if _, err := q.db.Exec(ctx, query, models.JobStatusSucceeded, time.Now().UTC(), jobID); err != nil {
		return fmt.Errorf("complete job %s: %w", jobID, err)
	}
	return nil
}

// Fail marks a job as failed and schedules a retry or dead-letters it.
func (q *Queue) Fail(ctx context.Context, jobID uuid.UUID, errCode, errMsg string) error {
	now := time.Now().UTC()

	var attemptCount, maxAttempts int
	const getJob = `SELECT attempt_count, max_attempts FROM processing_jobs WHERE id = $1`
	if err := q.db.QueryRow(ctx, getJob, jobID).Scan(&attemptCount, &maxAttempts); err != nil {
		return fmt.Errorf("get job for fail %s: %w", jobID, err)
	}

	newAttemptCount := attemptCount + 1
	var newStatus string
	var scheduledAt *time.Time

	if newAttemptCount >= maxAttempts {
		newStatus = models.JobStatusDeadLetter
	} else {
		newStatus = models.JobStatusRetryScheduled
		retryAt := now.Add(time.Duration(newAttemptCount*newAttemptCount) * 30 * time.Second)
		scheduledAt = &retryAt
	}

	const update = `
		UPDATE processing_jobs
		SET status = $1, attempt_count = $2, scheduled_at = $3, updated_at = $4,
		    error_code = $6, error_message = $7
		WHERE id = $5`
	if _, err := q.db.Exec(ctx, update, newStatus, newAttemptCount, scheduledAt, now, jobID, errCode, errMsg); err != nil {
		return fmt.Errorf("fail job %s: %w", jobID, err)
	}

	q.logger.Info("job failed",
		zap.String("job_id", jobID.String()),
		zap.String("new_status", newStatus),
		zap.String("error_code", errCode),
		zap.String("error_msg", errMsg),
	)
	return nil
}

// GetJob retrieves a job by ID.
func (q *Queue) GetJob(ctx context.Context, jobID uuid.UUID) (*models.ProcessingJob, error) {
	const query = `
		SELECT id, source_task_id, job_type, status, priority, dedupe_key, attempt_count, max_attempts, payload, leased_at, lease_expiry, scheduled_at, created_at, updated_at
		FROM processing_jobs
		WHERE id = $1`

	var job models.ProcessingJob
	err := q.db.QueryRow(ctx, query, jobID).Scan(
		&job.ID, &job.SourceTaskID, &job.JobType, &job.Status,
		&job.Priority, &job.DedupeKey, &job.AttemptCount, &job.MaxAttempts,
		&job.Payload, &job.LeasedAt, &job.LeaseExpiry, &job.ScheduledAt,
		&job.CreatedAt, &job.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get job %s: %w", jobID, err)
	}
	return &job, nil
}
