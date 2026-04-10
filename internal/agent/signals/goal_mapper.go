package signals

// GoalMapping represents a mapping from a signal type to affected goals.
type GoalMapping struct {
	SignalType    string   `json:"signal_type"`
	AffectedGoals []string `json:"affected_goals"`
}

// MapSignalToGoals returns the goals affected by a signal type.
// Returns nil if the signal type has no known goal mapping.
func MapSignalToGoals(signalType string) []string {
	goals, ok := GoalImpact[signalType]
	if !ok {
		return nil
	}
	// Return a copy to prevent mutation.
	out := make([]string, len(goals))
	copy(out, goals)
	return out
}

// MapSignalsToGoals returns all unique goals affected by a set of signals.
func MapSignalsToGoals(signals []Signal) map[string][]string {
	result := make(map[string][]string)
	for _, s := range signals {
		goals := MapSignalToGoals(s.SignalType)
		for _, g := range goals {
			result[g] = appendUnique(result[g], s.SignalType)
		}
	}
	return result
}

// SignalMatchesGoal returns true if the given signal type affects the goal.
func SignalMatchesGoal(signalType, goalType string) bool {
	goals, ok := GoalImpact[signalType]
	if !ok {
		return false
	}
	for _, g := range goals {
		if g == goalType {
			return true
		}
	}
	return false
}

// CountMatchingSignals returns how many signals in the set match the goal.
func CountMatchingSignals(signals []Signal, goalType string) int {
	count := 0
	for _, s := range signals {
		if SignalMatchesGoal(s.SignalType, goalType) {
			count++
		}
	}
	return count
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}
