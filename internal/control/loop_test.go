package control

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

// --- stubs ---

type stubQueuer struct {
	reclaimCount         atomic.Int64
	requeueCount         atomic.Int64
	statsCallCount       atomic.Int64
	failUnknownCallCount atomic.Int64

	reclaimResult     int64
	requeueResult     int64
	failUnknownResult int64
}

func (q *stubQueuer) ReclaimExpiredLeases(_ context.Context) (int64, error) {
	q.reclaimCount.Add(1)
	return q.reclaimResult, nil
}

func (q *stubQueuer) RequeueScheduledRetries(_ context.Context) (int64, error) {
	q.requeueCount.Add(1)
	return q.requeueResult, nil
}

func (q *stubQueuer) QueueStats(_ context.Context) (map[string]int64, error) {
	q.statsCallCount.Add(1)
	return map[string]int64{
		"queued":          0,
		"leased":          0,
		"retry_scheduled": 0,
		"failed":          0,
		"dead_letter":     0,
	}, nil
}

func (q *stubQueuer) FailUnknownJobTypes(_ context.Context, _ []string) (int64, error) {
	q.failUnknownCallCount.Add(1)
	return q.failUnknownResult, nil
}

// --- tests ---

func TestLoop_StartStop(t *testing.T) {
	q := &stubQueuer{}
	loop := New(q, nil, nil, nil, zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	loop.Start(ctx)

	// Give the initial scan a chance to run.
	time.Sleep(50 * time.Millisecond)

	cancel()
	loop.Stop()

	// Initial scan should have fired exactly once.
	if q.reclaimCount.Load() < 1 {
		t.Error("expected ReclaimExpiredLeases to be called at least once")
	}
	if q.statsCallCount.Load() < 1 {
		t.Error("expected QueueStats to be called at least once")
	}
}

func TestLoop_ScanCallsAllThreeMethods(t *testing.T) {
	q := &stubQueuer{}
	loop := New(q, nil, nil, nil, zap.NewNop())

	ctx := context.Background()
	// Call scan directly (not Start/Stop, to avoid the ticker goroutine).
	loop.scan(ctx)

	if got := q.reclaimCount.Load(); got != 1 {
		t.Errorf("ReclaimExpiredLeases: want 1 call, got %d", got)
	}
	if got := q.requeueCount.Load(); got != 1 {
		t.Errorf("RequeueScheduledRetries: want 1 call, got %d", got)
	}
	if got := q.failUnknownCallCount.Load(); got != 1 {
		t.Errorf("FailUnknownJobTypes: want 1 call, got %d", got)
	}
	if got := q.statsCallCount.Load(); got != 1 {
		t.Errorf("QueueStats: want 1 call, got %d", got)
	}
}

func TestLoop_NoPublishWhenNilPublisher(t *testing.T) {
	// Verify that a nil publisher doesn't panic even when reclaim > 0.
	q := &stubQueuer{reclaimResult: 3, requeueResult: 2}
	loop := New(q, nil, nil, nil, zap.NewNop())

	// Should not panic.
	loop.scan(context.Background())
}
