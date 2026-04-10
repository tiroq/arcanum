package income

// ScoreOpportunity computes a deterministic income score in [0, 1] for an opportunity.
//
// Formula:
//
//	value_score = clamp(estimated_value / MaxOpValue, 0, 1)
//	score       = value_score * WeightValue
//	            + confidence  * WeightConf
//	            - estimated_effort * WeightEffort
//
// All inputs are expected to be valid (validated by the engine before calling).
func ScoreOpportunity(o IncomeOpportunity) float64 {
	valueScore := o.EstimatedValue / MaxOpValue
	if valueScore > 1.0 {
		valueScore = 1.0
	} else if valueScore < 0 {
		valueScore = 0
	}

	score := valueScore*WeightValue +
		o.Confidence*WeightConf -
		o.EstimatedEffort*WeightEffort

	return clamp01(score)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
