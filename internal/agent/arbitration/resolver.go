package arbitration

import "sort"

// ResolveSignals applies deterministic arbitration rules to a set of signals
// and produces a final score adjustment with a full trace.
//
// Rules applied in order:
//  1. Hard Override — higher priority contradicting signal suppresses lower
//  2. Confidence Suppression — low calibrated confidence suppresses learning signals
//  3. Reinforcement — agreeing signals sum their contributions
//  4. Conflict Neutralization — unresolvable conflicts → neutral baseline
//  5. Exploration Isolation — exploration never overrides stability/calibration
//
// Fail-open: if signals is nil or empty, returns zero adjustment.
// Deterministic: same input always produces the same output.
func ResolveSignals(pathSignature string, signals []Signal, calibratedConfidence float64) ArbitrationResult {
	trace := ArbitrationTrace{
		PathSignature: pathSignature,
		RawSignals:    make([]Signal, len(signals)),
	}
	copy(trace.RawSignals, signals)

	result := ArbitrationResult{
		FinalAdjustment: 0,
		Trace:           trace,
	}

	if len(signals) == 0 {
		result.Trace.Reason = "no_signals"
		return result
	}

	// Sort signals by priority (highest first) for deterministic processing.
	sorted := make([]Signal, len(signals))
	copy(sorted, signals)
	sortSignals(sorted)

	// Phase 1: Apply Rule 5 — Exploration Isolation.
	// Exploration cannot override Stability or Calibration.
	active, suppressed := applyExplorationIsolation(sorted)
	result.Trace.SuppressedSignals = append(result.Trace.SuppressedSignals, suppressed...)
	if len(suppressed) > 0 {
		result.Trace.RulesApplied = append(result.Trace.RulesApplied, "exploration_isolation")
	}

	// Phase 2: Apply Rule 2 — Confidence Suppression.
	// Suppress learning signals when calibrated confidence is below threshold.
	active, suppressed = applyConfidenceSuppression(active, calibratedConfidence)
	result.Trace.SuppressedSignals = append(result.Trace.SuppressedSignals, suppressed...)
	if len(suppressed) > 0 {
		result.Trace.RulesApplied = append(result.Trace.RulesApplied, "confidence_suppression")
	}

	// Phase 3: Apply Rule 1 — Hard Override.
	// Higher priority signal suppresses contradicting lower priority signals.
	active, suppressed = applyHardOverride(active)
	result.Trace.SuppressedSignals = append(result.Trace.SuppressedSignals, suppressed...)
	if len(suppressed) > 0 {
		result.Trace.RulesApplied = append(result.Trace.RulesApplied, "hard_override")
	}

	if len(active) == 0 {
		result.Trace.Reason = "all_signals_suppressed"
		return result
	}

	// Phase 4: Check for conflicts among remaining signals.
	if hasConflict(active) {
		// Rule 4 — Conflict Neutralization.
		// Multiple conflicting signals with no dominant priority → pull toward neutral.
		result.Trace.RulesApplied = append(result.Trace.RulesApplied, "conflict_neutralization")
		result.FinalAdjustment = computeNeutralization(active)
		for _, s := range active {
			result.Trace.AppliedSignals = append(result.Trace.AppliedSignals, AppliedSignal{
				Signal:     s,
				Adjustment: s.Adjustment,
			})
		}
		result.Trace.FinalAdjustment = result.FinalAdjustment
		result.Trace.Reason = "conflict_neutralized"
		return result
	}

	// Phase 5: Rule 3 — Reinforcement.
	// All remaining signals agree → sum contributions.
	result.Trace.RulesApplied = append(result.Trace.RulesApplied, "reinforcement")
	totalAdj := 0.0
	var winner *Signal
	for i, s := range active {
		totalAdj += s.Adjustment
		result.Trace.AppliedSignals = append(result.Trace.AppliedSignals, AppliedSignal{
			Signal:     s,
			Adjustment: s.Adjustment,
		})
		if winner == nil || HigherPriority(s.Type, winner.Type) {
			sig := active[i]
			winner = &sig
		}
	}

	result.FinalAdjustment = totalAdj
	result.Trace.FinalAdjustment = totalAdj
	result.Trace.WinningSignal = winner

	if len(active) == 1 {
		result.Trace.Reason = "single_signal"
	} else {
		result.Trace.Reason = "signals_reinforced"
	}

	return result
}

