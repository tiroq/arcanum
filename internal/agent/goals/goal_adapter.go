package goals

import "context"

// GraphAdapter implements decision_graph.SystemGoalAlignmentProvider.
// It bridges the goals package to the decision graph without creating import cycles.
type GraphAdapter struct {
	goals []SystemGoal
}

// NewGoalGraphAdapter creates a GraphAdapter with the given system goals.
// If goals is nil or empty the adapter is a no-op (fail-open).
func NewGoalGraphAdapter(sysGoals []SystemGoal) *GraphAdapter {
	cp := make([]SystemGoal, len(sysGoals))
	copy(cp, sysGoals)
	return &GraphAdapter{goals: cp}
}

// ScoreAlignment scores an action against the loaded system goals and returns
// the alignment score, matched goal IDs, rejection status, and rejection reason.
// Implements decision_graph.SystemGoalAlignmentProvider.
func (a *GraphAdapter) ScoreAlignment(_ context.Context, actionType string, _ float64) (float64, []string, bool, string) {
	if len(a.goals) == 0 {
		return 0, nil, false, ""
	}
	result := ScoreGoalAlignment(actionType, a.goals)
	return result.AlignmentScore, result.MatchedGoals, result.RejectedByConstraints, result.RejectReason
}
