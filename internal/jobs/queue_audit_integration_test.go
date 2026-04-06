//go:build integration

package jobs

// Live audit-trail integration tests.
//
// Run with:
//   DATABASE_DSN="postgres://runeforge:runeforge@localhost:5432/runeforge" \
//   go test -tags integration -v ./internal/jobs/... -run TestLiveAudit

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// ─── TestLiveAudit_FullJobLifecycle ─────────────────────────────────────────

// TestLiveAudit_FullJobLifecycle exercises the complete job lifecycle and asserts
// that every expected audit event is recorded in the audit_events table.
func TestLiveAudit_FullJobLifecycle(t *testing.T) {
	pool := livePool(t)
	logger, _ := zap.NewDevelopment()
	auditor := audit.NewPostgresAuditRecorder(pool)
	q := NewQueue(pool, logger).WithAudit(auditor)

	ctx := context.Background()
	workerID := "audit-full-" + uuid.New().String()[:8]

	// 1. Enqueue → job.created
	job, err := q.Enqueue(ctx, EnqueueParams{
		SourceTaskID: uuid.MustParse(knownTestTaskID),
		JobType:      "audit_smoke_test",
		Priority:     1,
		MaxAttempts:  3,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if job == nil {
		t.Fatal("Enqueue returned nil job")
	}
	jobID := job.ID
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM processing_jobs WHERE id = $1`, jobID)     //nolint:errcheck
		pool.Exec(context.Background(), `DELETE FROM audit_events WHERE entity_id = $1`, jobID) //nolint:errcheck
	})

	requireAuditEvent(t, pool, jobID, "job.created", "after Enqueue")

	// 2. Lease → job.leased
	leased, err := q.Lease(ctx, workerID, []string{"audit_smoke_test"})
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	if leased == nil || leased.ID != jobID {
		t.Fatalf("expected to lease job %s, got %v", jobID, leased)
	}
	requireAuditEvent(t, pool, jobID, "job.leased", "after Lease")

	// 3. Fail (attempt 1) → job.failed + job.retry_scheduled
	if _, err := q.Fail(ctx, jobID, workerID, "test_err", "intentional failure"); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	requireAuditEvent(t, pool, jobID, "job.failed", "after Fail attempt 1")
	requireAuditEvent(t, pool, jobID, "job.retry_scheduled", "after Fail attempt 1")

	// Reset to queued so we can Lease again.
	_, err = pool.Exec(ctx, `UPDATE processing_jobs SET status = 'queued', scheduled_at = NULL WHERE id = $1`, jobID)
	if err != nil {
		t.Fatalf("reset to queued: %v", err)
	}

	// 4. Re-lease
	leased2, err := q.Lease(ctx, workerID, []string{"audit_smoke_test"})
	if err != nil || leased2 == nil || leased2.ID != jobID {
		t.Fatalf("Lease attempt 2: %v (leased=%v)", err, leased2)
	}

	// 5. RenewLease → job.lease_renewed
	renewed, err := q.RenewLease(ctx, jobID, workerID)
	if err != nil {
		t.Fatalf("RenewLease: %v", err)
	}
	if !renewed {
		t.Fatal("RenewLease returned false — ownership lost unexpectedly")
	}
	requireAuditEvent(t, pool, jobID, "job.lease_renewed", "after RenewLease")

	// 6. Complete → job.completed
	if err := q.Complete(ctx, jobID, workerID); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	requireAuditEvent(t, pool, jobID, "job.completed", "after Complete")

	printAuditTrail(t, pool, jobID)
}

// TestLiveAudit_DeadLetter verifies job.dead_letter is recorded when max attempts
// are exhausted.
func TestLiveAudit_DeadLetter(t *testing.T) {
	pool := livePool(t)
	logger, _ := zap.NewDevelopment()
	auditor := audit.NewPostgresAuditRecorder(pool)
	q := NewQueue(pool, logger).WithAudit(auditor)

	ctx := context.Background()
	workerID := "audit-dl-" + uuid.New().String()[:8]

	job, err := q.Enqueue(ctx, EnqueueParams{
		SourceTaskID: uuid.MustParse(knownTestTaskID),
		JobType:      "audit_dead_letter_test",
		Priority:     0,
		MaxAttempts:  1, // single attempt → dead_letter on first fail
	})
	if err != nil || job == nil {
		t.Fatalf("Enqueue: %v (job=%v)", err, job)
	}
	jobID := job.ID
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM processing_jobs WHERE id = $1`, jobID)     //nolint:errcheck
		pool.Exec(context.Background(), `DELETE FROM audit_events WHERE entity_id = $1`, jobID) //nolint:errcheck
	})

	leased, err := q.Lease(ctx, workerID, []string{"audit_dead_letter_test"})
	if err != nil || leased == nil || leased.ID != jobID {
		t.Fatalf("Lease: %v (leased=%v)", err, leased)
	}

	if _, err := q.Fail(ctx, jobID, workerID, "fatal", "always fails"); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	requireAuditEvent(t, pool, jobID, "job.failed", "after Fail → dead_letter")
	requireAuditEvent(t, pool, jobID, "job.dead_letter", "after Fail → dead_letter")

	printAuditTrail(t, pool, jobID)
}

