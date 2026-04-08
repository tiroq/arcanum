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

	lastDecision *StrategyDecision
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

// auditEvent records a strategy audit event.
func (e *Engine) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if e.auditor == nil {
		return
	}
	_ = e.auditor.RecordEvent(ctx, "strategy", uuid.New(), eventType,
		"system", "strategy_engine", payload)
}
