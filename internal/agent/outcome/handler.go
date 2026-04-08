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
	evaluator               *DBEvaluator
	store                   *Store
	memoryStore             *actionmemory.Store
	pathOutcomeEval         PathOutcomeEvaluator
	comparativeEvaluator    ComparativeEvaluator
	counterfactualEvaluator CounterfactualPredictionEvaluator
	metaOutcomeEval         MetaReasoningOutcomeEvaluator
	calibrationRecorder     CalibrationRecorder
	contextCalibrationRecorder ContextualCalibrationRecorder
	auditor                 audit.AuditRecorder
	logger                  *zap.Logger
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

// ComparativeEvaluator evaluates comparative path selection outcomes.
// Defined here to avoid import cycles — implemented in path_comparison package.
type ComparativeEvaluator interface {
	EvaluateComparison(ctx context.Context, decisionID string, selectedOutcome string) error
}

// WithComparativeEvaluator attaches a comparative evaluator for decision quality learning.
func (h *Handler) WithComparativeEvaluator(ce ComparativeEvaluator) *Handler {
	h.comparativeEvaluator = ce
	return h
}

// CounterfactualPredictionEvaluator evaluates counterfactual prediction accuracy.
// Defined here to avoid import cycles — implemented in counterfactual package.
type CounterfactualPredictionEvaluator interface {
	EvaluatePrediction(ctx context.Context, decisionID, pathSignature, goalType, actualOutcomeStatus string) error
}

// WithCounterfactualEvaluator attaches a counterfactual prediction evaluator.
func (h *Handler) WithCounterfactualEvaluator(ce CounterfactualPredictionEvaluator) *Handler {
	h.counterfactualEvaluator = ce
	return h
}

// MetaReasoningOutcomeEvaluator updates mode memory after action outcomes.
// Defined here to avoid import cycles — implemented in meta_reasoning package.
type MetaReasoningOutcomeEvaluator interface {
	RecordOutcome(ctx context.Context, mode string, goalType string, success bool)
}

// WithMetaReasoningEvaluator attaches a meta-reasoning evaluator for mode outcome learning.
func (h *Handler) WithMetaReasoningEvaluator(mr MetaReasoningOutcomeEvaluator) *Handler {
	h.metaOutcomeEval = mr
	return h
}

// CalibrationRecorder records calibration data (predicted confidence vs actual outcome).
// Defined here to avoid import cycles — implemented in calibration package.
type CalibrationRecorder interface {
	RecordCalibrationOutcome(ctx context.Context, decisionID string, predictedConfidence float64, actualOutcome string) error
}

// WithCalibrationRecorder attaches a calibration recorder for confidence tracking.
func (h *Handler) WithCalibrationRecorder(cr CalibrationRecorder) *Handler {
	h.calibrationRecorder = cr
	return h
}

// ContextualCalibrationRecorder records per-context calibration data after outcomes.
// Defined here to avoid import cycles — implemented in calibration package.
type ContextualCalibrationRecorder interface {
	RecordContextCalibrationOutcome(ctx context.Context, goalType, providerName, strategyType string, predictedConfidence float64, actualOutcome string) error
}

