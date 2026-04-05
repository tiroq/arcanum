package worker

// Concurrency tests for the worker semaphore and duplicate-processing prevention.
//
// These tests verify the invariant: a single worker process cannot process more
// than maxConcurrentJobs jobs at the same time, and the semaphore correctly
// gates the poll → lease → spawn pipeline.

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/db/models"
	"github.com/tiroq/arcanum/internal/jobs"
)

// ---- minimal jobs.Queuer stub used in tests ----

// stubQueue is an in-process queue stub that satisfies jobs.Queuer.
// It hands out pre-configured jobs one at a time and records lifecycle calls.
type stubQueue struct {
	mu      sync.Mutex
	pending []*models.ProcessingJob

	// tracking
	leaseCount    atomic.Int32
	completeCount atomic.Int32
	failCount     atomic.Int32

	// blockRelease lets tests hold job goroutines in-flight until signalled.
	blockRelease chan struct{}
}

func newStubQueue(jobs []*models.ProcessingJob) *stubQueue {
	return &stubQueue{
		pending:      jobs,
		blockRelease: make(chan struct{}),
	}
}

func (q *stubQueue) Lease(_ context.Context, _ string, _ []string) (*models.ProcessingJob, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) == 0 {
		return nil, nil
	}
	j := q.pending[0]
	q.pending = q.pending[1:]
	q.leaseCount.Add(1)
	return j, nil
}

func (q *stubQueue) Complete(_ context.Context, _ uuid.UUID, _ string) error {
	q.completeCount.Add(1)
	return nil
}

func (q *stubQueue) Fail(_ context.Context, _ uuid.UUID, _, _, _ string) error {
	q.failCount.Add(1)
	return nil
}

func (q *stubQueue) RenewLease(_ context.Context, _ uuid.UUID, _ string) (bool, error) {
	return true, nil
}

// compile-time assertion: stubQueue satisfies the interface
var _ jobs.Queuer = (*stubQueue)(nil)

// ---- helper: minimal worker with no real processors/DB ----

func newTestWorker(q jobs.Queuer) *Worker {
	logger, _ := zap.NewDevelopment()
	return &Worker{
		queue:    q,
		logger:   logger,
		workerID: "test-worker",
		sem:      make(chan struct{}, maxConcurrentJobs),
		stopCh:   make(chan struct{}),
	}
}

// ---- tests ----

// TestPollSemaphoreGatesLease verifies that poll() will not lease a job when all
// semaphore slots are already taken. This is the direct guard against the
// unbounded goroutine burst that enables lease-expiry duplicate processing.
func TestPollSemaphoreGatesLease(t *testing.T) {
	jobID := uuid.New()
	job := &models.ProcessingJob{
		ID:      jobID,
		JobType: "llm_rewrite",
		Status:  models.JobStatusLeased,
	}
	q := newStubQueue([]*models.ProcessingJob{job})

	w := newTestWorker(q)

	// Fill all semaphore slots manually — simulating maxConcurrentJobs running goroutines.
	for i := 0; i < maxConcurrentJobs; i++ {
		w.sem <- struct{}{}
	}

	ctx := context.Background()
	w.poll(ctx) // should detect full semaphore and return without leasing

	if got := q.leaseCount.Load(); got != 0 {
		t.Errorf("expected 0 Lease calls when semaphore is full, got %d", got)
	}

	// Drain the slots we added.
	for i := 0; i < maxConcurrentJobs; i++ {
		<-w.sem
	}
}

// TestPollSemaphoreReleasedOnNilJob verifies that poll() releases the semaphore
// slot immediately when Lease returns nil (no job available). Without this the
// slot would leak and the worker would permanently lose capacity.
func TestPollSemaphoreReleasedOnNilJob(t *testing.T) {
	q := newStubQueue(nil) // no jobs
	w := newTestWorker(q)

	ctx := context.Background()
	w.poll(ctx) // should acquire slot, get nil job, release slot

	// Semaphore should be fully empty (no leaked slots).
	if len(w.sem) != 0 {
		t.Errorf("expected empty semaphore after nil-job poll, got %d held slots", len(w.sem))
	}
}

// TestMaxConcurrencyNotExceeded verifies that once all semaphore slots are
// occupied, further poll() calls do not lease additional jobs. This is the
// primary guard against runaway goroutine spawning during a burst of queued
// work and is the limiting factor on the lease-expiry duplicate-execution path.
//
// We pre-fill maxConcurrentJobs semaphore slots to simulate goroutines already
// running, then enqueue extra jobs and confirm poll() skips them all.
func TestMaxConcurrencyNotExceeded(t *testing.T) {
	const extraJobs = 5

	var pending []*models.ProcessingJob
	for i := 0; i < extraJobs; i++ {
		pending = append(pending, &models.ProcessingJob{
			ID:      uuid.New(),
			JobType: "rules_classify",
			Status:  models.JobStatusLeased,
		})
	}

	q := newStubQueue(pending)
	w := newTestWorker(q)

	// Simulate maxConcurrentJobs goroutines already running by filling the sem.
	for i := 0; i < maxConcurrentJobs; i++ {
		w.sem <- struct{}{}
	}

	ctx := context.Background()
	// Fire extraJobs additional poll calls. All should be blocked by the full semaphore.
	for i := 0; i < extraJobs; i++ {
		w.poll(ctx)
	}

	// None of the extra jobs should have been leased.
	if got := q.leaseCount.Load(); got != 0 {
		t.Errorf("expected 0 Lease calls when semaphore is full, got %d", got)
	}

	// Semaphore should still be full (we didn't release the simulated slots).
	if len(w.sem) != maxConcurrentJobs {
		t.Errorf("expected semaphore to remain at capacity %d, got %d", maxConcurrentJobs, len(w.sem))
	}
	// Drain so the worker can GC cleanly.
	for i := 0; i < maxConcurrentJobs; i++ {
		<-w.sem
	}
}

// TestSemaphoreSlotAlwaysReleasedOnLeaseFail verifies that if the Lease call
// returns an error, the semaphore slot is properly released. Without this the
// worker would gradually starve its own concurrency.
func TestSemaphoreSlotAlwaysReleasedOnLeaseFail(t *testing.T) {
	q := &errorQueue{}
	w := newTestWorker(q)

	ctx := context.Background()
	w.poll(ctx) // should get error from Lease, still release the slot

	if len(w.sem) != 0 {
		t.Errorf("semaphore slot leaked after Lease error: %d held", len(w.sem))
	}
}

// errorQueue.Lease always returns an error.
type errorQueue struct{ stubQueue }

func (q *errorQueue) Lease(_ context.Context, _ string, _ []string) (*models.ProcessingJob, error) {
	return nil, context.DeadlineExceeded
}
