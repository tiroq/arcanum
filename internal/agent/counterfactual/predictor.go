package counterfactual

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// Predictor evaluates prediction accuracy after execution outcomes are known.
type Predictor struct {
	simulationStore *SimulationStore
	outcomeStore    *PredictionOutcomeStore
	memoryStore     *PredictionMemoryStore
	auditor         audit.AuditRecorder
	logger          *zap.Logger
}

// NewPredictor creates a Predictor.
func NewPredictor(
	simulationStore *SimulationStore,
	outcomeStore *PredictionOutcomeStore,
	memoryStore *PredictionMemoryStore,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Predictor {
	return &Predictor{
		simulationStore: simulationStore,
		outcomeStore:    outcomeStore,
		memoryStore:     memoryStore,
		auditor:         auditor,
		logger:          logger,
	}
}

// EvaluatePrediction compares the predicted value for the selected path
// against the actual outcome and updates prediction memory.
//
// Parameters:
//   - decisionID: links to the simulation result
//   - pathSignature: the path that was actually executed
//   - goalType: goal family context
//   - actualOutcomeStatus: "success", "neutral", or "failure"
func (p *Predictor) EvaluatePrediction(ctx context.Context, decisionID, pathSignature, goalType, actualOutcomeStatus string) error {
	// 1. Retrieve simulation result for this decision.
	sim, err := p.simulationStore.GetSimulation(ctx, decisionID)
	if err != nil {
		p.logger.Error("counterfactual_simulation_query_failed",
			zap.String("decision_id", decisionID),
			zap.Error(err),
		)
		return err
	}
	if sim == nil {
		// No simulation recorded → nothing to evaluate. Fail-open.
		return nil
	}

	// 2. Find the prediction for the executed path.
	var prediction *PathPrediction
	for i := range sim.Predictions {
		if sim.Predictions[i].PathSignature == pathSignature {
			prediction = &sim.Predictions[i]
			break
		}
	}
	if prediction == nil {
		// Path was not in the simulated set → nothing to evaluate.
		return nil
	}

	// 3. Map outcome to numeric value for comparison.
	actualValue := OutcomeToValue(actualOutcomeStatus)

	// 4. Compute prediction error.
	absoluteError := math.Abs(prediction.ExpectedValue - actualValue)

	// 5. Direction correctness: did the prediction correctly identify
	// whether the path would perform above or below average (0.5)?
	predictionDirection := prediction.ExpectedValue >= 0.5
	actualDirection := actualValue >= 0.5
	directionCorrect := predictionDirection == actualDirection

	// 6. Build and persist prediction outcome.
	outcome := PredictionOutcome{
		DecisionID:       decisionID,
		PathSignature:    pathSignature,
		GoalType:         goalType,
		PredictedValue:   prediction.ExpectedValue,
		ActualValue:      actualValue,
		AbsoluteError:    absoluteError,
		DirectionCorrect: directionCorrect,
		CreatedAt:        time.Now().UTC(),
	}

	if err := p.outcomeStore.SaveOutcome(ctx, outcome); err != nil {
		p.logger.Error("counterfactual_outcome_save_failed",
			zap.String("decision_id", decisionID),
			zap.Error(err),
		)
		return err
	}

	// 7. Update prediction memory.
	if err := p.memoryStore.RecordPrediction(ctx, pathSignature, goalType, absoluteError, directionCorrect); err != nil {
		p.logger.Error("counterfactual_memory_update_failed",
			zap.String("path_signature", pathSignature),
			zap.Error(err),
		)
	}

	// 8. Audit the prediction evaluation.
	p.auditEvent(ctx, "counterfactual.error_evaluated", map[string]any{
		"decision_id":       decisionID,
		"path_signature":    pathSignature,
		"goal_type":         goalType,
		"predicted_value":   prediction.ExpectedValue,
		"actual_value":      actualValue,
		"absolute_error":    absoluteError,
		"direction_correct": directionCorrect,
	})

	return nil
}

// OutcomeToValue maps an outcome status string to a numeric value.
func OutcomeToValue(status string) float64 {
	switch status {
	case "success":
		return OutcomeValueSuccess
	case "failure":
		return OutcomeValueFailure
	default: // "neutral" or unknown
		return OutcomeValueNeutral
	}
}

// auditEvent records a counterfactual audit event.
func (p *Predictor) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if p.auditor == nil {
		return
	}
	_ = p.auditor.RecordEvent(ctx, "counterfactual", uuid.New(), eventType,
		"system", "counterfactual_engine", payload)
}
