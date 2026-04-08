package strategylearning

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// StabilityProvider reads current stability state without importing stability.
type StabilityProvider interface {
	GetMode(ctx context.Context) string
	GetBlockedActions(ctx context.Context) []string
}

// ContinuationEngine evaluates whether step 2 of a strategy should be executed.
// It enforces all safety gates and hard limits.
//
// Hard limits:
//   - max 1 continuation per strategy (step 2 only)
//   - NEVER execute more than 2 steps total
//   - no continuation in safe_mode or throttled mode
//   - no continuation if system-wide failure_rate >= MaxContinuationFailureRate
type ContinuationEngine struct {
	store     *MemoryStore
	stability StabilityProvider
	auditor   audit.AuditRecorder
	logger    *zap.Logger
}

// NewContinuationEngine creates a ContinuationEngine.
func NewContinuationEngine(
	store *MemoryStore,
	stability StabilityProvider,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *ContinuationEngine {
	return &ContinuationEngine{
		store:     store,
		stability: stability,
		auditor:   auditor,
		logger:    logger,
	}
}

// EvaluateContinuation determines whether step 2 of a strategy should be executed.
//
// Parameters:
//   - strategyID: the UUID of the strategy plan
//   - strategyType: the strategy type
//   - goalType: the goal type
//   - step1OutcomeStatus: the outcome of step 1 ("success", "neutral", "failure")
//   - step2Action: the action type for step 2
//   - strategyConfidence: the confidence score from the strategy plan
//   - currentStep: the step number just completed (must be 1)
//
// ALL of the following must be true for continuation:
//  1. strategy has step 2 (step2Action non-empty)
//  2. step 1 outcome == neutral
//  3. system stability == normal
//  4. step 2 action NOT blocked by guardrails
//  5. strategy failure_rate < MaxContinuationFailureRate
//  6. strategy confidence >= MinContinuationConfidence
//  7. currentStep == 1 (hard limit: never continue past step 2)
func (ce *ContinuationEngine) EvaluateContinuation(
	ctx context.Context,
	strategyID string,
	strategyType string,
	goalType string,
	step1OutcomeStatus string,
	step2Action string,
	strategyConfidence float64,
	currentStep int,
) ContinuationDecision {
	now := time.Now().UTC()

	// Gate 0: Hard depth limit — NEVER execute past step 2.
	if currentStep != 1 {
		return ce.skip(ctx, strategyID, strategyType, now,
			"depth_limit: already past step 1")
	}

	// Gate 1: Must have a step 2.
	if step2Action == "" {
		return ce.skip(ctx, strategyID, strategyType, now,
			"no_step2: strategy has no second step")
	}

	// Gate 2: Step 1 outcome must be neutral.
	if step1OutcomeStatus != OutcomeNeutral {
		return ce.skip(ctx, strategyID, strategyType, now,
			"step1_not_neutral: outcome="+step1OutcomeStatus)
	}

	// Gate 3: System stability must be normal.
	if ce.stability != nil {
		mode := ce.stability.GetMode(ctx)
		if mode != "normal" {
			return ce.skip(ctx, strategyID, strategyType, now,
				"stability_not_normal: mode="+mode)
		}

		// Gate 4: Step 2 action must not be blocked.
		blocked := ce.stability.GetBlockedActions(ctx)
		for _, b := range blocked {
			if b == step2Action {
				return ce.skip(ctx, strategyID, strategyType, now,
					"step2_blocked: action="+step2Action)
			}
		}
	}

	// Gate 5: Strategy failure rate must be acceptable.
	if ce.store != nil {
		record, err := ce.store.GetMemory(ctx, strategyType, goalType)
		if err != nil {
			ce.logger.Warn("continuation_memory_lookup_failed", zap.Error(err))
			return ce.skip(ctx, strategyID, strategyType, now,
				"memory_error: "+err.Error())
		}
		if record != nil && record.TotalRuns >= MinSampleSize &&
			record.FailureRate >= MaxContinuationFailureRate {
			return ce.skip(ctx, strategyID, strategyType, now,
				"high_failure_rate: "+formatFloat(record.FailureRate))
		}
	}

	// Gate 6: Strategy confidence must be sufficient.
	if strategyConfidence < MinContinuationConfidence {
		return ce.skip(ctx, strategyID, strategyType, now,
			"low_confidence: "+formatFloat(strategyConfidence))
	}

	// All gates passed — continuation allowed.
	decision := ContinuationDecision{
		ShouldContinue: true,
		Step2Action:    step2Action,
		StrategyID:     strategyID,
		StrategyType:   strategyType,
		Reason:         "all_gates_passed",
	}

	ce.auditEvent(ctx, "strategy.continuation_executed", now, map[string]any{
		"strategy_id":        strategyID,
		"strategy_type":      strategyType,
		"goal_type":          goalType,
		"step2_action":       step2Action,
		"strategy_confidence": strategyConfidence,
	})

	return decision
}

// skip records a continuation skip and returns a non-continue decision.
func (ce *ContinuationEngine) skip(
	ctx context.Context,
	strategyID, strategyType string,
	now time.Time,
	reason string,
) ContinuationDecision {
	ce.auditEvent(ctx, "strategy.continuation_skipped", now, map[string]any{
		"strategy_id":   strategyID,
		"strategy_type": strategyType,
		"reason":        reason,
	})

	return ContinuationDecision{
		ShouldContinue: false,
		StrategyID:     strategyID,
		StrategyType:   strategyType,
		Reason:         reason,
	}
}

// auditEvent records a strategy learning audit event.
func (ce *ContinuationEngine) auditEvent(ctx context.Context, eventType string, _ time.Time, payload map[string]any) {
	if ce.auditor == nil {
		return
	}
	_ = ce.auditor.RecordEvent(ctx, "strategy_learning", uuid.New(), eventType,
		"system", "continuation_engine", payload)
}

// formatFloat formats a float64 with 2 decimal places for reason strings.
func formatFloat(f float64) string {
	// Use simple formatting to avoid fmt import.
	i := int(f * 100)
	whole := i / 100
	frac := i % 100
	if frac < 0 {
		frac = -frac
	}
	s := make([]byte, 0, 8)
	if whole == 0 && f < 0 {
		s = append(s, '-')
	}
	s = appendInt(s, whole)
	s = append(s, '.')
	if frac < 10 {
		s = append(s, '0')
	}
	s = appendInt(s, frac)
	return string(s)
}

func appendInt(b []byte, v int) []byte {
	if v < 0 {
		b = append(b, '-')
		v = -v
	}
	if v == 0 {
		return append(b, '0')
	}
	digits := make([]byte, 0, 4)
	for v > 0 {
		digits = append(digits, byte('0'+v%10))
		v /= 10
	}
	for i := len(digits) - 1; i >= 0; i-- {
		b = append(b, digits[i])
	}
	return b
}
