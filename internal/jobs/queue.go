package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
	"github.com/tiroq/arcanum/internal/db/models"
)

// leaseDuration is the initial validity window of every newly issued lease.
// RenewLease extends by the same duration, so a heartbeat firing once before
// expiry keeps the lease alive indefinitely.
const leaseDuration = 5 * time.Minute

// ErrUnknownJobType is returned when a job type is not in the known set.
var ErrUnknownJobType = fmt.Errorf("unknown job type")

// Queuer is the interface the worker depends on for job lifecycle operations.
// Declared here so consumers (worker, tests) can depend on the interface rather
// than the concrete *Queue, enabling unit testing without a real database.
// Maintenance operations (reclaim, requeue, stats) are NOT part of this interface;
// they are owned by the control loop which uses *Queue directly.
type Queuer interface {
	Lease(ctx context.Context, workerID string, jobTypes []string) (*models.ProcessingJob, error)
	// Complete and Fail require the callerʼs workerID so the ownership guard
	// in the SQL WHERE clause ensures only the current lease-holder may apply
	// the terminal transition.
	Complete(ctx context.Context, jobID uuid.UUID, workerID string) error
	Fail(ctx context.Context, jobID uuid.UUID, workerID, errCode, errMsg string) (*FailResult, error)
	// RenewLease extends lease_expiry for jobs still owned by workerID.
	// Returns (true, nil) on success, (false, nil) when ownership was lost,
	// and (false, err) on transient DB errors.
	RenewLease(ctx context.Context, jobID uuid.UUID, workerID string) (bool, error)
}

// EnqueueParams holds parameters for creating a new job.
type EnqueueParams struct {
	SourceTaskID uuid.UUID
	JobType      string
	Priority     int
	DedupeKey    *string
	MaxAttempts  int
}

// FailResult contains the outcome of a Fail operation.
// nil result with nil error means the ownership guard rejected the call.
type FailResult struct {
	NewStatus    string
	AttemptCount int
	MaxAttempts  int
	ScheduledAt  *time.Time // non-nil when NewStatus is retry_scheduled
}

// Queue manages ProcessingJob creation and status transitions.
type Queue struct {
	db     *pgxpool.Pool
	logger *zap.Logger
	audit  audit.AuditRecorder
}

// NewQueue creates a new Queue.
func NewQueue(db *pgxpool.Pool, logger *zap.Logger) *Queue {
	return &Queue{db: db, logger: logger}
}

// WithAudit attaches an audit recorder. Errors in audit recording never fail
// the main DB operation — they are logged at WARN level and discarded.
func (q *Queue) WithAudit(a audit.AuditRecorder) *Queue {
	q.audit = a
	return q
}

// record is a fire-and-forget audit helper. Errors are logged, never propagated.
func (q *Queue) record(ctx context.Context, entityType string, entityID uuid.UUID, eventType, actorType, actorID string, payload any) {
	if q.audit == nil {
		return
	}
	if err := q.audit.RecordEvent(ctx, entityType, entityID, eventType, actorType, actorID, payload); err != nil {
		q.logger.Warn("audit record failed",
			zap.String("event_type", eventType),
			zap.String("entity_id", entityID.String()),
			zap.Error(err),
		)
	}
}

// Enqueue creates a new job unless a dedupe_key with an active (non-terminal) status already exists.
// Returns nil job (no error) when deduplicated.
func (q *Queue) Enqueue(ctx context.Context, params EnqueueParams) (*models.ProcessingJob, error) {
	if !models.IsKnownJobType(params.JobType) {
		q.logger.Error("enqueue rejected: unknown job type",
			zap.String("job_type", params.JobType),
		)
		return nil, fmt.Errorf("%w: %s", ErrUnknownJobType, params.JobType)
	}

	if params.MaxAttempts <= 0 {
		params.MaxAttempts = 3
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

	// Atomic insert with dedup guard.
	// ON CONFLICT targets the partial unique index uq_processing_jobs_dedupe
	// (dedupe_key IS NOT NULL AND status NOT IN ('succeeded','dead_letter')).
	// DO NOTHING returns 0 rows affected when a live duplicate already exists,
	// which we treat as a successful deduplication — no error, nil job returned.
	// This is race-safe: no separate SELECT is needed.
	const insert = `
		INSERT INTO processing_jobs (id, source_task_id, job_type, status, priority, dedupe_key, attempt_count, max_attempts, payload, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, 0, $7, $8, $9, $9)
		ON CONFLICT (dedupe_key)
		    WHERE dedupe_key IS NOT NULL
		      AND status NOT IN ('succeeded', 'dead_letter')
		DO NOTHING`

	tag, err := q.db.Exec(ctx, insert,
		job.ID, job.SourceTaskID, job.JobType, job.Status,
		job.Priority, job.DedupeKey, job.MaxAttempts, job.Payload, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Conflict on dedupe_key — a non-terminal job already exists.
		if params.DedupeKey != nil {
			q.logger.Debug("job deduplicated", zap.String("dedupe_key", *params.DedupeKey))
		}
		return nil, nil
	}

	q.logger.Info("job enqueued",
		zap.String("job_id", job.ID.String()),
		zap.String("job_type", job.JobType),
	)

	q.record(ctx, "job", job.ID, "job.created", "system", "queue", map[string]any{
		"job_type":       job.JobType,
		"source_task_id": job.SourceTaskID.String(),
		"priority":       job.Priority,
	})

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

	q.record(ctx, "job", job.ID, "job.leased", "worker", workerID, map[string]any{
		"worker_id":     workerID,
		"attempt_count": job.AttemptCount,
	})

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
		return nil
	}
	q.record(ctx, "job", jobID, "job.completed", "worker", workerID, map[string]any{
		"worker_id": workerID,
	})
	return nil
}

