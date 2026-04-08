package strategylearning

import (
	"time"

	"github.com/google/uuid"
)

// --- Outcome Model ---

// StrategyOutcome captures the evaluated result of an executed strategy.
type StrategyOutcome struct {
	ID            uuid.UUID `json:"id"`
	StrategyID    uuid.UUID `json:"strategy_id"`
	StrategyType  string    `json:"strategy_type"`
	GoalType      string    `json:"goal_type"`
	Step1Action   string    `json:"step1_action"`
	Step2Executed bool      `json:"step2_executed"`
	FinalStatus   string    `json:"final_status"` // success | neutral | failure
	Improvement   bool      `json:"improvement"`
	EvaluatedAt   time.Time `json:"evaluated_at"`
}

// --- Memory Record ---

// StrategyMemoryRecord holds aggregate statistics for a strategy_type + goal_type pair.
type StrategyMemoryRecord struct {
	ID           uuid.UUID `json:"id"`
	StrategyType string    `json:"strategy_type"`
	GoalType     string    `json:"goal_type"`
	TotalRuns    int       `json:"total_runs"`
	SuccessRuns  int       `json:"success_runs"`
	FailureRuns  int       `json:"failure_runs"`
	NeutralRuns  int       `json:"neutral_runs"`
	SuccessRate  float64   `json:"success_rate"`
	FailureRate  float64   `json:"failure_rate"`
	LastUpdated  time.Time `json:"last_updated"`
}

// --- Feedback ---

// StrategyRecommendation is a deterministic learning signal for a strategy type.
type StrategyRecommendation string

const (
	RecommendPreferStrategy       StrategyRecommendation = "prefer_strategy"
	RecommendAvoidStrategy        StrategyRecommendation = "avoid_strategy"
	RecommendNeutralStrategy      StrategyRecommendation = "neutral"
	RecommendInsufficientStrategy StrategyRecommendation = "insufficient_data"
)

// StrategyFeedback captures the learning signal for a strategy type.
type StrategyFeedback struct {
	StrategyType   string                 `json:"strategy_type"`
	GoalType       string                 `json:"goal_type"`
	SuccessRate    float64                `json:"success_rate"`
	FailureRate    float64                `json:"failure_rate"`
	SampleSize     int                    `json:"sample_size"`
	Recommendation StrategyRecommendation `json:"recommendation"`
	LastUpdated    time.Time              `json:"last_updated"`
}

// --- Continuation Decision ---

// ContinuationDecision captures whether step 2 should be executed.
type ContinuationDecision struct {
	ShouldContinue bool   `json:"should_continue"`
	Step2Action    string `json:"step2_action,omitempty"`
	StrategyID     string `json:"strategy_id,omitempty"`
	StrategyType   string `json:"strategy_type,omitempty"`
	Reason         string `json:"reason"`
}

// --- Thresholds ---

const (
	// MinSampleSize is the minimum number of runs before feedback is generated.
	MinSampleSize = 5
	// PreferSuccessRate: strategy is preferred when success_rate >= this.
	PreferSuccessRate = 0.70
	// AvoidFailureRate: strategy is avoided when failure_rate >= this.
	AvoidFailureRate = 0.50
	// MinContinuationConfidence: minimum confidence to allow continuation.
	MinContinuationConfidence = 0.60
	// MaxContinuationFailureRate: do not continue strategies above this failure rate.
	MaxContinuationFailureRate = 0.50
	// MaxContinuationDepth: absolute max steps ever executed (step 1 + step 2).
	MaxContinuationDepth = 2
	// StrategyPreferBoost: scoring boost for preferred strategies.
	StrategyPreferBoost = 0.15
	// StrategyAvoidPenalty: scoring penalty for avoided strategies.
	StrategyAvoidPenalty = -0.30
)

// --- Outcome Status Constants ---

const (
	OutcomeSuccess = "success"
	OutcomeNeutral = "neutral"
	OutcomeFailure = "failure"
)
