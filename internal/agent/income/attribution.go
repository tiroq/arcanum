package income

// ComputeAccuracy computes the accuracy of a real outcome vs the estimated value.
//
//	accuracy = actual_value / estimated_value
//
// Special cases:
//   - If estimated_value is 0 or negative: accuracy = 0 (no meaningful ratio).
//   - Capped at 2.0 to bound the influence of overperformance.
//   - A failed outcome always returns 0.
func ComputeAccuracy(estimatedValue, actualValue float64, outcomeStatus string) float64 {
	if outcomeStatus == OutcomeFailed {
		return 0
	}
	if estimatedValue <= 0 {
		return 0
	}
	accuracy := actualValue / estimatedValue
	if accuracy < 0 {
		return 0
	}
	if accuracy > 2.0 {
		return 2.0
	}
	return accuracy
}

// ComputeConfidenceAdjustment derives a bounded confidence adjustment from learning stats.
//
// Formula:
//
//	delta = (avg_accuracy - 1.0) * LearningWeight
//	adjustment = clamp(delta, -LearningMaxConfAdj, +LearningMaxConfAdj)
//
// Interpretation:
//   - avg_accuracy > 1.0 → system underestimates → positive adjustment
//   - avg_accuracy < 1.0 → system overestimates → negative adjustment
//   - avg_accuracy == 1.0 → perfectly calibrated → zero adjustment
//
// Returns 0 if total_outcomes < MinLearningOutcomes (cold-start guard).
func ComputeConfidenceAdjustment(lr LearningRecord) float64 {
	if lr.TotalOutcomes < MinLearningOutcomes {
		return 0
	}
	delta := (lr.AvgAccuracy - 1.0) * LearningWeight
	if delta > LearningMaxConfAdj {
		return LearningMaxConfAdj
	}
	if delta < -LearningMaxConfAdj {
		return -LearningMaxConfAdj
	}
	return delta
}

// ComputeOutcomeFeedback derives a bounded path score adjustment from learning stats.
// Used by the decision graph to boost or penalize income-related paths.
//
//	feedback = (success_rate - 0.5) * 2 * max_effect
//
// Where max_effect is OutcomeFeedbackMaxBoost for positive, OutcomeFeedbackMaxPenalty for negative.
// Returns 0 if total_outcomes < MinLearningOutcomes (cold-start guard).
func ComputeOutcomeFeedback(lr LearningRecord) float64 {
	if lr.TotalOutcomes < MinLearningOutcomes {
		return 0
	}
	// Normalise success_rate around 0.5: values > 0.5 → positive, < 0.5 → negative.
	raw := (lr.SuccessRate - 0.5) * 2.0
	if raw > 0 {
		return clamp01(raw) * OutcomeFeedbackMaxBoost
	}
	// raw is negative: penalty
	if raw < -1.0 {
		raw = -1.0
	}
	return raw * OutcomeFeedbackMaxPenalty
}

// BuildAttribution computes an attribution record linking an outcome to its opportunity.
func BuildAttribution(opp IncomeOpportunity, outcome IncomeOutcome) AttributionRecord {
	return AttributionRecord{
		OutcomeID:       outcome.ID,
		OpportunityID:   outcome.OpportunityID,
		ProposalID:      outcome.ProposalID,
		OpportunityType: opp.OpportunityType,
		EstimatedValue:  opp.EstimatedValue,
		ActualValue:     outcome.ActualValue,
		Accuracy:        ComputeAccuracy(opp.EstimatedValue, outcome.ActualValue, outcome.OutcomeStatus),
		OutcomeStatus:   outcome.OutcomeStatus,
	}
}

// UpdateLearningFromAttribution incrementally updates a learning record with a new outcome.
func UpdateLearningFromAttribution(existing LearningRecord, attr AttributionRecord) LearningRecord {
	existing.TotalOutcomes++
	existing.TotalAccuracy += attr.Accuracy
	if attr.OutcomeStatus == OutcomeSucceeded {
		existing.SuccessCount++
	}
	if existing.TotalOutcomes > 0 {
		existing.AvgAccuracy = existing.TotalAccuracy / float64(existing.TotalOutcomes)
		existing.SuccessRate = float64(existing.SuccessCount) / float64(existing.TotalOutcomes)
	}
	existing.ConfidenceAdjustment = ComputeConfidenceAdjustment(existing)
	return existing
}
