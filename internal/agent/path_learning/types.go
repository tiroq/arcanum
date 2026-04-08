package pathlearning

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// --- Path Outcome Model ---

// PathOutcome captures the evaluated result of a selected decision path.
type PathOutcome struct {
	ID               uuid.UUID `json:"id"`
	PathID           string    `json:"path_id"`
	GoalType         string    `json:"goal_type"`
	PathSignature    string    `json:"path_signature"`
	PathLength       int       `json:"path_length"`
	FirstStepAction  string    `json:"first_step_action"`
	FirstStepStatus  string    `json:"first_step_status"` // success | neutral | failure
	ContinuationUsed bool      `json:"continuation_used"`
	FinalStatus      string    `json:"final_status"` // success | neutral | failure
	Improvement      bool      `json:"improvement"`
	EvaluatedAt      time.Time `json:"evaluated_at"`
}

// --- Path Memory Record ---

// PathMemoryRecord holds aggregate statistics for a path_signature + goal_type pair.
type PathMemoryRecord struct {
	ID            uuid.UUID `json:"id"`
	PathSignature string    `json:"path_signature"`
	GoalType      string    `json:"goal_type"`
	TotalRuns     int       `json:"total_runs"`
	SuccessRuns   int       `json:"success_runs"`
	FailureRuns   int       `json:"failure_runs"`
	NeutralRuns   int       `json:"neutral_runs"`
	SuccessRate   float64   `json:"success_rate"`
	FailureRate   float64   `json:"failure_rate"`
	LastUpdated   time.Time `json:"last_updated"`
}

// --- Transition Memory Record ---

// TransitionMemoryRecord holds aggregate statistics for a transition key + goal type pair.
type TransitionMemoryRecord struct {
	ID             uuid.UUID `json:"id"`
	GoalType       string    `json:"goal_type"`
	FromActionType string    `json:"from_action_type"`
	ToActionType   string    `json:"to_action_type"`
	TransitionKey  string    `json:"transition_key"` // canonical: "from->to"
	TotalUses      int       `json:"total_uses"`
	HelpfulUses    int       `json:"helpful_uses"`
	UnhelpfulUses  int       `json:"unhelpful_uses"`
	NeutralUses    int       `json:"neutral_uses"`
	HelpfulRate    float64   `json:"helpful_rate"`
	UnhelpfulRate  float64   `json:"unhelpful_rate"`
	LastUpdated    time.Time `json:"last_updated"`
}

// --- Feedback Types ---

// PathRecommendation is a deterministic learning signal for a path signature.
type PathRecommendation string

const (
	RecommendPreferPath  PathRecommendation = "prefer_path"
	RecommendAvoidPath   PathRecommendation = "avoid_path"
	RecommendNeutralPath PathRecommendation = "neutral"
)

// PathFeedback captures the learning signal for a path signature.
type PathFeedback struct {
	PathSignature  string             `json:"path_signature"`
	GoalType       string             `json:"goal_type"`
	SuccessRate    float64            `json:"success_rate"`
	FailureRate    float64            `json:"failure_rate"`
	SampleSize     int                `json:"sample_size"`
	Recommendation PathRecommendation `json:"recommendation"`
}

// TransitionRecommendation is a deterministic learning signal for a transition.
type TransitionRecommendation string

const (
	RecommendPreferTransition  TransitionRecommendation = "prefer_transition"
	RecommendAvoidTransition   TransitionRecommendation = "avoid_transition"
	RecommendNeutralTransition TransitionRecommendation = "neutral"
)

// TransitionFeedback captures the learning signal for a transition key.
type TransitionFeedback struct {
	TransitionKey  string                   `json:"transition_key"`
	GoalType       string                   `json:"goal_type"`
	HelpfulRate    float64                  `json:"helpful_rate"`
	UnhelpfulRate  float64                  `json:"unhelpful_rate"`
	SampleSize     int                      `json:"sample_size"`
	Recommendation TransitionRecommendation `json:"recommendation"`
}

