package objective

import "math"

// clamp01 clamps v to [0, 1].
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// --- Utility component scoring ---

// ComputeIncomeUtility produces a score ∈ [0,1] representing income progress.
// Blends verified income ratio and open opportunity quality.
func ComputeIncomeUtility(verifiedIncome, targetIncome, bestOppScore float64, openOppCount int) float64 {
	incomeRatio := 0.0
	if targetIncome > 0 {
		incomeRatio = clamp01(verifiedIncome / targetIncome)
	}
	oppSignal := 0.0
	if openOppCount > 0 {
		oppSignal = clamp01(bestOppScore)
	}
	// Blend: 70% real income progress, 30% future opportunity quality.
	return clamp01(incomeRatio*0.70 + oppSignal*0.30)
}

// ComputeFamilyUtility produces a score ∈ [0,1] representing family stability.
// Higher when pressure is low and family time is protected.
func ComputeFamilyUtility(pressureScore, blockedHours, minFamilyHours float64) float64 {
	// Low pressure = high family stability.
	pressureComponent := clamp01(1.0 - pressureScore)
	// Protected time ratio.
	timeComponent := 0.5 // neutral default
	if minFamilyHours > 0 {
		timeComponent = clamp01(blockedHours / minFamilyHours)
	}
	return clamp01(pressureComponent*0.60 + timeComponent*0.40)
}

// ComputeOwnerUtility produces a score ∈ [0,1] representing owner relief.
// Lower overload and higher available capacity = higher utility.
func ComputeOwnerUtility(ownerLoad, availableHoursToday, maxDailyHours float64) float64 {
	// Low load = high relief.
	loadComponent := clamp01(1.0 - ownerLoad)
	// Available capacity ratio.
	capacityComponent := 0.5 // neutral default
	if maxDailyHours > 0 {
		capacityComponent = clamp01(availableHoursToday / maxDailyHours)
	}
	return clamp01(loadComponent*0.60 + capacityComponent*0.40)
}

// ComputeExecutionUtility produces a score ∈ [0,1] representing execution readiness.
// Higher when there are few failures and plenty of capacity.
func ComputeExecutionUtility(failedActions, pendingActions, totalActions int, availableHoursToday, maxDailyHours float64) float64 {
	// Success ratio of completed (non-failed) actions.
	successComponent := 1.0 // no actions = fully ready
	if totalActions > 0 {
		successComponent = clamp01(1.0 - float64(failedActions)/float64(totalActions))
	}
	// Pending-to-total congestion (low pending = more ready).
	congestionComponent := 1.0
	if totalActions > 0 {
		congestionComponent = clamp01(1.0 - float64(pendingActions)/float64(totalActions))
	}
	// Capacity readiness.
	capacityReady := 0.5
	if maxDailyHours > 0 {
		capacityReady = clamp01(availableHoursToday / maxDailyHours)
	}
	return clamp01(successComponent*0.40 + congestionComponent*0.30 + capacityReady*0.30)
}

// ComputeStrategicUtility produces a score ∈ [0,1] representing strategy quality.
// Higher diversification and positive ROI = higher utility.
func ComputeStrategicUtility(diversificationIndex, portfolioROI float64, activeStrategies int) float64 {
	// Diversification [0,1] — already bounded.
	divComponent := clamp01(diversificationIndex)
	// ROI quality (normalised: $50/hr = 1.0).
	roiComponent := clamp01(portfolioROI / 50.0)
	// Strategy breadth: ≥3 active = full score.
	breadthComponent := clamp01(float64(activeStrategies) / 3.0)
	return clamp01(divComponent*0.40 + roiComponent*0.35 + breadthComponent*0.25)
}

// --- Utility aggregation ---

// ComputeUtilityScore aggregates individual utility components into a single score.
func ComputeUtilityScore(income, family, owner, execution, strategic float64) float64 {
	return clamp01(
		clamp01(income)*WeightIncome +
			clamp01(family)*WeightFamily +
			clamp01(owner)*WeightOwner +
			clamp01(execution)*WeightExecution +
			clamp01(strategic)*WeightStrategic,
	)
}

// --- Risk component scoring ---

