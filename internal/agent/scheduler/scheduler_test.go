package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"sync"

	"github.com/google/uuid"
	"github.com/tiroq/arcanum/internal/agent/actions"
	"go.uber.org/zap"
)

// --- Test helpers ---

type mockCycleRunner struct {
	mu       sync.Mutex
	calls    int
	blockCh  chan struct{}
	panicMsg string
	err      error
}

func (m *mockCycleRunner) RunCycle(ctx context.Context) (*actions.CycleReport, error) {
	m.mu.Lock()
	m.calls++
	panicMsg := m.panicMsg
	err := m.err
	blockCh := m.blockCh
	m.mu.Unlock()

	if panicMsg != "" {
		panic(panicMsg)
	}

	if blockCh != nil {
		select {
		case <-blockCh:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if err != nil {
		return nil, err
	}

	return &actions.CycleReport{
		CycleID:   "test-cycle",
		Timestamp: time.Now(),
	}, nil
}

func (m *mockCycleRunner) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// concurrencyTrackingRunner wraps a CycleRunner and tracks concurrent entries.
type concurrencyTrackingRunner struct {
	inner       CycleRunner
	current     int32
	maxObserved int32
}

func (c *concurrencyTrackingRunner) RunCycle(ctx context.Context) (*actions.CycleReport, error) {
	cur := atomic.AddInt32(&c.current, 1)
	defer atomic.AddInt32(&c.current, -1)

	for {
		old := atomic.LoadInt32(&c.maxObserved)
		if cur <= old || atomic.CompareAndSwapInt32(&c.maxObserved, old, cur) {
			break
		}
	}

	return c.inner.RunCycle(ctx)
}

type auditEntry struct {
	eventType string
	payload   any
}

type mockAuditor struct {
	mu      sync.Mutex
	entries []auditEntry
}

func (a *mockAuditor) RecordEvent(_ context.Context, _ string, _ uuid.UUID, eventType, _, _ string, payload any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, auditEntry{eventType: eventType, payload: payload})
	return nil
}

func (a *mockAuditor) eventTypes() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var types []string
	for _, e := range a.entries {
		types = append(types, e.eventType)
	}
	return types
}

func hasEvent(types []string, target string) bool {
	for _, t := range types {
		if t == target {
			return true
		}
	}
	return false
}

func countEvent(types []string, target string) int {
	n := 0
	for _, t := range types {
		if t == target {
			n++
		}
	}
	return n
}

func noopLogger() *zap.Logger {
	return zap.NewNop()
}

func newTestScheduler(runner CycleRunner, aud *mockAuditor) *Scheduler {
	return newTestSchedulerWithTiming(runner, aud, 50*time.Millisecond, 30*time.Millisecond)
}

func newTestSchedulerWithTiming(runner CycleRunner, aud *mockAuditor, interval, timeout time.Duration) *Scheduler {
	return New(runner, interval, timeout, aud, noopLogger())
}

// --- Tests ---

func TestScheduler_PeriodicExecution(t *testing.T) {
	runner := &mockCycleRunner{}
	aud := &mockAuditor{}
	s := newTestScheduler(runner, aud)

	s.Start()
	time.Sleep(180 * time.Millisecond)
	s.Stop()

	calls := runner.callCount()
	if calls < 2 {
		t.Fatalf("expected at least 2 cycle calls, got %d", calls)
	}

	types := aud.eventTypes()
	if !hasEvent(types, "scheduler.started") {
		t.Error("missing scheduler.started audit event")
	}
	if !hasEvent(types, "scheduler.stopped") {
		t.Error("missing scheduler.stopped audit event")
	}
	if !hasEvent(types, "scheduler.cycle_started") {
		t.Error("missing scheduler.cycle_started audit event")
	}
	if !hasEvent(types, "scheduler.cycle_completed") {
		t.Error("missing scheduler.cycle_completed audit event")
	}
}

func TestScheduler_SingleFlight(t *testing.T) {
	blockCh := make(chan struct{})
	runner := &mockCycleRunner{blockCh: blockCh}
	wrapper := &concurrencyTrackingRunner{inner: runner}
	aud := &mockAuditor{}
	s := newTestSchedulerWithTiming(wrapper, aud, 30*time.Millisecond, 500*time.Millisecond)

	s.Start()
	time.Sleep(150 * time.Millisecond)
	close(blockCh)
	time.Sleep(20 * time.Millisecond)
	s.Stop()

	maxC := atomic.LoadInt32(&wrapper.maxObserved)
	if maxC > 1 {
		t.Errorf("max concurrent cycles = %d, expected 1", maxC)
	}

	types := aud.eventTypes()
	if !hasEvent(types, "scheduler.cycle_skipped") {
		t.Error("expected cycle_skipped audit event for overlapping ticks")
	}
}

