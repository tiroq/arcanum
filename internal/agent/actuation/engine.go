package actuation

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// Engine orchestrates the actuation pipeline:
// gather inputs → evaluate rules → create decisions → persist → audit.
type Engine struct {
	store   *DecisionStore
	auditor audit.AuditRecorder
	logger  *zap.Logger

	reflection ReflectionProvider
	objective  ObjectiveProvider
}

// NewEngine creates a new actuation engine.
func NewEngine(store *DecisionStore, auditor audit.AuditRecorder, logger *zap.Logger) *Engine {
	return &Engine{
		store:   store,
		auditor: auditor,
		logger:  logger,
	}
}

// WithReflection sets the reflection provider.
func (e *Engine) WithReflection(p ReflectionProvider) *Engine {
	e.reflection = p
	return e
}

// WithObjective sets the objective provider.
func (e *Engine) WithObjective(p ObjectiveProvider) *Engine {
	e.objective = p
	return e
}

// Run executes the full actuation pipeline:
// 1. Gather inputs from reflection + objective providers (fail-open).
// 2. Evaluate deterministic rules.
// 3. Convert proposals to decisions.
// 4. Persist decisions.
// 5. Emit audit events.
func (e *Engine) Run(ctx context.Context) (ActuationRunResult, error) {
	now := time.Now().UTC()

	// Audit: run started.
	e.auditEvent(ctx, "actuation.run_started", map[string]any{"run_at": now})

	inputs := e.gatherInputs(ctx)
	proposals := EvaluateRules(inputs)

	// Sort proposals by priority descending for deterministic ordering.
	sort.Slice(proposals, func(i, j int) bool {
		return proposals[i].Priority > proposals[j].Priority
	})

	var decisions []ActuationDecision
	for _, p := range proposals {
		d := ActuationDecision{
			ID:             uuid.New().String(),
			Type:           p.Type,
			Reason:         p.Reason,
			SignalSource:   p.Source,
			Confidence:     clamp01(p.Confidence),
			Priority:       clamp01(p.Priority),
			RequiresReview: ReviewRequired(p.Type),
			Status:         StatusProposed,
			Target:         RoutingTarget[p.Type],
			ProposedAt:     now,
		}

		if err := e.store.Insert(ctx, d); err != nil {
			e.logger.Warn("actuation: failed to persist decision", zap.Error(err))
			continue
		}

		e.auditEvent(ctx, "actuation.decision_created", map[string]any{
			"decision_id":     d.ID,
			"type":            d.Type,
			"reason":          d.Reason,
			"signal_source":   d.SignalSource,
			"confidence":      d.Confidence,
			"priority":        d.Priority,
			"requires_review": d.RequiresReview,
			"target":          d.Target,
		})

		decisions = append(decisions, d)
	}

	return ActuationRunResult{
		RunAt:      now,
		Decisions:  decisions,
		InputsUsed: inputs,
	}, nil
}

// ApproveDecision transitions a decision from proposed to approved.
func (e *Engine) ApproveDecision(ctx context.Context, id string) (ActuationDecision, error) {
	return e.transitionDecision(ctx, id, StatusApproved, "actuation.decision_approved")
}

// RejectDecision transitions a decision from proposed to rejected.
func (e *Engine) RejectDecision(ctx context.Context, id string) (ActuationDecision, error) {
	return e.transitionDecision(ctx, id, StatusRejected, "actuation.decision_rejected")
}

// ExecuteDecision transitions a decision from approved to executed.
// This does NOT perform external side effects — it only marks the decision
// as executed to record that routing was completed.
func (e *Engine) ExecuteDecision(ctx context.Context, id string) (ActuationDecision, error) {
	return e.transitionDecision(ctx, id, StatusExecuted, "actuation.executed")
}

// ListDecisions returns recent actuation decisions.
func (e *Engine) ListDecisions(ctx context.Context, limit int) ([]ActuationDecision, error) {
	return e.store.List(ctx, limit)
}

// GetDecision returns a single decision by ID.
func (e *Engine) GetDecision(ctx context.Context, id string) (ActuationDecision, error) {
	return e.store.Get(ctx, id)
}

// transitionDecision handles state machine transitions with validation and audit.
func (e *Engine) transitionDecision(ctx context.Context, id string, target DecisionStatus, eventType string) (ActuationDecision, error) {
	d, err := e.store.Get(ctx, id)
	if err != nil {
		return ActuationDecision{}, err
	}

	if !ValidateTransition(d.Status, target) {
		return ActuationDecision{}, fmt.Errorf("invalid transition: %s → %s", d.Status, target)
	}

	now := time.Now().UTC()
	if err := e.store.UpdateStatus(ctx, id, target, &now); err != nil {
		return ActuationDecision{}, err
	}

	d.Status = target
	d.ResolvedAt = &now

	e.auditEvent(ctx, eventType, map[string]any{
		"decision_id":   d.ID,
		"type":          d.Type,
		"from_status":   string(d.Status),
		"to_status":     string(target),
		"signal_source": d.SignalSource,
	})

	return d, nil
}

// gatherInputs collects all inputs from providers. Fail-open: returns zero for unavailable providers.
func (e *Engine) gatherInputs(ctx context.Context) ActuationInputs {
	var inputs ActuationInputs

	if e.reflection != nil {
		signals, err := e.reflection.GetReflectionSignals(ctx)
		if err != nil {
			e.logger.Warn("actuation: failed to get reflection signals", zap.Error(err))
		} else {
			inputs.ReflectionSignals = signals
		}
	}

	if e.objective != nil {
		inputs.NetUtility = e.objective.GetNetUtility(ctx)
		inputs.UtilityScore = e.objective.GetUtilityScore(ctx)
		inputs.RiskScore = e.objective.GetRiskScore(ctx)
		inputs.FinancialRisk = e.objective.GetFinancialRisk(ctx)
		inputs.OverloadRisk = e.objective.GetOverloadRisk(ctx)
	}

	return inputs
}

// auditEvent emits an audit event. Fail-open: logs warning on failure.
func (e *Engine) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if e.auditor == nil {
		return
	}
	if err := e.auditor.RecordEvent(ctx, "actuation", uuid.Nil, eventType, "system", "actuation_engine", payload); err != nil {
		e.logger.Warn("actuation: failed to record audit event", zap.String("event", eventType), zap.Error(err))
	}
}
