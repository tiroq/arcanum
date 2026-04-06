package actionmemory

import (
	"context"
	"testing"
)

// mockStore simulates the Store for adapter tests without a real DB.
type mockMemoryStore struct {
	records map[string]*ActionMemoryRecord
}

func (m *mockMemoryStore) getByActionType(_ context.Context, actionType string) (*ActionMemoryRecord, error) {
	r, ok := m.records[actionType]
	if !ok {
		return nil, nil
	}
	return r, nil
}

// testableAdapter mirrors FeedbackAdapter logic using mockMemoryStore.
func testableAdapterShouldAvoid(record *ActionMemoryRecord) (bool, string) {
	if record == nil {
		return false, ""
	}
	fb := GenerateFeedback(record)
	if fb.Recommendation == RecommendAvoidAction {
		return true, "low historical success rate"
	}
	return false, ""
}

func TestAdapterShouldAvoid_NoHistory(t *testing.T) {
	avoid, _ := testableAdapterShouldAvoid(nil)
	if avoid {
		t.Error("should not avoid when no history exists")
	}
}

func TestAdapterShouldAvoid_InsufficientData(t *testing.T) {
	r := &ActionMemoryRecord{
		TotalRuns:   3,
		SuccessRuns: 0,
		FailureRuns: 3,
		FailureRate: 1.0,
	}
	avoid, _ := testableAdapterShouldAvoid(r)
	if avoid {
		t.Error("should not avoid with insufficient data, even with 100% failure")
	}
}

func TestAdapterShouldAvoid_HighFailure(t *testing.T) {
	r := &ActionMemoryRecord{
		ActionType:  "retry_job",
		TotalRuns:   10,
		SuccessRuns: 2,
		FailureRuns: 5,
		NeutralRuns: 3,
		SuccessRate: 0.2,
		FailureRate: 0.5,
	}
	avoid, reason := testableAdapterShouldAvoid(r)
	if !avoid {
		t.Error("should avoid action with failure_rate >= 0.5")
	}
	if reason == "" {
		t.Error("reason should be non-empty")
	}
}

func TestAdapterShouldAvoid_HealthyAction(t *testing.T) {
	r := &ActionMemoryRecord{
		ActionType:  "retry_job",
		TotalRuns:   10,
		SuccessRuns: 8,
		FailureRuns: 1,
		NeutralRuns: 1,
		SuccessRate: 0.8,
		FailureRate: 0.1,
	}
	avoid, _ := testableAdapterShouldAvoid(r)
	if avoid {
		t.Error("should not avoid a healthy action")
	}
}
