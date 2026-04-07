package policy

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/reflection"
)

// --- Proposal Generation Tests ---

func TestGenerateProposals_RepeatedLowValue(t *testing.T) {
	input := ProposalInput{
		ReflectionFindings: []reflection.Finding{
			{Rule: reflection.RuleRepeatedLowValue, ActionType: "trigger_resync"},
			{Rule: reflection.RuleRepeatedLowValue, ActionType: "trigger_resync"},
		},
		CurrentValues: map[PolicyParam]float64{ParamFeedbackAvoidPenalty: 0.40},
		StabilityMode: "normal",
	}

	proposals := GenerateProposals(input)

	found := false
	for _, p := range proposals {
		if p.Parameter == ParamFeedbackAvoidPenalty && p.Delta == 0.05 {
			found = true
			if p.NewValue != 0.45 {
				t.Errorf("expected new_value=0.45, got %f", p.NewValue)
			}
		}
	}
	if !found {
		t.Errorf("expected proposal for feedbackAvoidPenalty, got: %+v", proposals)
	}
}

func TestGenerateProposals_PlannerIgnoresFeedback(t *testing.T) {
	input := ProposalInput{
		ReflectionFindings: []reflection.Finding{
			{Rule: reflection.RulePlannerIgnoresFeedback},
			{Rule: reflection.RulePlannerIgnoresFeedback},
		},
		CurrentValues: map[PolicyParam]float64{ParamFeedbackAvoidPenalty: 0.40},
		StabilityMode: "normal",
	}

	proposals := GenerateProposals(input)

	found := false
	for _, p := range proposals {
		if p.Parameter == ParamFeedbackAvoidPenalty {
			found = true
		}
	}
	if !found {
		t.Error("expected proposal for feedbackAvoidPenalty")
	}
}

func TestGenerateProposals_EffectivePattern(t *testing.T) {
	input := ProposalInput{
		ActionMemory: []actionmemory.ActionMemoryRecord{
			{ActionType: "retry_job", TotalRuns: 10, SuccessRate: 0.80, FailureRate: 0.20},
		},
		CurrentValues: map[PolicyParam]float64{ParamFeedbackPreferBoost: 0.25},
		StabilityMode: "normal",
	}

	proposals := GenerateProposals(input)

	found := false
	for _, p := range proposals {
		if p.Parameter == ParamFeedbackPreferBoost && p.Delta == 0.03 {
			found = true
			if math.Abs(p.NewValue-0.28) > 1e-9 {
				t.Errorf("expected new_value=0.28, got %f", p.NewValue)
			}
		}
	}
	if !found {
		t.Errorf("expected proposal for feedbackPreferBoost, got: %+v", proposals)
	}
}

func TestGenerateProposals_HighNoopRatio(t *testing.T) {
	input := ProposalInput{
		ReflectionFindings: []reflection.Finding{
			{Rule: reflection.RulePlannerStalling},
		},
		CurrentValues: map[PolicyParam]float64{ParamNoopBasePenalty: 0.20},
		StabilityMode: "normal",
	}

	proposals := GenerateProposals(input)

	found := false
	for _, p := range proposals {
		if p.Parameter == ParamNoopBasePenalty && p.Delta == 0.05 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected proposal for noopBasePenalty, got: %+v", proposals)
	}
}

func TestGenerateProposals_HighNoopRatio_BlockedInSafeMode(t *testing.T) {
	input := ProposalInput{
		ReflectionFindings: []reflection.Finding{
			{Rule: reflection.RulePlannerStalling},
		},
		CurrentValues: map[PolicyParam]float64{ParamNoopBasePenalty: 0.20},
		StabilityMode: "safe_mode", // should suppress
	}

	proposals := GenerateProposals(input)

	for _, p := range proposals {
		if p.Parameter == ParamNoopBasePenalty {
			t.Error("noopBasePenalty proposal should be suppressed in safe_mode")
		}
	}
}

func TestGenerateProposals_RetryAmplification(t *testing.T) {
	input := ProposalInput{
		ActionMemory: []actionmemory.ActionMemoryRecord{
			{ActionType: "retry_job", TotalRuns: 10, SuccessRate: 0.30, FailureRate: 0.70},
		},
		CurrentValues: map[PolicyParam]float64{ParamHighRetryBoost: 0.15},
		StabilityMode: "normal",
	}

	proposals := GenerateProposals(input)

	found := false
	for _, p := range proposals {
		if p.Parameter == ParamHighRetryBoost && p.Delta == -0.05 {
			found = true
			if math.Abs(p.NewValue-0.10) > 1e-9 {
				t.Errorf("expected new_value=0.10, got %f", p.NewValue)
			}
		}
	}
	if !found {
		t.Errorf("expected proposal for highRetryBoost reduction, got: %+v", proposals)
	}
}

// --- Bounds Enforcement Tests ---

func TestFilterAndApply_BoundsEnforcement(t *testing.T) {
	proposals := []PolicyChange{
		{
			Parameter:  ParamFeedbackAvoidPenalty,
			OldValue:   0.98,
			NewValue:   1.03,
			Delta:      0.05,
			Reason:     "test",
			Confidence: 0.80,
		},
	}

	safe, _ := FilterAndApply(proposals, "normal")
	if len(safe) != 1 {
		t.Fatalf("expected 1 safe change, got %d", len(safe))
	}
	if safe[0].NewValue > 1.0 {
		t.Errorf("new_value %f exceeds max bound 1.0", safe[0].NewValue)
	}
}

