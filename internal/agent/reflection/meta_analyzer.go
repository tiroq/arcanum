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

	// Iteration 55A: Execution feedback thresholds.
	ExecRepeatedFailureThreshold = 2    // repeated_failure count ≥ this → inefficiency
	ExecFailureClusterThreshold  = 3    // execution_failure count ≥ this → risk flag
	ExecBlockedReviewThreshold   = 2    // blocked_by_review count ≥ this → friction improvement
	ExecSafeSuccessThreshold     = 3    // safe_action_succeeded count ≥ this for reinforcement
	ExecSafeSuccessLowFailRatio  = 0.25 // failure/total must be below this for reinforcement
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

	// Iteration 55A: Execution feedback rules.
	insights = ruleExecRepeatedFailure(data, insights)
	insights = ruleExecFailureCluster(data, insights)
	insights = ruleExecBlockedReview(data, insights)
	insights = ruleExecObjectiveAbort(data, insights)
	insights = ruleExecPositiveReinforcement(data, insights)
	insights = ruleExecGovernanceFriction(data, insights)

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

// --- Iteration 55A: Execution Feedback Rules ---

// Rule 6: REPEATED FAILURE — systemic inefficiency.
// If repeated_failure count ≥ 2 in window → emit inefficiency.
func ruleExecRepeatedFailure(data AggregatedData, insights ReflectionInsights) ReflectionInsights {
	ef := data.ExecutionFeedback
	if ef.RepeatedFailureCount >= ExecRepeatedFailureThreshold {
		severity := clampMeta(float64(ef.RepeatedFailureCount) / 5.0)
		insights.Inefficiencies = append(insights.Inefficiencies, Inefficiency{
			Type: "execution_repeated_failure",
			Description: fmt.Sprintf(
				"%d repeated execution failures detected — systemic problem likely",
				ef.RepeatedFailureCount,
			),
			Severity: severity,
		})
		insights.Signals = append(insights.Signals, ReflectionSignal{
			SignalType:  SignalExecutionInefficiency,
			Strength:    severity,
			ContextTags: []string{"execution", "repeated_failure"},
		})
	}
	return insights
}

// Rule 7: FAILURE CLUSTER — execution risk flag.
// If total execution failures ≥ 3 in window → emit risk.
func ruleExecFailureCluster(data AggregatedData, insights ReflectionInsights) ReflectionInsights {
	ef := data.ExecutionFeedback
	if ef.FailureCount >= ExecFailureClusterThreshold {
		severity := clampMeta(float64(ef.FailureCount) / 6.0)
		insights.RiskFlags = append(insights.RiskFlags, RiskFlag{
			Type: "execution_failure_cluster",
			Description: fmt.Sprintf(
				"%d execution failures in period — elevated execution risk",
				ef.FailureCount,
			),
			Severity: severity,
		})
		insights.Signals = append(insights.Signals, ReflectionSignal{
			SignalType:  SignalExecutionRisk,
			Strength:    severity,
			ContextTags: []string{"execution", "failure_cluster"},
		})
	}
	return insights
}

// Rule 8: BLOCKED BY REVIEW — workflow friction.
// If blocked_by_review ≥ 2 → emit improvement suggestion.
func ruleExecBlockedReview(data AggregatedData, insights ReflectionInsights) ReflectionInsights {
	ef := data.ExecutionFeedback
	if ef.BlockedByReviewCount >= ExecBlockedReviewThreshold {
		severity := clampMeta(float64(ef.BlockedByReviewCount) / 5.0)
		insights.Improvements = append(insights.Improvements, Improvement{
			Type: "workflow_friction",
			Description: fmt.Sprintf(
				"%d tasks blocked by review — approval bottleneck detected",
				ef.BlockedByReviewCount,
			),
		})
		insights.Signals = append(insights.Signals, ReflectionSignal{
			SignalType:  SignalWorkflowFriction,
			Strength:    severity,
			ContextTags: []string{"execution", "review_blocked"},
		})
	}
	return insights
}

// Rule 9: OBJECTIVE PENALTY ABORT — system instability.
// If any objective_penalty_abort occurred → emit instability warning.
func ruleExecObjectiveAbort(data AggregatedData, insights ReflectionInsights) ReflectionInsights {
	ef := data.ExecutionFeedback
	if ef.ObjectiveAbortCount >= 1 {
		severity := clampMeta(float64(ef.ObjectiveAbortCount) / 3.0)
		insights.RiskFlags = append(insights.RiskFlags, RiskFlag{
			Type: "system_instability",
			Description: fmt.Sprintf(
				"%d execution(s) aborted due to objective penalty — operating state may be unsafe",
				ef.ObjectiveAbortCount,
			),
			Severity: severity,
		})
		insights.Signals = append(insights.Signals, ReflectionSignal{
			SignalType:  SignalSystemInstability,
			Strength:    severity,
			ContextTags: []string{"execution", "objective_abort"},
		})
	}
	return insights
}

// Rule 10: POSITIVE REINFORCEMENT — safe success pattern.
// If safe_action_succeeded ≥ 3 and failure ratio is low → emit reinforcement.
func ruleExecPositiveReinforcement(data AggregatedData, insights ReflectionInsights) ReflectionInsights {
	ef := data.ExecutionFeedback
	if ef.SafeSuccessCount < ExecSafeSuccessThreshold {
		return insights
	}
	failRatio := 0.0
	if ef.TotalCount > 0 {
		failRatio = float64(ef.FailureCount) / float64(ef.TotalCount)
	}
	if failRatio > ExecSafeSuccessLowFailRatio {
		return insights
	}
	strength := clampMeta(float64(ef.SafeSuccessCount) / 6.0)
	insights.Improvements = append(insights.Improvements, Improvement{
		Type: "positive_reinforcement",
		Description: fmt.Sprintf(
			"%d safe actions succeeded with low failure rate (%.0f%%) — effective execution pattern",
			ef.SafeSuccessCount, failRatio*100,
		),
	})
	insights.Signals = append(insights.Signals, ReflectionSignal{
		SignalType:  SignalPositiveReinforcement,
		Strength:    strength,
		ContextTags: []string{"execution", "safe_success"},
	})
	return insights
}

// Rule 11: GOVERNANCE FRICTION — governance blocking execution.
// If blocked_by_governance ≥ 2 → emit governance friction signal.
func ruleExecGovernanceFriction(data AggregatedData, insights ReflectionInsights) ReflectionInsights {
	ef := data.ExecutionFeedback
	if ef.BlockedByGovernanceCount >= 2 {
		severity := clampMeta(float64(ef.BlockedByGovernanceCount) / 4.0)
		insights.Improvements = append(insights.Improvements, Improvement{
			Type: "governance_friction",
			Description: fmt.Sprintf(
				"%d tasks blocked by governance — intentional suppression or mode too restrictive",
				ef.BlockedByGovernanceCount,
			),
		})
		insights.Signals = append(insights.Signals, ReflectionSignal{
			SignalType:  SignalGovernanceFriction,
			Strength:    severity,
			ContextTags: []string{"execution", "governance_blocked"},
		})
	}
	return insights
}

func clampMeta(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
