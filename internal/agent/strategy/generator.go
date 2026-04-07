package strategy

import (
	"time"

	"github.com/google/uuid"
)

// Generate produces bounded strategy candidates for a goal.
// Deterministic: same inputs always produce the same candidates.
func Generate(goalID, goalType string, now time.Time) []StrategyPlan {
	family := GoalFamilyForType(goalType)
	types := StrategiesForFamily(family)

	var plans []StrategyPlan
	for _, st := range types {
		plan := buildPlan(st, goalID, goalType, now)
		plans = append(plans, plan)
	}
	return plans
}

// buildPlan creates a StrategyPlan with explicit steps for a given strategy type.
func buildPlan(st StrategyType, goalID, goalType string, now time.Time) StrategyPlan {
	plan := StrategyPlan{
		ID:           uuid.New(),
		GoalID:       goalID,
		GoalType:     goalType,
		StrategyType: st,
		CreatedAt:    now,
	}

	switch st {
	case StrategyDirectRetry:
		plan.Steps = []StrategyStep{
			{Order: 1, ActionType: "retry_job"},
		}
		plan.Explanation = "retry failed job directly"

	case StrategyObserveThenRetry:
		plan.Steps = []StrategyStep{
			{Order: 1, ActionType: "log_recommendation", Params: map[string]any{
				"_ctx_observation_marker": true,
			}},
			{Order: 2, ActionType: "retry_job",
				Condition: "if issue unresolved after observation window"},
		}
		plan.Explanation = "observe system first, then retry if needed"

	case StrategyRetryThenRecommend:
		plan.Steps = []StrategyStep{
			{Order: 1, ActionType: "retry_job"},
			{Order: 2, ActionType: "log_recommendation",
				Condition: "if retry outcome is neutral or failure",
				Optional:  true},
		}
		plan.Explanation = "retry first, then recommend if inconclusive"

	case StrategyDirectResync:
		plan.Steps = []StrategyStep{
			{Order: 1, ActionType: "trigger_resync"},
		}
		plan.Explanation = "resync sources directly"

	case StrategyObserveThenResync:
		plan.Steps = []StrategyStep{
			{Order: 1, ActionType: "log_recommendation", Params: map[string]any{
				"_ctx_observation_marker": true,
			}},
			{Order: 2, ActionType: "trigger_resync",
				Condition: "if backlog persists after observation window"},
		}
		plan.Explanation = "observe backlog first, then resync if needed"

	case StrategyRecommendOnly:
		plan.Steps = []StrategyStep{
			{Order: 1, ActionType: "log_recommendation"},
		}
		plan.Explanation = "log recommendation only"

	case StrategyNoop:
		plan.Steps = []StrategyStep{
			{Order: 1, ActionType: "noop"},
		}
		plan.Explanation = "no action needed"
	}

	return plan
}
