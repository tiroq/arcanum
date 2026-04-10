package portfolio

import "math"

// --- Scoring ---

// ComputeROI returns revenue per hour; zero-safe.
func ComputeROI(totalRevenue, totalTimeSpent float64) float64 {
	if totalTimeSpent <= 0 {
		return 0
	}
	return totalRevenue / totalTimeSpent
}

// ComputeConversionRate returns won/total; zero-safe.
func ComputeConversionRate(wonCount, totalCount int) float64 {
	if totalCount <= 0 {
		return 0
	}
	return float64(wonCount) / float64(totalCount)
}

// ComputeAllocationScores computes raw allocation scores for a set of strategies.
// Returns a map of strategy ID → raw score ∈ [0, 1].
//
// Formula (per spec):
//
//	allocation_score =
//	  roi_component     * 0.35
//	+ stability_component * 0.25
//	+ speed_component     * 0.20
//	+ pressure_alignment  * 0.20
//
// Family-safe rules are integrated into pressureAlignment:
//   - High pressure → favour faster time-to-value, more stable strategies.
//   - Low capacity  → favour high ROI/hour, short cycle.
//   - Family threatened → penalise slow speculative strategies.
func ComputeAllocationScores(
	strategies []Strategy,
	performances map[string]StrategyPerformance,
	financialPressure float64,
	familyPriorityHigh bool,
) map[string]float64 {
	scores := make(map[string]float64, len(strategies))
	if len(strategies) == 0 {
		return scores
	}

	// Find max expected return for normalisation.
	maxReturn := 0.0
	for _, st := range strategies {
		if st.ExpectedReturnPerHr > maxReturn {
			maxReturn = st.ExpectedReturnPerHr
		}
	}
	if maxReturn <= 0 {
		maxReturn = 1.0 // prevent division by zero
	}

	for _, st := range strategies {
		// ROI component: normalised expected return + real ROI if available.
		roiNorm := st.ExpectedReturnPerHr / maxReturn
		if perf, ok := performances[st.ID]; ok && perf.TotalEstimatedHours >= MinSamplesForPerformance {
			realROI := ComputeROI(perf.TotalVerifiedRevenue, perf.TotalEstimatedHours)
			realROINorm := clamp01(realROI / maxReturn)
			// Blend: 60% real, 40% expected when we have data.
			roiNorm = 0.60*realROINorm + 0.40*roiNorm
		}

		// Stability component: direct stability_score ∈ [0, 1].
		stability := clamp01(st.StabilityScore)

		// Speed component: prefer short time-to-first-value.
		// Normalised: 1 - (ttfv / MaxTimeToFirstValue), clamped.
		speedNorm := clamp01(1.0 - st.TimeToFirstValue/MaxTimeToFirstValue)

		// Pressure alignment: high pressure → prefer high returns + speed + stability.
		// Under high pressure, boost strategies with high ROI and fast TTF.
		pressureAlignment := clamp01(financialPressure * (0.5*roiNorm + 0.3*speedNorm + 0.2*stability))

		// Family-safe penalty: penalise slow speculative strategies when family is threatened.
		if familyPriorityHigh {
			// Penalise high time_to_first_value (slow), low stability (volatile), low confidence.
			volatilityPenalty := clamp01(1.0 - st.StabilityScore)
			if st.TimeToFirstValue > MaxTimeToFirstValue*0.5 && volatilityPenalty > 0.5 {
				// Slow speculative: reduce ROI and speed components.
				roiNorm *= 0.7
				speedNorm *= 0.5
			}
		}

		score := ROIWeight*roiNorm +
			StabilityWeight*stability +
			SpeedWeight*speedNorm +
			PressureWeight*pressureAlignment

		scores[st.ID] = clamp01(score)
	}

	return scores
}

// NormaliseAllocations converts raw scores to hour allocations respecting
// min/max fraction constraints and total available hours.
// Returns both allocations (hours map) and weights (fraction map).
func NormaliseAllocations(
	rawScores map[string]float64,
	totalAvailableHours float64,
) (hours map[string]float64, weights map[string]float64) {
	n := len(rawScores)
	if n == 0 || totalAvailableHours <= 0 {
		return map[string]float64{}, map[string]float64{}
	}

	// Sum scores.
	total := 0.0
	for _, s := range rawScores {
		total += s
	}
	if total <= 0 {
		// Equal distribution.
		eq := totalAvailableHours / float64(n)
		w := 1.0 / float64(n)
		result := make(map[string]float64, n)
		wResult := make(map[string]float64, n)
		for id := range rawScores {
			result[id] = eq
			wResult[id] = w
		}
		return result, wResult
	}

	// Initial proportional allocation.
	allocations := make(map[string]float64, n)
	for id, s := range rawScores {
		allocations[id] = (s / total) * totalAvailableHours
	}

	// Enforce min/max fraction constraints (two passes).
	minHours := totalAvailableHours * MinAllocationFraction
	maxHours := totalAvailableHours * MaxAllocationFraction

	for pass := 0; pass < 2; pass++ {
		excess := 0.0
		uncapped := 0

		for id, hrs := range allocations {
			if hrs > maxHours {
				excess += hrs - maxHours
				allocations[id] = maxHours
			} else if hrs < minHours {
				allocations[id] = minHours
			} else {
				uncapped++
			}
		}

		// Redistribute excess to uncapped strategies.
		if excess > 0 && uncapped > 0 {
			share := excess / float64(uncapped)
			for id, hrs := range allocations {
				if hrs > minHours && hrs < maxHours {
					allocations[id] = math.Min(hrs+share, maxHours)
				}
			}
		}
	}

	// Compute weights from final allocations.
	totalAlloc := 0.0
	for _, h := range allocations {
		totalAlloc += h
	}
	wts := make(map[string]float64, n)
	for id, h := range allocations {
		if totalAlloc > 0 {
			wts[id] = h / totalAlloc
		}
	}

	return allocations, wts
}

