package strategy

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/audit"
)

// StabilityProvider reads current stability state without importing stability.
type StabilityProvider interface {
	GetMode(ctx context.Context) string
	GetBlockedActions(ctx context.Context) []string
}

// Engine orchestrates bounded multi-step strategy planning.
// Execution mode: plan_full_execute_first — persist full strategy,
// return only step 1 for execution in the current cycle.
type Engine struct {
	store     *Store
	stability StabilityProvider
	auditor   audit.AuditRecorder
	logger    *zap.Logger

	lastDecision           *StrategyDecision
	lastPortfolioSelection *PortfolioSelection
}

// NewEngine creates a strategy Engine.
func NewEngine(
	store *Store,
	stability StabilityProvider,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Engine {
	return &Engine{
		store:     store,
		stability: stability,
		auditor:   auditor,
		logger:    logger,
	}
}

// LastDecision returns the most recent strategy decision for API visibility.
func (e *Engine) LastDecision() *StrategyDecision {
	return e.lastDecision
}

// LastPortfolioSelection returns the most recent portfolio selection for API visibility.
func (e *Engine) LastPortfolioSelection() *PortfolioSelection {
	return e.lastPortfolioSelection
}

// Store returns the underlying strategy store for API queries.
func (e *Engine) GetStore() *Store {
	return e.store
}

// Evaluate generates, scores, and selects a bounded strategy for a goal.
// Returns the StrategyDecision and the first-step action type to execute.
//
// Fail-open: any error falls back to empty decision (caller uses tactical).
func (e *Engine) Evaluate(
	ctx context.Context,
	goalID string,
	goalType string,
	actionFeedback map[string]actionmemory.ActionFeedback,
	candidateScores map[string]float64,
	candidateConf map[string]float64,
	strategyFeedback map[string]StrategyFeedbackSignal,
	now time.Time,
) StrategyDecision {
	// 1. Generate bounded candidates.
	candidates := Generate(goalID, goalType, now)

	// 2. Build scoring input.
	input := ScoreInput{
		ActionFeedback:   actionFeedback,
		CandidateScores:  candidateScores,
		CandidateConf:    candidateConf,
		StrategyFeedback: strategyFeedback,
		StabilityMode:    "normal",
	}
	if e.stability != nil {
		input.StabilityMode = e.stability.GetMode(ctx)
		input.BlockedActions = e.stability.GetBlockedActions(ctx)
	}

	// 3. Score all candidates.
	scored := ScoreStrategies(candidates, input)

	// 4. Audit generation.
	e.auditEvent(ctx, "strategy.generated", map[string]any{
		"goal_id":         goalID,
		"goal_type":       goalType,
		"candidate_count": len(scored),
	})

	// 5. Select best strategy.
	decision := Select(scored, goalID, goalType, now)

	// 6. Audit selection.
	selected := SelectedPlan(decision)
	if selected != nil {
		e.auditEvent(ctx, "strategy.selected", map[string]any{
			"goal_id":          goalID,
			"goal_type":        goalType,
			"strategy_type":    string(selected.StrategyType),
			"expected_utility": selected.ExpectedUtility,
			"risk_score":       selected.RiskScore,
			"confidence":       selected.Confidence,
			"step_count":       selected.StepCount(),
			"explanation":      selected.Explanation,
			"selected":         true,
		})
	}

	// 7. Persist decision (best-effort).
	if e.store != nil && len(decision.CandidateStrategies) > 0 {
		if err := e.store.Save(ctx, decision); err != nil {
			e.logger.Warn("strategy_persist_failed", zap.Error(err))
		}
	}

	e.lastDecision = &decision
	return decision
}

// EvaluatePortfolio runs the full portfolio competition pipeline:
// generate → score → enrich → portfolio score → select.
// Returns the portfolio selection and the underlying strategy decision.
//
// Parameters:
//   - continuationGains: per-strategy continuation gain rates from strategy_learning
//   - shouldExplore: deterministic toggle for exploration override
//
// Fail-open: any error falls back to standard Evaluate().
func (e *Engine) EvaluatePortfolio(
	ctx context.Context,
	goalID string,
	goalType string,
	actionFeedback map[string]actionmemory.ActionFeedback,
	candidateScores map[string]float64,
	candidateConf map[string]float64,
	strategyFeedback map[string]StrategyFeedbackSignal,
	continuationGains map[string]float64,
	shouldExplore bool,
	now time.Time,
) (PortfolioSelection, StrategyDecision) {
	// 1. Generate bounded candidates.
	candidates := Generate(goalID, goalType, now)

	// 2. Build scoring input.
	stabilityMode := "normal"
	var blockedActions []string
	if e.stability != nil {
		stabilityMode = e.stability.GetMode(ctx)
		blockedActions = e.stability.GetBlockedActions(ctx)
	}

	baseInput := ScoreInput{
		ActionFeedback:   actionFeedback,
		CandidateScores:  candidateScores,
		CandidateConf:    candidateConf,
		StrategyFeedback: strategyFeedback,
		StabilityMode:    stabilityMode,
		BlockedActions:   blockedActions,
	}

	// 3. Score all candidates (existing pipeline).
	scored := ScoreStrategies(candidates, baseInput)

	// 4. Build portfolio with enrichment from all signal sources.
	portfolioInput := PortfolioInput{
		Base:              baseInput,
		StrategyMemory:    strategyFeedback,
		ContinuationGains: continuationGains,
		StabilityMode:     stabilityMode,
	}

	portfolio := BuildPortfolio(scored, portfolioInput)

	// 5. Audit generation.
	e.auditEvent(ctx, "strategy.portfolio_generated", map[string]any{
		"goal_id":         goalID,
		"goal_type":       goalType,
		"candidate_count": len(portfolio),
		"explore_toggle":  shouldExplore,
	})

	// 6. Select from portfolio.
	selectConfig := PortfolioSelectConfig{
		ShouldExplore: shouldExplore,
		StabilityMode: stabilityMode,
	}
	selection := SelectFromPortfolio(portfolio, selectConfig)

	// 7. Audit portfolio selection.
	if selection.Selected != nil {
		payload := map[string]any{
			"goal_id":          goalID,
			"goal_type":        goalType,
			"strategy_type":    string(selection.Selected.StrategyType),
			"final_score":      selection.Selected.FinalScore,
			"expected_value":   selection.Selected.ExpectedValue,
			"risk_score":       selection.Selected.RiskScore,
			"confidence":       selection.Selected.Confidence,
			"reason":           selection.Reason,
			"exploration_used": selection.ExplorationUsed,
		}
		e.auditEvent(ctx, "strategy.portfolio_selected", payload)
	}

	// 8. Build the underlying StrategyDecision for backward compat.
	decision := Select(scored, goalID, goalType, now)

	// If portfolio selected a different strategy, override the decision.
	if selection.Selected != nil && selection.Selected.Plan != nil {
		// Update the decision to reflect the portfolio selection.
		for i := range decision.CandidateStrategies {
			decision.CandidateStrategies[i].Selected = false
		}
		for i := range decision.CandidateStrategies {
			if decision.CandidateStrategies[i].ID.String() == selection.Selected.PlanID {
				decision.CandidateStrategies[i].Selected = true
				decision.SelectedStrategyID = decision.CandidateStrategies[i].ID
				break
			}
		}
		decision.Reason = "portfolio: " + selection.Reason
	}

	// 9. Persist decision (best-effort).
	if e.store != nil && len(decision.CandidateStrategies) > 0 {
		if err := e.store.Save(ctx, decision); err != nil {
			e.logger.Warn("strategy_persist_failed", zap.Error(err))
		}
	}

	e.lastDecision = &decision
	e.lastPortfolioSelection = &selection
	return selection, decision
}

// auditEvent records a strategy audit event.
func (e *Engine) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if e.auditor == nil {
		return
	}
	_ = e.auditor.RecordEvent(ctx, "strategy", uuid.New(), eventType,
		"system", "strategy_engine", payload)
}