// --- Outcome Status Constants ---

const (
	OutcomeSuccess = "success"
	OutcomeNeutral = "neutral"
	OutcomeFailure = "failure"
)

// --- Feedback Thresholds ---

const (
	// Path feedback thresholds.
	PathPreferSuccessRate float64 = 0.7
	PathAvoidFailureRate  float64 = 0.5
	PathMinSampleSize     int     = 5

	// Transition feedback thresholds.
	TransitionPreferHelpfulRate  float64 = 0.6
	TransitionAvoidUnhelpfulRate float64 = 0.5
	TransitionMinSampleSize      int     = 5
)

// --- Score Adjustment Constants ---

const (
	// Path-level adjustments to FinalScore.
	PathPreferAdjustment float64 = 0.10
	PathAvoidAdjustment  float64 = -0.20

	// Transition-level adjustments to FinalScore (per edge).
	TransitionPreferAdjustment float64 = 0.05
	TransitionAvoidAdjustment  float64 = -0.10
)

// --- Canonical Key Functions ---

// BuildPathSignature creates a canonical path signature from an ordered list of action types.
// Example: ["retry_job", "log_recommendation"] → "retry_job>log_recommendation"
func BuildPathSignature(actionTypes []string) string {
	return strings.Join(actionTypes, ">")
}

// BuildTransitionKey creates a canonical transition key.
// Example: ("retry_job", "log_recommendation") → "retry_job->log_recommendation"
func BuildTransitionKey(fromAction, toAction string) string {
	return fromAction + "->" + toAction
}

// ExtractTransitions derives transition keys from an ordered list of action types.
// Example: ["retry_job", "log_recommendation", "noop"]
//
//	→ [("retry_job", "log_recommendation"), ("log_recommendation", "noop")]
func ExtractTransitions(actionTypes []string) []TransitionPair {
	if len(actionTypes) < 2 {
		return nil
	}
	pairs := make([]TransitionPair, 0, len(actionTypes)-1)
	for i := 0; i < len(actionTypes)-1; i++ {
		pairs = append(pairs, TransitionPair{
			From: actionTypes[i],
			To:   actionTypes[i+1],
		})
	}
	return pairs
}

// TransitionPair holds a from→to pair of action types.
type TransitionPair struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// --- Feedback Generation (Pure Functions) ---

// GeneratePathFeedback produces a deterministic recommendation from a path memory record.
func GeneratePathFeedback(record *PathMemoryRecord) PathFeedback {
	fb := PathFeedback{
		PathSignature:  record.PathSignature,
		GoalType:       record.GoalType,
		SuccessRate:    record.SuccessRate,
		FailureRate:    record.FailureRate,
		SampleSize:     record.TotalRuns,
		Recommendation: RecommendNeutralPath,
	}

	if record.TotalRuns < PathMinSampleSize {
		return fb
	}

	if record.SuccessRate >= PathPreferSuccessRate {
		fb.Recommendation = RecommendPreferPath
	} else if record.FailureRate >= PathAvoidFailureRate {
		fb.Recommendation = RecommendAvoidPath
	}

	return fb
}

// GenerateTransitionFeedback produces a deterministic recommendation from a transition memory record.
func GenerateTransitionFeedback(record *TransitionMemoryRecord) TransitionFeedback {
	fb := TransitionFeedback{
		TransitionKey:  record.TransitionKey,
		GoalType:       record.GoalType,
		HelpfulRate:    record.HelpfulRate,
		UnhelpfulRate:  record.UnhelpfulRate,
		SampleSize:     record.TotalUses,
		Recommendation: RecommendNeutralTransition,
	}

	if record.TotalUses < TransitionMinSampleSize {
		return fb
	}

	if record.HelpfulRate >= TransitionPreferHelpfulRate {
		fb.Recommendation = RecommendPreferTransition
	} else if record.UnhelpfulRate >= TransitionAvoidUnhelpfulRate {
		fb.Recommendation = RecommendAvoidTransition
	}

	return fb
}
