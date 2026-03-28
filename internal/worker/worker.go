package worker

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiroq/arcanum/internal/audit"
	"github.com/tiroq/arcanum/internal/jobs"
	"github.com/tiroq/arcanum/internal/messaging"
	"github.com/tiroq/arcanum/internal/metrics"
	"github.com/tiroq/arcanum/internal/processors"
)

const pollInterval = 2 * time.Second

// Worker executes processing jobs.
type Worker struct {
	queue     *jobs.Queue
	registry  *processors.Registry
	publisher *messaging.Publisher
	db        *pgxpool.Pool
	audit     audit.AuditRecorder
	metrics   *metrics.Metrics
	logger    *zap.Logger
	workerID  string

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// New creates a new Worker.
func New(
	queue *jobs.Queue,
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
	jobTypes := []string{"llm_rewrite", "llm_routing", "rules_classify", "composite"}
	job, err := w.queue.Lease(ctx, w.workerID, jobTypes)
	if err != nil {
		w.logger.Error("lease job failed", zap.Error(err))
		return
	}
	if job == nil {
		// No jobs available — normal steady-state.
		return
	}

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		if err := w.RunJob(ctx, job); err != nil {
			w.logger.Error("run job failed",
				zap.String("job_id", job.ID.String()),
				zap.Error(err),
			)
		}
	}()
}
