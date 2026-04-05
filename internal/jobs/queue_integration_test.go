//go:build integration

package jobs

// Live integration tests for duplicate-processing state guards.
//
// Run with:
//   DATABASE_DSN="postgres://runeforge:runeforge@localhost:5432/runeforge" \
//   go test -tags integration -v ./internal/jobs/... -run TestLive
//
// These tests connect to the real Postgres instance and exercise the actual
// SQL queries that guard against duplicate-processing state corruption.
// They are NOT unit tests — they verify live DB behavior.

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/db/models"
)

// ─── helpers ────────────────────────────────────────────────────────────────

func livePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		t.Skip("DATABASE_DSN not set — skipping live integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to DB: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping DB: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func liveQueue(t *testing.T, pool *pgxpool.Pool) *Queue {
	t.Helper()
	logger, _ := zap.NewDevelopment()
	return NewQueue(pool, logger)
}

// createTestJob inserts a fresh processing_job whose source_task_id is fixed to
// the known test task installed by migration seeds. Returns the new job's ID.
// It is cleaned up via t.Cleanup.
const knownTestTaskID = "a1111111-1111-1111-1111-111111111111"

func createTestJob(t *testing.T, pool *pgxpool.Pool, jobType string, maxAttempts int) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO processing_jobs
		    (id, source_task_id, job_type, status, priority, attempt_count, max_attempts, payload, created_at, updated_at)
		VALUES ($1, $2, $3, 'queued', 0, 0, $4, '{}', NOW(), NOW())`,
		id, knownTestTaskID, jobType, maxAttempts,
	)
	if err != nil {
		t.Fatalf("insert test job: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM processing_jobs WHERE id = $1`, id) //nolint:errcheck
	})
	return id
}