// ComputeFinancialRisk produces a score ∈ [0,1] representing financial instability.
// Driven by pressure, low verified income, and low buffer.
func ComputeFinancialRisk(pressureScore, verifiedIncome, targetIncome float64) float64 {
	// Direct pressure signal.
	pressureComponent := clamp01(pressureScore)
	// Income gap risk.
	incomeGapComponent := 0.0
	if targetIncome > 0 {
		incomeGapComponent = clamp01(1.0 - verifiedIncome/targetIncome)
	}
	return clamp01(pressureComponent*0.60 + incomeGapComponent*0.40)
}

// ComputeOverloadRisk produces a score ∈ [0,1] representing owner overload.
func ComputeOverloadRisk(ownerLoad, availableHoursToday, maxDailyHours float64) float64 {
	loadComponent := clamp01(ownerLoad)
	capacityExhaustion := 0.0
	if maxDailyHours > 0 {
		capacityExhaustion = clamp01(1.0 - availableHoursToday/maxDailyHours)
	}
	return clamp01(loadComponent*0.60 + capacityExhaustion*0.40)
}

// ComputeExecutionRisk produces a score ∈ [0,1] representing execution fragility.
func ComputeExecutionRisk(failedActions, totalActions int) float64 {
	if totalActions == 0 {
		return 0
	}
	return clamp01(float64(failedActions) / float64(totalActions))
}

// ComputeConcentrationRisk produces a score ∈ [0,1] representing strategy concentration.
// Uses dominant allocation share and inverse diversification.
func ComputeConcentrationRisk(dominantAllocation, diversificationIndex float64, activeStrategies int) float64 {
	// High dominant allocation = high concentration.
	dominantComponent := clamp01(dominantAllocation)
	// Low diversification = high concentration risk.
	divComponent := clamp01(1.0 - diversificationIndex)
	// Single strategy = max breadth risk; ≥3 = zero breadth risk.
	breadthRisk := 0.0
	if activeStrategies > 0 {
		breadthRisk = clamp01(1.0 - float64(activeStrategies-1)/2.0)
	}
	return clamp01(dominantComponent*0.40 + divComponent*0.35 + breadthRisk*0.25)
}

// ComputePricingRisk produces a score ∈ [0,1] representing pricing confidence weakness.
func ComputePricingRisk(pricingConfidence, winRate float64) float64 {
	// Low confidence = high risk.
	confComponent := clamp01(1.0 - pricingConfidence)
	// Low win rate = high pricing risk.
	winComponent := clamp01(1.0 - winRate)
	return clamp01(confComponent*0.60 + winComponent*0.40)
}

// --- Risk aggregation ---

// ComputeRiskScore aggregates individual risk components into a single score.
func ComputeRiskScore(financial, overload, execution, concentration, pricing float64) float64 {
	return clamp01(
		clamp01(financial)*WeightFinancialRisk +
			clamp01(overload)*WeightOverloadRisk +
			clamp01(execution)*WeightExecutionRisk +
			clamp01(concentration)*WeightConcentrationRisk +
			clamp01(pricing)*WeightPricingRisk,
	)
}

// --- Net utility ---

// ComputeNetUtility merges utility and risk into a single bounded score.
func ComputeNetUtility(utility, risk float64) float64 {
	return clamp01(utility - risk*RiskPenaltyWeight)
}

// --- Dominant factor identification ---

// DominantPositiveFactor returns the name of the highest-weight utility component.
func DominantPositiveFactor(income, family, owner, execution, strategic float64) string {
	factors := map[string]float64{
		"income":    income * WeightIncome,
		"family":    family * WeightFamily,
		"owner":     owner * WeightOwner,
		"execution": execution * WeightExecution,
		"strategic": strategic * WeightStrategic,
	}
	return dominant(factors)
}

// DominantRiskFactor returns the name of the highest-weight risk component.
func DominantRiskFactor(financial, overload, execution, concentration, pricing float64) string {
	factors := map[string]float64{
		"financial":     financial * WeightFinancialRisk,
		"overload":      overload * WeightOverloadRisk,
		"execution":     execution * WeightExecutionRisk,
		"concentration": concentration * WeightConcentrationRisk,
		"pricing":       pricing * WeightPricingRisk,
	}
	return dominant(factors)
}

func dominant(factors map[string]float64) string {
	best := ""
	bestVal := -math.MaxFloat64
	for k, v := range factors {
		if v > bestVal || (v == bestVal && k < best) {
			bestVal = v
			best = k
		}
	}
	return best
}

// --- Objective signal for planner ---