// WithContextualCalibrationRecorder attaches a contextual calibration recorder (Iteration 26).
func (h *Handler) WithContextualCalibrationRecorder(cr ContextualCalibrationRecorder) *Handler {
	h.contextCalibrationRecorder = cr
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

	// Evaluate comparative outcome (Iteration 22, best-effort).
	h.evaluateComparativeOutcome(ctx, action, *o)

	// Evaluate counterfactual prediction accuracy (Iteration 23, best-effort).
	h.evaluateCounterfactualPrediction(ctx, action, *o)

	// Evaluate meta-reasoning outcome (Iteration 24, best-effort).
	h.evaluateMetaReasoningOutcome(ctx, action, *o)

	// Record calibration data (Iteration 25, best-effort).
	h.recordCalibration(ctx, action, *o)

	// Record contextual calibration data (Iteration 26, best-effort).
	h.recordContextCalibration(ctx, action, *o)

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

// evaluateComparativeOutcome extracts the decision ID from Action.Params and evaluates
// the comparative outcome. Only runs when decision ID is present (decision graph override)
// and a ComparativeEvaluator is configured. Best-effort: failures are logged.
func (h *Handler) evaluateComparativeOutcome(ctx context.Context, action actions.Action, o ActionOutcome) {
	if h.comparativeEvaluator == nil {
		return
	}

	decisionID, _ := action.Params["_ctx_decision_id"].(string)
	if decisionID == "" {
		return // No decision context — action was not from a decision graph override.
	}

	selectedOutcome := string(o.OutcomeStatus)

	if err := h.comparativeEvaluator.EvaluateComparison(ctx, decisionID, selectedOutcome); err != nil {
		h.logger.Warn("comparative_evaluation_failed",
			zap.String("action_id", action.ID),
			zap.String("decision_id", decisionID),
			zap.Error(err),
		)
	}
}

// evaluateCounterfactualPrediction extracts the decision ID and path signature from
// Action.Params and evaluates the prediction accuracy. Only runs when decision ID
// and path signature are present and a CounterfactualPredictionEvaluator is configured.
// Best-effort: failures are logged.
func (h *Handler) evaluateCounterfactualPrediction(ctx context.Context, action actions.Action, o ActionOutcome) {
	if h.counterfactualEvaluator == nil {
		return
	}

	decisionID, _ := action.Params["_ctx_decision_id"].(string)
	if decisionID == "" {
		return // No decision context.
	}

	pathSig, _ := action.Params["_ctx_path_signature"].(string)
	if pathSig == "" {
		return // No path context.
	}

	goalType, _ := action.Params["_ctx_goal_type"].(string)
	actualOutcome := string(o.OutcomeStatus)

	if err := h.counterfactualEvaluator.EvaluatePrediction(ctx, decisionID, pathSig, goalType, actualOutcome); err != nil {
		h.logger.Warn("counterfactual_evaluation_failed",
			zap.String("action_id", action.ID),
			zap.String("decision_id", decisionID),
			zap.Error(err),
		)
	}
}

// evaluateMetaReasoningOutcome extracts meta mode from Action.Params and updates
// mode memory. Only runs when meta mode is present and a MetaReasoningOutcomeEvaluator
// is configured. Best-effort: failures are logged.
func (h *Handler) evaluateMetaReasoningOutcome(ctx context.Context, action actions.Action, o ActionOutcome) {
	if h.metaOutcomeEval == nil {
		return
	}

	metaMode, _ := action.Params["_ctx_meta_mode"].(string)
	if metaMode == "" {
		return // No meta-reasoning context.
	}

	goalType, _ := action.Params["_ctx_goal_type"].(string)
	success := o.OutcomeStatus == "success"

	h.metaOutcomeEval.RecordOutcome(ctx, metaMode, goalType, success)
}

// recordCalibration extracts predicted confidence and decision ID from Action.Params
// and records a calibration data point. Only runs when a CalibrationRecorder is configured
// and decision ID is present. Best-effort: failures are logged.
func (h *Handler) recordCalibration(ctx context.Context, action actions.Action, o ActionOutcome) {
	if h.calibrationRecorder == nil {
		return
	}

	decisionID, _ := action.Params["_ctx_decision_id"].(string)
	if decisionID == "" {
		return // No decision context — action was not from a decision graph override.
	}

	// Extract predicted confidence. This is the confidence of the selected path's first node,
	// embedded by the planner. Fall back to checking float64 (JSON default).
	predictedConfidence := 0.0
	if v, ok := action.Params["_ctx_predicted_confidence"].(float64); ok {
		predictedConfidence = v
	} else {
		return // No confidence data available.
	}

	actualOutcome := string(o.OutcomeStatus)

	if err := h.calibrationRecorder.RecordCalibrationOutcome(ctx, decisionID, predictedConfidence, actualOutcome); err != nil {
		h.logger.Warn("calibration_record_failed",
			zap.String("action_id", action.ID),
			zap.String("decision_id", decisionID),
			zap.Error(err),
		)
	}
}

// recordContextCalibration records per-context calibration data for contextual
// confidence adjustment (Iteration 26). Extracts goal_type, provider_name,
// strategy_type, and predicted_confidence from Action.Params.
// Best-effort: failures are logged but do not block outcome processing.
func (h *Handler) recordContextCalibration(ctx context.Context, action actions.Action, o ActionOutcome) {
	if h.contextCalibrationRecorder == nil {
		return
	}

	predictedConfidence := 0.0
	if v, ok := action.Params["_ctx_predicted_confidence"].(float64); ok {
		predictedConfidence = v
	} else {
		return // No confidence data available.
	}

	goalType, _ := action.Params["_ctx_goal_type"].(string)
	providerName, _ := action.Params["_ctx_provider_name"].(string)
	strategyType, _ := action.Params["_ctx_strategy_type"].(string)

	actualOutcome := string(o.OutcomeStatus)

	if err := h.contextCalibrationRecorder.RecordContextCalibrationOutcome(ctx, goalType, providerName, strategyType, predictedConfidence, actualOutcome); err != nil {
		h.logger.Warn("context_calibration_record_failed",
			zap.String("action_id", action.ID),
			zap.String("goal_type", goalType),
			zap.Error(err),
		)
	}
}
