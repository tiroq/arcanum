package exploration

import (
	"context"
	"time"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/planning"
)

// PlannerAdapter adapts the exploration Engine to the planning.ExplorationProvider
// interface so the planner can call exploration without importing this package directly.
type PlannerAdapter struct {
	engine *Engine
}

// NewPlannerAdapter creates a PlannerAdapter wrapping the exploration engine.
func NewPlannerAdapter(engine *Engine) *PlannerAdapter {
	return &PlannerAdapter{engine: engine}
}

// EvaluateForPlanner implements planning.ExplorationProvider.
func (a *PlannerAdapter) EvaluateForPlanner(
	ctx context.Context,
	decision planning.PlanningDecision,
	globalFeedback map[string]actionmemory.ActionFeedback,
) planning.ExplorationDecision {
	now := time.Now().UTC()
	d := a.engine.Evaluate(ctx, decision, globalFeedback, now)
	return planning.ExplorationDecision{
		Chosen:           d.Chosen,
		ChosenActionType: d.ChosenActionType,
		DecisionReason:   d.DecisionReason,
	}
}