func TestFilterAndApply_MaxDeltaEnforcement(t *testing.T) {
	proposals := []PolicyChange{
		{
			Parameter:  ParamFeedbackAvoidPenalty,
			OldValue:   0.50,
			NewValue:   0.60,
			Delta:      0.10, // exceeds MaxDelta of 0.05
			Reason:     "test",
			Confidence: 0.80,
		},
	}

	safe, _ := FilterAndApply(proposals, "normal")
	if len(safe) != 1 {
		t.Fatalf("expected 1 safe change, got %d", len(safe))
	}
	if math.Abs(safe[0].Delta) > 0.05+1e-9 {
		t.Errorf("delta %f exceeds max 0.05", safe[0].Delta)
	}
}

func TestFilterAndApply_MaxChangesPerCycle(t *testing.T) {
	proposals := []PolicyChange{
		{Parameter: ParamFeedbackAvoidPenalty, OldValue: 0.40, NewValue: 0.45, Delta: 0.05, Reason: "a", Confidence: 0.80},
		{Parameter: ParamFeedbackPreferBoost, OldValue: 0.25, NewValue: 0.28, Delta: 0.03, Reason: "b", Confidence: 0.80},
		{Parameter: ParamNoopBasePenalty, OldValue: 0.20, NewValue: 0.25, Delta: 0.05, Reason: "c", Confidence: 0.80},
	}

	safe, rejected := FilterAndApply(proposals, "normal")
	if len(safe) > MaxChangesPerCycle {
		t.Errorf("expected at most %d changes, got %d", MaxChangesPerCycle, len(safe))
	}
	if len(rejected) == 0 {
		t.Error("expected at least 1 rejected change due to cycle limit")
	}
}

func TestFilterAndApply_SafeModeRejectsAll(t *testing.T) {
	proposals := []PolicyChange{
		{Parameter: ParamFeedbackAvoidPenalty, OldValue: 0.40, NewValue: 0.45, Delta: 0.05, Reason: "a", Confidence: 0.80},
	}

	safe, rejected := FilterAndApply(proposals, "safe_mode")
	if len(safe) != 0 {
		t.Error("expected 0 safe changes in safe_mode")
	}
	if len(rejected) != 1 {
		t.Errorf("expected 1 rejected, got %d", len(rejected))
	}
}

func TestFilterAndApply_LowConfidenceRejected(t *testing.T) {
	proposals := []PolicyChange{
		{Parameter: ParamFeedbackAvoidPenalty, OldValue: 0.40, NewValue: 0.45, Delta: 0.05, Reason: "a", Confidence: 0.50},
	}

	safe, rejected := FilterAndApply(proposals, "normal")
	if len(safe) != 0 {
		t.Error("expected 0 safe changes for low confidence")
	}
	if len(rejected) != 1 {
		t.Errorf("expected 1 rejected, got %d", len(rejected))
	}
}

// --- Validation Tests ---

func TestValidateChange_Valid(t *testing.T) {
	c := PolicyChange{
		Parameter: ParamFeedbackAvoidPenalty,
		OldValue:  0.40,
		NewValue:  0.45,
		Delta:     0.05,
	}
	if err := ValidateChange(c); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestValidateChange_ExceedsBounds(t *testing.T) {
	c := PolicyChange{
		Parameter: ParamFeedbackAvoidPenalty,
		OldValue:  0.40,
		NewValue:  1.50,
		Delta:     1.10,
	}
	if err := ValidateChange(c); err == nil {
		t.Error("expected validation error for out-of-bounds")
	}
}

// --- Planner Non-Regression Test ---

func TestDefaultScoringParams_MatchConstants(t *testing.T) {
	params := DefaultValues
	expected := map[PolicyParam]float64{
		ParamFeedbackAvoidPenalty:     0.40,
		ParamFeedbackPreferBoost:      0.25,
		ParamHighBacklogResyncPenalty: 0.30,
		ParamHighRetryBoost:           0.15,
		ParamSafetyPreferenceBoost:    0.05,
		ParamNoopBasePenalty:          0.20,
	}

	for k, v := range expected {
		if got, ok := params[k]; !ok {
			t.Errorf("missing default for %s", k)
		} else if math.Abs(got-v) > 1e-9 {
			t.Errorf("default for %s: got %f, want %f", k, got, v)
		}
	}
}

// --- Change Record Types Test ---

func TestChangeRecord_Fields(t *testing.T) {
	r := ChangeRecord{
		ID:        uuid.New(),
		Parameter: "feedbackAvoidPenalty",
		OldValue:  0.40,
		NewValue:  0.45,
		Reason:    "test",
		Applied:   true,
		CreatedAt: time.Now(),
	}

	if r.Parameter != "feedbackAvoidPenalty" {
		t.Error("parameter mismatch")
	}
	if !r.Applied {
		t.Error("expected applied=true")
	}
}

func TestNoProposals_WhenNoSignals(t *testing.T) {
	input := ProposalInput{
		CurrentValues: map[PolicyParam]float64{ParamFeedbackAvoidPenalty: 0.40},
		StabilityMode: "normal",
	}

	proposals := GenerateProposals(input)
	if len(proposals) != 0 {
		t.Errorf("expected 0 proposals with no signals, got %d", len(proposals))
	}
}
