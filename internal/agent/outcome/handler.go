package outcome

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/actions"
	"github.com/tiroq/arcanum/internal/audit"
)

// Handler implements actions.OutcomeHandler by evaluating, persisting,
// and auditing the real-world outcome of each executed action.
type Handler struct {
	evaluator       *DBEvaluator
	store           *Store
	memoryStore     *actionmemory.Store
	pathOutcomeEval PathOutcomeEvaluator
	auditor         audit.AuditRecorder
	logger          *zap.Logger
}

// PathOutcomeEvaluator evaluates path-level outcomes after action outcomes.
// Defined here to avoid import cycles — implemented in path_learning package.
type PathOutcomeEvaluator interface {
	EvaluatePathOutcome(
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
	) error
}

// NewHandler creates an OutcomeHandler.
func NewHandler(evaluator *DBEvaluator, store *Store, auditor audit.AuditRecorder, logger *zap.Logger) *Handler {
	return &Handler{
		evaluator: evaluator,
		store:     store,
		auditor:   auditor,
		logger:    logger,
	}
}

// WithMemoryStore attaches an action memory store for learning accumulation.
func (h *Handler) WithMemoryStore(ms *actionmemory.Store) *Handler {
	h.memoryStore = ms
	return h
}

// WithPathOutcomeEvaluator attaches a path outcome evaluator for path-level learning.
func (h *Handler) WithPathOutcomeEvaluator(pe PathOutcomeEvaluator) *Handler {
	h.pathOutcomeEval = pe
	return h
}

// HandleOutcome evaluates the action's real-world impact, persists the
// outcome, updates action memory, and emits audit events.
func (h *Handler) HandleOutcome(ctx context.Context, action actions.Action, result actions.ActionResult) error {
	o, err := h.evaluator.Evaluate(ctx, action, result)
	if err != nil {
		return fmt.Errorf("evaluate outcome: %w", err)
	}

	if err := h.store.Save(ctx, o); err != nil {
		return fmt.Errorf("persist outcome: %w", err)
	}

	// Audit: action.outcome_evaluated
	if h.auditor != nil {
		entityID, parseErr := uuid.Parse(action.ID)
		if parseErr != nil {
			entityID = uuid.New()
		}
		_ = h.auditor.RecordEvent(ctx, "action", entityID, "action.outcome_evaluated", "system", "action_engine", map[string]any{
			"action_id":       action.ID,
			"outcome_id":      o.ID.String(),
			"outcome_status":  string(o.OutcomeStatus),
			"effect_detected": o.EffectDetected,
			"improvement":     o.Improvement,
		})
	}

	h.logger.Info("outcome_evaluated",
		zap.String("action_id", action.ID),
		zap.String("outcome_status", string(o.OutcomeStatus)),
		zap.Bool("effect_detected", o.EffectDetected),
		zap.Bool("improvement", o.Improvement),
	)

	// Update action memory (best-effort).
	if h.memoryStore != nil {
		memInput := actionmemory.OutcomeInput{
			ActionType:    o.ActionType,
			TargetType:    o.TargetType,
			TargetID:      o.TargetID,
			OutcomeStatus: string(o.OutcomeStatus),
		}
		if memErr := h.memoryStore.Update(ctx, memInput); memErr != nil {
			h.logger.Warn("action_memory_update_failed",
				zap.String("action_id", action.ID),
				zap.Error(memErr),
			)
		} else {
			// Generate and audit feedback signal.
			record, _ := h.memoryStore.GetByActionType(ctx, o.ActionType)
			fb := actionmemory.GenerateFeedback(record)
			h.auditFeedback(ctx, action, fb)
		}

		// Update contextual memory (best-effort).
		// Context dimensions are embedded in Action.Params by the adaptive planner.
		h.updateContextualMemory(ctx, action, *o)

		// Update provider-context memory (best-effort).
		h.updateProviderContextMemory(ctx, action, *o)
	}

	// Evaluate path outcome (Iteration 21, best-effort).
	h.evaluatePathOutcome(ctx, action, *o)

	return nil
}

// auditFeedback emits an action.feedback_generated audit event.
func (h *Handler) auditFeedback(ctx context.Context, action actions.Action, fb actionmemory.ActionFeedback) {
	if h.auditor == nil {
		return
	}

	entityID, err := uuid.Parse(action.ID)
	if err != nil {
		entityID = uuid.New()
	}

	_ = h.auditor.RecordEvent(ctx, "action", entityID, "action.feedback_generated", "system", "action_engine", map[string]any{
		"action_type":    fb.ActionType,
		"success_rate":   fb.SuccessRate,
		"failure_rate":   fb.FailureRate,
		"sample_size":    fb.SampleSize,
		"recommendation": string(fb.Recommendation),
	})

	h.logger.Info("feedback_generated",
		zap.String("action_type", fb.ActionType),
		zap.Float64("success_rate", fb.SuccessRate),
		zap.Float64("failure_rate", fb.FailureRate),
		zap.String("recommendation", string(fb.Recommendation)),
	)
}

