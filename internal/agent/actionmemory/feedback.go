package actionmemory

import "time"

// Feedback thresholds.
const (
	minSampleSize          = 5
	avoidFailureThreshold  = 0.5
	preferSuccessThreshold = 0.7
)

// Recommendation is a deterministic learning signal.
type Recommendation string

const (
	RecommendInsufficientData Recommendation = "insufficient_data"
	RecommendAvoidAction      Recommendation = "avoid_action"
	RecommendPreferAction     Recommendation = "prefer_action"
	RecommendNeutral          Recommendation = "neutral"
)

// ActionFeedback captures the learning signal for an action type.
type ActionFeedback struct {
	ActionType     string         `json:"action_type"`
	SuccessRate    float64        `json:"success_rate"`
	FailureRate    float64        `json:"failure_rate"`
	SampleSize     int            `json:"sample_size"`
	Recommendation Recommendation `json:"recommendation"`
	LastUpdated    time.Time      `json:"last_updated"`
}

// GenerateFeedback produces a deterministic feedback signal from a memory record.
// If record is nil (no history), returns insufficient_data.
func GenerateFeedback(record *ActionMemoryRecord) ActionFeedback {
	if record == nil {
		return ActionFeedback{
			Recommendation: RecommendInsufficientData,
		}
	}

	fb := ActionFeedback{
		ActionType:  record.ActionType,
		SuccessRate: record.SuccessRate,
		FailureRate: record.FailureRate,
		SampleSize:  record.TotalRuns,
		LastUpdated: record.LastUpdated,
	}

	switch {
	case record.TotalRuns < minSampleSize:
		fb.Recommendation = RecommendInsufficientData
	case record.FailureRate >= avoidFailureThreshold:
		fb.Recommendation = RecommendAvoidAction
	case record.SuccessRate >= preferSuccessThreshold:
		fb.Recommendation = RecommendPreferAction
	default:
		fb.Recommendation = RecommendNeutral
	}

	return fb
}
