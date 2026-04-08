package calibration

import "time"

// --- Mode-Specific Calibration Types (Iteration 28) ---

// ModeCalibrationRecord captures a single prediction-vs-outcome data point
// scoped to a specific reasoning mode (graph, direct, conservative, exploratory).
type ModeCalibrationRecord struct {
	DecisionID          string    `json:"decision_id"`
	GoalType            string    `json:"goal_type"`
	Mode                string    `json:"mode"`
	PredictedConfidence float64   `json:"predicted_confidence"`
	ActualOutcome       string    `json:"actual_outcome"`
	CreatedAt           time.Time `json:"created_at"`
}

// ModeCalibrationBucket aggregates outcomes for a confidence range within a mode.
type ModeCalibrationBucket struct {
	Mode          string  `json:"mode"`
	MinConfidence float64 `json:"min_confidence"`
	MaxConfidence float64 `json:"max_confidence"`
	Count         int     `json:"count"`
	SuccessCount  int     `json:"success_count"`
	Accuracy      float64 `json:"accuracy"`
	AvgConfidence float64 `json:"avg_confidence"`
}

// ModeCalibrationSummary provides a full calibration picture for a single mode.
type ModeCalibrationSummary struct {
	Mode                     string                  `json:"mode"`
	Buckets                  []ModeCalibrationBucket `json:"buckets"`
	ExpectedCalibrationError float64                 `json:"expected_calibration_error"`
	OverconfidenceScore      float64                 `json:"overconfidence_score"`
	UnderconfidenceScore     float64                 `json:"underconfidence_score"`
	TotalRecords             int                     `json:"total_records"`
	LastUpdated              time.Time               `json:"last_updated"`
}

const (
	// ModeCalibrationWeight controls the strength of mode-specific confidence correction.
	// adjustment = (mode_accuracy - avg_mode_confidence) × ModeCalibrationWeight
	ModeCalibrationWeight = 0.25

	// ModeMaxAdjustment is the maximum absolute correction from mode calibration.
	ModeMaxAdjustment = 0.15

	// ModeMinBucketSamples is the minimum number of samples in a bucket
	// before it's used for mode calibration.
	ModeMinBucketSamples = 3

	// ModeMaxTrackerRecords is the sliding window bound per mode.
	ModeMaxTrackerRecords = 500
)

// KnownModes lists the valid reasoning modes.
// Unknown modes are recorded but do not affect calibration.
var KnownModes = []string{"graph", "direct", "conservative", "exploratory"}

// IsKnownMode returns true if the mode is one of the recognized reasoning modes.
func IsKnownMode(mode string) bool {
	for _, m := range KnownModes {
		if m == mode {
			return true
		}
	}
	return false
}