// updateContextualMemory extracts context dimensions from Action.Params
// and updates the contextual memory store. Dimensions are embedded at
// planning time by the adaptive planner. If missing, the update is
// silently skipped (backward compatible with non-adaptive actions).
func (h *Handler) updateContextualMemory(ctx context.Context, action actions.Action, o ActionOutcome) {
	goalType, _ := action.Params["_ctx_goal_type"].(string)
	failureBucket, _ := action.Params["_ctx_failure_bucket"].(string)
	backlogBucket, _ := action.Params["_ctx_backlog_bucket"].(string)

	// All three required — skip if any dimension is missing.
	if goalType == "" || failureBucket == "" || backlogBucket == "" {
		return
	}

	ctxInput := actionmemory.ContextOutcomeInput{
		ActionType:    o.ActionType,
		GoalType:      goalType,
		JobType:       "", // not tracked yet
		FailureBucket: failureBucket,
		BacklogBucket: backlogBucket,
		OutcomeStatus: string(o.OutcomeStatus),
	}
	if err := h.memoryStore.UpdateContext(ctx, ctxInput); err != nil {
		h.logger.Warn("contextual_memory_update_failed",
			zap.String("action_id", action.ID),
			zap.String("goal_type", goalType),
			zap.Error(err),
		)
	} else {
		h.logger.Info("contextual_memory_updated",
			zap.String("action_type", o.ActionType),
			zap.String("goal_type", goalType),
			zap.String("failure_bucket", failureBucket),
			zap.String("backlog_bucket", backlogBucket),
			zap.String("outcome_status", string(o.OutcomeStatus)),
		)
	}
}

// updateProviderContextMemory extracts provider dimensions from Action.Params
// and updates the provider-context memory store. Provider dimensions are
// embedded at planning time. If missing, the update is silently skipped
// (backward compatible with non-provider-aware actions).
func (h *Handler) updateProviderContextMemory(ctx context.Context, action actions.Action, o ActionOutcome) {
	providerName, _ := action.Params["_ctx_provider_name"].(string)
	if providerName == "" {
		return // No provider info — skip silently.
	}

	goalType, _ := action.Params["_ctx_goal_type"].(string)
	modelRole, _ := action.Params["_ctx_model_role"].(string)
	failureBucket, _ := action.Params["_ctx_failure_bucket"].(string)
	backlogBucket, _ := action.Params["_ctx_backlog_bucket"].(string)

	pcInput := actionmemory.ProviderContextOutcomeInput{
		ActionType:    o.ActionType,
		GoalType:      goalType,
		JobType:       "",
		ProviderName:  providerName,
		ModelRole:     modelRole,
		FailureBucket: failureBucket,
		BacklogBucket: backlogBucket,
		OutcomeStatus: string(o.OutcomeStatus),
	}
	if err := h.memoryStore.UpdateProviderContext(ctx, pcInput); err != nil {
		h.logger.Warn("provider_context_memory_update_failed",
			zap.String("action_id", action.ID),
			zap.String("provider_name", providerName),
			zap.Error(err),
		)
	} else {
		h.logger.Info("provider_context_memory_updated",
			zap.String("action_type", o.ActionType),
			zap.String("provider_name", providerName),
			zap.String("model_role", modelRole),
			zap.String("outcome_status", string(o.OutcomeStatus)),
		)
	}
}

// evaluatePathOutcome extracts path metadata from Action.Params and evaluates
// the path outcome. Only runs when path metadata is present (decision graph override)
// and a PathOutcomeEvaluator is configured. Best-effort: failures are logged.
func (h *Handler) evaluatePathOutcome(ctx context.Context, action actions.Action, o ActionOutcome) {
	if h.pathOutcomeEval == nil {
		return
	}

	// Extract path metadata embedded by the adaptive planner (Iteration 21).
	pathSig, _ := action.Params["_ctx_path_signature"].(string)
	if pathSig == "" {
		return // No path context — action was not from a decision graph override.
	}

	goalType, _ := action.Params["_ctx_goal_type"].(string)
	strategyID, _ := action.Params["_ctx_strategy_id"].(string)

	// Recover path action types.
	var pathActionTypes []string
	if raw, ok := action.Params["_ctx_path_action_types"]; ok {
		switch v := raw.(type) {
		case []string:
			pathActionTypes = v
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					pathActionTypes = append(pathActionTypes, s)
				}
			}
		}
	}

	if len(pathActionTypes) == 0 {
		return // Cannot evaluate without path action types.
	}

	pathLengthRaw, _ := action.Params["_ctx_path_length"].(int)
	if pathLengthRaw == 0 {
		// Try float64 cast (JSON numbers are float64).
		if f, ok := action.Params["_ctx_path_length"].(float64); ok {
			pathLengthRaw = int(f)
		}
	}

	// Determine first step status from the action outcome.
	firstStepStatus := string(o.OutcomeStatus)

	// Continuation: check if step 2 was executed.
	// Step metadata from the params context.
	continuationUsed := false
	step2Status := ""
	executedTransitions := 0
	// Only the first step is executed by default; continuation requires separate logic.
	// executedTransitions = 0 means only first step ran (no transitions actualized).

	if err := h.pathOutcomeEval.EvaluatePathOutcome(
		ctx,
		strategyID,
		goalType,
		pathActionTypes,
		firstStepStatus,
		continuationUsed,
		step2Status,
		firstStepStatus, // finalStatus = firstStepStatus when only first step executed
		o.Improvement,
		executedTransitions,
	); err != nil {
		h.logger.Warn("path_outcome_evaluation_failed",
			zap.String("action_id", action.ID),
			zap.String("path_signature", pathSig),
			zap.Error(err),
		)
	}
}
