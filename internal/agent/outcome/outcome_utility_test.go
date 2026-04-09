package outcome

import (
	"testing"

	"github.com/google/uuid"
	"github.com/tiroq/arcanum/internal/agent/actions"
)

// TestComputeUtilityValues_SuccessIncome verifies income utility computation on success.
func TestComputeUtilityValues_SuccessIncome(t *testing.T) {
	action := actions.Action{
		ID:     uuid.New().String(),
		Params: map[string]any{"_ctx_goal_type": "income"},
	}
	o := &ActionOutcome{
		OutcomeStatus: OutcomeSuccess,
	}

	computeUtilityValues(action, o)

	if o.IncomeValue != 1.0 {
		t.Errorf("expected income_value=1.0, got %.4f", o.IncomeValue)
	}
	if o.FamilyValue != 0 {
		t.Errorf("expected family_value=0, got %.4f", o.FamilyValue)
	}
	if o.OwnerReliefValue != 0 {
		t.Errorf("expected owner_relief_value=0, got %.4f", o.OwnerReliefValue)
	}
	if o.RiskCost != 0 {
		t.Errorf("expected risk_cost=0 for success, got %.4f", o.RiskCost)
	}
	expectedUtility := 1.0
	if o.UtilityScore != expectedUtility {
		t.Errorf("expected utility_score=%.4f, got %.4f", expectedUtility, o.UtilityScore)
	}
}

// TestComputeUtilityValues_FailureAddsRiskCost verifies risk cost on failure.
func TestComputeUtilityValues_FailureAddsRiskCost(t *testing.T) {
	action := actions.Action{
		ID:     uuid.New().String(),
		Params: map[string]any{"_ctx_goal_type": "income"},
	}
	o := &ActionOutcome{
		OutcomeStatus: OutcomeFailure,
	}

	computeUtilityValues(action, o)

	if o.IncomeValue != 0 {
		t.Errorf("expected income_value=0 on failure, got %.4f", o.IncomeValue)
	}
	if o.RiskCost != 0.5 {
		t.Errorf("expected risk_cost=0.5 on failure, got %.4f", o.RiskCost)
	}
	if o.UtilityScore != -0.5 {
		t.Errorf("expected utility_score=-0.5 on failure, got %.4f", o.UtilityScore)
	}
}

// TestComputeUtilityValues_NeutralOutcome checks partial value on neutral outcome.
func TestComputeUtilityValues_NeutralOutcome(t *testing.T) {
	action := actions.Action{
		ID:     uuid.New().String(),
		Params: map[string]any{"_ctx_goal_type": "safety"},
	}
	o := &ActionOutcome{
		OutcomeStatus: OutcomeNeutral,
	}

	computeUtilityValues(action, o)

	expectedFamily := 1.0 * 0.3 // multiplier for neutral
	if o.FamilyValue != expectedFamily {
		t.Errorf("expected family_value=%.4f (neutral), got %.4f", expectedFamily, o.FamilyValue)
	}
}

// TestComputeUtilityValues_FamilyStabilityGoalID checks goal ID fallback.
func TestComputeUtilityValues_FamilyStabilityGoalID(t *testing.T) {
	// no _ctx_goal_type param → falls back to GoalID
	action := actions.Action{
		ID:     uuid.New().String(),
		Params: map[string]any{},
	}
	o := &ActionOutcome{
		OutcomeStatus: OutcomeSuccess,
		GoalID:        "family_stability",
	}

	computeUtilityValues(action, o)

	if o.FamilyValue != 1.0 {
		t.Errorf("expected family_value=1.0 from goal ID, got %.4f", o.FamilyValue)
	}
}

// TestComputeUtilityValues_NoGoalType checks defaults when no goal type is provided.
func TestComputeUtilityValues_NoGoalType(t *testing.T) {
	action := actions.Action{
		ID:     uuid.New().String(),
		Params: map[string]any{},
	}
	o := &ActionOutcome{
		OutcomeStatus: OutcomeSuccess,
	}

	computeUtilityValues(action, o)

	if o.IncomeValue != 0 || o.FamilyValue != 0 || o.OwnerReliefValue != 0 {
		t.Error("expected all utility values to be 0 when no goal type provided")
	}
	if o.UtilityScore != 0 {
		t.Errorf("expected utility_score=0, got %.4f", o.UtilityScore)
	}
}

// TestComputeUtilityValues_UtilityScoreFormula verifies the utility formula.
func TestComputeUtilityValues_UtilityScoreFormula(t *testing.T) {
	action := actions.Action{
		ID:     uuid.New().String(),
		Params: map[string]any{"_ctx_goal_type": "operational"},
	}
	o := &ActionOutcome{OutcomeStatus: OutcomeSuccess}

	computeUtilityValues(action, o)

	expected := o.IncomeValue + o.FamilyValue + o.OwnerReliefValue - o.RiskCost
	if o.UtilityScore != expected {
		t.Errorf("utility_score should equal income+family+relief-risk: expected %.4f, got %.4f",
			expected, o.UtilityScore)
	}
}
