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
//
// Iteration 19: uses portfolio evaluation when available.
type PlannerAdapter struct {
	engine *Engine
	// explorationTrigger is a deterministic function that returns true
	// when the exploration engine says this goal should explore.
	// Set via WithExplorationTrigger. When nil, exploration is disabled.
	explorationTrigger func(goalType string) bool
}

// NewPlannerAdapter creates a PlannerAdapter wrapping the strategy engine.
func NewPlannerAdapter(engine *Engine) *PlannerAdapter {
	return &PlannerAdapter{engine: engine}
}

// WithExplorationTrigger sets a deterministic exploration trigger function.
func (a *PlannerAdapter) WithExplorationTrigger(fn func(goalType string) bool) *PlannerAdapter {
	a.explorationTrigger = fn
	return a
}

// EvaluateForPlanner implements planning.StrategyProvider.
// Iteration 19: runs portfolio competition pipeline, falls back to standard evaluation.
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
	continuationGains := make(map[string]float64)
	if len(strategyLearning) > 0 {
		sfMap = make(map[string]StrategyFeedbackSignal, len(strategyLearning))
		for k, v := range strategyLearning {
			sfMap[k] = StrategyFeedbackSignal{
				Recommendation:     v.Recommendation,
				SuccessRate:        v.SuccessRate,
				FailureRate:        v.FailureRate,
				SampleSize:         v.SampleSize,
				PreferContinuation: v.PreferContinuation,
				AvoidContinuation:  v.AvoidContinuation,
			}
			// Extract continuation gain rate for portfolio enrichment.
			if v.PreferContinuation {
				continuationGains[k] = 0.7 // preferred → high gain
			} else if v.AvoidContinuation {
				continuationGains[k] = 0.2 // avoided → low gain
			}
		}
	}

	// Determine exploration toggle.
	shouldExplore := false
	if a.explorationTrigger != nil {
		shouldExplore = a.explorationTrigger(decision.GoalType)
	}

	// Run portfolio evaluation (Iteration 19).
	selection, sd := a.engine.EvaluatePortfolio(ctx, decision.GoalID, decision.GoalType,
		feedbackMap, candidateScores, candidateConf, sfMap, continuationGains, shouldExplore, now)

	// Determine if strategy wants to override the tactical action.
	override := planning.StrategyOverride{
		Applied: false,
	}

	// Use portfolio selection to determine the override.
	if selection.Selected == nil || selection.Selected.Plan == nil {
		override.Reason = "no portfolio selection"
		return override
	}

	selectedPlan := selection.Selected.Plan
	if selectedPlan.StrategyType == StrategyNoop {
		override.Reason = "portfolio selected noop"
		return override
	}

	// Mode A: execute only the first step.
	firstStep := selectedPlan.FirstStep()
	if firstStep.ActionType == "" {
		override.Reason = "selected strategy has no steps"
		return override
	}

	// Only override if the strategy's first step differs from tactical.
	if firstStep.ActionType == decision.SelectedActionType {
		override.Reason = "portfolio agrees with tactical selection"
		return override
	}

	override.Applied = true
	override.ActionType = firstStep.ActionType
	override.StrategyID = selectedPlan.ID.String()
	override.StrategyType = string(selectedPlan.StrategyType)
	override.Reason = sd.Reason
	if selection.ExplorationUsed {
		override.Reason = "portfolio_exploration: " + selection.Reason
	}

	return override
}
