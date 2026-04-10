package signals

import "math"

// ComputeDerivedState computes all derived state metrics from a set of signals.
// Deterministic: same input always produces same output.
// Returns a map of derived state key → value.
func ComputeDerivedState(signals []Signal) map[string]float64 {
	derived := map[string]float64{
		DerivedFailureRate:       0,
		DerivedDeadLetterRate:    0,
		DerivedOwnerLoadScore:    0,
		DerivedIncomePressure:    0,
		DerivedInfraCostPressure: 0,
	}

	if len(signals) == 0 {
		return derived
	}

	var failCount, deadLetterCount float64
	var pendingCount, overdueCount float64
	var cogLoadSum float64
	var cogLoadN int
	var incomeGapSum, costSpikeSum float64
	var incomeN, costN int
	var totalCount float64

	for _, s := range signals {
		totalCount++
		switch s.SignalType {
		case SignalFailedJobs:
			failCount += s.Value
		case SignalDeadLetterSpike:
			deadLetterCount += s.Value
		case SignalPendingTasks:
			pendingCount += s.Value
		case SignalOverdueTasks:
			overdueCount += s.Value
		case SignalHighCognitiveLoad:
			cogLoadSum += s.Value
			cogLoadN++
		case SignalIncomeGap:
			incomeGapSum += s.Value
			incomeN++
		case SignalCostSpike:
			costSpikeSum += s.Value
			costN++
		}
	}

	// failure_rate: ratio of failure signals to total, scaled by value
	if totalCount > 0 {
		derived[DerivedFailureRate] = clamp01(failCount / (totalCount * 10))
	}

	// dead_letter_rate: ratio of dead letter signals to total, scaled by value
	if totalCount > 0 {
		derived[DerivedDeadLetterRate] = clamp01(deadLetterCount / (totalCount * 10))
	}

	// owner_load_score: composite of cognitive load + pending + overdue
	// Each component normalised to [0, 1] and averaged.
	var loadComponents int
	var loadSum float64

	if cogLoadN > 0 {
		loadSum += clamp01(cogLoadSum / float64(cogLoadN))
		loadComponents++
	}
	if pendingCount > 0 {
		loadSum += clamp01(pendingCount / 100) // 100 pending → max load
		loadComponents++
	}
	if overdueCount > 0 {
		loadSum += clamp01(overdueCount / 20) // 20 overdue → max load
		loadComponents++
	}
	if loadComponents > 0 {
		derived[DerivedOwnerLoadScore] = clamp01(loadSum / float64(loadComponents))
	}

	// income_pressure: average income gap normalised against a $5000 baseline
	if incomeN > 0 {
		derived[DerivedIncomePressure] = clamp01((incomeGapSum / float64(incomeN)) / 5000)
	}

	// infra_cost_pressure: average cost spike normalised against a $500 baseline
	if costN > 0 {
		derived[DerivedInfraCostPressure] = clamp01((costSpikeSum / float64(costN)) / 500)
	}

	return derived
}

// clamp01 clamps v to [0, 1].
func clamp01(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
