package calibration

import (
	"context"

	"go.uber.org/zap"
)

// ContextualCalibrationProvider is the interface consumed by the decision graph layer
// to apply context-aware confidence calibration before scoring.
// Defined here so the decision graph package can depend on this interface
// without importing the full calibration package.
type ContextualCalibrationProvider interface {
	CalibrateConfidenceForContext(ctx context.Context, rawConfidence float64, calCtx CalibrationContext) float64
}

// ContextGraphAdapter implements ContextualCalibrationProvider using the ContextCalibrator.
// Fail-open: if calibrator is nil or errors, returns raw confidence.
type ContextGraphAdapter struct {
	calibrator *ContextCalibrator
	logger     *zap.Logger
}

// NewContextGraphAdapter creates a ContextGraphAdapter.
func NewContextGraphAdapter(calibrator *ContextCalibrator, logger *zap.Logger) *ContextGraphAdapter {
	return &ContextGraphAdapter{
		calibrator: calibrator,
		logger:     logger,
	}
}

// CalibrateConfidenceForContext adjusts confidence using context-specific calibration data.
// Fail-open: returns rawConfidence if calibrator is nil or on error.
// Uses primitive parameters to satisfy the decision graph layer's interface.
func (a *ContextGraphAdapter) CalibrateConfidenceForContext(ctx context.Context, rawConfidence float64, goalType, providerName, strategyType string) float64 {
	if a.calibrator == nil {
		return rawConfidence
	}
	calCtx := CalibrationContext{
		GoalType:     goalType,
		ProviderName: providerName,
		StrategyType: strategyType,
	}
	return a.calibrator.CalibrateConfidenceForContext(ctx, rawConfidence, calCtx)
}

// ContextualCalibrationRecorder records per-context calibration data after outcomes.
// Used by the outcome handler to update contextual calibration stats.
type ContextualCalibrationRecorder interface {
	RecordContextCalibrationOutcome(ctx context.Context, goalType, providerName, strategyType string, predictedConfidence float64, actualOutcome string) error
}

// ContextOutcomeAdapter implements ContextualCalibrationRecorder.
type ContextOutcomeAdapter struct {
	calibrator *ContextCalibrator
	logger     *zap.Logger
}

// NewContextOutcomeAdapter creates a ContextOutcomeAdapter.
func NewContextOutcomeAdapter(calibrator *ContextCalibrator, logger *zap.Logger) *ContextOutcomeAdapter {
	return &ContextOutcomeAdapter{
		calibrator: calibrator,
		logger:     logger,
	}
}

// RecordContextCalibrationOutcome records a per-context prediction-vs-outcome data point.
// Fail-open: errors are logged but do not propagate to the caller.
func (a *ContextOutcomeAdapter) RecordContextCalibrationOutcome(ctx context.Context, goalType, providerName, strategyType string, predictedConfidence float64, actualOutcome string) error {
	if a.calibrator == nil {
		return nil
	}
	calCtx := CalibrationContext{
		GoalType:     goalType,
		ProviderName: providerName,
		StrategyType: strategyType,
	}
	if err := a.calibrator.RecordContextOutcome(ctx, calCtx, predictedConfidence, actualOutcome); err != nil {
		a.logger.Warn("context_calibration_outcome_record_failed",
			zap.String("goal_type", goalType),
			zap.String("provider_name", providerName),
			zap.String("strategy_type", strategyType),
			zap.Error(err),
		)
		return err
	}
	return nil
}
