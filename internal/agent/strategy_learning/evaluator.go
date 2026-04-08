package strategylearning

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// Evaluator determines strategy outcomes from action-level outcomes and
// updates strategy memory. It is the bridge between action-level
// learning (Iteration 5) and strategy-level learning (this iteration).
type Evaluator struct {
	store   *MemoryStore
	auditor audit.AuditRecorder
	logger  *zap.Logger
}

// NewEvaluator creates a strategy outcome Evaluator.
func NewEvaluator(store *MemoryStore, auditor audit.AuditRecorder, logger *zap.Logger) *Evaluator {
	return &Evaluator{
		store:   store,
		auditor: auditor,
		logger:  logger,
	}
}

// EvaluateOutcome determines the strategy-level outcome from an action outcome.
// Backward-compatible: delegates to EvaluateOutcomeWithSteps with empty step-level fields.
func (e *Evaluator) EvaluateOutcome(
	ctx context.Context,
	strategyID uuid.UUID,
	strategyType string,
	goalType string,
	step1Action string,
	actionOutcomeStatus string,
	improvement bool,
	step2Executed bool,
) (*StrategyOutcome, error) {
	return e.EvaluateOutcomeWithSteps(ctx, strategyID, strategyType, goalType,
		step1Action, actionOutcomeStatus, improvement, step2Executed,
		"", "") // empty step1/step2 statuses → legacy path
}

// EvaluateOutcomeWithSteps determines the strategy-level outcome with
// step-level granularity and continuation gain detection.
//
// Parameters:
//   - step1Status: the outcome of step 1 ("success", "neutral", "failure")
//   - step2Status: the outcome of step 2 ("success", "neutral", "failure", "")
//     Empty string means step 2 was not executed.
//
// Continuation gain = step2 succeeded AND step1 did NOT succeed.
// This captures "step 2 added value beyond step 1".
func (e *Evaluator) EvaluateOutcomeWithSteps(
	ctx context.Context,
	strategyID uuid.UUID,
	strategyType string,
	goalType string,
	step1Action string,
	actionOutcomeStatus string,
	improvement bool,
	step2Executed bool,
	step1Status string,
	step2Status string,
) (*StrategyOutcome, error) {
	now := time.Now().UTC()

	// Determine final strategy status from action outcome (backward compat).
	finalStatus := classifyOutcome(actionOutcomeStatus)

	// Compute continuation signals.
	continuationUsed := step2Executed && step2Status != ""
	continuationGain := continuationUsed &&
		step2Status == OutcomeSuccess &&
		step1Status != OutcomeSuccess

	outcome := StrategyOutcome{
		ID:               uuid.New(),
		StrategyID:       strategyID,
		StrategyType:     strategyType,
		GoalType:         goalType,
		Step1Action:      step1Action,
		Step2Executed:    step2Executed,
		FinalStatus:      finalStatus,
		Improvement:      improvement,
		EvaluatedAt:      now,
		Step1Status:      step1Status,
		Step2Status:      step2Status,
		ContinuationUsed: continuationUsed,
		ContinuationGain: continuationGain,
	}

	// Persist outcome (best-effort).
	if err := e.store.SaveOutcome(ctx, outcome); err != nil {
		e.logger.Warn("strategy_outcome_persist_failed", zap.Error(err))
	}

	// Update strategy memory counters with step-level data.
	if err := e.store.UpdateMemoryWithContinuation(ctx, strategyType, goalType,
		finalStatus, step1Status, step2Status,
		continuationUsed, continuationGain); err != nil {
		e.logger.Warn("strategy_memory_update_failed", zap.Error(err))
	}

	// Audit the outcome evaluation.
	e.auditEvent(ctx, "strategy.outcome_evaluated", map[string]any{
		"strategy_id":       strategyID.String(),
		"strategy_type":     strategyType,
		"goal_type":         goalType,
		"step1_action":      step1Action,
		"step2_executed":    step2Executed,
		"final_status":      finalStatus,
		"improvement":       improvement,
		"step1_status":      step1Status,
		"step2_status":      step2Status,
		"continuation_used": continuationUsed,
		"continuation_gain": continuationGain,
	})

	// Generate and audit feedback signal.
	fb := e.generateFeedback(ctx, strategyType, goalType)
	if fb != nil {
		e.auditEvent(ctx, "strategy.feedback_generated", map[string]any{
			"strategy_type":       fb.StrategyType,
			"goal_type":           fb.GoalType,
			"recommendation":      string(fb.Recommendation),
			"success_rate":        fb.SuccessRate,
			"failure_rate":        fb.FailureRate,
			"sample_size":         fb.SampleSize,
			"prefer_continuation": fb.PreferContinuation,
			"avoid_continuation":  fb.AvoidContinuation,
		})
	}

	return &outcome, nil
}

// generateFeedback produces a feedback signal from the current memory state.
// Returns nil if memory lookup fails (fail-open).
func (e *Evaluator) generateFeedback(ctx context.Context, strategyType, goalType string) *StrategyFeedback {
	record, err := e.store.GetMemory(ctx, strategyType, goalType)
	if err != nil || record == nil {
		return nil
	}
	fb := GenerateFeedback(record)
	return &fb
}

// GetStore returns the underlying memory store.
func (e *Evaluator) GetStore() *MemoryStore {
	return e.store
}

// --- Classification ---

// classifyOutcome maps an action-level outcome status to a strategy-level status.
func classifyOutcome(actionOutcomeStatus string) string {
	switch actionOutcomeStatus {
	case "success":
		return OutcomeSuccess
	case "failure":
		return OutcomeFailure
	default:
		return OutcomeNeutral
	}
}

// --- Feedback Generation ---

// GenerateFeedback produces a deterministic feedback signal from a memory record.
func GenerateFeedback(record *StrategyMemoryRecord) StrategyFeedback {
	if record == nil {
		return StrategyFeedback{
			Recommendation: RecommendInsufficientStrategy,
		}
	}

	fb := StrategyFeedback{
		StrategyType:   record.StrategyType,
		GoalType:       record.GoalType,
		SuccessRate:    record.SuccessRate,
		FailureRate:    record.FailureRate,
		SampleSize:     record.TotalRuns,
		LastUpdated:    record.LastUpdated,
		Recommendation: RecommendNeutralStrategy,
	}

	switch {
	case record.TotalRuns < MinSampleSize:
		fb.Recommendation = RecommendInsufficientStrategy
	case record.FailureRate >= AvoidFailureRate:
		fb.Recommendation = RecommendAvoidStrategy
	case record.SuccessRate >= PreferSuccessRate:
		fb.Recommendation = RecommendPreferStrategy
	default:
		fb.Recommendation = RecommendNeutralStrategy
	}

	// Continuation signals (Iteration 18.1).
	// Only emit signals when we have enough continuation samples.
	if record.ContinuationUsedRuns >= MinContinuationSampleSize {
		if record.ContinuationGainRate >= PreferContinuationGainRate {
			fb.PreferContinuation = true
		} else if record.ContinuationGainRate <= AvoidContinuationGainRate {
			fb.AvoidContinuation = true
		}
	}

	return fb
}

// --- Audit ---

func (e *Evaluator) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if e.auditor == nil {
		return
	}
	_ = e.auditor.RecordEvent(ctx, "strategy_learning", uuid.New(), eventType,
		"system", "strategy_evaluator", payload)
}
