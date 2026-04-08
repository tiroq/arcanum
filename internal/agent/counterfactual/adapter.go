package counterfactual

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"

	decision_graph "github.com/tiroq/arcanum/internal/agent/decision_graph"
	"github.com/tiroq/arcanum/internal/audit"
)

// CounterfactualSimulationProvider is the interface used by the decision graph layer
// to run counterfactual simulation without importing this package directly.
// Defined in decision_graph/planner_adapter.go; implemented here.

// CounterfactualPredictionEvaluator is the interface used by the outcome handler
// to evaluate prediction accuracy after execution (fail-open).
type CounterfactualPredictionEvaluator interface {
	EvaluatePrediction(ctx context.Context, decisionID, pathSignature, goalType, actualOutcomeStatus string) error
}

// --- GraphAdapter (implements CounterfactualSimulationProvider) ---

// GraphAdapter bridges the counterfactual simulation layer to the decision graph.
type GraphAdapter struct {
	simulationStore *SimulationStore
	memoryStore     *PredictionMemoryStore
	pathLearning    PathLearningSignalSource
	comparativeLP   ComparativeLearningSignalSource
	auditor         audit.AuditRecorder
	logger          *zap.Logger
}

// PathLearningSignalSource retrieves path/transition feedback signals.
type PathLearningSignalSource interface {
	GetAllPathFeedbackMap(ctx context.Context, goalType string) map[string]string
	GetAllTransitionFeedbackMap(ctx context.Context, goalType string) map[string]string
}

// ComparativeLearningSignalSource retrieves comparative learning signals.
type ComparativeLearningSignalSource interface {
	GetAllComparativeFeedbackMap(ctx context.Context, goalType string) map[string]string
	GetAllComparativeWinRates(ctx context.Context, goalType string) map[string]float64
	GetAllComparativeLossRates(ctx context.Context, goalType string) map[string]float64
}

// NewGraphAdapter creates a GraphAdapter.
func NewGraphAdapter(
	simulationStore *SimulationStore,
	memoryStore *PredictionMemoryStore,
	pathLearning PathLearningSignalSource,
	comparativeLearning ComparativeLearningSignalSource,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *GraphAdapter {
	return &GraphAdapter{
		simulationStore: simulationStore,
		memoryStore:     memoryStore,
		pathLearning:    pathLearning,
		comparativeLP:   comparativeLearning,
		auditor:         auditor,
		logger:          logger,
	}
}

// SimulateAndSave runs counterfactual simulation and persists results.
// Returns predictions as CounterfactualPredictionExport. Fail-open: returns empty export on error.
func (a *GraphAdapter) SimulateAndSave(ctx context.Context, decisionID, goalType string, pathScores map[string]float64, pathLengths map[string]int) decision_graph.CounterfactualPredictionExport {
	export := decision_graph.CounterfactualPredictionExport{
		Predictions: make(map[string]float64),
		Confidences: make(map[string]float64),
	}

	// Collect signals from existing learning layers (fail-open on each).
	signals := a.collectSignals(ctx, goalType)

	// Run simulation.
	simResult := SimulateTopKPaths(decisionID, goalType, pathScores, pathLengths, signals)

	if len(simResult.Predictions) == 0 {
		return export
	}

	// Persist simulation result (best-effort).
	if err := a.simulationStore.SaveSimulation(ctx, simResult); err != nil {
		a.logger.Warn("counterfactual_simulation_save_failed",
			zap.String("decision_id", decisionID),
			zap.Error(err),
		)
	}

	// Audit the simulation.
	a.auditEvent(ctx, "counterfactual.simulated", map[string]any{
		"decision_id":      decisionID,
		"goal_type":        goalType,
		"prediction_count": len(simResult.Predictions),
	})

	// Build export.
	for _, pred := range simResult.Predictions {
		export.Predictions[pred.PathSignature] = pred.ExpectedValue
		export.Confidences[pred.PathSignature] = pred.Confidence
	}

	return export
}

// collectSignals gathers all available signals for simulation from existing learning layers.
func (a *GraphAdapter) collectSignals(ctx context.Context, goalType string) *SimulationSignals {
	signals := &SimulationSignals{
		PathFeedback:           make(map[string]string),
		TransitionFeedback:     make(map[string]string),
		ComparativeFeedback:    make(map[string]string),
		ComparativeWinRates:    make(map[string]float64),
		ComparativeLossRates:   make(map[string]float64),
		HistoricalFailureRates: make(map[string]float64),
	}

	// Path learning signals.
	if a.pathLearning != nil {
		signals.PathFeedback = a.pathLearning.GetAllPathFeedbackMap(ctx, goalType)
		signals.TransitionFeedback = a.pathLearning.GetAllTransitionFeedbackMap(ctx, goalType)
	}

	// Comparative learning signals.
	if a.comparativeLP != nil {
		signals.ComparativeFeedback = a.comparativeLP.GetAllComparativeFeedbackMap(ctx, goalType)
		signals.ComparativeWinRates = a.comparativeLP.GetAllComparativeWinRates(ctx, goalType)
		signals.ComparativeLossRates = a.comparativeLP.GetAllComparativeLossRates(ctx, goalType)
	}

	return signals
}

// auditEvent records a counterfactual audit event.
func (a *GraphAdapter) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if a.auditor == nil {
		return
	}
	_ = a.auditor.RecordEvent(ctx, "counterfactual", uuid.New(), eventType,
		"system", "counterfactual_engine", payload)
}