// TestLiveAudit_ReclaimExpiredLease verifies job.reclaimed is recorded when
// ReclaimExpiredLeases recovers a job with an expired lease.
func TestLiveAudit_ReclaimExpiredLease(t *testing.T) {
	pool := livePool(t)
	logger, _ := zap.NewDevelopment()
	auditor := audit.NewPostgresAuditRecorder(pool)
	q := NewQueue(pool, logger).WithAudit(auditor)

	ctx := context.Background()
	workerID := "audit-rc-" + uuid.New().String()[:8]

	job, err := q.Enqueue(ctx, EnqueueParams{
		SourceTaskID: uuid.MustParse(knownTestTaskID),
		JobType:      "audit_reclaim_test",
		Priority:     0,
		MaxAttempts:  3,
	})
	if err != nil || job == nil {
		t.Fatalf("Enqueue: %v (job=%v)", err, job)
	}
	jobID := job.ID
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM processing_jobs WHERE id = $1`, jobID)     //nolint:errcheck
		pool.Exec(context.Background(), `DELETE FROM audit_events WHERE entity_id = $1`, jobID) //nolint:errcheck
	})

	leased, err := q.Lease(ctx, workerID, []string{"audit_reclaim_test"})
	if err != nil || leased == nil || leased.ID != jobID {
		t.Fatalf("Lease: %v (leased=%v)", err, leased)
	}

	// Backdate the lease so it appears expired.
	expireLease(t, pool, jobID)

	n, err := q.ReclaimExpiredLeases(ctx)
	if err != nil {
		t.Fatalf("ReclaimExpiredLeases: %v", err)
	}
	if n < 1 {
		t.Fatalf("expected at least 1 reclaimed job, got %d", n)
	}

	requireAuditEvent(t, pool, jobID, "job.reclaimed", "after ReclaimExpiredLeases")

	printAuditTrail(t, pool, jobID)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// requireAuditEvent fails the test if event_type is not found for entity_id.
func requireAuditEvent(t *testing.T, pool *pgxpool.Pool, entityID uuid.UUID, eventType, when string) {
	t.Helper()
	var count int
	err := pool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM audit_events WHERE entity_id = $1 AND event_type = $2`,
		entityID, eventType,
	).Scan(&count)
	if err != nil {
		t.Fatalf("requireAuditEvent(%s) query: %v", eventType, err)
	}
	if count == 0 {
		t.Errorf("MISSING audit event %q %s  (entity_id=%s)", eventType, when, entityID)
	} else {
		t.Logf("OK  audit %-30s  %s", eventType, when)
	}
}

// printAuditTrail logs every audit_event for a job in chronological order,
// helping manual verification of the lifecycle sequence.
func printAuditTrail(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID) {
	t.Helper()
	rows, err := pool.Query(
		context.Background(),
		`SELECT event_type, actor_type, actor_id, occurred_at
		 FROM audit_events
		 WHERE entity_id = $1
		 ORDER BY occurred_at ASC`,
		jobID,
	)
	if err != nil {
		t.Logf("printAuditTrail: %v", err)
		return
	}
	defer rows.Close()

	t.Logf("── audit trail for job %s ─────────────────────────────────", jobID)
	for rows.Next() {
		var evType, actorType, actorID string
		var at time.Time
		if scanErr := rows.Scan(&evType, &actorType, &actorID, &at); scanErr != nil {
			t.Logf("  scan error: %v", scanErr)
			continue
		}
		t.Logf("  [%s]  %-30s  actor=%s/%s", at.Format("15:04:05.000"), evType, actorType, actorID)
	}
}
