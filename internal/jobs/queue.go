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

// leaseDuration is the initial validity window of every newly issued lease.
// RenewLease extends by the same duration, so a heartbeat firing once before
// expiry keeps the lease alive indefinitely.
const leaseDuration = 5 * time.Minute

// Queuer is the interface the worker depends on for job lifecycle operations.
// Declared here so consumers (worker, tests) can depend on the interface rather
// than the concrete *Queue, enabling unit testing without a real database.
type Queuer interface {
	Lease(ctx context.Context, workerID string, jobTypes []string) (*models.ProcessingJob, error)
	// Complete and Fail require the callerʼs workerID so the ownership guard
	// in the SQL WHERE clause ensures only the current lease-holder may apply
	// the terminal transition.
	Complete(ctx context.Context, jobID uuid.UUID, workerID string) error
	Fail(ctx context.Context, jobID uuid.UUID, workerID, errCode, errMsg string) error
	// RenewLease extends lease_expiry for jobs still owned by workerID.
	// Returns (true, nil) on success, (false, nil) when ownership was lost,
	// and (false, err) on transient DB errors.
	RenewLease(ctx context.Context, jobID uuid.UUID, workerID string) (bool, error)
	ReclaimExpiredLeases(ctx context.Context) (int64, error)
	RequeueScheduledRetries(ctx context.Context) (int64, error)
}

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
// The leased_by_worker_id column is set to workerID so that Complete, Fail, and
// RenewLease can enforce that only the current lease-holder may act on the job.
func (q *Queue) Lease(ctx context.Context, workerID string, jobTypes []string) (*models.ProcessingJob, error) {
	now := time.Now().UTC()
	expiry := now.Add(leaseDuration)

	const query = `
		UPDATE processing_jobs
		SET status = $1, leased_at = $2, lease_expiry = $3, leased_by_worker_id = $5, updated_at = $2
		WHERE id = (
			SELECT id FROM processing_jobs
			WHERE status = 'queued'
			  AND (scheduled_at IS NULL OR scheduled_at <= $2)
			  AND job_type = ANY($4)
			ORDER BY priority DESC, created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, source_task_id, job_type, status, priority, dedupe_key, attempt_count, max_attempts, payload, leased_at, lease_expiry, leased_by_worker_id, scheduled_at, created_at, updated_at`

	var job models.ProcessingJob
	err := q.db.QueryRow(ctx, query, models.JobStatusLeased, now, expiry, jobTypes, workerID).Scan(
		&job.ID, &job.SourceTaskID, &job.JobType, &job.Status,
		&job.Priority, &job.DedupeKey, &job.AttemptCount, &job.MaxAttempts,
		&job.Payload, &job.LeasedAt, &job.LeaseExpiry, &job.LeasedByWorkerID, &job.ScheduledAt,
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
// The workerID ownership guard ensures a stale goroutine cannot mark a job as
// succeeded after its lease was reclaimed and re-issued to a different worker.
func (q *Queue) Complete(ctx context.Context, jobID uuid.UUID, workerID string) error {
	const query = `
		UPDATE processing_jobs
		SET status = $1, leased_by_worker_id = NULL, updated_at = $2
		WHERE id = $3 AND status = 'leased' AND leased_by_worker_id = $4`
	tag, err := q.db.Exec(ctx, query, models.JobStatusSucceeded, time.Now().UTC(), jobID, workerID)
	if err != nil {
		return fmt.Errorf("complete job %s: %w", jobID, err)
	}
	if tag.RowsAffected() == 0 {
		q.logger.Warn("complete: ownership guard rejected — lease no longer owned by this worker",
			zap.String("job_id", jobID.String()),
			zap.String("worker_id", workerID),
		)
	}
	return nil
}

// Fail marks a job as failed and schedules a retry or dead-letters it.
// The UPDATE is atomic — attempt_count is incremented and the new status is
// computed in a single statement, eliminating the read-then-write TOCTOU race.
// The workerID ownership guard ensures only the current lease-holder can fail
// the job, preventing stale goroutines from corrupting a re-leased job.
func (q *Queue) Fail(ctx context.Context, jobID uuid.UUID, workerID, errCode, errMsg string) error {
	now := time.Now().UTC()

	const query = `
		UPDATE processing_jobs
		SET
		    status               = CASE
		                               WHEN attempt_count + 1 >= max_attempts THEN 'dead_letter'
		                               ELSE 'retry_scheduled'
		                           END,
		    attempt_count        = attempt_count + 1,
		    leased_by_worker_id  = NULL,
		    scheduled_at         = CASE
		                               WHEN attempt_count + 1 >= max_attempts THEN NULL
		                               ELSE $1::timestamptz + make_interval(secs => ((attempt_count + 1)::float8 * (attempt_count + 1)::float8 * 30.0))
		                           END,
		    updated_at           = $1,
		    error_code           = $2,
		    error_message        = $3
		WHERE id = $4 AND status = 'leased' AND leased_by_worker_id = $5
		RETURNING status, attempt_count`

	var newStatus string
	var newAttemptCount int
	err := q.db.QueryRow(ctx, query, now, errCode, errMsg, jobID, workerID).Scan(&newStatus, &newAttemptCount)
	if err == pgx.ErrNoRows {
		// Ownership guard rejected — either the lease was reclaimed by maintenance
		// and this worker no longer owns the job, or another worker re-leased it.
		q.logger.Warn("fail: ownership guard rejected — lease no longer owned by this worker",
			zap.String("job_id", jobID.String()),
			zap.String("worker_id", workerID),
			zap.String("error_code", errCode),
		)
		return nil
	}
	if err != nil {
		return fmt.Errorf("fail job %s: %w", jobID, err)
	}

	q.logger.Info("job failed",
		zap.String("job_id", jobID.String()),
		zap.String("new_status", newStatus),
		zap.Int("attempt_count", newAttemptCount),
		zap.String("worker_id", workerID),
		zap.String("error_code", errCode),
		zap.String("error_msg", errMsg),
	)
	return nil
}

// RenewLease extends the lease_expiry for a job that workerID still owns.
// Returns (true, nil) on successful renewal.
// Returns (false, nil) when the job is no longer owned by workerID — this
// signals the caller that it should abort execution (ownership was lost).
// Returns (false, err) on transient database errors.
func (q *Queue) RenewLease(ctx context.Context, jobID uuid.UUID, workerID string) (bool, error) {
	now := time.Now().UTC()
	newExpiry := now.Add(leaseDuration)
	const query = `
		UPDATE processing_jobs
		SET lease_expiry = $1, updated_at = $2
		WHERE id = $3 AND status = 'leased' AND leased_by_worker_id = $4`
	tag, err := q.db.Exec(ctx, query, newExpiry, now, jobID, workerID)
	if err != nil {
		return false, fmt.Errorf("renew lease for job %s: %w", jobID, err)
	}
	if tag.RowsAffected() == 0 {
		q.logger.Warn("renew lease: ownership lost — job is no longer leased by this worker",
			zap.String("job_id", jobID.String()),
			zap.String("worker_id", workerID),
		)
		return false, nil
	}
	q.logger.Debug("lease renewed",
		zap.String("job_id", jobID.String()),
		zap.String("worker_id", workerID),
		zap.Time("new_expiry", newExpiry),
	)
	return true, nil
}

// ReclaimExpiredLeases moves jobs whose lease has expired back to queued status.
// leased_by_worker_id is cleared so the next worker starts with a clean ownership
// record and the previous worker's Complete/Fail calls are properly rejected.
func (q *Queue) ReclaimExpiredLeases(ctx context.Context) (int64, error) {
	now := time.Now().UTC()
	const query = `
		UPDATE processing_jobs
		SET status = 'queued', leased_at = NULL, lease_expiry = NULL,
		    leased_by_worker_id = NULL, updated_at = $1
		WHERE status = 'leased' AND lease_expiry < $1`
	tag, err := q.db.Exec(ctx, query, now)
	if err != nil {
		return 0, fmt.Errorf("reclaim expired leases: %w", err)
	}
	return tag.RowsAffected(), nil
}

// RequeueScheduledRetries moves retry_scheduled jobs whose scheduled_at has passed back to queued.
func (q *Queue) RequeueScheduledRetries(ctx context.Context) (int64, error) {
	now := time.Now().UTC()
	const query = `
		UPDATE processing_jobs
		SET status = 'queued', updated_at = $1
		WHERE status = 'retry_scheduled' AND scheduled_at <= $1`
	tag, err := q.db.Exec(ctx, query, now)
	if err != nil {
		return 0, fmt.Errorf("requeue scheduled retries: %w", err)
	}
	return tag.RowsAffected(), nil
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
