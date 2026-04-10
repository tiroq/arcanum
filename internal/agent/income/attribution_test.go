package income

import (
	"context"
	"testing"
)

// --- Accuracy computation tests ---

func TestComputeAccuracy_SucceededExact(t *testing.T) {
	acc := ComputeAccuracy(1000, 1000, OutcomeSucceeded)
	if acc != 1.0 {
		t.Errorf("expected 1.0 for exact match, got %f", acc)
	}
}

func TestComputeAccuracy_SucceededOverperformance(t *testing.T) {
	acc := ComputeAccuracy(1000, 1500, OutcomeSucceeded)
	if acc != 1.5 {
		t.Errorf("expected 1.5, got %f", acc)
	}
}

func TestComputeAccuracy_SucceededCappedAt2(t *testing.T) {
	acc := ComputeAccuracy(1000, 5000, OutcomeSucceeded)
	if acc != 2.0 {
		t.Errorf("expected cap at 2.0, got %f", acc)
	}
}

func TestComputeAccuracy_SucceededPartial(t *testing.T) {
	acc := ComputeAccuracy(1000, 500, OutcomeSucceeded)
	if acc < 0.49 || acc > 0.51 {
		t.Errorf("expected ~0.5, got %f", acc)
	}
}

func TestComputeAccuracy_FailedAlwaysZero(t *testing.T) {
	acc := ComputeAccuracy(1000, 1000, OutcomeFailed)
	if acc != 0 {
		t.Errorf("expected 0 for failed outcome, got %f", acc)
	}
}

func TestComputeAccuracy_PartialStatus(t *testing.T) {
	acc := ComputeAccuracy(1000, 300, OutcomePartial)
	if acc < 0.29 || acc > 0.31 {
		t.Errorf("expected ~0.3, got %f", acc)
	}
}

func TestComputeAccuracy_ZeroEstimate(t *testing.T) {
	acc := ComputeAccuracy(0, 500, OutcomeSucceeded)
	if acc != 0 {
		t.Errorf("expected 0 when estimated_value is 0, got %f", acc)
	}
}

func TestComputeAccuracy_NegativeEstimate(t *testing.T) {
	acc := ComputeAccuracy(-100, 500, OutcomeSucceeded)
	if acc != 0 {
		t.Errorf("expected 0 when estimated_value is negative, got %f", acc)
	}
}

func TestComputeAccuracy_ZeroActual(t *testing.T) {
	acc := ComputeAccuracy(1000, 0, OutcomeSucceeded)
	if acc != 0 {
		t.Errorf("expected 0 when actual_value is 0, got %f", acc)
	}
}

func TestComputeAccuracy_NegativeActualClamped(t *testing.T) {
	acc := ComputeAccuracy(1000, -500, OutcomeSucceeded)
	if acc != 0 {
		t.Errorf("expected 0 when actual_value is negative, got %f", acc)
	}
}

// --- Confidence adjustment tests ---

func TestComputeConfidenceAdjustment_ColdStart(t *testing.T) {
	lr := LearningRecord{TotalOutcomes: 2, AvgAccuracy: 0.5}
	adj := ComputeConfidenceAdjustment(lr)
	if adj != 0 {
		t.Errorf("expected 0 for cold start (< MinLearningOutcomes), got %f", adj)
	}
}

func TestComputeConfidenceAdjustment_Perfect(t *testing.T) {
	lr := LearningRecord{TotalOutcomes: 10, AvgAccuracy: 1.0}
	adj := ComputeConfidenceAdjustment(lr)
	if adj != 0 {
		t.Errorf("expected 0 for perfect accuracy, got %f", adj)
	}
}

func TestComputeConfidenceAdjustment_Overestimate(t *testing.T) {
	// avg_accuracy 0.5 means system overestimates
	lr := LearningRecord{TotalOutcomes: 10, AvgAccuracy: 0.5}
	adj := ComputeConfidenceAdjustment(lr)
	// delta = (0.5 - 1.0) * 0.30 = -0.15 → clamped to -0.10
	if adj != -LearningMaxConfAdj {
		t.Errorf("expected -%.2f (clamped), got %f", LearningMaxConfAdj, adj)
	}
}

func TestComputeConfidenceAdjustment_Underestimate(t *testing.T) {
	// avg_accuracy 1.3 means system underestimates
	lr := LearningRecord{TotalOutcomes: 10, AvgAccuracy: 1.3}
	adj := ComputeConfidenceAdjustment(lr)
	// delta = (1.3 - 1.0) * 0.30 = 0.09
	if adj < 0.08 || adj > 0.10 {
		t.Errorf("expected ~0.09, got %f", adj)
	}
}

