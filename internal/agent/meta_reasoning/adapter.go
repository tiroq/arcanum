package meta_reasoning

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// Engine orchestrates meta-reasoning: selects a mode, records the selection,
// and provides mode-specific configuration to the decision graph.
type Engine struct {
	memoryStore  *MemoryStore
	historyStore *HistoryStore
	auditor      audit.AuditRecorder
	logger       *zap.Logger

	// lastDecision stores the most recent mode decision for API visibility.
	lastDecision *ModeDecision
	// lastMode tracks the previously selected mode for inertia.
	lastModeByGoal map[string]DecisionMode
}

// NewEngine creates a meta-reasoning Engine.
func NewEngine(ms *MemoryStore, hs *HistoryStore, auditor audit.AuditRecorder, logger *zap.Logger) *Engine {
	return &Engine{
		memoryStore:    ms,
		historyStore:   hs,
		auditor:        auditor,
		logger:         logger,
		lastModeByGoal: make(map[string]DecisionMode),
	}
}

// Evaluate selects a reasoning mode for the given input, records the decision,
// and returns the ModeDecision. Fail-open: returns ModeGraph on any error.
func (e *Engine) Evaluate(ctx context.Context, input MetaInput) ModeDecision {
	// Inject inertia from previous selection for this goal type.
	if lastMode, ok := e.lastModeByGoal[input.GoalType]; ok {
		input.LastMode = &lastMode
	}

	// Retrieve historical memory for scoring (best-effort).
	memoryByMode := make(map[DecisionMode]*ModeMemoryRecord)
	if e.memoryStore != nil {
		mem, err := e.memoryStore.GetAllForGoal(ctx, input.GoalType)
		if err != nil {
			e.logger.Warn("meta_reasoning_memory_fetch_failed",
				zap.String("goal_type", input.GoalType),
				zap.Error(err),
			)
			// Continue with empty memory — fail-open.
		} else {
			memoryByMode = mem
		}
	}

	// Select mode.
	decision := SelectModeWithScoring(input, memoryByMode)

	// Update tracking state.
	e.lastDecision = &decision
	e.lastModeByGoal[input.GoalType] = decision.Mode

	// Record selection in memory (best-effort).
	if e.memoryStore != nil {
		if err := e.memoryStore.RecordSelection(ctx, decision.Mode, input.GoalType); err != nil {
			e.logger.Warn("meta_reasoning_selection_record_failed",
				zap.String("goal_type", input.GoalType),
				zap.String("mode", string(decision.Mode)),
				zap.Error(err),
			)
		}
	}

	// Record in history (best-effort).
	if e.historyStore != nil {
		if err := e.historyStore.RecordDecision(ctx, input.GoalType, decision); err != nil {
			e.logger.Warn("meta_reasoning_history_record_failed",
				zap.String("goal_type", input.GoalType),
				zap.Error(err),
			)
		}
	}

	// Audit event.
	e.auditEvent(ctx, "meta.mode_selected", map[string]any{
		"goal_type":  input.GoalType,
		"mode":       string(decision.Mode),
		"confidence": decision.Confidence,
		"reason":     decision.Reason,
	})

	e.logger.Info("meta_reasoning_mode_selected",
		zap.String("goal_type", input.GoalType),
		zap.String("mode", string(decision.Mode)),
		zap.Float64("confidence", decision.Confidence),
		zap.String("reason", decision.Reason),
	)

	return decision
}

// RecordOutcome updates mode memory after an action outcome.
// Fail-open: errors are logged but not returned.
func (e *Engine) RecordOutcome(ctx context.Context, mode string, goalType string, success bool) {
	dm := DecisionMode(mode)
	if !dm.IsValid() {
		return
	}

	if e.memoryStore != nil {
		if err := e.memoryStore.RecordOutcome(ctx, dm, goalType, success); err != nil {
			e.logger.Warn("meta_reasoning_outcome_record_failed",
				zap.String("goal_type", goalType),
				zap.String("mode", mode),
				zap.Bool("success", success),
				zap.Error(err),
			)
		}
	}

	// Update history with outcome (best-effort).
	outcomeStr := "failure"
	if success {
		outcomeStr = "success"
	}
	if e.historyStore != nil {
		if err := e.historyStore.UpdateOutcome(ctx, goalType, dm, outcomeStr); err != nil {
			e.logger.Warn("meta_reasoning_history_outcome_failed",
				zap.String("goal_type", goalType),
				zap.Error(err),
			)
		}
	}

	// Audit the outcome evaluation.
	e.auditEvent(ctx, "meta.mode_outcome", map[string]any{
		"goal_type": goalType,
		"mode":      mode,
		"success":   success,
		"outcome":   outcomeStr,
	})
}

// LastDecision returns the most recent mode decision for API visibility.
func (e *Engine) LastDecision() *ModeDecision {
	return e.lastDecision
}

// MemoryStore returns the memory store for API handlers.
func (e *Engine) MemoryStoreRef() *MemoryStore {
	return e.memoryStore
}

// HistoryStore returns the history store for API handlers.
func (e *Engine) HistoryStoreRef() *HistoryStore {
	return e.historyStore
}

// auditEvent records a meta-reasoning audit event.
func (e *Engine) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if e.auditor == nil {
		return
	}
	_ = e.auditor.RecordEvent(ctx, "meta_reasoning", uuid.New(), eventType,
		"system", "meta_reasoning_engine", payload)
}

// --- GraphPlannerAdapter bridge ---

// GraphAdapter implements decision_graph.MetaReasoningProvider.
// Bridges meta_reasoning.Engine to the decision_graph package interface.
type GraphAdapter struct {
	engine *Engine
}

// NewGraphAdapter creates a GraphAdapter.
func NewGraphAdapter(engine *Engine) *GraphAdapter {
	return &GraphAdapter{engine: engine}
}

// SelectMode implements decision_graph.MetaReasoningProvider.
// Returns: mode string, confidence float64, reason string.
func (a *GraphAdapter) SelectMode(ctx context.Context, goalType string, failureRate, confidence, risk float64, stabilityMode string, missedWinCount int, noopRate, lowValueRate float64) (string, float64, string) {
	input := MetaInput{
		GoalType:           goalType,
		FailureRate:        failureRate,
		Confidence:         confidence,
		Risk:               risk,
		StabilityMode:      stabilityMode,
		MissedWinCount:     missedWinCount,
		RecentNoopRate:     noopRate,
		RecentLowValueRate: lowValueRate,
	}
	d := a.engine.Evaluate(ctx, input)
	return string(d.Mode), d.Confidence, d.Reason
}

// RecordOutcome implements decision_graph.MetaReasoningProvider.
func (a *GraphAdapter) RecordOutcome(ctx context.Context, mode string, goalType string, success bool) {
	a.engine.RecordOutcome(ctx, mode, goalType, success)
}