// --- Extended Comparative Adapter (adds win/loss rate accessors) ---

// ExtendedComparativeAdapter wraps the path_comparison GraphAdapter and adds
// win/loss rate accessors for counterfactual simulation.
type ExtendedComparativeAdapter struct {
	memoryStore ComparativeMemorySource
	logger      *zap.Logger
}

// ComparativeMemorySource reads comparative memory records.
type ComparativeMemorySource interface {
	ListMemoryByGoalType(ctx context.Context, goalType string) ([]ComparativeMemoryInfo, error)
}

// ComparativeMemoryInfo mirrors the fields needed from path_comparison.ComparativeMemoryRecord.
type ComparativeMemoryInfo struct {
	PathSignature  string
	GoalType       string
	WinRate        float64
	LossRate       float64
	SelectionCount int
	MissedWinCount int
}

// NewExtendedComparativeAdapter creates an ExtendedComparativeAdapter.
func NewExtendedComparativeAdapter(memoryStore ComparativeMemorySource, logger *zap.Logger) *ExtendedComparativeAdapter {
	return &ExtendedComparativeAdapter{
		memoryStore: memoryStore,
		logger:      logger,
	}
}

// GetAllComparativeFeedbackMap returns comparative feedback.
func (a *ExtendedComparativeAdapter) GetAllComparativeFeedbackMap(ctx context.Context, goalType string) map[string]string {
	if a.memoryStore == nil {
		return make(map[string]string)
	}

	records, err := a.memoryStore.ListMemoryByGoalType(ctx, goalType)
	if err != nil {
		a.logger.Warn("counterfactual_comparative_feedback_failed",
			zap.String("goal_type", goalType),
			zap.Error(err),
		)
		return make(map[string]string)
	}

	result := make(map[string]string, len(records))
	for _, r := range records {
		result[r.PathSignature] = classifyComparativeFeedback(r)
	}
	return result
}

// GetAllComparativeWinRates returns win rates per path signature.
func (a *ExtendedComparativeAdapter) GetAllComparativeWinRates(ctx context.Context, goalType string) map[string]float64 {
	if a.memoryStore == nil {
		return make(map[string]float64)
	}

	records, err := a.memoryStore.ListMemoryByGoalType(ctx, goalType)
	if err != nil {
		return make(map[string]float64)
	}

	result := make(map[string]float64, len(records))
	for _, r := range records {
		result[r.PathSignature] = r.WinRate
	}
	return result
}

// GetAllComparativeLossRates returns loss rates per path signature.
func (a *ExtendedComparativeAdapter) GetAllComparativeLossRates(ctx context.Context, goalType string) map[string]float64 {
	if a.memoryStore == nil {
		return make(map[string]float64)
	}

	records, err := a.memoryStore.ListMemoryByGoalType(ctx, goalType)
	if err != nil {
		return make(map[string]float64)
	}

	result := make(map[string]float64, len(records))
	for _, r := range records {
		result[r.PathSignature] = r.LossRate
	}
	return result
}

// classifyComparativeFeedback maps memory record to a recommendation string.
func classifyComparativeFeedback(r ComparativeMemoryInfo) string {
	if r.MissedWinCount >= 3 {
		return "underexplored_path"
	}
	if r.SelectionCount < 5 {
		return "neutral"
	}
	if r.WinRate >= 0.7 {
		return "prefer_path"
	}
	if r.LossRate >= 0.5 {
		return "avoid_path"
	}
	return "neutral"
}
