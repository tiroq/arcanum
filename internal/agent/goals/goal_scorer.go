package goals

import "fmt"

// GoalAlignmentResult carries the outcome of goal-alignment scoring for one action.
type GoalAlignmentResult struct {
	// AlignmentScore ∈ [0, 1] — additive bonus applied to the existing path score.
	AlignmentScore float64 `json:"alignment_score"`

	// MatchedGoals lists the IDs of goals that matched this action.
	MatchedGoals []string `json:"matched_goals,omitempty"`

	// RejectedByConstraints is true when the action violates at least one goal constraint.
	// A rejected action MUST NOT be executed.
	RejectedByConstraints bool `json:"rejected_by_constraints"`

	// RejectReason describes why the action was rejected (empty when not rejected).
	RejectReason string `json:"reject_reason,omitempty"`
}

// Scoring constants.
const (
	// preferredActionBoost is awarded when the action is in the preferred_actions
	// list of a high-priority goal (priority ≥ highPriorityThreshold).
	preferredActionBoost = 0.20

	// signalMatchBoost is awarded when the current action matches a signal-to-action mapping.
	signalMatchBoost = 0.10

	// highPriorityThreshold defines the minimum priority for preferred action bonuses.
	highPriorityThreshold = 0.80
)

// ScoreGoalAlignment evaluates an action type against a slice of system goals and
// returns an alignment result. The function is pure: same inputs → same output.
//
// Rules (in order):
//  1. For each goal, check constraint violations first — if violated, return rejected immediately.
//  2. For high-priority goals: award preferredActionBoost if action is in preferred_actions.
//  3. For all goals: award signalMatchBoost if action matches the signal→action mapping.
//
// Final alignment score is clamped to [0, 1]. Scores accumulate across goals.
func ScoreGoalAlignment(actionType string, sysGoals []SystemGoal) GoalAlignmentResult {
	result := GoalAlignmentResult{}

	for _, g := range sysGoals {
		// 1. Constraint enforcement — reject immediately.
		if violated, reason := checkConstraint(actionType, g); violated {
			result.RejectedByConstraints = true
			result.RejectReason = reason
			return result
		}

		// 2. Preferred action boost (high-priority goals only).
		if g.Priority >= highPriorityThreshold {
			for _, pa := range g.PreferredActions {
				if pa == actionType {
					result.AlignmentScore += preferredActionBoost
					result.MatchedGoals = appendUnique(result.MatchedGoals, g.ID)
					break
				}
			}
		}

		// 3. Signal match boost.
		for _, sig := range g.Signals {
			if signalActivatesAction(sig, actionType) {
				result.AlignmentScore += signalMatchBoost
				result.MatchedGoals = appendUnique(result.MatchedGoals, g.ID)
				break
			}
		}
	}

	// Clamp to [0, 1].
	if result.AlignmentScore > 1.0 {
		result.AlignmentScore = 1.0
	}

	return result
}

// checkConstraint returns (violated, reason) for a given action against a goal's constraints.
func checkConstraint(actionType string, g SystemGoal) (bool, string) {
	if len(g.Constraints) == 0 {
		return false, ""
	}

	// forbid_actions: list of action types that are strictly forbidden.
	if raw, ok := g.Constraints["forbid_actions"]; ok {
		switch v := raw.(type) {
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok && s == actionType {
					return true, fmt.Sprintf("goal %q forbids action %q", g.ID, actionType)
				}
			}
		case []string:
			for _, s := range v {
				if s == actionType {
					return true, fmt.Sprintf("goal %q forbids action %q", g.ID, actionType)
				}
			}
		}
	}

	return false, ""
}

// signalActivatesAction maps a well-known signal name to the action types it enables.
// Only explicit mappings are defined — anything else returns false.
func signalActivatesAction(signal, actionType string) bool {
	switch signal {
	case "failed_jobs":
		return actionType == "retry_job"
	case "pending_tasks":
		return actionType == "summarize_state" || actionType == "generate_plan" ||
			actionType == "prioritize_tasks"
	case "new_opportunities":
		return actionType == "propose_income_action" || actionType == "analyze_opportunity" ||
			actionType == "schedule_work"
	case "dead_letter_queue":
		return actionType == "retry_job" || actionType == "log_recommendation"
	case "retry_attempts":
		return actionType == "retry_job"
	case "action_outcomes":
		return actionType == "record_outcome" || actionType == "update_memory"
	case "decision_results":
		return actionType == "adjust_strategy" || actionType == "record_outcome"
	}
	return false
}

// appendUnique appends s to slice only if it is not already present.
func appendUnique(slice []string, s string) []string {
	for _, existing := range slice {
		if existing == s {
			return slice
		}
	}
	return append(slice, s)
}
