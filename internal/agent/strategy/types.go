package strategy

import (
	"time"

	"github.com/google/uuid"
)

// --- Strategy Types ---

// StrategyType identifies a bounded multi-step strategy family.
type StrategyType string

const (
	StrategyDirectRetry        StrategyType = "direct_retry"
	StrategyObserveThenRetry   StrategyType = "observe_then_retry"
	StrategyRetryThenRecommend StrategyType = "retry_then_recommend"
	StrategyDirectResync       StrategyType = "direct_resync"
	StrategyObserveThenResync  StrategyType = "observe_then_resync"
	StrategyRecommendOnly      StrategyType = "recommendation_only"
	StrategyNoop               StrategyType = "noop"
)

// GoalFamily classifies a goal type into a strategy generation family.
type GoalFamily string

const (
	GoalFamilyRetry    GoalFamily = "retry"
	GoalFamilyBacklog  GoalFamily = "backlog"
	GoalFamilyAdvisory GoalFamily = "advisory"
)

// GoalFamilyForType maps a goal type to a strategy family.
func GoalFamilyForType(goalType string) GoalFamily {
	switch goalType {
	case "reduce_retry_rate", "investigate_failed_jobs":
		return GoalFamilyRetry
	case "resolve_queue_backlog":
		return GoalFamilyBacklog
	default:
		return GoalFamilyAdvisory
	}
}

// StrategiesForFamily returns the candidate strategy types for a goal family.
func StrategiesForFamily(family GoalFamily) []StrategyType {
	switch family {
	case GoalFamilyRetry:
		return []StrategyType{
			StrategyDirectRetry,
			StrategyObserveThenRetry,
			StrategyRetryThenRecommend,
			StrategyRecommendOnly,
			StrategyNoop,
		}
	case GoalFamilyBacklog:
		return []StrategyType{
			StrategyDirectResync,
			StrategyObserveThenResync,
			StrategyRecommendOnly,
			StrategyNoop,
		}
	case GoalFamilyAdvisory:
		return []StrategyType{
			StrategyRecommendOnly,
			StrategyNoop,
		}
	default:
		return []StrategyType{StrategyNoop}
	}
}

// --- Strategy Step ---

// StrategyStep represents one bounded action within a strategy plan.
type StrategyStep struct {
	Order      int            `json:"order"`
	ActionType string         `json:"action_type"`
	Params     map[string]any `json:"params,omitempty"`
	Condition  string         `json:"condition,omitempty"`
	Optional   bool           `json:"optional"`
}

// --- Strategy Plan ---

// StrategyPlan represents a bounded multi-step plan for achieving a goal.
// Maximum depth: 3 steps. Preferred: 2 or fewer.
type StrategyPlan struct {
	ID              uuid.UUID      `json:"id"`
	GoalID          string         `json:"goal_id"`
	GoalType        string         `json:"goal_type"`
	StrategyType    StrategyType   `json:"strategy_type"`
	Steps           []StrategyStep `json:"steps"`
	ExpectedUtility float64        `json:"expected_utility"`
	RiskScore       float64        `json:"risk_score"`
	Confidence      float64        `json:"confidence"`
	Explanation     string         `json:"explanation"`
	Exploratory     bool           `json:"exploratory"`
	Selected        bool           `json:"selected"`
	CreatedAt       time.Time      `json:"created_at"`
}

// StepCount returns the number of steps in the plan.
func (sp *StrategyPlan) StepCount() int {
	return len(sp.Steps)
}

// FirstStep returns the first step of the plan.
func (sp *StrategyPlan) FirstStep() StrategyStep {
	if len(sp.Steps) == 0 {
		return StrategyStep{}
	}
	return sp.Steps[0]
}

// --- Strategy Decision ---

// StrategyDecision captures the full deliberation for a goal:
// which strategies were considered, which was selected, and why.
type StrategyDecision struct {
	GoalID              string         `json:"goal_id"`
	GoalType            string         `json:"goal_type"`
	CandidateStrategies []StrategyPlan `json:"candidate_strategies"`
	SelectedStrategyID  uuid.UUID      `json:"selected_strategy_id"`
	Reason              string         `json:"reason"`
	CreatedAt           time.Time      `json:"created_at"`
}

// --- Scoring Constants ---

const (
	// MaxStrategyDepth is the absolute ceiling for strategy steps.
	MaxStrategyDepth = 3

	// RiskPerStep is the risk penalty applied per additional step.
	RiskPerStep = 0.15

	// SimplicityBias: when utility difference is below this,
	// prefer the simpler (fewer steps) strategy.
	SimplicityBias = 0.05

	// MultiStepConfidenceMultiplier: multi-step strategies multiply
	// confidence by this for each extra step beyond the first.
	MultiStepConfidenceMultiplier = 0.85

	// MinUtilityThreshold: strategies below this utility are rejected.
	MinUtilityThreshold = 0.05

	// StabilityThrottlePenalty: penalty multiplier for multi-step
	// strategies when stability mode is throttled.
	StabilityThrottlePenalty = 0.3

	// ExploratoryUtilityDiscount: exploratory strategies get this
	// discount to their expected utility score.
	ExploratoryUtilityDiscount = 0.10
)

// ExecutionMode documents how strategy steps are executed.
// Mode A: persist full plan, execute only step 1 in this cycle.
const ExecutionMode = "plan_full_execute_first"
