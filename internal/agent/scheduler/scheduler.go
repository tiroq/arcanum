package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/actions"
	"github.com/tiroq/arcanum/internal/audit"
)

// CycleRunner is the interface the scheduler calls on each tick.
// *actions.Engine satisfies this implicitly via its RunCycle method.
type CycleRunner interface {
	RunCycle(ctx context.Context) (*actions.CycleReport, error)
}

// Scheduler runs the action engine periodically with single-flight protection,
// bounded timeouts, and full audit visibility. At most one cycle runs at a time.
type Scheduler struct {
	runner   CycleRunner
	interval time.Duration
	timeout  time.Duration
	logger   *zap.Logger
	auditor  audit.AuditRecorder

	mu      sync.Mutex
	running bool
	started bool
	stopCh  chan struct{}
	wg      sync.WaitGroup

	lastRunAt    time.Time
	lastDuration time.Duration
}

// New creates a Scheduler. It does NOT start automatically.
func New(
	runner CycleRunner,
	interval time.Duration,
	timeout time.Duration,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Scheduler {
	return &Scheduler{
		runner:   runner,
		interval: interval,
		timeout:  timeout,
		auditor:  auditor,
		logger:   logger,
	}
}

// Status holds the observable state of the scheduler for the API layer.
type Status struct {
	Enabled         bool      `json:"enabled"`
	Started         bool      `json:"started"`
	Running         bool      `json:"running"`
	IntervalSeconds int       `json:"interval_seconds"`
	TimeoutSeconds  int       `json:"timeout_seconds"`
	LastRunAt       time.Time `json:"last_run_at"`
	LastDurationMs  int64     `json:"last_duration_ms"`
}

// Start begins the periodic scheduling loop. Safe to call multiple times;
// duplicate calls are no-ops.
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		s.logger.Info("scheduler_already_started")
		return
	}
	s.started = true
	s.stopCh = make(chan struct{})
	s.mu.Unlock()

	s.auditEvent("scheduler.started", map[string]any{
		"interval_seconds": int(s.interval.Seconds()),
		"timeout_seconds":  int(s.timeout.Seconds()),
	})
	s.logger.Info("scheduler_started",
		zap.Duration("interval", s.interval),
		zap.Duration("timeout", s.timeout),
	)

	s.wg.Add(1)
	go s.loop()
}

// Stop halts the scheduling loop and waits for any running cycle to complete.
// Safe to call on an already-stopped scheduler.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	s.started = false
	close(s.stopCh)
	s.mu.Unlock()

	// Wait for loop goroutine AND any running cycle goroutine to finish.
	s.wg.Wait()

	s.auditEvent("scheduler.stopped", map[string]any{
		"interval_seconds": int(s.interval.Seconds()),
	})
	s.logger.Info("scheduler_stopped")
}

// GetStatus returns the current observable state.
func (s *Scheduler) GetStatus(enabled bool) Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{
		Enabled:         enabled,
		Started:         s.started,
		Running:         s.running,
		IntervalSeconds: int(s.interval.Seconds()),
		TimeoutSeconds:  int(s.timeout.Seconds()),
		LastRunAt:       s.lastRunAt,
		LastDurationMs:  s.lastDuration.Milliseconds(),
	}
}

// loop is the main ticker goroutine. It dispatches cycle attempts without
// blocking on execution, so it can detect and audit skipped ticks.
func (s *Scheduler) loop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.tryRunCycle()
		}
	}
}

// tryRunCycle attempts to start one cycle. If a cycle is already running,
// it emits a skip audit event and returns immediately. The cycle itself
// runs in a tracked goroutine so the loop remains responsive.
func (s *Scheduler) tryRunCycle() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		cycleID := uuid.New().String()
		s.auditEvent("scheduler.cycle_skipped", map[string]any{
			"cycle_id": cycleID,
			"reason":   "previous cycle still running",
		})
		s.logger.Warn("scheduler_cycle_skipped",
			zap.String("cycle_id", cycleID),
			zap.String("reason", "previous cycle still running"),
		)
		return
	}
	s.running = true
	s.wg.Add(1)
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
		}()
		s.executeCycle()
	}()
}

// executeCycle runs runner.RunCycle with timeout and panic recovery.
func (s *Scheduler) executeCycle() {
	cycleID := uuid.New().String()
	start := time.Now()

	s.auditEvent("scheduler.cycle_started", map[string]any{
		"cycle_id":        cycleID,
		"timeout_seconds": int(s.timeout.Seconds()),
	})
	s.logger.Info("scheduler_cycle_started", zap.String("cycle_id", cycleID))

	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	var report *actions.CycleReport
	var cycleErr error

	// Panic recovery — must not kill the scheduler loop.
	func() {
		defer func() {
			if r := recover(); r != nil {
				cycleErr = fmt.Errorf("panic: %v", r)
				s.logger.Error("scheduler_cycle_panic",
					zap.String("cycle_id", cycleID),
					zap.Any("panic", r),
				)
			}
		}()
		report, cycleErr = s.runner.RunCycle(ctx)
	}()

	duration := time.Since(start)
	s.mu.Lock()
	s.lastRunAt = start
	s.lastDuration = duration
	s.mu.Unlock()

	if cycleErr != nil {
		s.auditEvent("scheduler.cycle_failed", map[string]any{
			"cycle_id":    cycleID,
			"duration_ms": duration.Milliseconds(),
			"reason":      cycleErr.Error(),
		})
		s.logger.Error("scheduler_cycle_failed",
			zap.String("cycle_id", cycleID),
			zap.Duration("duration", duration),
			zap.Error(cycleErr),
		)
		return
	}

	payload := map[string]any{
		"cycle_id":    cycleID,
		"duration_ms": duration.Milliseconds(),
	}
	if report != nil {
		payload["actions_planned"] = len(report.Planned)
		payload["actions_executed"] = len(report.Executed)
		payload["actions_rejected"] = len(report.Rejected)
		payload["actions_failed"] = len(report.Failed)
	}
	s.auditEvent("scheduler.cycle_completed", payload)
	s.logger.Info("scheduler_cycle_completed",
		zap.String("cycle_id", cycleID),
		zap.Duration("duration", duration),
	)
}

// auditEvent records an audit event for a scheduler lifecycle step.
func (s *Scheduler) auditEvent(eventType string, payload map[string]any) {
	if s.auditor == nil {
		return
	}

	entityID := uuid.New()
	if err := s.auditor.RecordEvent(
		context.Background(),
		"scheduler",
		entityID,
		eventType,
		"system",
		"scheduler",
		payload,
	); err != nil {
		s.logger.Warn("scheduler_audit_failed",
			zap.String("event_type", eventType),
			zap.Error(err),
		)
	}
}