func TestComputeConfidenceAdjustment_ClampPositive(t *testing.T) {
	lr := LearningRecord{TotalOutcomes: 10, AvgAccuracy: 2.0}
	adj := ComputeConfidenceAdjustment(lr)
	// delta = (2.0 - 1.0) * 0.30 = 0.30 → clamped to 0.10
	if adj != LearningMaxConfAdj {
		t.Errorf("expected +%.2f (clamped), got %f", LearningMaxConfAdj, adj)
	}
}

// --- Outcome feedback tests ---

func TestComputeOutcomeFeedback_ColdStart(t *testing.T) {
	lr := LearningRecord{TotalOutcomes: 2, SuccessRate: 0.9}
	fb := ComputeOutcomeFeedback(lr)
	if fb != 0 {
		t.Errorf("expected 0 for cold start, got %f", fb)
	}
}

func TestComputeOutcomeFeedback_HighSuccessRate(t *testing.T) {
	lr := LearningRecord{TotalOutcomes: 10, SuccessRate: 0.9}
	fb := ComputeOutcomeFeedback(lr)
	// raw = (0.9 - 0.5) * 2 = 0.8 → feedback = 0.8 * 0.10 = 0.08
	if fb < 0.07 || fb > 0.09 {
		t.Errorf("expected ~0.08, got %f", fb)
	}
}

func TestComputeOutcomeFeedback_LowSuccessRate(t *testing.T) {
	lr := LearningRecord{TotalOutcomes: 10, SuccessRate: 0.1}
	fb := ComputeOutcomeFeedback(lr)
	// raw = (0.1 - 0.5) * 2 = -0.8 → feedback = -0.8 * 0.10 = -0.08
	if fb > -0.07 || fb < -0.09 {
		t.Errorf("expected ~-0.08, got %f", fb)
	}
}

func TestComputeOutcomeFeedback_Neutral(t *testing.T) {
	lr := LearningRecord{TotalOutcomes: 10, SuccessRate: 0.5}
	fb := ComputeOutcomeFeedback(lr)
	if fb != 0 {
		t.Errorf("expected 0 for neutral success rate, got %f", fb)
	}
}

func TestComputeOutcomeFeedback_MaxBoost(t *testing.T) {
	lr := LearningRecord{TotalOutcomes: 10, SuccessRate: 1.0}
	fb := ComputeOutcomeFeedback(lr)
	// raw = (1.0 - 0.5) * 2 = 1.0 → feedback = clamp01(1.0) * 0.10 = 0.10
	if fb != OutcomeFeedbackMaxBoost {
		t.Errorf("expected max boost %.2f, got %f", OutcomeFeedbackMaxBoost, fb)
	}
}

func TestComputeOutcomeFeedback_MaxPenalty(t *testing.T) {
	lr := LearningRecord{TotalOutcomes: 10, SuccessRate: 0.0}
	fb := ComputeOutcomeFeedback(lr)
	// raw = (0 - 0.5) * 2 = -1.0 → feedback = -1.0 * 0.10 = -0.10
	if fb != -OutcomeFeedbackMaxPenalty {
		t.Errorf("expected max penalty -%.2f, got %f", OutcomeFeedbackMaxPenalty, fb)
	}
}

// --- BuildAttribution tests ---

func TestBuildAttribution_Succeeded(t *testing.T) {
	opp := IncomeOpportunity{
		ID:              "opp-1",
		OpportunityType: "consulting",
		EstimatedValue:  1000,
	}
	outcome := IncomeOutcome{
		ID:            "out-1",
		OpportunityID: "opp-1",
		ProposalID:    "prop-1",
		OutcomeStatus: OutcomeSucceeded,
		ActualValue:   800,
	}
	attr := BuildAttribution(opp, outcome)
	if attr.OutcomeID != "out-1" {
		t.Errorf("expected outcome_id out-1, got %s", attr.OutcomeID)
	}
	if attr.OpportunityType != "consulting" {
		t.Errorf("expected opportunity_type consulting, got %s", attr.OpportunityType)
	}
	if attr.EstimatedValue != 1000 {
		t.Errorf("expected estimated_value 1000, got %f", attr.EstimatedValue)
	}
	if attr.ActualValue != 800 {
		t.Errorf("expected actual_value 800, got %f", attr.ActualValue)
	}
	// accuracy = 800/1000 = 0.8
	if attr.Accuracy < 0.79 || attr.Accuracy > 0.81 {
		t.Errorf("expected accuracy ~0.8, got %f", attr.Accuracy)
	}
}

func TestBuildAttribution_Failed(t *testing.T) {
	opp := IncomeOpportunity{
		ID:              "opp-2",
		OpportunityType: "service",
		EstimatedValue:  2000,
	}
	outcome := IncomeOutcome{
		ID:            "out-2",
		OpportunityID: "opp-2",
		OutcomeStatus: OutcomeFailed,
		ActualValue:   500,
	}
	attr := BuildAttribution(opp, outcome)
	if attr.Accuracy != 0 {
		t.Errorf("expected accuracy 0 for failed outcome, got %f", attr.Accuracy)
	}
}

