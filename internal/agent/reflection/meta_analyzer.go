package reflection

import "fmt"

// --- Iteration 49: Deterministic Meta-Analyzer ---

// Analysis thresholds — all constants, no randomness.
const (
	LowEfficiencyThreshold     = 15.0 // value_per_hour below this → inefficiency
	OverloadThreshold          = 0.7  // owner_load_score above this → risk
	PricingMisalignmentThresh  = 0.7  // avg_accuracy below this → pricing issue
	IncomeInstabilityMinWins   = 3    // fewer verified wins → instability risk
	AutomationMinRepeats       = 3    // repeated manual actions ≥ this → automation opportunity
	ReflectionSignalBoostMax   = 0.10 // max scoring boost from reflection signals
	ReflectionSignalBoostScale = 0.05 // per-signal strength multiplier
)

// MetaAnalyze runs deterministic analysis rules on aggregated data
// and returns structured insights and decision graph signals.
func MetaAnalyze(data AggregatedData) ReflectionInsights {
	insights := ReflectionInsights{}

	insights = ruleLowEfficiency(data, insights)
	insights = ruleOverload(data, insights)
	insights = rulePricingMisalignment(data, insights)
	insights = ruleIncomeInstability(data, insights)
	insights = ruleAutomationOpportunity(data, insights)

	return insights
}

// Rule 1: LOW EFFICIENCY
// If avg_value_per_hour < LowEfficiencyThreshold → inefficiency.
func ruleLowEfficiency(data AggregatedData, insights ReflectionInsights) ReflectionInsights {
	if data.ValuePerHour <= 0 {
		return insights
	}
	if data.ValuePerHour < LowEfficiencyThreshold {
		severity := 1.0 - (data.ValuePerHour / LowEfficiencyThreshold)
		if severity > 1.0 {
			severity = 1.0
		}
		insights.Inefficiencies = append(insights.Inefficiencies, Inefficiency{
			Type: "low_efficiency",
			Description: fmt.Sprintf(
				"Average value per hour ($%.2f) is below threshold ($%.2f)",
				data.ValuePerHour, LowEfficiencyThreshold,
			),
			Severity: severity,
		})
		insights.Signals = append(insights.Signals, ReflectionSignal{
			SignalType:  SignalLowEfficiency,
			Strength:    severity,
			ContextTags: []string{"efficiency", "value_per_hour"},
		})
	}
	return insights
}

// Rule 2: OVERLOAD
// If owner_load_score > OverloadThreshold → risk_flag.
func ruleOverload(data AggregatedData, insights ReflectionInsights) ReflectionInsights {
	if data.OwnerLoadScore > OverloadThreshold {
		severity := (data.OwnerLoadScore - OverloadThreshold) / (1.0 - OverloadThreshold)
		if severity > 1.0 {
			severity = 1.0
		}
		insights.RiskFlags = append(insights.RiskFlags, RiskFlag{
			Type: "overload_risk",
			Description: fmt.Sprintf(
				"Owner load score (%.2f) exceeds threshold (%.2f)",
				data.OwnerLoadScore, OverloadThreshold,
			),
			Severity: severity,
		})
		insights.Signals = append(insights.Signals, ReflectionSignal{
			SignalType:  SignalOverloadRisk,
			Strength:    severity,
			ContextTags: []string{"capacity", "overload"},
		})
	}
	return insights
}

// Rule 3: PRICING MISALIGNMENT
// If avg_accuracy < PricingMisalignmentThresh → pricing issue.
func rulePricingMisalignment(data AggregatedData, insights ReflectionInsights) ReflectionInsights {
	if data.AvgAccuracy <= 0 {
		return insights
	}
	if data.AvgAccuracy < PricingMisalignmentThresh {
		severity := 1.0 - (data.AvgAccuracy / PricingMisalignmentThresh)
		if severity > 1.0 {
			severity = 1.0
		}
		insights.Inefficiencies = append(insights.Inefficiencies, Inefficiency{
			Type: "pricing_misalignment",
			Description: fmt.Sprintf(
				"Average pricing accuracy (%.2f) is below threshold (%.2f)",
				data.AvgAccuracy, PricingMisalignmentThresh,
			),
			Severity: severity,
		})
		insights.Signals = append(insights.Signals, ReflectionSignal{
			SignalType:  SignalPricingMisalignment,
			Strength:    severity,
			ContextTags: []string{"pricing", "accuracy"},
		})
	}
	return insights
}

// Rule 4: INCOME INSTABILITY
// If verified income is low and few successful outcomes → risk.
func ruleIncomeInstability(data AggregatedData, insights ReflectionInsights) ReflectionInsights {
	successCount := 0
	if data.ActionsCount > 0 {
		successCount = int(float64(data.ActionsCount) * data.SuccessRate)
	}

	if successCount < IncomeInstabilityMinWins && data.ActionsCount > 0 {
		severity := 1.0 - (float64(successCount) / float64(IncomeInstabilityMinWins))
		if severity < 0 {
			severity = 0
		}
		if severity > 1.0 {
			severity = 1.0
		}
		insights.RiskFlags = append(insights.RiskFlags, RiskFlag{
			Type: "income_instability",
			Description: fmt.Sprintf(
				"Only %d successful outcomes out of %d total (below minimum %d)",
				successCount, data.ActionsCount, IncomeInstabilityMinWins,
			),
			Severity: severity,
		})
		insights.Signals = append(insights.Signals, ReflectionSignal{
			SignalType:  SignalIncomeInstability,
			Strength:    severity,
			ContextTags: []string{"income", "stability"},
		})
	}
	return insights
}

// Rule 5: AUTOMATION OPPORTUNITY
// If repeated manual actions ≥ AutomationMinRepeats → improvement.
func ruleAutomationOpportunity(data AggregatedData, insights ReflectionInsights) ReflectionInsights {
	for actionType, count := range data.ManualActionCounts {
		if count >= AutomationMinRepeats {
			severity := float64(count) / float64(AutomationMinRepeats*3) // caps at 1.0 for 9+ repeats
			if severity > 1.0 {
				severity = 1.0
			}
			insights.Improvements = append(insights.Improvements, Improvement{
				Type: "automation_opportunity",
				Description: fmt.Sprintf(
					"Action %q executed %d times — candidate for automation",
					actionType, count,
				),
				ActionType: actionType,
			})
			insights.Signals = append(insights.Signals, ReflectionSignal{
				SignalType:  SignalAutomationOpportunity,
				Strength:    severity,
				ContextTags: []string{"automation", actionType},
			})
		}
	}
	return insights
}
