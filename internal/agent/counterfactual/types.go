package counterfactual

import "time"

// --- Prediction (produced BEFORE execution) ---

// PathPrediction holds the simulated expected outcome for a candidate path.
type PathPrediction struct {
	PathSignature   string             `json:"path_signature"`
	ExpectedValue   float64            `json:"expected_value"`
	ExpectedRisk    float64            `json:"expected_risk"`
	Confidence      float64            `json:"confidence"`
	SourceBreakdown map[string]float64 `json:"source_breakdown"`
}

// SimulationResult captures all predictions for a single decision.
type SimulationResult struct {
	DecisionID  string           `json:"decision_id"`
	GoalType    string           `json:"goal_type"`
	Predictions []PathPrediction `json:"predictions"`
	CreatedAt   time.Time        `json:"created_at"`
}

// --- Prediction Outcome (evaluated AFTER execution) ---

// PredictionOutcome compares the prediction against actual results.
type PredictionOutcome struct {
	DecisionID       string    `json:"decision_id"`
	PathSignature    string    `json:"path_signature"`
	GoalType         string    `json:"goal_type"`
	PredictedValue   float64   `json:"predicted_value"`
	ActualValue      float64   `json:"actual_value"`
	AbsoluteError    float64   `json:"absolute_error"`
	DirectionCorrect bool      `json:"direction_correct"`
	CreatedAt        time.Time `json:"created_at"`
}

// --- Prediction Memory (accumulated accuracy tracking) ---

// PredictionMemoryRecord tracks prediction accuracy per path + goal.
type PredictionMemoryRecord struct {
	PathSignature         string    `json:"path_signature"`
	GoalType              string    `json:"goal_type"`
	TotalPredictions      int       `json:"total_predictions"`
	TotalError            float64   `json:"total_error"`
	AvgError              float64   `json:"avg_error"`
	DirectionAccuracy     float64   `json:"direction_accuracy"`
	DirectionCorrectCount int       `json:"direction_correct_count"`
	LastUpdated           time.Time `json:"last_updated"`
}

// --- Simulation Input Signals ---

// SimulationSignals carries all existing signals needed for counterfactual simulation.
// These are collected from path learning, comparative learning, and strategy learning.
type SimulationSignals struct {
	// PathFeedback: map[pathSignature] → recommendation ("prefer_path", "avoid_path", "neutral")
	PathFeedback map[string]string `json:"path_feedback"`

	// TransitionFeedback: map[transitionKey] → recommendation
	TransitionFeedback map[string]string `json:"transition_feedback"`

	// ComparativeFeedback: map[pathSignature] → recommendation
	ComparativeFeedback map[string]string `json:"comparative_feedback"`

	// ComparativeWinRates: map[pathSignature] → win rate (0.0 to 1.0)
	ComparativeWinRates map[string]float64 `json:"comparative_win_rates"`

	// ComparativeLossRates: map[pathSignature] → loss rate (0.0 to 1.0)
	ComparativeLossRates map[string]float64 `json:"comparative_loss_rates"`

	// HistoricalFailureRates: map[pathSignature] → failure rate (0.0 to 1.0)
	HistoricalFailureRates map[string]float64 `json:"historical_failure_rates"`
}

// --- Constants ---

const (
	// MaxSimulatedPaths is the maximum number of alternative paths to simulate.
	MaxSimulatedPaths = 3

	// PredictionWeight controls how much the prediction adjusts the original score.
	// AdjustedScore = OriginalScore + (PredictedValue - OriginalScore) * PredictionWeight
	PredictionWeight = 0.20

	// Signal weights for simulation.
	SignalWeightPathLearning        = 0.30
	SignalWeightComparativeLearning = 0.30
	SignalWeightTransitionLearning  = 0.20
	SignalWeightHistorical          = 0.20

	// Risk signal weights.
	RiskWeightHistorical  = 0.50
	RiskWeightPathLength  = 0.30
	RiskWeightComparative = 0.20

	// Path length risk penalty per node beyond 1.
	PathLengthRiskPenalty = 0.10

	// Minimum confidence to assign any weight to a signal.
	MinSignalConfidence = 0.01

	// Outcome value mapping for error calculation.
	OutcomeValueSuccess = 1.0
	OutcomeValueNeutral = 0.5
	OutcomeValueFailure = 0.0
)