// --- UpdateLearningFromAttribution tests ---

func TestUpdateLearning_FirstOutcome(t *testing.T) {
	existing := LearningRecord{OpportunityType: "consulting"}
	attr := AttributionRecord{
		OutcomeStatus: OutcomeSucceeded,
		Accuracy:      0.8,
	}
	updated := UpdateLearningFromAttribution(existing, attr)
	if updated.TotalOutcomes != 1 {
		t.Errorf("expected total_outcomes 1, got %d", updated.TotalOutcomes)
	}
	if updated.SuccessCount != 1 {
		t.Errorf("expected success_count 1, got %d", updated.SuccessCount)
	}
	if updated.AvgAccuracy < 0.79 || updated.AvgAccuracy > 0.81 {
		t.Errorf("expected avg_accuracy ~0.8, got %f", updated.AvgAccuracy)
	}
	if updated.SuccessRate != 1.0 {
		t.Errorf("expected success_rate 1.0, got %f", updated.SuccessRate)
	}
	// confidence_adjustment = 0 because MinLearningOutcomes not met
	if updated.ConfidenceAdjustment != 0 {
		t.Errorf("expected 0 confidence_adjustment (cold start), got %f", updated.ConfidenceAdjustment)
	}
}

func TestUpdateLearning_MultipleOutcomes(t *testing.T) {
	existing := LearningRecord{
		OpportunityType: "consulting",
		TotalOutcomes:   4,
		SuccessCount:    3,
		TotalAccuracy:   3.6, // sum of accuracies
	}
	attr := AttributionRecord{
		OutcomeStatus: OutcomeFailed,
		Accuracy:      0, // failed
	}
	updated := UpdateLearningFromAttribution(existing, attr)
	if updated.TotalOutcomes != 5 {
		t.Errorf("expected total_outcomes 5, got %d", updated.TotalOutcomes)
	}
	if updated.SuccessCount != 3 {
		t.Errorf("expected success_count 3 (failure), got %d", updated.SuccessCount)
	}
	// avg_accuracy = 3.6 / 5 = 0.72
	if updated.AvgAccuracy < 0.71 || updated.AvgAccuracy > 0.73 {
		t.Errorf("expected avg_accuracy ~0.72, got %f", updated.AvgAccuracy)
	}
	// success_rate = 3/5 = 0.6
	if updated.SuccessRate < 0.59 || updated.SuccessRate > 0.61 {
		t.Errorf("expected success_rate ~0.6, got %f", updated.SuccessRate)
	}
	// confidence_adjustment should be non-zero now (5 >= MinLearningOutcomes)
	// delta = (0.72 - 1.0) * 0.30 = -0.084
	if updated.ConfidenceAdjustment > -0.07 || updated.ConfidenceAdjustment < -0.10 {
		t.Errorf("expected negative confidence_adjustment ~-0.084, got %f", updated.ConfidenceAdjustment)
	}
}

func TestUpdateLearning_PartialOutcome(t *testing.T) {
	existing := LearningRecord{OpportunityType: "automation"}
	attr := AttributionRecord{
		OutcomeStatus: OutcomePartial,
		Accuracy:      0.5,
	}
	updated := UpdateLearningFromAttribution(existing, attr)
	if updated.SuccessCount != 0 {
		t.Errorf("expected success_count 0 for partial, got %d", updated.SuccessCount)
	}
	if updated.AvgAccuracy < 0.49 || updated.AvgAccuracy > 0.51 {
		t.Errorf("expected avg_accuracy ~0.5, got %f", updated.AvgAccuracy)
	}
}

// --- Outcome source validation tests ---

func TestValidOutcomeSources(t *testing.T) {
	valid := []string{"manual", "system", "external"}
	for _, s := range valid {
		if !validOutcomeSources[s] {
			t.Errorf("expected %q to be a valid outcome source", s)
		}
	}
}

func TestInvalidOutcomeSources(t *testing.T) {
	invalid := []string{"", "webhook", "api", "unknown"}
	for _, s := range invalid {
		if validOutcomeSources[s] {
			t.Errorf("expected %q to NOT be a valid outcome source", s)
		}
	}
}

// --- Graph adapter outcome feedback tests ---

func TestGraphAdapter_GetOutcomeFeedback_NilEngine(t *testing.T) {
	adapter := NewGraphAdapter(nil, nil)
	fb := adapter.GetOutcomeFeedback(context.Background(), "propose_income_action")
	if fb != 0 {
		t.Errorf("expected 0 for nil engine, got %f", fb)
	}
}