// sortSignals sorts signals by priority (highest = lowest SignalType value first),
// then by Recommendation descending (avoid before prefer) for deterministic output.
func sortSignals(signals []Signal) {
	sort.SliceStable(signals, func(i, j int) bool {
		if signals[i].Type != signals[j].Type {
			return signals[i].Type < signals[j].Type
		}
		return signals[i].Recommendation > signals[j].Recommendation
	})
}

// applyExplorationIsolation implements Rule 5:
// If Stability or Calibration signals exist with a directional recommendation (prefer/avoid),
// any Exploration signal that contradicts them is suppressed.
func applyExplorationIsolation(signals []Signal) (active []Signal, suppressed []SuppressedSignal) {
	// Find if there are blocking signals (Stability or Calibration) with directional intent.
	hasBlocker := false
	for _, s := range signals {
		if IsExplorationBlockedBy(s.Type) && s.Recommendation != RecommendNeutral {
			hasBlocker = true
			break
		}
	}

	if !hasBlocker {
		return signals, nil
	}

	for _, s := range signals {
		if s.Type == SignalExploration {
			suppressed = append(suppressed, SuppressedSignal{
				Signal: s,
				Rule:   "exploration_isolation",
				Reason: "exploration cannot override stability/calibration",
			})
		} else {
			active = append(active, s)
		}
	}

	if len(suppressed) == 0 {
		return signals, nil
	}
	return active, suppressed
}

// applyConfidenceSuppression implements Rule 2:
// If calibrated confidence < threshold, suppress learning signals.
func applyConfidenceSuppression(signals []Signal, calibratedConfidence float64) (active []Signal, suppressed []SuppressedSignal) {
	if calibratedConfidence >= ConfidenceSuppressionThreshold {
		return signals, nil
	}

	for _, s := range signals {
		if IsLearningSignal(s.Type) {
			suppressed = append(suppressed, SuppressedSignal{
				Signal: s,
				Rule:   "confidence_suppression",
				Reason: "calibrated confidence below threshold",
			})
		} else {
			active = append(active, s)
		}
	}

	if len(suppressed) == 0 {
		return signals, nil
	}
	return active, suppressed
}

// applyHardOverride implements Rule 1:
// For each signal, if a higher-priority signal has an opposite recommendation,
// the lower-priority signal is suppressed.
func applyHardOverride(signals []Signal) (active []Signal, suppressed []SuppressedSignal) {
	if len(signals) <= 1 {
		return signals, nil
	}

	// Signals are already sorted by priority (highest first).
	// Track the highest-priority directional recommendation seen so far.
	type seen struct {
		sType SignalType
		rec   Recommendation
	}
	var dominants []seen

	for _, s := range signals {
		if s.Recommendation != RecommendNeutral {
			dominants = append(dominants, seen{sType: s.Type, rec: s.Recommendation})
		}
	}

	for _, s := range signals {
		overridden := false
		if s.Recommendation != RecommendNeutral {
			for _, d := range dominants {
				if HigherPriority(d.sType, s.Type) && contradicts(d.rec, s.Recommendation) {
					suppressed = append(suppressed, SuppressedSignal{
						Signal: s,
						Rule:   "hard_override",
						Reason: d.sType.String() + " overrides " + s.Type.String(),
					})
					overridden = true
					break
				}
			}
		}
		if !overridden {
			active = append(active, s)
		}
	}

	return active, suppressed
}

// contradicts returns true if two recommendations point in opposite directions.
func contradicts(a, b Recommendation) bool {
	return (a == RecommendPrefer && b == RecommendAvoid) ||
		(a == RecommendAvoid && b == RecommendPrefer)
}

// hasConflict returns true if the active signals contain both prefer and avoid recommendations.
func hasConflict(signals []Signal) bool {
	hasPrefer := false
	hasAvoid := false
	for _, s := range signals {
		if s.Recommendation == RecommendPrefer {
			hasPrefer = true
		}
		if s.Recommendation == RecommendAvoid {
			hasAvoid = true
		}
	}
	return hasPrefer && hasAvoid
}

// computeNeutralization computes a neutralized adjustment when signals conflict (Rule 4).
// Pulls the total adjustment toward 0 (neutral delta), scaled by NeutralizationStrength.
func computeNeutralization(signals []Signal) float64 {
	if len(signals) == 0 {
		return 0
	}
	total := 0.0
	for _, s := range signals {
		total += s.Adjustment
	}
	// Pull toward zero by NeutralizationStrength.
	return total * (1 - NeutralizationStrength)
}
