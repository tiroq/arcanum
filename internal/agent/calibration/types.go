package calibration

import "time"

// CalibrationRecord captures a single prediction-vs-outcome data point.
type CalibrationRecord struct {
	DecisionID          string    `json:"decision_id"`
	PredictedConfidence float64   `json:"predicted_confidence"`
	ActualOutcome       string    `json:"actual_outcome"`
	CreatedAt           time.Time `json:"created_at"`
}

// CalibrationBucket aggregates outcomes for a confidence range.
type CalibrationBucket struct {
	MinConfidence float64 `json:"min_confidence"`
	MaxConfidence float64 `json:"max_confidence"`
	Count         int     `json:"count"`
	SuccessCount  int     `json:"success_count"`
	Accuracy      float64 `json:"accuracy"`
	AvgConfidence float64 `json:"avg_confidence"`
}

// CalibrationSummary provides a full calibration picture.
type CalibrationSummary struct {
	Buckets                  []CalibrationBucket `json:"buckets"`
	ExpectedCalibrationError float64             `json:"expected_calibration_error"`
	OverconfidenceScore      float64             `json:"overconfidence_score"`
	UnderconfidenceScore     float64             `json:"underconfidence_score"`
	TotalRecords             int                 `json:"total_records"`
	LastUpdated              time.Time           `json:"last_updated"`
}

const (
	CalibrationWeight = 0.3
	MinBucketSamples  = 3
	MaxTrackerRecords = 500
	BucketCount       = 5
)

// BucketBoundaries returns the fixed bucket boundaries.
func BucketBoundaries() [BucketCount][2]float64 {
	return [BucketCount][2]float64{
		{0.0, 0.2},
		{0.2, 0.4},
		{0.4, 0.6},
		{0.6, 0.8},
		{0.8, 1.0},
	}
}

// BucketIndex returns the bucket index for a given confidence value.
func BucketIndex(confidence float64) int {
	if confidence >= 1.0 {
		return BucketCount - 1
	}
	if confidence < 0.0 {
		return 0
	}
	idx := int(confidence/0.2 + 1e-9)
	if idx >= BucketCount {
		return BucketCount - 1
	}
	return idx
}

// OutcomeIsSuccess returns true if the outcome counts as success.
func OutcomeIsSuccess(outcome string) bool {
	return outcome == "success"
}