func TestGraphAdapter_GetOutcomeFeedback_NilAdapter(t *testing.T) {
	var adapter *GraphAdapter
	fb := adapter.GetOutcomeFeedback(context.Background(), "propose_income_action")
	if fb != 0 {
		t.Errorf("expected 0 for nil adapter, got %f", fb)
	}
}

// --- PerformanceStats zero value test ---

func TestPerformanceStats_ZeroValue(t *testing.T) {
	stats := PerformanceStats{}
	if stats.TotalOutcomes != 0 || stats.OverallAccuracy != 0 || stats.OverallSuccessRate != 0 {
		t.Error("expected zero-value PerformanceStats")
	}
}

// --- LearningRecord zero value test ---

func TestLearningRecord_ZeroValue(t *testing.T) {
	lr := LearningRecord{}
	if lr.TotalOutcomes != 0 || lr.AvgAccuracy != 0 || lr.SuccessRate != 0 {
		t.Error("expected zero-value LearningRecord")
	}
}

// --- AttributionRecord type test ---

func TestAttributionRecord_Fields(t *testing.T) {
	attr := AttributionRecord{
		OutcomeID:       "out-1",
		OpportunityID:   "opp-1",
		ProposalID:      "prop-1",
		OpportunityType: "consulting",
		EstimatedValue:  1000,
		ActualValue:     800,
		Accuracy:        0.8,
		OutcomeStatus:   OutcomeSucceeded,
	}
	if attr.OutcomeID != "out-1" {
		t.Errorf("unexpected OutcomeID: %s", attr.OutcomeID)
	}
	if attr.Accuracy != 0.8 {
		t.Errorf("unexpected Accuracy: %f", attr.Accuracy)
	}
}

// --- Constants sanity tests (Iteration 39) ---

func TestLearningConstants_Bounds(t *testing.T) {
	if LearningWeight <= 0 || LearningWeight > 1 {
		t.Errorf("LearningWeight out of (0,1]: %f", LearningWeight)
	}
	if LearningMaxConfAdj <= 0 || LearningMaxConfAdj > 0.30 {
		t.Errorf("LearningMaxConfAdj out of (0,0.30]: %f", LearningMaxConfAdj)
	}
	if MinLearningOutcomes < 1 {
		t.Errorf("MinLearningOutcomes must be ≥1: %d", MinLearningOutcomes)
	}
	if OutcomeFeedbackMaxBoost <= 0 || OutcomeFeedbackMaxBoost > 0.20 {
		t.Errorf("OutcomeFeedbackMaxBoost out of (0,0.20]: %f", OutcomeFeedbackMaxBoost)
	}
	if OutcomeFeedbackMaxPenalty <= 0 || OutcomeFeedbackMaxPenalty > 0.20 {
		t.Errorf("OutcomeFeedbackMaxPenalty out of (0,0.20]: %f", OutcomeFeedbackMaxPenalty)
	}
}

// --- Opportunity closing on outcome tests ---

func TestIncomeOutcome_DefaultSource(t *testing.T) {
	o := IncomeOutcome{
		ID:            "out-1",
		OpportunityID: "opp-1",
		OutcomeStatus: OutcomeSucceeded,
		ActualValue:   100,
	}
	if o.OutcomeSource != "" {
		t.Errorf("expected empty default (store fills in), got %q", o.OutcomeSource)
	}
}

func TestIncomeOutcome_VerifiedDefault(t *testing.T) {
	o := IncomeOutcome{}
	if o.Verified {
		t.Error("expected Verified=false by default")
	}
}

// --- Edge case: overperformance clamping ---

func TestComputeAccuracy_ExtremeOverperformance(t *testing.T) {
	acc := ComputeAccuracy(100, 1000000, OutcomeSucceeded)
	if acc != 2.0 {
		t.Errorf("expected cap at 2.0, got %f", acc)
	}
}

// --- Feedback symmetry test ---

func TestComputeOutcomeFeedback_Symmetry(t *testing.T) {
	high := LearningRecord{TotalOutcomes: 10, SuccessRate: 0.8}
	low := LearningRecord{TotalOutcomes: 10, SuccessRate: 0.2}
	fbHigh := ComputeOutcomeFeedback(high)
	fbLow := ComputeOutcomeFeedback(low)
	if fbHigh <= 0 {
		t.Errorf("expected positive feedback for high success rate, got %f", fbHigh)
	}
	if fbLow >= 0 {
		t.Errorf("expected negative feedback for low success rate, got %f", fbLow)
	}
	// Magnitude should be roughly symmetric
	if fbHigh+fbLow > 0.02 || fbHigh+fbLow < -0.02 {
		t.Errorf("expected roughly symmetric magnitude: high=%f low=%f sum=%f", fbHigh, fbLow, fbHigh+fbLow)
	}
}
