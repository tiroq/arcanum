package policy

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/reflection"
	"github.com/tiroq/arcanum/internal/agent/stability"
	"github.com/tiroq/arcanum/internal/audit"
)

// Engine orchestrates the policy adaptation cycle:
// collect → generate proposals → filter → apply → record → audit.
type Engine struct {
	store           *Store
	memoryStore     *actionmemory.Store
	reflectionStore *reflection.Store
	stabilityEngine *stability.Engine
	evaluator       *Evaluator
	auditor         audit.AuditRecorder
	logger          *zap.Logger
}

// NewEngine creates a policy Engine.
func NewEngine(
	store *Store,
	memoryStore *actionmemory.Store,
	reflectionStore *reflection.Store,
	stabilityEngine *stability.Engine,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Engine {
	return &Engine{
		store:           store,
		memoryStore:     memoryStore,
		reflectionStore: reflectionStore,
		stabilityEngine: stabilityEngine,
		evaluator:       NewEvaluator(store, memoryStore, logger),
		auditor:         auditor,
		logger:          logger,
	}
}

// CycleResult is the output of one policy adaptation cycle.
type CycleResult struct {
	Proposed []PolicyChange `json:"proposed"`
	Applied  []PolicyChange `json:"applied"`
	Rejected []PolicyChange `json:"rejected"`
}

// RunCycle executes one full policy adaptation cycle.
func (e *Engine) RunCycle(ctx context.Context) (*CycleResult, error) {
	// 1. Collect current inputs.
	currentValues, err := e.store.GetAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("get policy state: %w", err)
	}

	memories, err := e.memoryStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("load action memory: %w", err)
	}

	findings, err := e.reflectionStore.ListRecent(ctx, 50)
	if err != nil {
		return nil, fmt.Errorf("load reflection findings: %w", err)
	}

	stabilityMode := "normal"
	if e.stabilityEngine != nil {
		st, err := e.stabilityEngine.GetState(ctx)
		if err == nil {
			stabilityMode = string(st.Mode)
		}
	}

	// 2. Generate proposals (deterministic).
	input := ProposalInput{
		ReflectionFindings: findings,
		ActionMemory:       memories,
		CurrentValues:      currentValues,
		StabilityMode:      stabilityMode,
	}
	proposals := GenerateProposals(input)

	result := &CycleResult{Proposed: proposals}

	if len(proposals) == 0 {
		e.logger.Info("policy_cycle_no_proposals")
		return result, nil
	}

	// 3. Filter through safety bounds.
	safe, rejected := FilterAndApply(proposals, stabilityMode)
	result.Applied = safe
	result.Rejected = rejected

	// 4. Apply safe changes.
	for _, c := range safe {
		if err := ValidateChange(c); err != nil {
			e.logger.Warn("policy_change_validation_failed",
				zap.String("parameter", string(c.Parameter)),
				zap.Error(err),
			)
			continue
		}

		if err := e.store.Set(ctx, c.Parameter, c.NewValue); err != nil {
			e.logger.Error("policy_set_failed",
				zap.String("parameter", string(c.Parameter)),
				zap.Error(err),
			)
			continue
		}

		// Record in change history.
		if _, err := e.store.RecordChange(ctx, c, true); err != nil {
			e.logger.Warn("policy_record_change_failed", zap.Error(err))
		}

		// Audit.
		e.auditEvent(ctx, "policy.change_applied", map[string]any{
			"parameter": c.Parameter,
			"old_value": c.OldValue,
			"new_value": c.NewValue,
			"delta":     c.Delta,
			"reason":    c.Reason,
		})

		e.logger.Info("policy_change_applied",
			zap.String("parameter", string(c.Parameter)),
			zap.Float64("old_value", c.OldValue),
			zap.Float64("new_value", c.NewValue),
			zap.Float64("delta", c.Delta),
			zap.String("reason", c.Reason),
		)
	}

	// Record rejected proposals as not-applied for history.
	for _, c := range rejected {
		if _, err := e.store.RecordChange(ctx, c, false); err != nil {
			e.logger.Warn("policy_record_rejected_failed", zap.Error(err))
		}
		e.auditEvent(ctx, "policy.change_proposed", map[string]any{
			"parameter":  c.Parameter,
			"old_value":  c.OldValue,
			"new_value":  c.NewValue,
			"delta":      c.Delta,
			"reason":     c.Reason,
			"confidence": c.Confidence,
			"rejected":   true,
		})
	}

	return result, nil
}

// EvaluateChanges reviews previously applied changes to determine impact.
func (e *Engine) EvaluateChanges(ctx context.Context) (*EvaluationResult, error) {
	result, err := e.evaluator.EvaluateChanges(ctx)
	if err != nil {
		return nil, err
	}

	if result.Evaluated > 0 {
		e.auditEvent(ctx, "policy.change_evaluated", map[string]any{
			"evaluated": result.Evaluated,
			"improved":  result.Improved,
			"regressed": result.Regressed,
		})
	}

	return result, nil
}

// GetState returns the current policy state.
func (e *Engine) GetState(ctx context.Context) (*PolicyState, error) {
	vals, err := e.store.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	return &PolicyState{Values: vals}, nil
}

// ListChanges returns recent policy changes.
func (e *Engine) ListChanges(ctx context.Context, limit int) ([]ChangeRecord, error) {
	return e.store.ListChanges(ctx, limit)
}

func (e *Engine) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if e.auditor == nil {
		return
	}
	if err := e.auditor.RecordEvent(ctx, "policy", uuid.Nil, eventType, "system", "policy_engine", payload); err != nil {
		e.logger.Warn("audit_event_failed", zap.String("event_type", eventType), zap.Error(err))
	}
}
