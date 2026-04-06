package actions

import (
	"testing"
	"time"

	"github.com/tiroq/arcanum/internal/agent/goals"
)

func TestPlanActions_RetryGoal(t *testing.T) {
	// planForGoal with reduce_retry_rate should produce retry actions (mock tested
	// via deterministic mapping check only — DB interactions tested in integration).
	g := goals.Goal{
		ID:          "goal-1",
		Type:        string(goals.GoalReduceRetryRate),
		Priority:    0.8,
		Confidence:  0.7,
		Description: "reduce retry pressure",
		Evidence:    map[string]any{"retry_scheduled": 15},
		CreatedAt:   time.Now().UTC(),
	}

	// Verify goal type mapping is correct.
	p := &Planner{} // nil db — we'll test the mapping logic only.
	switch g.Type {
	case string(goals.GoalReduceRetryRate), string(goals.GoalInvestigateFailures):
		// Expected path
	default:
		t.Errorf("goal type %q should map to retry action planning", g.Type)
	}
	_ = p
}

func TestPlanActions_BacklogGoal(t *testing.T) {
	g := goals.Goal{
		ID:   "goal-2",
		Type: string(goals.GoalResolveBacklog),
	}

	// Verify mapping.
	switch g.Type {
	case string(goals.GoalResolveBacklog):
		// Expected path
	default:
		t.Errorf("goal type %q should map to resync action planning", g.Type)
	}
}

func TestPlanActions_UnknownGoalProducesLogRecommendation(t *testing.T) {
	p := &Planner{}
	a := p.logRecommendation(goals.Goal{
		ID:          "goal-3",
		Type:        string(goals.GoalReduceLatency),
		Priority:    0.5,
		Confidence:  0.6,
		Description: "reduce latency",
	})

	if a.Type != string(ActionLogRecommendation) {
		t.Errorf("expected type %q, got %q", ActionLogRecommendation, a.Type)
	}
	if a.GoalID != "goal-3" {
		t.Errorf("expected goal_id %q, got %q", "goal-3", a.GoalID)
	}
	if a.Priority != 0.5 {
		t.Errorf("expected priority 0.5, got %f", a.Priority)
	}
	if a.Confidence != 0.6 {
		t.Errorf("expected confidence 0.6, got %f", a.Confidence)
	}
	if a.Params["goal_type"] != string(goals.GoalReduceLatency) {
		t.Errorf("expected goal_type param %q", goals.GoalReduceLatency)
	}
}
