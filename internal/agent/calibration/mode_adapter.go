package calibration

import (
	"context"

	"go.uber.org/zap"
)

// --- Mode-Specific Calibration Adapters (Iteration 28) ---

// ModeCalibrationProvider is the interface consumed by the decision graph layer
// to apply mode-specific confidence calibration after contextual calibration.
// Uses primitive parameters to avoid type coupling across packages.
type ModeCalibrationProvider interface {
	CalibrateConfidenceForMode(ctx context.Context, rawConfidence float64, mode string) float64
}

// ModeGraphAdapter implements ModeCalibrationProvider using the ModeCalibrator.
// Fail-open: if calibrator is nil or errors, returns raw confidence.
type ModeGraphAdapter struct {
	calibrator *ModeCalibrator
	logger     *zap.Logger
}

// NewModeGraphAdapter creates a ModeGraphAdapter.
func NewModeGraphAdapter(calibrator *ModeCalibrator, logger *zap.Logger) *ModeGraphAdapter {
	return &ModeGraphAdapter{
		calibrator: calibrator,
		logger:     logger,
	}
}

// CalibrateConfidenceForMode adjusts confidence using mode-specific calibration data.
// Fail-open: returns rawConfidence if calibrator is nil or on error.
func (a *ModeGraphAdapter) CalibrateConfidenceForMode(ctx context.Context, rawConfidence float64, mode string) float64 {
	if a.calibrator == nil {
		return rawConfidence
	}
	return a.calibrator.CalibrateConfidenceForMode(ctx, rawConfidence, mode)
}

// ModeCalibrationRecorder records mode-specific calibration data after outcomes.
// Used by the outcome handler to update mode calibration stats.
type ModeCalibrationRecorder interface {
	RecordModeCalibrationOutcome(ctx context.Context, decisionID, goalType, mode string, predictedConfidence float64, actualOutcome string) error
}

// ModeOutcomeAdapter implements ModeCalibrationRecorder.
type ModeOutcomeAdapter struct {
	calibrator *ModeCalibrator
	logger     *zap.Logger
}

// NewModeOutcomeAdapter creates a ModeOutcomeAdapter.
func NewModeOutcomeAdapter(calibrator *ModeCalibrator, logger *zap.Logger) *ModeOutcomeAdapter {
	return &ModeOutcomeAdapter{
		calibrator: calibrator,
		logger:     logger,
	}
}

// RecordModeCalibrationOutcome records a mode-specific prediction-vs-outcome data point.
// Fail-open: errors are logged but do not propagate to the caller.
func (a *ModeOutcomeAdapter) RecordModeCalibrationOutcome(ctx context.Context, decisionID, goalType, mode string, predictedConfidence float64, actualOutcome string) error {
	if a.calibrator == nil {
		return nil
	}
	if err := a.calibrator.RecordOutcome(ctx, decisionID, goalType, mode, predictedConfidence, actualOutcome); err != nil {
		a.logger.Warn("mode_calibration_outcome_record_failed",
			zap.String("decision_id", decisionID),
			zap.String("mode", mode),
			zap.Error(err),
		)
		return err
	}
	return nil
}
