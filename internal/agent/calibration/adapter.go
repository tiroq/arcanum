package calibration

import (
	"context"

	"go.uber.org/zap"
)

// GraphCalibrationProvider is the interface consumed by the decision graph layer
// to calibrate node confidence values before scoring.
// Defined here so the decision graph package can depend on this interface
// without importing the full calibration package.
type GraphCalibrationProvider interface {
	CalibrateConfidence(ctx context.Context, rawConfidence float64) float64
}

// GraphAdapter implements GraphCalibrationProvider using the Calibrator.
// Fail-open: if calibrator is nil or errors, returns raw confidence.
type GraphAdapter struct {
	calibrator *Calibrator
	logger     *zap.Logger
}

// NewGraphAdapter creates a GraphAdapter.
func NewGraphAdapter(calibrator *Calibrator, logger *zap.Logger) *GraphAdapter {
	return &GraphAdapter{
		calibrator: calibrator,
		logger:     logger,
	}
}

// CalibrateConfidence adjusts confidence using current calibration data.
// Fail-open: returns rawConfidence if calibrator is nil or on error.
func (a *GraphAdapter) CalibrateConfidence(ctx context.Context, rawConfidence float64) float64 {
	if a.calibrator == nil {
		return rawConfidence
	}
	return a.calibrator.CalibrateConfidence(ctx, rawConfidence)
}

// MetaReasoningCalibrationProvider is the interface consumed by the meta-reasoning
// layer to access calibration signals for mode selection.
type MetaReasoningCalibrationProvider interface {
	GetCalibrationSignals(ctx context.Context) (overconfidence, underconfidence float64)
}

// MetaReasoningAdapter implements MetaReasoningCalibrationProvider.
type MetaReasoningAdapter struct {
	calibrator *Calibrator
	logger     *zap.Logger
}

// NewMetaReasoningAdapter creates a MetaReasoningAdapter.
func NewMetaReasoningAdapter(calibrator *Calibrator, logger *zap.Logger) *MetaReasoningAdapter {
	return &MetaReasoningAdapter{
		calibrator: calibrator,
		logger:     logger,
	}
}

// GetCalibrationSignals returns the current overconfidence and underconfidence scores.
// Fail-open: returns (0, 0) on any error.
func (a *MetaReasoningAdapter) GetCalibrationSignals(ctx context.Context) (overconfidence, underconfidence float64) {
	if a.calibrator == nil {
		return 0, 0
	}
	summary, err := a.calibrator.GetSummary(ctx)
	if err != nil || summary == nil {
		return 0, 0
	}
	return summary.OverconfidenceScore, summary.UnderconfidenceScore
}

// CounterfactualCalibrationProvider is the interface consumed by the counterfactual
// layer to reduce prediction weight when calibration is poor.
type CounterfactualCalibrationProvider interface {
	GetCalibrationQuality(ctx context.Context) float64
}

// CounterfactualAdapter implements CounterfactualCalibrationProvider.
type CounterfactualAdapter struct {
	calibrator *Calibrator
	logger     *zap.Logger
}

// NewCounterfactualAdapter creates a CounterfactualAdapter.
func NewCounterfactualAdapter(calibrator *Calibrator, logger *zap.Logger) *CounterfactualAdapter {
	return &CounterfactualAdapter{
		calibrator: calibrator,
		logger:     logger,
	}
}

// GetCalibrationQuality returns a quality score (0–1) based on ECE.
// Lower ECE = higher quality. Maps ECE to quality as: quality = max(0, 1 - ECE*2).
// Fail-open: returns 1.0 (full quality) on any error.
func (a *CounterfactualAdapter) GetCalibrationQuality(ctx context.Context) float64 {
	if a.calibrator == nil {
		return 1.0
	}
	summary, err := a.calibrator.GetSummary(ctx)
	if err != nil || summary == nil {
		return 1.0
	}
	quality := 1.0 - summary.ExpectedCalibrationError*2
	if quality < 0 {
		quality = 0
	}
	return quality
}

// OutcomeCalibrationRecorder is the interface consumed by the outcome handler
// to record calibration data after each decision outcome.
type OutcomeCalibrationRecorder interface {
	RecordCalibrationOutcome(ctx context.Context, decisionID string, predictedConfidence float64, actualOutcome string) error
}

// OutcomeAdapter implements OutcomeCalibrationRecorder.
type OutcomeAdapter struct {
	calibrator *Calibrator
	logger     *zap.Logger
}

// NewOutcomeAdapter creates an OutcomeAdapter.
func NewOutcomeAdapter(calibrator *Calibrator, logger *zap.Logger) *OutcomeAdapter {
	return &OutcomeAdapter{
		calibrator: calibrator,
		logger:     logger,
	}
}

// RecordCalibrationOutcome records a prediction-vs-outcome data point.
// Fail-open: errors are logged but do not propagate.
func (a *OutcomeAdapter) RecordCalibrationOutcome(ctx context.Context, decisionID string, predictedConfidence float64, actualOutcome string) error {
	if a.calibrator == nil {
		return nil
	}
	if err := a.calibrator.RecordOutcome(ctx, decisionID, predictedConfidence, actualOutcome); err != nil {
		a.logger.Warn("calibration_outcome_record_failed",
			zap.String("decision_id", decisionID),
			zap.Error(err),
		)
		return err
	}
	return nil
}