// expireLease backdates lease_expiry to 1 minute ago, simulating a slow job
// whose 5-minute lease window has elapsed.
func expireLease(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE processing_jobs SET lease_expiry = NOW() - interval '1 minute' WHERE id = $1`, jobID)
	if err != nil {
		t.Fatalf("expire lease: %v", err)
	}
}

// jobRow fetches status and attempt_count for a job, panics on error.
func jobRow(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID) (status string, attemptCount int) {
	t.Helper()
	err := pool.QueryRow(context.Background(),
		`SELECT status, attempt_count FROM processing_jobs WHERE id = $1`, jobID,
	).Scan(&status, &attemptCount)
	if err != nil {
		t.Fatalf("query job row: %v", err)
	}
	return
}

// ─── ScenarioA: Complete after reclaim (no re-lease) ────────────────────────
//
//	Timeline:
//	  G1 leases J → lease expires → maintenance reclaims (status='queued') →
//	  G1 calls Complete(J)  [old lease, no current holder]
//	  Expected: 0 rows affected, status stays 'queued'.
//	  Result: PROTECTED by fix.
func TestLiveScenarioA_CompleteAfterReclaimNoRelease(t *testing.T) {
	pool := livePool(t)
	q := liveQueue(t, pool)
	ctx := context.Background()

	jobID := createTestJob(t, pool, "rules_classify", 3)

	// G1 leases the job.
	g1job, err := q.Lease(ctx, "worker-g1", []string{"rules_classify"})
	if err != nil || g1job == nil {
		t.Fatalf("G1 lease failed: err=%v job=%v", err, g1job)
	}
	if g1job.ID != jobID {
		t.Fatalf("got unexpected job %s (wanted %s)", g1job.ID, jobID)
	}

	// Simulate slow processing: expire the lease.
	expireLease(t, pool, jobID)

	// Maintenance reclaims the expired lease.
	reclaimed, err := q.ReclaimExpiredLeases(ctx)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if reclaimed == 0 {
		t.Fatal("expected at least 1 reclaimed lease")
	}

	statusAfterReclaim, _ := jobRow(t, pool, jobID)
	if statusAfterReclaim != "queued" {
		t.Fatalf("expected status=queued after reclaim, got %s", statusAfterReclaim)
	}

	// G1 goroutine finishes (after reclaim) and calls Complete.
	if err := q.Complete(ctx, jobID); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	statusFinal, _ := jobRow(t, pool, jobID)
	if statusFinal != "queued" {
		t.Errorf("SCENARIO A FAILED: stale Complete() changed status to %q (expected 'queued')", statusFinal)
	} else {
		t.Logf("SCENARIO A PASS: stale Complete() after reclaim had no effect (status=%s)", statusFinal)
	}
}

// ─── ScenarioB: Complete after reclaim + re-lease ───────────────────────────
//
//	Timeline:
//	  G1 leases J → lease expires → maintenance reclaims → G2 re-leases →
//	  G1 calls Complete(J)   [G2 still holds a valid lease]
//	  Expected: Complete() succeeds (1 row) because status='leased' matches.
//	  The guard cannot distinguish G1's stale call from G2's valid one.
//	  Result: STATE GAP — G1 can mark G2's leased job as succeeded.
//	  Fix status: NOT prevented by the AND status='leased' guard alone.
//	  Lease heartbeat with leased_by_worker_id required to close this gap.
func TestLiveScenarioB_CompleteAfterReclaimAndRelease(t *testing.T) {
	pool := livePool(t)
	q := liveQueue(t, pool)
	ctx := context.Background()

	jobID := createTestJob(t, pool, "rules_classify", 3)

	// G1 leases.
	g1job, err := q.Lease(ctx, "worker-g1", []string{"rules_classify"})
	if err != nil || g1job == nil {
		t.Fatalf("G1 lease: %v / %v", err, g1job)
	}

	// Expire lease, maintenance reclaims.
	expireLease(t, pool, jobID)
	if _, err := q.ReclaimExpiredLeases(ctx); err != nil {
		t.Fatalf("reclaim: %v", err)
	}

	// G2 re-leases the same job.
	g2job, err := q.Lease(ctx, "worker-g2", []string{"rules_classify"})
	if err != nil || g2job == nil {
		t.Fatalf("G2 lease: %v / %v", err, g2job)
	}
	if g2job.ID != jobID {
		t.Fatalf("G2 leased different job %s (expected %s)", g2job.ID, jobID)
	}

	// G1 (still running) calls Complete — G2 also holds the lease.
	if err := q.Complete(ctx, jobID); err != nil {
		t.Fatalf("G1 Complete: %v", err)
	}

	statusAfterG1Complete, _ := jobRow(t, pool, jobID)

	// This is the known gap: the guard passes because status='leased' is still true.
	if statusAfterG1Complete == models.JobStatusSucceeded {
		t.Logf("SCENARIO B GAP CONFIRMED: G1's stale Complete() marked G2's leased job as 'succeeded'.")
		t.Logf("  → Both G1 and G2 are now executing the same job.")
		t.Logf("  → Job is now in terminal state; G2's future Complete() will be a no-op (0 rows).")
		t.Logf("  → State will be deterministically 'succeeded', but DUPLICATE compute happened.")
		t.Logf("  → Fix required: leased_by_worker_id column + heartbeat to close this gap.")
	} else {
		t.Logf("SCENARIO B result: status=%s (unexpected — check logic)", statusAfterG1Complete)
	}

	// Now simulate G2 finishing and calling Complete — must be a no-op.
	if err := q.Complete(ctx, jobID); err != nil {
		t.Fatalf("G2 Complete: %v", err)
	}
	statusFinal, _ := jobRow(t, pool, jobID)
	if statusFinal != models.JobStatusSucceeded {
		t.Errorf("expected final status=succeeded, got %s", statusFinal)
	} else {
		t.Logf("SCENARIO B: G2 Complete() is a no-op after G1 already succeeded. Final state correct.")
	}
}

// ─── ScenarioC: Fail after reclaim (no re-lease) ────────────────────────────
//
//	Timeline:
//	  G1 leases J → lease expires → maintenance reclaims (status='queued') →
//	  G1 calls Fail(J)
//	  Expected: 0 rows affected, status stays 'queued', attempt_count unchanged.
//	  Result: PROTECTED by fix.
func TestLiveScenarioC_FailAfterReclaimNoRelease(t *testing.T) {
	pool := livePool(t)
	q := liveQueue(t, pool)
	ctx := context.Background()

	jobID := createTestJob(t, pool, "rules_classify", 3)

	// G1 leases.
	g1job, err := q.Lease(ctx, "worker-g1", []string{"rules_classify"})
	if err != nil || g1job == nil {
		t.Fatalf("G1 lease: %v / %v", err, g1job)
	}

	expireLease(t, pool, jobID)
	if _, err := q.ReclaimExpiredLeases(ctx); err != nil {
		t.Fatalf("reclaim: %v", err)
	}

	// G1 finishes with an error AFTER reclaim.
	if err := q.Fail(ctx, jobID, "TIMEOUT", "processing exceeded timeout"); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	statusFinal, attemptFinal := jobRow(t, pool, jobID)
	if statusFinal != "queued" || attemptFinal != 0 {
		t.Errorf("SCENARIO C FAILED: stale Fail() changed state — status=%s attempt_count=%d (expected queued/0)",
			statusFinal, attemptFinal)
	} else {
		t.Logf("SCENARIO C PASS: stale Fail() after reclaim had no effect (status=%s attempt=%d)",
			statusFinal, attemptFinal)
	}
}

// ─── ScenarioD: G1 Fail while G2 holds the lease ────────────────────────────
//
//	Timeline:
//	  G1 leases J → lease expires → maintenance reclaims → G2 re-leases →
//	  G1 calls Fail(J)       [G2 holds the lease, real DB status='leased']
//	  Expected: G1's Fail() hits AND status='leased' — it MATCHES (gap).
//	             G2's subsequent Complete() → 0 rows (status='retry_scheduled').
//	             G2's work is silently discarded even though it succeeded.
//	  Result: STATE CORRUPTION GAP — G1 can fail G2's in-flight job.
//	  Fix required: leased_by_worker_id column.
func TestLiveScenarioD_G1FailWhileG2Holds(t *testing.T) {
	pool := livePool(t)
	q := liveQueue(t, pool)
	ctx := context.Background()

	jobID := createTestJob(t, pool, "rules_classify", 3)

	// G1 leases.
	_, err := q.Lease(ctx, "worker-g1", []string{"rules_classify"})
	if err != nil {
		t.Fatalf("G1 lease: %v", err)
	}

	// Expire lease, reclaim, G2 re-leases.
	expireLease(t, pool, jobID)
	if _, err := q.ReclaimExpiredLeases(ctx); err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	g2job, err := q.Lease(ctx, "worker-g2", []string{"rules_classify"})
	if err != nil || g2job == nil {
		t.Fatalf("G2 lease: %v / %v", err, g2job)
	}

	statusBeforeG1Fail, _ := jobRow(t, pool, jobID)
	if statusBeforeG1Fail != "leased" {
		t.Fatalf("expected G2 to hold lease (status=leased), got %s", statusBeforeG1Fail)
	}

	// G1 goroutine finishes with error — hits the guard.
	if err := q.Fail(ctx, jobID, "G1_TIMEOUT", "G1 processing timed out"); err != nil {
		t.Fatalf("G1 Fail: %v", err)
	}

	statusAfterG1Fail, attemptAfterG1Fail := jobRow(t, pool, jobID)

	if statusAfterG1Fail == "retry_scheduled" || statusAfterG1Fail == "dead_letter" {
		t.Logf("SCENARIO D GAP CONFIRMED: G1's stale Fail() corrupted G2's in-flight lease.")
		t.Logf("  → status=%s attempt_count=%d", statusAfterG1Fail, attemptAfterG1Fail)
		t.Logf("  → G2 will call Complete() which will get 0 rows → G2's success is discarded.")
		t.Logf("  → Fix required: leased_by_worker_id column + hearbeat check in Fail().")

		// Confirm G2's complete is now blocked.
		if err := q.Complete(ctx, jobID); err != nil {
			t.Fatalf("G2 Complete: %v", err)
		}
		statusFinal, _ := jobRow(t, pool, jobID)
		t.Logf("  → After G2 Complete: status=%s (G2's success was discarded)", statusFinal)
	} else if statusAfterG1Fail == "leased" {
		t.Logf("SCENARIO D: G1's Fail() had no effect — status still 'leased'. This would be correct behavior.")
		t.Logf("  → This would mean Fail() did NOT match because... manual check required.")
	} else {
		t.Logf("SCENARIO D: unexpected status=%s after G1 Fail", statusAfterG1Fail)
	}
}

// ─── ScenarioE: Concurrent Fail() — atomicity of attempt_count increment ────
//
//	Two goroutines concurrently call Fail() on the same leased job.
//	Only one must succeed (the one whose AND status='leased' matches first).
//	The other must get 0 rows (because the first changed status away from 'leased').
//	attempt_count must be exactly 1 after the dust settles.
func TestLiveScenarioE_ConcurrentFail_AtomicAttemptCount(t *testing.T) {
	pool := livePool(t)
	q := liveQueue(t, pool)
	ctx := context.Background()

	jobID := createTestJob(t, pool, "rules_classify", 3)

	// Lease the job (single lease holder).
	leased, err := q.Lease(ctx, "worker-concurrent", []string{"rules_classify"})
	if err != nil || leased == nil {
		t.Fatalf("lease: %v / %v", err, leased)
	}

	// Fire two concurrent Fail() calls.
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if err := q.Fail(ctx, jobID, fmt.Sprintf("ERR%d", n), fmt.Sprintf("goroutine %d failed", n)); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		t.Errorf("concurrent Fail() returned error: %v", e)
	}

	statusFinal, attemptFinal := jobRow(t, pool, jobID)

	if attemptFinal != 1 {
		t.Errorf("SCENARIO E FAIL: attempt_count=%d (expected 1 — only one Fail() should have taken effect)", attemptFinal)
	} else {
		t.Logf("SCENARIO E PASS: concurrent Fail() → attempt_count=%d status=%s (atomic, correct)",
			attemptFinal, statusFinal)
	}
}

// ─── ScenarioF: Stale goroutine cannot re-fail a succeeded job ───────────────
//
//	G1 processes, succeeds, calls Complete. Later a second stale Complete or
//	Fail fires. Both must be no-ops.
func TestLiveScenarioF_StaleCallsOnSucceededJob(t *testing.T) {
	pool := livePool(t)
	q := liveQueue(t, pool)
	ctx := context.Background()

	jobID := createTestJob(t, pool, "rules_classify", 3)

	leased, err := q.Lease(ctx, "worker-f", []string{"rules_classify"})
	if err != nil || leased == nil {
		t.Fatalf("lease: %v / %v", err, leased)
	}

	// Normal success path.
	if err := q.Complete(ctx, jobID); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	status, attempt := jobRow(t, pool, jobID)
	if status != models.JobStatusSucceeded {
		t.Fatalf("expected succeeded after Complete, got %s", status)
	}

	// Stale Complete (from a duplicate execute path).
	if err := q.Complete(ctx, jobID); err != nil {
		t.Fatalf("stale Complete: %v", err)
	}
	// Stale Fail.
	if err := q.Fail(ctx, jobID, "STALE", "arrived late"); err != nil {
		t.Fatalf("stale Fail: %v", err)
	}

	statusFinal, attemptFinal := jobRow(t, pool, jobID)
	if statusFinal != models.JobStatusSucceeded || attemptFinal != attempt {
		t.Errorf("SCENARIO F FAIL: stale calls changed state — status=%s attempt=%d (was succeeded/%d)",
			statusFinal, attemptFinal, attempt)
	} else {
		t.Logf("SCENARIO F PASS: stale Complete+Fail on succeeded job had no effect (status=%s attempt=%d)",
			statusFinal, attemptFinal)
	}
}