// Fail marks a job as failed and schedules a retry or dead-letters it.
// The UPDATE is atomic — attempt_count is incremented and the new status is
// computed in a single statement, eliminating the read-then-write TOCTOU race.
// The workerID ownership guard ensures only the current lease-holder can fail
// the job, preventing stale goroutines from corrupting a re-leased job.
func (q *Queue) Fail(ctx context.Context, jobID uuid.UUID, workerID, errCode, errMsg string) (*FailResult, error) {
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
		RETURNING status, attempt_count, max_attempts, scheduled_at`

	var newStatus string
	var newAttemptCount int
	var maxAttempts int
	var scheduledAt *time.Time
	err := q.db.QueryRow(ctx, query, now, errCode, errMsg, jobID, workerID).Scan(&newStatus, &newAttemptCount, &maxAttempts, &scheduledAt)
	if err == pgx.ErrNoRows {
		// Ownership guard rejected — either the lease was reclaimed by maintenance
		// and this worker no longer owns the job, or another worker re-leased it.
		q.logger.Warn("fail: ownership guard rejected — lease no longer owned by this worker",
			zap.String("job_id", jobID.String()),
			zap.String("worker_id", workerID),
			zap.String("error_code", errCode),
		)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fail job %s: %w", jobID, err)
	}

	q.logger.Info("job failed",
		zap.String("job_id", jobID.String()),
		zap.String("new_status", newStatus),
		zap.Int("attempt_count", newAttemptCount),
		zap.String("worker_id", workerID),
		zap.String("error_code", errCode),
		zap.String("error_msg", errMsg),
	)

	q.record(ctx, "job", jobID, "job.failed", "worker", workerID, map[string]any{
		"worker_id":     workerID,
		"attempt_count": newAttemptCount,
		"error_code":    errCode,
		"error":         errMsg,
	})

	disposition := "job.retry_scheduled"
	if newStatus == models.JobStatusDeadLetter {
		disposition = "job.dead_letter"
	}
	q.record(ctx, "job", jobID, disposition, "worker", workerID, map[string]any{
		"worker_id":     workerID,
		"attempt_count": newAttemptCount,
	})

	return &FailResult{
		NewStatus:    newStatus,
		AttemptCount: newAttemptCount,
		MaxAttempts:  maxAttempts,
		ScheduledAt:  scheduledAt,
	}, nil
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

	q.record(ctx, "job", jobID, "job.lease_renewed", "worker", workerID, map[string]any{
		"worker_id": workerID,
	})

	return true, nil
}

// ReclaimExpiredLeases moves jobs whose lease has expired back to queued status.
// leased_by_worker_id is cleared so the next worker starts with a clean ownership
// record and the previous worker's Complete/Fail calls are properly rejected.
// The CTE captures the old leased_by_worker_id before clearing it so we can
// record a per-job audit event identifying which worker lost the lease.
func (q *Queue) ReclaimExpiredLeases(ctx context.Context) (int64, error) {
	now := time.Now().UTC()
	const query = `
		WITH expired AS (
			SELECT id, leased_by_worker_id AS prev_owner
			FROM processing_jobs
			WHERE status = 'leased' AND lease_expiry < $1
			FOR UPDATE
		)
		UPDATE processing_jobs
		SET status = 'queued', leased_at = NULL, lease_expiry = NULL,
		    leased_by_worker_id = NULL, updated_at = $1
		FROM expired
		WHERE processing_jobs.id = expired.id
		RETURNING processing_jobs.id, expired.prev_owner`

	rows, err := q.db.Query(ctx, query, now)
	if err != nil {
		return 0, fmt.Errorf("reclaim expired leases: %w", err)
	}
	defer rows.Close()

	var count int64
	for rows.Next() {
		var id uuid.UUID
		var prevOwner *string
		if scanErr := rows.Scan(&id, &prevOwner); scanErr != nil {
			q.logger.Warn("reclaim: scan row failed", zap.Error(scanErr))
			continue
		}
		count++
		prev := ""
		if prevOwner != nil {
			prev = *prevOwner
		}
		q.record(ctx, "job", id, "job.reclaimed", "system", "maintenance", map[string]any{
			"prev_owner": prev,
		})
	}
	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("reclaim expired leases: iterate rows: %w", err)
	}
	return count, nil
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

// Retry moves a failed or dead-lettered job back to queued status so it can be
// picked up by a worker again. Returns pgx.ErrNoRows if the job does not exist
// or is not in a retryable state.
func (q *Queue) Retry(ctx context.Context, jobID uuid.UUID) error {
	now := time.Now().UTC()
	const query = `
		UPDATE processing_jobs
		SET status = 'queued', scheduled_at = NULL, updated_at = $1
		WHERE id = $2
		  AND status IN ('failed', 'dead_letter')`
	tag, err := q.db.Exec(ctx, query, now, jobID)
	if err != nil {
		return fmt.Errorf("retry job %s: %w", jobID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("retry job %s: %w", jobID, pgx.ErrNoRows)
	}

	q.logger.Info("job retried", zap.String("job_id", jobID.String()))

	q.record(ctx, "job", jobID, "job.retried", "system", "queue", map[string]any{
		"job_id": jobID.String(),
	})

	return nil
}

// QueueStats returns a count of jobs in each active status.
// Used by the control loop to monitor queue health. The returned map contains
// keys for: "queued", "leased", "retry_scheduled", "failed", "dead_letter".
func (q *Queue) QueueStats(ctx context.Context) (map[string]int64, error) {
	const query = `
		SELECT status, COUNT(*) AS cnt
		FROM processing_jobs
		WHERE status IN ('queued', 'leased', 'retry_scheduled', 'failed', 'dead_letter')
		GROUP BY status`

	rows, err := q.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("queue stats: %w", err)
	}
	defer rows.Close()

	stats := map[string]int64{
		"queued":          0,
		"leased":          0,
		"retry_scheduled": 0,
		"failed":          0,
		"dead_letter":     0,
	}
	for rows.Next() {
		var status string
		var cnt int64
		if err := rows.Scan(&status, &cnt); err != nil {
			return nil, fmt.Errorf("queue stats scan: %w", err)
		}
		stats[status] = cnt
	}
	return stats, rows.Err()
}

// FailUnknownJobTypes transitions any queued jobs whose job_type is not in the
// known set to dead_letter. This prevents unknown types from remaining silently
// stuck in the queue forever. Returns the number of affected jobs.
func (q *Queue) FailUnknownJobTypes(ctx context.Context, knownTypes []string) (int64, error) {
	now := time.Now().UTC()
	const query = `
		UPDATE processing_jobs
		SET status = 'dead_letter',
		    error_code = 'UNKNOWN_JOB_TYPE',
		    error_message = 'job type is not recognised by any processor',
		    updated_at = $1
		WHERE status = 'queued'
		  AND job_type != ALL($2)
		RETURNING id, job_type`

	rows, err := q.db.Query(ctx, query, now, knownTypes)
	if err != nil {
		return 0, fmt.Errorf("fail unknown job types: %w", err)
	}
	defer rows.Close()

	var count int64
	for rows.Next() {
		var id uuid.UUID
		var jobType string
		if err := rows.Scan(&id, &jobType); err != nil {
			q.logger.Warn("fail unknown job types: scan row", zap.Error(err))
			continue
		}
		count++
		q.logger.Warn("dead-lettered unknown job type",
			zap.String("job_id", id.String()),
			zap.String("job_type", jobType),
		)
		q.record(ctx, "job", id, "job.dead_letter", "system", "control_loop", map[string]any{
			"reason":   "unknown_job_type",
			"job_type": jobType,
		})
	}
	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("fail unknown job types: iterate rows: %w", err)
	}
	return count, nil
}