// ComputeObjectiveSignal translates net utility into a bounded planner signal.
func ComputeObjectiveSignal(netUtility float64, dominantPos, dominantRisk string) ObjectiveSignal {
	delta := netUtility - NeutralNetUtility
	if delta >= 0 {
		strength := clamp01(delta / (1.0 - NeutralNetUtility))
		boost := strength * ObjectiveBoostMax
		return ObjectiveSignal{
			SignalType:  "objective_boost",
			Strength:    boost,
			Explanation: "net utility above neutral; dominant positive: " + dominantPos,
			ContextTags: []string{"utility", dominantPos},
		}
	}
	strength := clamp01(-delta / NeutralNetUtility)
	penalty := -strength * ObjectivePenaltyMax
	return ObjectiveSignal{
		SignalType:  "objective_penalty",
		Strength:    penalty,
		Explanation: "net utility below neutral; dominant risk: " + dominantRisk,
		ContextTags: []string{"risk", dominantRisk},
	}
}

// --- Full pipeline from inputs ---

// ComputeFromInputs runs the full scoring pipeline from raw inputs.
func ComputeFromInputs(inputs ObjectiveInputs) (ObjectiveState, RiskState, ObjectiveSummary) {
	// Utility components.
	incomeUtil := ComputeIncomeUtility(inputs.VerifiedMonthlyIncome, inputs.TargetMonthlyIncome, inputs.BestOpenOppScore, inputs.OpenOpportunityCount)
	familyUtil := ComputeFamilyUtility(inputs.PressureScore, inputs.BlockedHoursToday, inputs.MinFamilyTimeHours)
	ownerUtil := ComputeOwnerUtility(inputs.OwnerLoadScore, inputs.AvailableHoursToday, inputs.MaxDailyWorkHours)
	executionUtil := ComputeExecutionUtility(inputs.FailedActionCount, inputs.PendingActionCount, inputs.TotalActionCount, inputs.AvailableHoursToday, inputs.MaxDailyWorkHours)
	strategicUtil := ComputeStrategicUtility(inputs.DiversificationIndex, inputs.PortfolioROI, inputs.ActiveStrategies)

	utility := ComputeUtilityScore(incomeUtil, familyUtil, ownerUtil, executionUtil, strategicUtil)

	// Risk components.
	financialRisk := ComputeFinancialRisk(inputs.PressureScore, inputs.VerifiedMonthlyIncome, inputs.TargetMonthlyIncome)
	overloadRisk := ComputeOverloadRisk(inputs.OwnerLoadScore, inputs.AvailableHoursToday, inputs.MaxDailyWorkHours)
	executionRisk := ComputeExecutionRisk(inputs.FailedActionCount, inputs.TotalActionCount)
	concentrationRisk := ComputeConcentrationRisk(inputs.DominantAllocation, inputs.DiversificationIndex, inputs.ActiveStrategies)
	pricingRisk := ComputePricingRisk(inputs.PricingConfidence, inputs.WinRate)

	risk := ComputeRiskScore(financialRisk, overloadRisk, executionRisk, concentrationRisk, pricingRisk)

	netUtil := ComputeNetUtility(utility, risk)
	domPos := DominantPositiveFactor(incomeUtil, familyUtil, ownerUtil, executionUtil, strategicUtil)
	domRisk := DominantRiskFactor(financialRisk, overloadRisk, executionRisk, concentrationRisk, pricingRisk)

	objState := ObjectiveState{
		VerifiedIncomeScore:     incomeUtil,
		IncomeGrowthScore:       clamp01(inputs.BestOpenOppScore),
		OwnerReliefScore:        ownerUtil,
		FamilyStabilityScore:    familyUtil,
		StrategyQualityScore:    strategicUtil,
		ExecutionReadinessScore: executionUtil,
	}

	riskState := RiskState{
		FinancialInstabilityRisk:  financialRisk,
		OverloadRisk:              overloadRisk,
		ExecutionRisk:             executionRisk,
		StrategyConcentrationRisk: concentrationRisk,
		PricingConfidenceRisk:     pricingRisk,
	}

	summary := ObjectiveSummary{
		UtilityScore:           utility,
		RiskScore:              risk,
		NetUtility:             netUtil,
		DominantPositiveFactor: domPos,
		DominantRiskFactor:     domRisk,
	}

	return objState, riskState, summary
}
