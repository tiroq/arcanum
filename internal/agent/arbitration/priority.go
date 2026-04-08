package arbitration

// Priority returns the priority rank of a signal type.
// Lower number = higher priority.
// Stability=1 (highest), Exploration=7 (lowest).
func Priority(s SignalType) int {
	return int(s) + 1
}

// HigherPriority returns true if a has strictly higher priority than b.
func HigherPriority(a, b SignalType) bool {
	return Priority(a) < Priority(b)
}

// AllSignalTypes returns all signal types in priority order (highest first).
func AllSignalTypes() []SignalType {
	return []SignalType{
		SignalStability,
		SignalCalibration,
		SignalCausal,
		SignalComparative,
		SignalPathLearning,
		SignalTransitionLearning,
		SignalExploration,
	}
}

// IsLearningSignal returns true for signals suppressed by low calibration confidence.
// Rule 2: PathLearning, TransitionLearning, Comparative are suppressed when
// calibrated confidence < ConfidenceSuppressionThreshold.
func IsLearningSignal(s SignalType) bool {
	return s == SignalPathLearning || s == SignalTransitionLearning || s == SignalComparative
}

// IsExplorationBlockedBy returns true if the blocker signal type prevents
// exploration from overriding. Rule 5: Exploration never overrides Stability or Calibration.
func IsExplorationBlockedBy(blocker SignalType) bool {
	return blocker == SignalStability || blocker == SignalCalibration
}
