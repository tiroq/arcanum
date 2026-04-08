package calibration

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// ContextCalibrator orchestrates contextual confidence calibration:
// recording per-context outcomes, resolving calibration errors by context,
// and applying bounded confidence adjustments.
type ContextCalibrator struct {
	store   *ContextStore
	auditor audit.AuditRecorder
	logger  *zap.Logger
}

// NewContextCalibrator creates a ContextCalibrator.
func NewContextCalibrator(store *ContextStore, auditor audit.AuditRecorder, logger *zap.Logger) *ContextCalibrator {
	return &ContextCalibrator{
		store:   store,
		auditor: auditor,
		logger:  logger,
	}
}

// RecordContextOutcome records a prediction-vs-outcome data point for all
// applicable context levels. This updates the incremental rolling averages
// at L0, L1, L2, and L3.
func (c *ContextCalibrator) RecordContextOutcome(ctx context.Context, calCtx CalibrationContext, predictedConfidence float64, actualOutcome string) error {
	actualSuccess := 0.0
	if OutcomeIsSuccess(actualOutcome) {
		actualSuccess = 1.0
	}

	// Update all applicable context levels.
	keys := contextKeys(calCtx)
	for _, key := range keys {
		if err := c.store.UpsertContext(ctx, key, predictedConfidence, actualSuccess); err != nil {
			c.logger.Error("context_calibration_upsert_failed",
				zap.String("goal_type", key.GoalType),
				zap.String("provider_name", key.ProviderName),
				zap.String("strategy_type", key.StrategyType),
				zap.Error(err),
			)
			return err
		}
	}

	c.auditEvent(ctx, "calibration.context_updated", map[string]any{
		"goal_type":            calCtx.GoalType,
		"provider_name":        calCtx.ProviderName,
		"strategy_type":        calCtx.StrategyType,
		"predicted_confidence": predictedConfidence,
		"actual_outcome":       actualOutcome,
		"levels_updated":       len(keys),
	})

	return nil
}

// ResolveCalibration finds the best matching context calibration record using
// the fallback chain: L0 → L1 → L2 → L3 → none.
// Returns the calibration error and the resolution level.
// If no matching record with sufficient samples exists, returns (0, "").
func (c *ContextCalibrator) ResolveCalibration(ctx context.Context, calCtx CalibrationContext) (calibrationError float64, level string, err error) {
	keys := contextKeys(calCtx)
	levels := contextLevels(calCtx)

	for i, key := range keys {
		rec, lookupErr := c.store.GetByContext(ctx, key)
		if lookupErr != nil {
			c.logger.Error("context_calibration_lookup_failed",
				zap.String("level", levels[i]),
				zap.Error(lookupErr),
			)
			return 0, "", lookupErr
		}
		if rec != nil && rec.SampleCount >= ContextMinSamples {
			return rec.CalibrationError, levels[i], nil
		}
	}

	return 0, ContextLevelNone, nil
}

// CalibrateConfidenceForContext adjusts a raw confidence value based on the
// context-specific calibration error.
//
// calibration_error = avg_predicted - avg_actual
// If error > 0: overconfident → reduce confidence (subtract error)
// If error < 0: underconfident → increase confidence (subtract negative = add)
//
// delta = clamp(error, -0.20, +0.20)
// adjusted = clamp(original - delta, 0, 1)
//
// Fail-open: returns rawConfidence on any error or missing data.
func (c *ContextCalibrator) CalibrateConfidenceForContext(ctx context.Context, rawConfidence float64, calCtx CalibrationContext) float64 {
	calError, level, err := c.ResolveCalibration(ctx, calCtx)
	if err != nil || level == ContextLevelNone {
		return rawConfidence // fail-open
	}

	adjusted := ApplyContextualCalibration(rawConfidence, calError)

	c.auditEvent(ctx, "calibration.context_applied", map[string]any{
		"goal_type":           calCtx.GoalType,
		"provider_name":       calCtx.ProviderName,
		"strategy_type":       calCtx.StrategyType,
		"original_confidence": rawConfidence,
		"adjusted_confidence": adjusted,
		"calibration_error":   calError,
		"resolution_level":    level,
	})

	return adjusted
}

// ApplyContextualCalibration is the pure, deterministic confidence adjustment function.
// Exported for use in contexts where calibration error is already resolved.
//
// delta = clamp(calibrationError, -ContextMaxAdjustment, +ContextMaxAdjustment)
// adjusted = clamp(rawConfidence - delta, 0, 1)
func ApplyContextualCalibration(rawConfidence float64, calibrationError float64) float64 {
	delta := calibrationError
	if delta > ContextMaxAdjustment {
		delta = ContextMaxAdjustment
	}
	if delta < -ContextMaxAdjustment {
		delta = -ContextMaxAdjustment
	}
	adjusted := rawConfidence - delta
	return clamp01(adjusted)
}

// auditEvent records a contextual calibration audit event.
func (c *ContextCalibrator) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if c.auditor == nil {
		return
	}
	_ = c.auditor.RecordEvent(ctx, "calibration", uuid.New(), eventType,
		"system", "context_calibration_engine", payload)
}

// contextLevels returns the resolution level names corresponding to contextKeys.
func contextLevels(calCtx CalibrationContext) []string {
	levels := make([]string, 0, 4)
	if calCtx.GoalType != "" && calCtx.ProviderName != "" && calCtx.StrategyType != "" {
		levels = append(levels, ContextLevelL0)
	}
	if calCtx.GoalType != "" && calCtx.StrategyType != "" {
		levels = append(levels, ContextLevelL1)
	}
	if calCtx.GoalType != "" {
		levels = append(levels, ContextLevelL2)
	}
	levels = append(levels, ContextLevelL3)
	return levels
}