// ComputeDiversificationIndex returns a Herfindahl-like index ∈ [0,1].
// 0 = perfectly concentrated (one strategy), 1 = perfectly diversified.
func ComputeDiversificationIndex(allocations map[string]float64) float64 {
	total := 0.0
	for _, hrs := range allocations {
		total += hrs
	}
	if total <= 0 || len(allocations) <= 1 {
		return 0
	}

	// Herfindahl index = sum(share^2). Normalised so 1/N gives maximum diversity.
	hhi := 0.0
	for _, hrs := range allocations {
		share := hrs / total
		hhi += share * share
	}

	// Normalise: 1 - ((HHI - 1/N) / (1 - 1/N)).
	n := float64(len(allocations))
	minHHI := 1.0 / n
	if hhi <= minHHI {
		return 1.0
	}
	return clamp01(1.0 - (hhi-minHHI)/(1.0-minHHI))
}

// --- Strategic Signals ---

// DetectSignals analyses strategy performance and allocation to produce strategic signals.
func DetectSignals(
	strategies []Strategy,
	performances map[string]StrategyPerformance,
	allocations map[string]float64,
	totalAvailableHours float64,
) []StrategicSignal {
	var signals []StrategicSignal

	for _, st := range strategies {
		perf, hasPerf := performances[st.ID]
		allocHrs := allocations[st.ID]

		// Underperforming: low real ROI despite allocation.
		if hasPerf && perf.TotalEstimatedHours >= MinSamplesForPerformance {
			roi := ComputeROI(perf.TotalVerifiedRevenue, perf.TotalEstimatedHours)
			if roi < LowROIThreshold && allocHrs > 0 {
				strength := clamp01(1.0 - roi/LowROIThreshold)
				signals = append(signals, StrategicSignal{
					StrategyID:   st.ID,
					StrategyType: st.Type,
					SignalType:   "underperforming",
					Score:        strength,
					Reason:       "ROI below threshold",
				})
			}
		}

		// Over-allocated: allocated fraction exceeds max.
		if totalAvailableHours > 0 && allocHrs > 0 {
			fraction := allocHrs / totalAvailableHours
			if fraction > MaxAllocationFraction {
				strength := clamp01((fraction - MaxAllocationFraction) / MaxAllocationFraction)
				signals = append(signals, StrategicSignal{
					StrategyID:   st.ID,
					StrategyType: st.Type,
					SignalType:   "over_allocated",
					Score:        strength,
					Reason:       "allocation exceeds concentration cap",
				})
			}
		}

		// High potential: high expected return, good stability, sufficient performance.
		if st.ExpectedReturnPerHr >= HighROIThreshold && st.StabilityScore >= (1.0-HighVolatilityThreshold) {
			strength := clamp01(st.ExpectedReturnPerHr / (HighROIThreshold * 2))
			if hasPerf && perf.TotalEstimatedHours >= MinSamplesForPerformance {
				realROI := ComputeROI(perf.TotalVerifiedRevenue, perf.TotalEstimatedHours)
				if realROI >= HighROIThreshold {
					strength = clamp01(strength + 0.2)
				}
			}
			signals = append(signals, StrategicSignal{
				StrategyID:   st.ID,
				StrategyType: st.Type,
				SignalType:   "high_potential",
				Score:        strength,
				Reason:       "high expected return with acceptable stability",
			})
		}
	}

	return signals
}

// --- Decision Graph Scoring ---

// ComputeStrategyBoost returns the boost or penalty for an action type
// based on the strategy it belongs to. Range: [-StrategyPenaltyMax, +StrategyPriorityBoostMax].
func ComputeStrategyBoost(
	strategyType string,
	strategies []Strategy,
	performances map[string]StrategyPerformance,
) float64 {
	// Find the best active strategy of this type.
	var best *Strategy
	for i := range strategies {
		if strategies[i].Type == strategyType && strategies[i].Status == StatusActive {
			if best == nil || strategies[i].ExpectedReturnPerHr > best.ExpectedReturnPerHr {
				best = &strategies[i]
			}
		}
	}
	if best == nil {
		return 0
	}

	// Check real performance.
	perf, hasPerf := performances[best.ID]
	if hasPerf && perf.TotalEstimatedHours >= MinSamplesForPerformance {
		roi := ComputeROI(perf.TotalVerifiedRevenue, perf.TotalEstimatedHours)
		if roi >= HighROIThreshold {
			return StrategyPriorityBoostMax * clamp01(roi/(HighROIThreshold*2))
		}
		if roi < LowROIThreshold {
			return -StrategyPenaltyMax * clamp01(1.0-roi/LowROIThreshold)
		}
	}

	// Fall back to expected return.
	if best.ExpectedReturnPerHr >= HighROIThreshold {
		return StrategyPriorityBoostMax * clamp01(best.ExpectedReturnPerHr/(HighROIThreshold*2))
	}

	return 0
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
