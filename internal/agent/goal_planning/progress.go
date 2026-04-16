package goal_planning

import "time"

// MeasureProgress computes progress for a subgoal based on its target metric
// and current value. Returns a progress score in [0,1].
// This is a pure function.
func MeasureProgress(sg Subgoal) float64 {
	if sg.TargetValue <= 0 {
		// Metric where target is 0 (e.g. governance_violations).
		// Progress = 1.0 when current is at or below target, else decrease.
		if sg.CurrentValue <= 0 {
			return 1.0
		}
		return clamp01(1.0 - sg.CurrentValue/5.0) // linear decay, 0 at 5+
	}

	// Standard metric: progress = current / target.
	return clamp01(sg.CurrentValue / sg.TargetValue)
}

// IsStale checks if a subgoal's progress tracking is stale.
func IsStale(sg Subgoal, now time.Time) bool {
	return now.Sub(sg.UpdatedAt).Hours() > ProgressStaleHours
}

// ShouldAutoComplete checks if a subgoal has met its completion criteria.
func ShouldAutoComplete(sg Subgoal) bool {
	return sg.Status == SubgoalActive && sg.ProgressScore >= MinProgressToComplete
}

// ShouldBlock checks if a subgoal should be marked blocked.
func ShouldBlock(sg Subgoal, now time.Time) bool {
	if sg.Status != SubgoalActive {
		return false
	}
	// Stale + low progress = blocked.
	return IsStale(sg, now) && sg.ProgressScore < BlockedProgressThreshold
}

// IsDependencyMet checks if the subgoal's dependency (if any) is completed.
func IsDependencyMet(sg Subgoal, allSubgoals []Subgoal) bool {
	if sg.DependsOn == "" {
		return true
	}
	for _, dep := range allSubgoals {
		if dep.ID == sg.DependsOn {
			return dep.Status == SubgoalCompleted
		}
	}
	return false // dependency not found = not met
}

// ComputeOverallProgress computes the weighted average progress across subgoals.
// Weights by priority. Returns [0,1].
func ComputeOverallProgress(subgoals []Subgoal) float64 {
	if len(subgoals) == 0 {
		return 0
	}
	totalWeight := 0.0
	weightedProgress := 0.0
	for _, sg := range subgoals {
		w := sg.Priority
		if w <= 0 {
			w = 0.1 // minimum weight
		}
		totalWeight += w
		weightedProgress += w * sg.ProgressScore
	}
	if totalWeight == 0 {
		return 0
	}
	return clamp01(weightedProgress / totalWeight)
}

// ComputeTaskUrgency determines urgency for a task emission based on
// the subgoal's horizon, progress gap, and goal priority.
func ComputeTaskUrgency(sg Subgoal, now time.Time) float64 {
	horizonDays, ok := HorizonDays[sg.Horizon]
	if !ok {
		horizonDays = 90
	}

	// Time pressure: how much of the horizon has elapsed since creation.
	elapsed := now.Sub(sg.CreatedAt).Hours() / 24
	timePressure := clamp01(elapsed / float64(horizonDays))

	// Progress gap: how far from completion.
	progressGap := clamp01(1.0 - sg.ProgressScore)

	// Blend: time pressure + progress gap.
	return clamp01(timePressure*0.50 + progressGap*0.50)
}

// ComputeTaskPriority blends urgency, goal priority, and progress gap
// into a single emission priority score [0,1].
func ComputeTaskPriority(urgency, goalPriority, progressScore float64) float64 {
	progressGap := clamp01(1.0 - progressScore)
	return clamp01(
		urgency*WeightUrgency +
			goalPriority*WeightGoalPriority +
			progressGap*WeightProgressGap,
	)
}
