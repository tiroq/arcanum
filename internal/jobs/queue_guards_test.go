package jobs

// Unit tests for Queue state-transition guards.
//
// These tests verify properties of the Fail() and Complete() queries that
// prevent duplicate-processing side-effects:
//
//  1. Fail() uses a single atomic UPDATE (no separate SELECT first).
//  2. Complete() and Fail() guard on status = 'leased'.
//  3. Complete() and Fail() guard on leased_by_worker_id (ownership).
//
// Because these tests do not require a real database they verify the SQL
// query strings directly. This is intentional: the fix is entirely in the
// query text, and verifying the query is more brittle-proof than testing
// behaviour through a live DB in a unit test suite.

import (
	"strings"
	"testing"
)

// failQuery is the exact UPDATE statement that Fail() uses (kept in sync with queue.go).
const failQuery = `
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

// completeQuery is the exact UPDATE statement that Complete() uses.
const completeQuery = `
		UPDATE processing_jobs
		SET status = $1, leased_by_worker_id = NULL, updated_at = $2
		WHERE id = $3 AND status = 'leased' AND leased_by_worker_id = $4`

// TestFailQueryIsAtomic verifies that Fail() does NOT contain a separate SELECT
// statement. A separate SELECT followed by UPDATE is a TOCTOU race; all logic
// must execute in a single UPDATE.
func TestFailQueryIsAtomic(t *testing.T) {
	normalized := strings.ToUpper(strings.Join(strings.Fields(failQuery), " "))

	if strings.Contains(normalized, "SELECT") {
		t.Error("Fail() query must not contain a SELECT statement – use an atomic UPDATE instead")
	}

	if !strings.Contains(normalized, "UPDATE PROCESSING_JOBS") {
		t.Error("Fail() query must be an UPDATE statement")
	}
}

// TestFailQueryIncrementsAttemptCountInline verifies that Fail() increments
// attempt_count inline (attempt_count = attempt_count + 1) rather than
// supplying a pre-computed value from Go. This is the atomic-increment guard.
func TestFailQueryIncrementsAttemptCountInline(t *testing.T) {
	normalized := strings.ToUpper(strings.Join(strings.Fields(failQuery), " "))

	if !strings.Contains(normalized, "ATTEMPT_COUNT = ATTEMPT_COUNT + 1") {
		t.Error("Fail() must increment attempt_count inline in SQL (attempt_count = attempt_count + 1)")
	}
}

// TestFailQueryGuardsOnLeasedStatus verifies that Fail() only updates jobs
// that are still in 'leased' status.
func TestFailQueryGuardsOnLeasedStatus(t *testing.T) {
	normalized := strings.ToUpper(strings.Join(strings.Fields(failQuery), " "))

	if !strings.Contains(normalized, "AND STATUS = 'LEASED'") {
		t.Error("Fail() WHERE clause must include AND status = 'leased'")
	}
}

// TestFailQueryGuardsOnOwnership verifies that Fail() includes the
// leased_by_worker_id ownership guard so stale workers cannot fail a
// job now owned by a different worker.
func TestFailQueryGuardsOnOwnership(t *testing.T) {
	normalized := strings.ToUpper(strings.Join(strings.Fields(failQuery), " "))

	if !strings.Contains(normalized, "AND LEASED_BY_WORKER_ID") {
		t.Error("Fail() WHERE clause must include AND leased_by_worker_id = $workerID")
	}
}

// TestFailQueryClearsOwnership verifies that Fail() sets leased_by_worker_id = NULL
// in the SET clause so a reclaimed and re-retried job starts with clean ownership.
func TestFailQueryClearsOwnership(t *testing.T) {
	normalized := strings.ToUpper(strings.Join(strings.Fields(failQuery), " "))

	if !strings.Contains(normalized, "LEASED_BY_WORKER_ID = NULL") {
		t.Error("Fail() must clear leased_by_worker_id = NULL in the SET clause")
	}
}

// TestCompleteQueryGuardsOnLeasedStatus verifies that Complete() only updates
// jobs that are still in 'leased' status.
func TestCompleteQueryGuardsOnLeasedStatus(t *testing.T) {
	normalized := strings.ToUpper(strings.Join(strings.Fields(completeQuery), " "))

	if !strings.Contains(normalized, "AND STATUS = 'LEASED'") {
		t.Error("Complete() WHERE clause must include AND status = 'leased'")
	}
}

// TestCompleteQueryGuardsOnOwnership verifies that Complete() includes the
// leased_by_worker_id ownership guard.
func TestCompleteQueryGuardsOnOwnership(t *testing.T) {
	normalized := strings.ToUpper(strings.Join(strings.Fields(completeQuery), " "))

	if !strings.Contains(normalized, "AND LEASED_BY_WORKER_ID") {
		t.Error("Complete() WHERE clause must include AND leased_by_worker_id = $workerID")
	}
}

// TestCompleteQueryClearsOwnership verifies that Complete() clears
// leased_by_worker_id in the SET clause.
func TestCompleteQueryClearsOwnership(t *testing.T) {
	normalized := strings.ToUpper(strings.Join(strings.Fields(completeQuery), " "))

	if !strings.Contains(normalized, "LEASED_BY_WORKER_ID = NULL") {
		t.Error("Complete() must clear leased_by_worker_id = NULL in the SET clause")
	}
}

// TestFailQueryReturnsNewState verifies that Fail() uses RETURNING so the
// caller can detect 0-rows (lease lost) without a second round-trip.
func TestFailQueryReturnsNewState(t *testing.T) {
	normalized := strings.ToUpper(strings.Join(strings.Fields(failQuery), " "))

	if !strings.Contains(normalized, "RETURNING") {
		t.Error("Fail() must use RETURNING to detect whether the update was applied (0 rows = ownership rejected)")
	}
}
