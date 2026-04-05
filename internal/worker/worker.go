package worker

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
	"github.com/tiroq/arcanum/internal/jobs"
	"github.com/tiroq/arcanum/internal/messaging"
	"github.com/tiroq/arcanum/internal/metrics"
	"github.com/tiroq/arcanum/internal/processors"
)

const pollInterval = 2 * time.Second

// maxConcurrentJobs is the maximum number of job goroutines that may run in
// parallel within a single worker process. This bound prevents a burst of
// queued jobs from spawning unbounded goroutines and, critically, limits the
// window in which lease-expiry races can cause duplicate execution.
const maxConcurrentJobs = 4

// Worker executes processing jobs.
type Worker struct {
	queue     jobs.Queuer
	registry  *processors.Registry
	publisher *messaging.Publisher
	db        *pgxpool.Pool
	audit     audit.AuditRecorder
	metrics   *metrics.Metrics
	logger    *zap.Logger
	workerID  string

	// sem is a counting semaphore that limits how many job goroutines may run
	// concurrently. Acquiring a slot before leasing ensures we never lease a job
	// we cannot start immediately, preventing unnecessary lease churn.
	sem    chan struct{}
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// New creates a new Worker.
func New(
	queue jobs.Queuer,
	registry *processors.Registry,
	publisher *messaging.Publisher,
	db *pgxpool.Pool,
	auditor audit.AuditRecorder,
	m *metrics.Metrics,
	logger *zap.Logger,
	workerID string,
) *Worker {
	return &Worker{
		queue:     queue,
		registry:  registry,
		publisher: publisher,
		db:        db,
		audit:     auditor,
		metrics:   m,
		logger:    logger,
		workerID:  workerID,
		sem:       make(chan struct{}, maxConcurrentJobs),
		stopCh:    make(chan struct{}),
	}
}

// Start begins polling for leased jobs and processing them.
func (w *Worker) Start(ctx context.Context) error {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-w.stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.poll(ctx)
			}
		}
	}()

	return nil
}

// Stop gracefully shuts down the worker.
func (w *Worker) Stop() {
	close(w.stopCh)
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		w.logger.Warn("worker stop timed out")
	}
}

func (w *Worker) poll(ctx context.Context) {
	// Acquire a concurrency slot before attempting to lease.
	// Non-blocking: if all slots are taken we skip this tick — the running jobs
	// already hold the slots and will release them when they finish.
	select {
	case w.sem <- struct{}{}:
	default:
		// At max concurrency — skip this poll tick.
		return
	}

	jobTypes := []string{"llm_rewrite", "llm_routing", "rules_classify", "composite"}
	job, err := w.queue.Lease(ctx, w.workerID, jobTypes)
	if err != nil {
		<-w.sem // release slot — no goroutine was spawned
		w.logger.Error("lease job failed", zap.Error(err))
		return
	}
	if job == nil {
		<-w.sem // release slot — no job to run
		return
	}

	w.wg.Add(1)
	go func() {
		defer func() { <-w.sem }() // release slot when job finishes
		defer w.wg.Done()
		if err := w.RunJob(ctx, job); err != nil {
			w.logger.Error("run job failed",
				zap.String("job_id", job.ID.String()),
				zap.Error(err),
			)
		}
	}()
}
