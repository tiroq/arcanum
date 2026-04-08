package strategylearning

import (
	"context"

	"github.com/tiroq/arcanum/internal/agent/planning"
)

// PlannerAdapter adapts strategy learning feedback to the
// planning.StrategyLearningProvider interface so the planner
// can use strategy-level feedback without importing this package directly.
type PlannerAdapter struct {
	store *MemoryStore
}

// NewPlannerAdapter creates a PlannerAdapter.
func NewPlannerAdapter(store *MemoryStore) *PlannerAdapter {
	return &PlannerAdapter{store: store}
}

// GetStrategyFeedback implements planning.StrategyLearningProvider.
// Returns a map of strategy_type -> StrategyLearningFeedback for a given goal type.
func (a *PlannerAdapter) GetStrategyFeedback(ctx context.Context, goalType string) map[string]planning.StrategyLearningFeedback {
	records, err := a.store.ListMemory(ctx)
	if err != nil {
		return nil
	}

	result := make(map[string]planning.StrategyLearningFeedback)
	for _, r := range records {
		if r.GoalType != goalType {
			continue
		}
		fb := GenerateFeedback(&r)
		result[r.StrategyType] = planning.StrategyLearningFeedback{
			StrategyType:   r.StrategyType,
			SuccessRate:    fb.SuccessRate,
			FailureRate:    fb.FailureRate,
			SampleSize:     fb.SampleSize,
			Recommendation: string(fb.Recommendation),
		}
	}
	return result
}