func TestScheduler_SkipVisibility(t *testing.T) {
	blockCh := make(chan struct{})
	runner := &mockCycleRunner{blockCh: blockCh}
	aud := &mockAuditor{}
	s := newTestSchedulerWithTiming(runner, aud, 30*time.Millisecond, 200*time.Millisecond)

	s.Start()
	time.Sleep(120 * time.Millisecond)
	close(blockCh)
	time.Sleep(20 * time.Millisecond)
	s.Stop()

	types := aud.eventTypes()
	skips := countEvent(types, "scheduler.cycle_skipped")
	if skips < 1 {
		t.Errorf("expected at least 1 scheduler.cycle_skipped event, got %d", skips)
	}
}

func TestScheduler_TimeoutEnforcement(t *testing.T) {
	blockCh := make(chan struct{})
	runner := &mockCycleRunner{blockCh: blockCh}
	aud := &mockAuditor{}
	s := newTestSchedulerWithTiming(runner, aud, 200*time.Millisecond, 50*time.Millisecond)

	s.Start()
	time.Sleep(350 * time.Millisecond)
	s.Stop()
	close(blockCh)

	types := aud.eventTypes()
	if !hasEvent(types, "scheduler.cycle_failed") {
		t.Error("expected scheduler.cycle_failed for timed-out cycle")
	}
}

func TestScheduler_PanicRecovery(t *testing.T) {
	runner := &mockCycleRunner{panicMsg: "test panic"}
	aud := &mockAuditor{}
	s := newTestScheduler(runner, aud)

	s.Start()
	time.Sleep(120 * time.Millisecond)
	s.Stop()

	calls := runner.callCount()
	if calls < 2 {
		t.Fatalf("expected at least 2 calls despite panics, got %d", calls)
	}

	types := aud.eventTypes()
	if !hasEvent(types, "scheduler.cycle_failed") {
		t.Error("expected scheduler.cycle_failed for panicked cycle")
	}
	if !hasEvent(types, "scheduler.stopped") {
		t.Error("scheduler should stop cleanly after panics")
	}
}

func TestScheduler_StartStopLifecycle(t *testing.T) {
	runner := &mockCycleRunner{}
	aud := &mockAuditor{}
	s := newTestScheduler(runner, aud)

	s.Start()
	s.Start()
	time.Sleep(80 * time.Millisecond)

	s.Stop()
	s.Stop()

	types := aud.eventTypes()
	starts := countEvent(types, "scheduler.started")
	stops := countEvent(types, "scheduler.stopped")
	if starts != 1 {
		t.Errorf("expected 1 scheduler.started, got %d", starts)
	}
	if stops != 1 {
		t.Errorf("expected 1 scheduler.stopped, got %d", stops)
	}
}

func TestScheduler_StatusReporting(t *testing.T) {
	runner := &mockCycleRunner{}
	aud := &mockAuditor{}
	s := newTestScheduler(runner, aud)

	st := s.GetStatus(true)
	if st.Started {
		t.Error("expected started=false before Start()")
	}
	if st.Running {
		t.Error("expected running=false before Start()")
	}

	s.Start()
	time.Sleep(80 * time.Millisecond)

	st = s.GetStatus(true)
	if !st.Started {
		t.Error("expected started=true after Start()")
	}
	if !st.Enabled {
		t.Error("expected enabled=true when passed true")
	}

	s.Stop()

	st = s.GetStatus(false)
	if st.Started {
		t.Error("expected started=false after Stop()")
	}
}

func TestScheduler_StatusDuringRunning(t *testing.T) {
	blockCh := make(chan struct{})
	runner := &mockCycleRunner{blockCh: blockCh}
	aud := &mockAuditor{}
	s := newTestSchedulerWithTiming(runner, aud, 30*time.Millisecond, 500*time.Millisecond)

	s.Start()
	time.Sleep(60 * time.Millisecond)
	st := s.GetStatus(true)
	close(blockCh)
	s.Stop()

	if !st.Running {
		t.Error("expected running=true while cycle is blocked")
	}
}

func TestScheduler_CleanShutdownDuringCycle(t *testing.T) {
	blockCh := make(chan struct{})
	runner := &mockCycleRunner{blockCh: blockCh}
	aud := &mockAuditor{}
	s := newTestSchedulerWithTiming(runner, aud, 30*time.Millisecond, 2*time.Second)

	s.Start()
	time.Sleep(60 * time.Millisecond)

	go func() {
		time.Sleep(50 * time.Millisecond)
		close(blockCh)
	}()
	s.Stop()

	st := s.GetStatus(true)
	if st.Started {
		t.Error("expected started=false after Stop()")
	}
	if st.Running {
		t.Error("expected running=false after Stop()")
	}
}
