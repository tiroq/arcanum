package stability

import (
	"testing"
)

func TestSchedulerAdapter_RecordCycleResult_SlidingWindow(t *testing.T) {
	// Test the RecordCycleResult sliding-window decay logic.
	e := &Engine{}

	// Record 10 errors to trigger the decay.
	for i := 0; i < 10; i++ {
		e.RecordCycleResult(errForTest{})
	}

	if e.recentCycleTotal != 10 {
		t.Errorf("expected 10 total, got %d", e.recentCycleTotal)
	}
	if e.recentCycleErrors != 10 {
		t.Errorf("expected 10 errors, got %d", e.recentCycleErrors)
	}

	// 11th record triggers sliding-window halving.
	e.RecordCycleResult(nil)

	// After: total++=11, err==nil (errors=10), then 11>10 → errors=10/2=5, total=11/2=5.
	if e.recentCycleTotal != 5 {
		t.Errorf("after decay expected total=5, got %d", e.recentCycleTotal)
	}
	if e.recentCycleErrors != 5 {
		t.Errorf("after decay expected errors=5, got %d", e.recentCycleErrors)
	}
}

func TestSchedulerAdapter_RecordCycleResult_SuccessVsFailure(t *testing.T) {
	e := &Engine{}

	e.RecordCycleResult(nil) // success
	if e.recentCycleTotal != 1 {
		t.Errorf("expected total=1, got %d", e.recentCycleTotal)
	}
	if e.recentCycleErrors != 0 {
		t.Errorf("expected errors=0, got %d", e.recentCycleErrors)
	}

	e.RecordCycleResult(errForTest{}) // failure
	if e.recentCycleTotal != 2 {
		t.Errorf("expected total=2, got %d", e.recentCycleTotal)
	}
	if e.recentCycleErrors != 1 {
		t.Errorf("expected errors=1, got %d", e.recentCycleErrors)
	}
}

type errForTest struct{}

func (errForTest) Error() string { return "test error" }
