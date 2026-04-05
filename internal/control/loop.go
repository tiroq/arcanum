// Package control implements the monitoring/control layer for Arcanum.
//
// The control loop is the distinct component responsible for:
//   - Reclaiming jobs whose leases have expired.
//   - Requeuing jobs that are past their retry_scheduled time.
//   - Monitoring queue state and emitting alerts for anomalous conditions.
//   - Publishing all control observations as explicit bus events.
//
// By isolating these responsibilities here, the worker can focus exclusively
// on job execution. The control layer owns technical recovery and visibility.
package control

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
	"github.com/tiroq/arcanum/internal/contracts/events"
	"github.com/tiroq/arcanum/internal/contracts/subjects"
	"github.com/tiroq/arcanum/internal/messaging"
	"github.com/tiroq/arcanum/internal/metrics"
)

// scanInterval is how often the control loop runs a full maintenance cycle.
const scanInterval = 30 * time.Second

// backlogThreshold is the number of queued jobs above which a queue_backlog
// alert is emitted to the bus.
const backlogThreshold int64 = 50

// ControlQueuer defines the queue operations the control loop depends on.
// *jobs.Queue satisfies this interface.
type ControlQueuer interface {
	ReclaimExpiredLeases(ctx context.Context) (int64, error)
	RequeueScheduledRetries(ctx context.Context) (int64, error)
	QueueStats(ctx context.Context) (map[string]int64, error)
}

// Loop is the monitoring and recovery control component.
// It runs on its own ticker, independent of the worker execution path.
type Loop struct {
	queue   ControlQueuer
	pub     *messaging.Publisher
	audit   audit.AuditRecorder
	metrics *metrics.Metrics
	logger  *zap.Logger

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// New creates a new control Loop. pub, a, and m may be nil; all nil-safe.
func New(
	queue ControlQueuer,
	pub *messaging.Publisher,
	a audit.AuditRecorder,
	m *metrics.Metrics,
	logger *zap.Logger,
) *Loop {
	return &Loop{
		queue:   queue,
		pub:     pub,
		audit:   a,
		metrics: m,
		logger:  logger,
		stopCh:  make(chan struct{}),
	}
}

// Start launches the control loop's scan goroutine. Returns immediately.
func (l *Loop) Start(ctx context.Context) {
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		ticker := time.NewTicker(scanInterval)
		defer ticker.Stop()

		// Run once immediately so the system state is clean on startup.
		l.scan(ctx)

		for {
			select {
			case <-l.stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				l.scan(ctx)
			}
		}
	}()
}

// Stop waits for the current scan to finish then shuts down.
func (l *Loop) Stop() {
	close(l.stopCh)
	done := make(chan struct{})
	go func() {
		l.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		l.logger.Warn("control loop stop timed out")
	}
}

// scan runs one full control cycle.
func (l *Loop) scan(ctx context.Context) {
	l.reclaimExpiredLeases(ctx)
	l.requeueOverdueRetries(ctx)
	l.checkQueueState(ctx)
}

func (l *Loop) reclaimExpiredLeases(ctx context.Context) {
	count, err := l.queue.ReclaimExpiredLeases(ctx)
	if err != nil {
		l.logger.Error("control: reclaim expired leases", zap.Error(err))
		return
	}
	if count == 0 {
		return
	}

	l.logger.Warn("control: reclaimed expired leases", zap.Int64("count", count))

	if l.metrics != nil {
		l.metrics.JobsReclaimed.Add(float64(count))
		l.metrics.ControlReclaims.Add(float64(count))
		l.metrics.ControlAlerts.WithLabelValues("lease_expired").Add(float64(count))
	}

	l.publish(ctx, subjects.SubjectControlAlertLeaseExpired,
		events.NewLeaseExpiredAlertEvent(count))

	l.publish(ctx, subjects.SubjectControlResultReclaimCompleted,
		events.NewReclaimCompletedEvent(count))

	// Per-job audit events are already written by ReclaimExpiredLeases (job.reclaimed).
	// Record a summary-level audit event for the control action itself.
	if l.audit != nil {
		//nolint:errcheck
		l.audit.RecordEvent(ctx, "system", uuid.Nil,
			"control.reclaim_completed", "control_loop", "scheduler",
			map[string]any{"reclaimed_count": count})
	}
}

func (l *Loop) requeueOverdueRetries(ctx context.Context) {
	count, err := l.queue.RequeueScheduledRetries(ctx)
	if err != nil {
		l.logger.Error("control: requeue scheduled retries", zap.Error(err))
		return
	}
	if count == 0 {
		return
	}

	l.logger.Info("control: requeued overdue retries", zap.Int64("count", count))

	if l.metrics != nil {
		l.metrics.JobsRetried.Add(float64(count))
		l.metrics.ControlRequeues.Add(float64(count))
		l.metrics.ControlAlerts.WithLabelValues("retry_overdue").Add(float64(count))
	}

	l.publish(ctx, subjects.SubjectControlAlertRetryOverdue,
		events.NewRetryOverdueAlertEvent(count))

	l.publish(ctx, subjects.SubjectControlResultRetryRequeueCompleted,
		events.NewRetryRequeueCompletedEvent(count))

	if l.audit != nil {
		//nolint:errcheck
		l.audit.RecordEvent(ctx, "system", uuid.Nil,
			"control.retry_requeue_completed", "control_loop", "scheduler",
			map[string]any{"requeued_count": count})
	}
}

func (l *Loop) checkQueueState(ctx context.Context) {
	stats, err := l.queue.QueueStats(ctx)
	if err != nil {
		l.logger.Error("control: queue stats", zap.Error(err))
		return
	}

	queued := stats["queued"]
	leased := stats["leased"]
	retry := stats["retry_scheduled"]

	l.logger.Debug("control: queue state scan",
		zap.Int64("queued", queued),
		zap.Int64("leased", leased),
		zap.Int64("retry_scheduled", retry),
	)

	if l.metrics != nil {
		l.metrics.QueueDepthQueued.Set(float64(queued))
		l.metrics.QueueDepthLeased.Set(float64(leased))
		l.metrics.QueueDepthRetry.Set(float64(retry))
	}

	if queued > backlogThreshold {
		l.logger.Warn("control: queue backlog alert",
			zap.Int64("queued", queued),
			zap.Int64("threshold", backlogThreshold),
		)
		if l.metrics != nil {
			l.metrics.ControlAlerts.WithLabelValues("queue_backlog").Inc()
		}
		l.publish(ctx, subjects.SubjectControlAlertQueueBacklog,
			events.NewQueueBacklogAlertEvent(queued, backlogThreshold))
	}
}

// publish is a nil-safe helper that logs on failure but never propagates errors.
func (l *Loop) publish(ctx context.Context, subject string, payload any) {
	if l.pub == nil {
		return
	}
	if err := l.pub.Publish(ctx, subject, payload); err != nil {
		l.logger.Warn("control: publish failed",
			zap.String("subject", subject),
			zap.Error(err),
		)
	}
}
