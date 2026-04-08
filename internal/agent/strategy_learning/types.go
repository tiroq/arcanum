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
	FinalStatus   string    `json:"final_status"` // success | neutral | failure — kept for backward compat
	Improvement   bool      `json:"improvement"`
	EvaluatedAt   time.Time `json:"evaluated_at"`

	// --- Iteration 18.1: step-level signals ---
	Step1Status      string `json:"step1_status"`      // success | neutral | failure
	Step2Status      string `json:"step2_status"`      // success | neutral | failure | "" (skipped)
	ContinuationUsed bool   `json:"continuation_used"` // true if step 2 was executed
	ContinuationGain bool   `json:"continuation_gain"` // true if step 2 improved over step 1
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

	// --- Iteration 18.1: step-level + continuation tracking ---
	Step1SuccessRuns     int     `json:"step1_success_runs"`
	Step2SuccessRuns     int     `json:"step2_success_runs"`
	ContinuationUsedRuns int     `json:"continuation_used_runs"`
	ContinuationGainRuns int     `json:"continuation_gain_runs"`
	Step1SuccessRate     float64 `json:"step1_success_rate"`
	Step2SuccessRate     float64 `json:"step2_success_rate"`
	ContinuationGainRate float64 `json:"continuation_gain_rate"`

	// --- Iteration 19: portfolio tracking ---
	SelectionCount int     `json:"selection_count"`
	WinCount       int     `json:"win_count"`
	WinRate        float64 `json:"win_rate"`
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

	// --- Iteration 18.1: continuation signals ---
	PreferContinuation bool `json:"prefer_continuation"`
	AvoidContinuation  bool `json:"avoid_continuation"`
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

	// PreferContinuationGainRate: continuation is preferred above this gain rate.
	PreferContinuationGainRate = 0.60
	// AvoidContinuationGainRate: continuation is avoided below this gain rate.
	AvoidContinuationGainRate = 0.30
	// MinContinuationSampleSize: minimum continuation uses before gating.
	MinContinuationSampleSize = 5
	// ContinuationPreferBoost: scoring boost for preferred continuation.
	ContinuationPreferBoost = 0.10
	// ContinuationAvoidPenalty: scoring penalty for avoided continuation.
	ContinuationAvoidPenalty = -0.15
)

// --- Outcome Status Constants ---

const (
	OutcomeSuccess = "success"
	OutcomeNeutral = "neutral"
	OutcomeFailure = "failure"
)
