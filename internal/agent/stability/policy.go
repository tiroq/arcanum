package stability

// ApplyPolicy maps detection findings to concrete stability state changes.
// It is deterministic: same findings + current state = same new state.
// Returns the new state and a human-readable reason summary.
func ApplyPolicy(current *State, result DetectionResult) (*State, string) {
	next := &State{
		ID:                 current.ID,
		Mode:               current.Mode,
		ThrottleMultiplier: current.ThrottleMultiplier,
		BlockedActionTypes: copyStrings(current.BlockedActionTypes),
		Reason:             current.Reason,
		UpdatedAt:          current.UpdatedAt,
	}

	// Process findings in priority order: recovery first (can override),
	// then instability (highest severity), then others.
	hasRecovery := false
	hasInstability := false

	for _, f := range result.Findings {
		switch f.Finding {
		case FindingStabilityRecovered:
			hasRecovery = true
		case FindingCycleInstability:
			hasInstability = true
		}
	}

	// Recovery takes precedence when no instability is also present.
	if hasRecovery && !hasInstability {
		next.Mode = ModeNormal
		next.ThrottleMultiplier = 1.0
		next.BlockedActionTypes = []string{}
		next.Reason = "stability_recovered: system returned to healthy pattern"
		return next, "stability_recovered"
	}

	// Instability → safe_mode.
	if hasInstability {
		next.Mode = ModeSafeMode
		next.ThrottleMultiplier = 3.0
		next.Reason = "cycle_instability_detected: repeated cycle failures"
		return next, "cycle_instability_detected"
	}

	// Process remaining findings.
	for _, f := range result.Findings {
		switch f.Finding {
		case FindingNoopLoop:
			if next.Mode == ModeNormal {
				next.Mode = ModeThrottled
			}
			if next.ThrottleMultiplier < 2.0 {
				next.ThrottleMultiplier = 2.0
			}
			next.Reason = "noop_loop_detected: excessive noop selections"

		case FindingLowValueLoop:
			if f.ActionType != "" && !next.IsActionBlocked(f.ActionType) {
				next.BlockedActionTypes = append(next.BlockedActionTypes, f.ActionType)
			}
			if next.Mode == ModeNormal {
				next.Mode = ModeThrottled
			}
			next.Reason = "low_value_loop_detected: blocking " + f.ActionType

		case FindingRetryAmplification:
			if !next.IsActionBlocked("retry_job") {
				next.BlockedActionTypes = append(next.BlockedActionTypes, "retry_job")
			}
			if next.Mode == ModeNormal {
				next.Mode = ModeThrottled
			}
			next.Reason = "retry_amplification_detected: blocking retry_job"
		}
	}

	return next, next.Reason
}

func copyStrings(src []string) []string {
	if src == nil {
		return []string{}
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}
