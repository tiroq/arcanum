package strategy

import (
	"context"
	"time"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/planning"
)

// PlannerAdapter adapts the strategy Engine to the planning.StrategyProvider
// interface so the planner can call strategy evaluation without importing
// this package directly.
type PlannerAdapter struct {
	engine *Engine
}

// NewPlannerAdapter creates a PlannerAdapter wrapping the strategy engine.
func NewPlannerAdapter(engine *Engine) *PlannerAdapter {
	return &PlannerAdapter{engine: engine}
}

// EvaluateForPlanner implements planning.StrategyProvider.
func (a *PlannerAdapter) EvaluateForPlanner(
	ctx context.Context,
	decision planning.PlanningDecision,
	globalFeedback map[string]actionmemory.ActionFeedback,
	strategyLearning map[string]planning.StrategyLearningFeedback,
) planning.StrategyOverride {
	now := time.Now().UTC()

	// Build candidate scores and confidence from the tactical decision.
	candidateScores := make(map[string]float64, len(decision.Candidates))
	candidateConf := make(map[string]float64, len(decision.Candidates))
	for _, c := range decision.Candidates {
		candidateScores[c.ActionType] = c.Score
		candidateConf[c.ActionType] = c.Confidence
	}

	// Convert action memory feedback to the map the engine expects.
	feedbackMap := make(map[string]actionmemory.ActionFeedback, len(globalFeedback))
	for k, v := range globalFeedback {
		feedbackMap[k] = v
	}

	// Convert strategy learning feedback to engine signal type.
	var sfMap map[string]StrategyFeedbackSignal
	if len(strategyLearning) > 0 {
		sfMap = make(map[string]StrategyFeedbackSignal, len(strategyLearning))
		for k, v := range strategyLearning {
			sfMap[k] = StrategyFeedbackSignal{
				Recommendation: v.Recommendation,
				SuccessRate:    v.SuccessRate,
				FailureRate:    v.FailureRate,
				SampleSize:     v.SampleSize,
			}
		}
	}

	sd := a.engine.Evaluate(ctx, decision.GoalID, decision.GoalType,
		feedbackMap, candidateScores, candidateConf, sfMap, now)

	// Determine if strategy wants to override the tactical action.
	override := planning.StrategyOverride{
		Applied: false,
	}

	selected := SelectedPlan(sd)
	if selected == nil || selected.StrategyType == StrategyNoop {
		override.Reason = "no strategy selected or noop"
		return override
	}

	// Mode A: execute only the first step.
	firstStep := selected.FirstStep()
	if firstStep.ActionType == "" {
		override.Reason = "selected strategy has no steps"
		return override
	}

	// Only override if the strategy's first step differs from tactical.
	if firstStep.ActionType == decision.SelectedActionType {
		override.Reason = "strategy agrees with tactical selection"
		return override
	}

	override.Applied = true
	override.ActionType = firstStep.ActionType
	override.StrategyID = selected.ID.String()
	override.StrategyType = string(selected.StrategyType)
	override.Reason = sd.Reason

	return override
}
