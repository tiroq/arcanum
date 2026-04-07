package causal

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/policy"
	"github.com/tiroq/arcanum/internal/agent/stability"
	"github.com/tiroq/arcanum/internal/audit"
)

// Engine orchestrates the causal reasoning cycle:
// collect signals → analyze → persist → audit.
type Engine struct {
	store           *Store
	policyStore     *policy.Store
	memoryStore     *actionmemory.Store
	stabilityEngine *stability.Engine
	auditor         audit.AuditRecorder
	logger          *zap.Logger
}

// NewEngine creates a causal reasoning Engine.
func NewEngine(
	store *Store,
	policyStore *policy.Store,
	memoryStore *actionmemory.Store,
	stabilityEngine *stability.Engine,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Engine {
	return &Engine{
		store:           store,
		policyStore:     policyStore,
		memoryStore:     memoryStore,
		stabilityEngine: stabilityEngine,
		auditor:         auditor,
		logger:          logger,
	}
}

// RunAnalysis executes one causal analysis pass.
func (e *Engine) RunAnalysis(ctx context.Context) (*AnalysisResult, error) {
	e.auditEvent(ctx, "causal.evaluation_started", map[string]any{
		"timestamp": time.Now().UTC(),
	})

	input, err := e.collectInput(ctx)
	if err != nil {
		return nil, fmt.Errorf("collect causal input: %w", err)
	}

	attributions := Analyze(*input)

	// Persist and audit each attribution.
	for i := range attributions {
		if err := e.store.Save(ctx, &attributions[i]); err != nil {
			e.logger.Warn("causal_save_failed",
				zap.String("subject_type", string(attributions[i].SubjectType)),
				zap.Error(err),
			)
			continue
		}

		e.auditEvent(ctx, "causal.attribution_created", map[string]any{
			"subject_type": attributions[i].SubjectType,
			"subject_id":   attributions[i].SubjectID,
			"attribution":  attributions[i].Attribution,
			"confidence":   attributions[i].Confidence,
		})
	}

	result := &AnalysisResult{
		Attributions: attributions,
		Analyzed:     len(attributions),
		Timestamp:    input.Timestamp,
	}

	e.auditEvent(ctx, "causal.evaluation_completed", map[string]any{
		"attributions_count": len(attributions),
		"timestamp":          time.Now().UTC(),
	})

	return result, nil
}

// ListRecent returns recent causal attributions.
func (e *Engine) ListRecent(ctx context.Context, limit int) ([]CausalAttribution, error) {
	return e.store.ListRecent(ctx, limit)
}

// ListBySubject returns attributions for a specific subject.
func (e *Engine) ListBySubject(ctx context.Context, subjectID uuid.UUID) ([]CausalAttribution, error) {
	return e.store.ListBySubject(ctx, subjectID)
}

func (e *Engine) collectInput(ctx context.Context) (*AnalysisInput, error) {
	now := time.Now().UTC()

	input := &AnalysisInput{
		Timestamp: now,
	}

	// Collect recent policy changes.
	policyChanges, err := e.policyStore.ListChanges(ctx, 20)
	if err != nil {
		return nil, fmt.Errorf("list policy changes: %w", err)
	}

	var recentWindow = 30 * time.Minute
	var appliedCount int
	for _, pc := range policyChanges {
		if now.Sub(pc.CreatedAt) > recentWindow {
			continue
		}
		input.RecentPolicyChanges = append(input.RecentPolicyChanges, PolicyChangeRecord{
			ID:                  pc.ID,
			Parameter:           pc.Parameter,
			OldValue:            pc.OldValue,
			NewValue:            pc.NewValue,
			Applied:             pc.Applied,
			CreatedAt:           pc.CreatedAt,
			ImprovementDetected: pc.ImprovementDetected,
		})
		if pc.Applied {
			appliedCount++
		}
	}
	input.SimultaneousChanges = appliedCount

	// Collect action memory.
	memories, err := e.memoryStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list action memory: %w", err)
	}
	for _, m := range memories {
		input.ActionMemory = append(input.ActionMemory, ActionMemorySummary{
			ActionType:  m.ActionType,
			TotalRuns:   m.TotalRuns,
			SuccessRate: m.SuccessRate,
			FailureRate: m.FailureRate,
		})
	}

	// Collect stability state.
	if e.stabilityEngine != nil {
		st, err := e.stabilityEngine.GetState(ctx)
		if err == nil {
			input.StabilityMode = string(st.Mode)
			// Detect if stability mode changed recently by checking the update timestamp.
			if now.Sub(st.UpdatedAt) < recentWindow && st.Mode != stability.ModeNormal {
				input.StabilityChanged = true
				// We approximate the previous mode — if currently escalated, previous was likely lower.
				switch st.Mode {
				case stability.ModeSafeMode:
					input.PreviousMode = "throttled"
				case stability.ModeThrottled:
					input.PreviousMode = "normal"
				default:
					input.PreviousMode = "normal"
				}
			}
			// If mode is normal but recently updated, it may have de-escalated.
			if now.Sub(st.UpdatedAt) < recentWindow && st.Mode == stability.ModeNormal {
				input.StabilityChanged = true
				input.PreviousMode = "throttled" // approximate
			}
		} else {
			input.StabilityMode = "normal"
		}
	} else {
		input.StabilityMode = "normal"
	}

	// Detect external instability signals from action memory.
	for _, m := range input.ActionMemory {
		if m.TotalRuns >= 5 && m.FailureRate >= 0.50 {
			input.HighSystemFailure = true
			break
		}
	}

	return input, nil
}

func (e *Engine) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if e.auditor == nil {
		return
	}
	if err := e.auditor.RecordEvent(ctx, "causal", uuid.Nil, eventType, "system", "causal_engine", payload); err != nil {
		e.logger.Warn("audit_event_failed", zap.String("event_type", eventType), zap.Error(err))
	}
}
