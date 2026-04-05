package orchestrator

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	nats "github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tiroq/arcanum/internal/contracts/events"
	"github.com/tiroq/arcanum/internal/contracts/subjects"
	"github.com/tiroq/arcanum/internal/jobs"
	"github.com/tiroq/arcanum/internal/messaging"
	"github.com/tiroq/arcanum/internal/metrics"
	"github.com/tiroq/arcanum/internal/processors"
)

// Orchestrator consumes job events and manages job lifecycle.
type Orchestrator struct {
	queue      *jobs.Queue
	publisher  *messaging.Publisher
	subscriber *messaging.Subscriber
	registry   *processors.Registry
	db         *pgxpool.Pool
	metrics    *metrics.Metrics
	logger     *zap.Logger

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// New creates a new Orchestrator.
func New(
	queue *jobs.Queue,
	publisher *messaging.Publisher,
	subscriber *messaging.Subscriber,
	registry *processors.Registry,
	db *pgxpool.Pool,
	m *metrics.Metrics,
	logger *zap.Logger,
) *Orchestrator {
	return &Orchestrator{
		queue:      queue,
		publisher:  publisher,
		subscriber: subscriber,
		registry:   registry,
		db:         db,
		metrics:    m,
		logger:     logger,
		stopCh:     make(chan struct{}),
	}
}

// Start subscribes to job events and command subjects, then begins processing.
func (o *Orchestrator) Start(ctx context.Context) error {
	if err := o.subscriber.Subscribe(subjects.SubjectJobCreated, "orchestrator-job-created", func(msg *nats.Msg) {
		o.wg.Add(1)
		go func() {
			defer o.wg.Done()
			defer msg.Ack() //nolint:errcheck

			var evt events.JobCreatedEvent
			if err := json.Unmarshal(msg.Data, &evt); err != nil {
				o.logger.Error("unmarshal job created event", zap.Error(err))
				return
			}

			jobID, err := uuid.Parse(evt.JobID)
			if err != nil {
				o.logger.Error("parse job id", zap.Error(err))
				return
			}

			job, err := o.queue.GetJob(ctx, jobID)
			if err != nil {
				o.logger.Error("get job", zap.String("job_id", evt.JobID), zap.Error(err))
				return
			}

			if !CanTransition(job.Status, "leased") {
				o.logger.Warn("job not in leasable state",
					zap.String("job_id", evt.JobID),
					zap.String("status", job.Status),
				)
				return
			}

			if _, err := o.registry.FindFor(job.JobType); err != nil {
				o.logger.Warn("no processor for job type",
					zap.String("job_type", job.JobType),
					zap.Error(err),
				)
				return
			}

			o.logger.Info("job routed to worker",
				zap.String("job_id", job.ID.String()),
				zap.String("job_type", job.JobType),
			)
		}()
	}); err != nil {
		return err
	}

	if err := o.subscriber.Subscribe(subjects.SubjectCommandTaskResync, "orchestrator-cmd-task-resync", func(msg *nats.Msg) {
		o.wg.Add(1)
		go func() {
			defer o.wg.Done()
			defer msg.Ack() //nolint:errcheck

			var cmd events.TaskResyncCommandEvent
			if err := json.Unmarshal(msg.Data, &cmd); err != nil {
				o.logger.Error("unmarshal task resync command", zap.Error(err))
				return
			}

			sourceTaskID, err := uuid.Parse(cmd.SourceTaskID)
			if err != nil {
				o.logger.Error("parse source_task_id in resync command", zap.Error(err))
				return
			}

			job, err := o.queue.Enqueue(ctx, jobs.EnqueueParams{
				SourceTaskID: sourceTaskID,
				JobType:      cmd.JobType,
				Priority:     cmd.Priority,
				MaxAttempts:  3,
			})
			if err != nil {
				o.logger.Error("enqueue resync job", zap.String("source_task_id", cmd.SourceTaskID), zap.Error(err))
				return
			}
			if job == nil {
				// Deduplicated — a non-terminal job already exists.
				o.logger.Info("resync deduplicated", zap.String("source_task_id", cmd.SourceTaskID))
				return
			}

			evt := events.NewJobCreatedEvent(job.ID.String(), cmd.SourceTaskID, cmd.JobType, cmd.Priority, "")
			if err := o.publisher.Publish(ctx, subjects.SubjectJobCreated, evt); err != nil {
				o.logger.Error("publish job created after resync", zap.String("job_id", job.ID.String()), zap.Error(err))
			}

			o.logger.Info("resync job enqueued",
				zap.String("job_id", job.ID.String()),
				zap.String("source_task_id", cmd.SourceTaskID),
			)
		}()
	}); err != nil {
		return err
	}

	return o.subscriber.Subscribe(subjects.SubjectCommandJobRetry, "orchestrator-cmd-job-retry", func(msg *nats.Msg) {
		o.wg.Add(1)
		go func() {
			defer o.wg.Done()
			defer msg.Ack() //nolint:errcheck

			var cmd events.JobRetryCommandEvent
			if err := json.Unmarshal(msg.Data, &cmd); err != nil {
				o.logger.Error("unmarshal job retry command", zap.Error(err))
				return
			}

			jobID, err := uuid.Parse(cmd.JobID)
			if err != nil {
				o.logger.Error("parse job_id in retry command", zap.Error(err))
				return
			}

			if err := o.queue.Retry(ctx, jobID); err != nil {
				o.logger.Error("retry job", zap.String("job_id", cmd.JobID), zap.Error(err))
				return
			}

			job, err := o.queue.GetJob(ctx, jobID)
			if err != nil {
				o.logger.Error("get job after retry", zap.String("job_id", cmd.JobID), zap.Error(err))
				return
			}

			dedupeKey := ""
			if job.DedupeKey != nil {
				dedupeKey = *job.DedupeKey
			}
			evt := events.NewJobCreatedEvent(job.ID.String(), job.SourceTaskID.String(), job.JobType, job.Priority, dedupeKey)
			if err := o.publisher.Publish(ctx, subjects.SubjectJobCreated, evt); err != nil {
				o.logger.Error("publish job created after retry", zap.String("job_id", cmd.JobID), zap.Error(err))
			}

			o.logger.Info("job retry enqueued",
				zap.String("job_id", cmd.JobID),
			)
		}()
	})
}

// Stop gracefully shuts down the orchestrator.
func (o *Orchestrator) Stop() {
	close(o.stopCh)

	done := make(chan struct{})
	go func() {
		o.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		o.logger.Warn("orchestrator stop timed out")
	}
}
