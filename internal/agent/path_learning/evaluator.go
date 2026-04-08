package pathlearning

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// Evaluator evaluates path outcomes and updates path and transition memory.
type Evaluator struct {
	memoryStore     *MemoryStore
	transitionStore *TransitionStore
	auditor         audit.AuditRecorder
	logger          *zap.Logger
}

// NewEvaluator creates a path learning evaluator.
func NewEvaluator(
	memoryStore *MemoryStore,
	transitionStore *TransitionStore,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Evaluator {
	return &Evaluator{
		memoryStore:     memoryStore,
		transitionStore: transitionStore,
		auditor:         auditor,
		logger:          logger,
	}
}

// EvaluatePathOutcome evaluates a path outcome and updates memory stores.
//
// Parameters:
//   - pathID: unique identifier for this path execution
//   - goalType: the goal type this path addresses
//   - pathActionTypes: ordered list of action types in the selected path
//   - firstStepStatus: outcome of the first step (success|neutral|failure)
//   - continuationUsed: whether continuation (step 2) was executed
//   - step2Status: outcome of step 2, empty if not executed
//   - finalStatus: overall outcome status (success|neutral|failure)
//   - improvement: whether the path led to measurable improvement
//   - executedTransitions: number of transitions that actually occurred (0 or 1 typically)
func (e *Evaluator) EvaluatePathOutcome(
	ctx context.Context,
	pathID string,
	goalType string,
	pathActionTypes []string,
	firstStepStatus string,
	continuationUsed bool,
	step2Status string,
	finalStatus string,
	improvement bool,
	executedTransitions int,
) error {
	pathSignature := BuildPathSignature(pathActionTypes)

	outcome := PathOutcome{
		ID:               uuid.New(),
		PathID:           pathID,
		GoalType:         goalType,
		PathSignature:    pathSignature,
		PathLength:       len(pathActionTypes),
		FirstStepAction:  firstStepAction(pathActionTypes),
		FirstStepStatus:  firstStepStatus,
		ContinuationUsed: continuationUsed,
		FinalStatus:      finalStatus,
		Improvement:      improvement,
		EvaluatedAt:      time.Now().UTC(),
	}

	// 1. Save path outcome record.
	if err := e.memoryStore.SavePathOutcome(ctx, outcome); err != nil {
		e.logger.Error("save_path_outcome_failed", zap.Error(err))
		return err
	}

	// 2. Update path memory (always — even on partial execution).
	if err := e.memoryStore.UpdatePathMemory(ctx, pathSignature, goalType, finalStatus); err != nil {
		e.logger.Error("update_path_memory_failed", zap.Error(err))
		// Non-fatal: continue with transition updates.
	}

	// 3. Update transition memory ONLY for transitions that actually occurred.
	// executedTransitions indicates how many transitions ran (0 = first step only).
	if executedTransitions > 0 && continuationUsed {
		transitions := ExtractTransitions(pathActionTypes)
		maxTransitions := executedTransitions
		if maxTransitions > len(transitions) {
			maxTransitions = len(transitions)
		}

		for i := 0; i < maxTransitions; i++ {
			tr := transitions[i]
			helpfulness := classifyTransitionHelpfulness(firstStepStatus, step2Status, finalStatus, improvement)

			if err := e.transitionStore.UpdateTransitionMemory(ctx, goalType, tr.From, tr.To, helpfulness); err != nil {
				e.logger.Error("update_transition_memory_failed",
					zap.String("transition_key", BuildTransitionKey(tr.From, tr.To)),
					zap.Error(err),
				)
			}

			// Audit transition feedback.
			e.auditEvent(ctx, "transition.feedback_generated", map[string]any{
				"goal_type":      goalType,
				"transition_key": BuildTransitionKey(tr.From, tr.To),
				"helpfulness":    helpfulness,
				"final_status":   finalStatus,
			})
		}
	}

	// 4. Audit path outcome.
	e.auditEvent(ctx, "path.outcome_evaluated", map[string]any{
		"path_id":              pathID,
		"goal_type":            goalType,
		"path_signature":       pathSignature,
		"path_length":          len(pathActionTypes),
		"first_step_status":    firstStepStatus,
		"continuation_used":    continuationUsed,
		"final_status":         finalStatus,
		"improvement":          improvement,
		"executed_transitions": executedTransitions,
	})

	// 5. Generate and audit path feedback.
	record, err := e.memoryStore.GetPathMemory(ctx, pathSignature, goalType)
	if err == nil && record != nil {
		fb := GeneratePathFeedback(record)
		if fb.Recommendation != RecommendNeutralPath {
			e.auditEvent(ctx, "path.feedback_generated", map[string]any{
				"path_signature": pathSignature,
				"goal_type":      goalType,
				"recommendation": string(fb.Recommendation),
				"success_rate":   fb.SuccessRate,
				"failure_rate":   fb.FailureRate,
				"sample_size":    fb.SampleSize,
			})
		}
	}

	return nil
}

// classifyTransitionHelpfulness determines whether a transition was helpful, unhelpful, or neutral.
//
// Rules:
//   - Helpful: step 1 was neutral/failure AND final result is success or improvement occurred
//   - Unhelpful: step 2 executed AND final result stayed neutral/failure AND no improvement
//   - Neutral: insufficient evidence or ambiguous final result
func classifyTransitionHelpfulness(firstStepStatus, step2Status, finalStatus string, improvement bool) string {
	// If step 2 didn't produce a meaningful status, treat as neutral.
	if step2Status == "" {
		return "neutral"
	}

	// Helpful: step 1 was not success, but final result is success or improvement.
	if firstStepStatus != OutcomeSuccess {
		if finalStatus == OutcomeSuccess || improvement {
			return "helpful"
		}
	}

	// Helpful: step 2 was success and contributed to final success.
	if step2Status == OutcomeSuccess && finalStatus == OutcomeSuccess {
		return "helpful"
	}

	// Unhelpful: step 2 ran but final result still neutral/failure and no improvement.
	if (finalStatus == OutcomeNeutral || finalStatus == OutcomeFailure) && !improvement {
		return "unhelpful"
	}

	return "neutral"
}

// firstStepAction extracts the first action type from the path.
func firstStepAction(actionTypes []string) string {
	if len(actionTypes) == 0 {
		return ""
	}
	return actionTypes[0]
}

// auditEvent records a path learning audit event.
func (e *Evaluator) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if e.auditor == nil {
		return
	}
	_ = e.auditor.RecordEvent(ctx, "path_learning", uuid.New(), eventType,
		"system", "path_learning_engine", payload)
}
