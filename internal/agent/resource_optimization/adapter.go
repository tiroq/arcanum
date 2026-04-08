package resource_optimization

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// GraphAdapter implements decision_graph.ResourceOptimizationProvider.
// Bridges the resource optimization layer to the decision graph for:
//   - mode adjustment (meta-reasoning integration)
//   - path penalty (path scoring integration)
//   - outcome recording (resource tracking)
//
// All methods are fail-open: nil tracker → no adjustment, no error.
type GraphAdapter struct {
	tracker *Tracker
	auditor audit.AuditRecorder
	logger  *zap.Logger
}

// NewGraphAdapter creates a GraphAdapter.
func NewGraphAdapter(tracker *Tracker, auditor audit.AuditRecorder, logger *zap.Logger) *GraphAdapter {
	return &GraphAdapter{
		tracker: tracker,
		auditor: auditor,
		logger:  logger,
	}
}

// GetModeAdjustment returns a bounded score adjustment for the given mode.
// Implements decision_graph.ResourceOptimizationProvider.
// Returns 0 on nil tracker or missing profile (fail-open).
func (a *GraphAdapter) GetModeAdjustment(ctx context.Context, mode, goalType string, confidence, successRate float64, stabilityMode string) float64 {
	if a.tracker == nil {
		return 0
	}
	profile := a.tracker.GetProfile(ctx, mode, goalType)
	adj := ComputeModeAdjustment(mode, profile, confidence, successRate, stabilityMode)

	if adj != 0 {
		a.auditEvent(ctx, "resource.adjustment_applied", map[string]any{
			"mode":           mode,
			"goal_type":      goalType,
			"adjustment":     adj,
			"confidence":     confidence,
			"success_rate":   successRate,
			"stability_mode": stabilityMode,
		})
	}

	return adj
}

// GetPathPenalty returns a bounded resource penalty for a specific path.
// Implements decision_graph.ResourceOptimizationProvider.
// Returns 0 on nil tracker or missing profile (fail-open).
func (a *GraphAdapter) GetPathPenalty(ctx context.Context, mode, goalType string, pathLength int, stabilityMode string) float64 {
	if a.tracker == nil {
		return 0
	}
	profile := a.tracker.GetProfile(ctx, mode, goalType)
	return ComputePathResourcePenalty(pathLength, profile, stabilityMode)
}

// RecordOutcome records resource metrics after a decision.
// Implements decision_graph.ResourceOutcomeRecorder.
// Fail-open: errors are logged but not returned.
func (a *GraphAdapter) RecordOutcome(ctx context.Context, mode, goalType string, latencyMs, reasoningDepth, pathLength, tokenCost, executionCost float64) {
	if a.tracker == nil {
		return
	}

	if err := a.tracker.RecordDecisionOutcome(ctx, mode, goalType, latencyMs, reasoningDepth, pathLength, tokenCost, executionCost); err != nil {
		a.logger.Warn("resource_profile_update_failed",
			zap.String("mode", mode),
			zap.String("goal_type", goalType),
			zap.Error(err),
		)
		return
	}

	// Get updated profile for audit.
	profile := a.tracker.GetProfile(ctx, mode, goalType)
	signals := ComputeSignalsFromProfile(profile)

	a.auditEvent(ctx, "resource.profile_updated", map[string]any{
		"mode":             mode,
		"goal_type":        goalType,
		"avg_latency_ms":   profile.AvgLatencyMs,
		"avg_token_cost":   profile.AvgTokenCost,
		"avg_exec_cost":    profile.AvgExecutionCost,
		"sample_count":     profile.SampleCount,
		"efficiency_score": signals.EfficiencyScore,
	})

	// Record for API visibility.
	RecordDecision(ResourceDecisionRecord{
		Mode:     mode,
		GoalType: goalType,
		Signals:  signals,
	})

	// Detect and emit pressure state.
	profiles := a.tracker.GetAllProfiles(ctx)
	pressure := DetectPressure(profiles)
	if pressure != "none" {
		a.auditEvent(ctx, "resource.pressure_detected", map[string]any{
			"pressure_state": pressure,
			"mode":           mode,
			"goal_type":      goalType,
		})
	}
}

// GetProfiles returns all resource profiles for API.
func (a *GraphAdapter) GetProfiles(ctx context.Context) []ResourceProfile {
	if a.tracker == nil {
		return nil
	}
	return a.tracker.GetAllProfiles(ctx)
}

// GetSummary returns the aggregate resource summary for API.
func (a *GraphAdapter) GetSummary(ctx context.Context) ResourceSummary {
	if a.tracker == nil {
		return ResourceSummary{
			PressureState:  "none",
			ProfilesByMode: make(map[string][]ResourceProfile),
		}
	}
	return a.tracker.BuildSummary(ctx)
}

// auditEvent records a resource optimization audit event.
func (a *GraphAdapter) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if a.auditor == nil {
		return
	}
	_ = a.auditor.RecordEvent(ctx, "resource_optimization", uuid.New(), eventType,
		"system", "resource_optimization_engine", payload)
}

// OutcomeAdapter implements outcome.ResourceOutcomeRecorder for wiring
// resource tracking into the outcome handler.
type OutcomeAdapter struct {
	adapter *GraphAdapter
	logger  *zap.Logger
}

// NewOutcomeAdapter creates an OutcomeAdapter.
func NewOutcomeAdapter(adapter *GraphAdapter, logger *zap.Logger) *OutcomeAdapter {
	return &OutcomeAdapter{adapter: adapter, logger: logger}
}

// RecordResourceOutcome records resource metrics extracted from action params.
// Fail-open: nil adapter → noop.
func (a *OutcomeAdapter) RecordResourceOutcome(ctx context.Context, mode, goalType string, latencyMs, reasoningDepth, pathLength, tokenCost, executionCost float64) {
	if a.adapter == nil {
		return
	}
	a.adapter.RecordOutcome(ctx, mode, goalType, latencyMs, reasoningDepth, pathLength, tokenCost, executionCost)
}
