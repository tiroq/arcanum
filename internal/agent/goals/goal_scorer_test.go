package goals

import (
	"fmt"
	"testing"
)

// -----------------------------------------------------------------------------
// 4.2 Scoring tests
// -----------------------------------------------------------------------------

func makeIncomeGoal() SystemGoal {
	return SystemGoal{
		ID:               "monthly_income_growth",
		Type:             "income",
		Priority:         0.95,
		Description:      "Grow income",
		PreferredActions: []string{"propose_income_action", "analyze_opportunity"},
		Signals:          []string{"new_opportunities"},
	}
}

func makeSafetyGoal() SystemGoal {
	return SystemGoal{
		ID:          "family_stability",
		Type:        "safety",
		Priority:    1.0,
		Description: "Keep family safe",
		Constraints: map[string]interface{}{
			"forbid_actions": []interface{}{"irreversible_financial_commitment", "unsafe_external_calls"},
			"max_risk_score": 0.3,
		},
	}
}

// TestScoreGoalAlignment_PreferredAction checks that a preferred action gets the boost.
func TestScoreGoalAlignment_PreferredAction(t *testing.T) {
	goals := []SystemGoal{makeIncomeGoal()}
	result := ScoreGoalAlignment("propose_income_action", goals)

	if result.RejectedByConstraints {
		t.Fatal("propose_income_action should not be rejected")
	}
	if result.AlignmentScore < preferredActionBoost {
		t.Errorf("expected alignment score >= %.2f for preferred action, got %.4f",
			preferredActionBoost, result.AlignmentScore)
	}
	if len(result.MatchedGoals) == 0 {
		t.Error("expected at least one matched goal")
	}
	if result.MatchedGoals[0] != "monthly_income_growth" {
		t.Errorf("expected matched goal monthly_income_growth, got %q", result.MatchedGoals[0])
	}
}

// TestScoreGoalAlignment_SignalMatch checks the signal-to-action mapping bonus.
func TestScoreGoalAlignment_SignalMatch(t *testing.T) {
	// new_opportunities signal maps to propose_income_action.
	goals := []SystemGoal{makeIncomeGoal()}
	result := ScoreGoalAlignment("propose_income_action", goals)

	// Should have both preferred action boost + signal boost for propose_income_action.
	// propose_income_action is both in preferred_actions AND matches new_opportunities signal.
	if result.AlignmentScore < preferredActionBoost {
		t.Errorf("expected at least preferredActionBoost %.2f, got %.4f",
			preferredActionBoost, result.AlignmentScore)
	}
}

// TestScoreGoalAlignment_SignalOnlyMatch checks a signal match without preferred status.
func TestScoreGoalAlignment_SignalOnlyMatch(t *testing.T) {
	// failed_jobs → retry_job action
	g := SystemGoal{
		ID:       "system_reliability",
		Type:     "operational",
		Priority: 0.9,
		Signals:  []string{"failed_jobs"},
	}
	result := ScoreGoalAlignment("retry_job", []SystemGoal{g})
	if result.AlignmentScore < signalMatchBoost {
		t.Errorf("expected signal match boost %.2f, got %.4f", signalMatchBoost, result.AlignmentScore)
	}
}

// TestScoreGoalAlignment_ConstraintViolation checks that a forbidden action is rejected.
func TestScoreGoalAlignment_ConstraintViolation(t *testing.T) {
	goals := []SystemGoal{makeSafetyGoal()}
	result := ScoreGoalAlignment("unsafe_external_calls", goals)

	if !result.RejectedByConstraints {
		t.Fatal("unsafe_external_calls should be rejected by family_stability constraints")
	}
	if result.RejectReason == "" {
		t.Error("expected non-empty reject reason")
	}
	// Score must be effectively zero on rejection.
	if result.AlignmentScore != 0 {
		t.Errorf("rejected action should have zero alignment score, got %.4f", result.AlignmentScore)
	}
}

// TestScoreGoalAlignment_NoGoals checks that with no goals the score is 0 and no rejection.
func TestScoreGoalAlignment_NoGoals(t *testing.T) {
	result := ScoreGoalAlignment("propose_income_action", nil)
	if result.RejectedByConstraints {
		t.Error("should not be rejected with no goals")
	}
	if result.AlignmentScore != 0 {
		t.Errorf("expected 0 alignment with no goals, got %.4f", result.AlignmentScore)
	}
}

// TestScoreGoalAlignment_Clamped checks that the score never exceeds 1.0.
func TestScoreGoalAlignment_Clamped(t *testing.T) {
	// Many high-priority goals all preferring the same action.
	var manyGoals []SystemGoal
	for i := 0; i < 10; i++ {
		manyGoals = append(manyGoals, SystemGoal{
			ID:               fmt.Sprintf("goal_%d", i),
			Type:             "income",
			Priority:         1.0,
			PreferredActions: []string{"propose_income_action"},
			Signals:          []string{"new_opportunities"},
		})
	}
	result := ScoreGoalAlignment("propose_income_action", manyGoals)
	if result.AlignmentScore > 1.0 {
		t.Errorf("alignment score must be clamped to [0,1], got %.4f", result.AlignmentScore)
	}
}

// TestScoreGoalAlignment_LowPriorityNoPreferredBoost ensures low-priority goals
// do not give the preferred action boost.
func TestScoreGoalAlignment_LowPriorityNoPreferredBoost(t *testing.T) {
	g := SystemGoal{
		ID:               "low_prio",
		Type:             "learning",
		Priority:         0.5, // Below highPriorityThreshold (0.80)
		PreferredActions: []string{"propose_income_action"},
	}
	result := ScoreGoalAlignment("propose_income_action", []SystemGoal{g})
	// No preferred boost since priority < threshold, and no signals match.
	if result.AlignmentScore != 0 {
		t.Errorf("expected 0 alignment for low-priority preferred action, got %.4f", result.AlignmentScore)
	}
}

// 4.4 Integration test: income goal prefers propose_income_action over noop.
func TestScoreGoalAlignment_Integration_IncomePrefersIncomeAction(t *testing.T) {
	incomeGoal := SystemGoal{
		ID:               "monthly_income_growth",
		Type:             "income",
		Priority:         0.95,
		Description:      "Grow income",
		PreferredActions: []string{"propose_income_action"},
		Signals:          []string{"new_opportunities"},
	}
	goals := []SystemGoal{incomeGoal}

	incomeResult := ScoreGoalAlignment("propose_income_action", goals)
	otherResult := ScoreGoalAlignment("noop", goals)

	if incomeResult.AlignmentScore <= otherResult.AlignmentScore {
		t.Errorf("income action should have higher alignment than noop: income=%.4f noop=%.4f",
			incomeResult.AlignmentScore, otherResult.AlignmentScore)
	}
}

// 4.4 Integration test: forbidden action is rejected while income action passes.
func TestScoreGoalAlignment_Integration_ForbiddenRejected(t *testing.T) {
	safetyGoal := SystemGoal{
		ID:          "family_stability",
		Type:        "safety",
		Priority:    1.0,
		Description: "Safety first",
		Constraints: map[string]interface{}{
			"forbid_actions": []interface{}{"irreversible_financial_commitment"},
		},
	}
	incomeGoal := SystemGoal{
		ID:               "income_growth",
		Type:             "income",
		Priority:         0.95,
		Description:      "Grow income",
		PreferredActions: []string{"propose_income_action"},
	}
	goals := []SystemGoal{safetyGoal, incomeGoal}

	// Forbidden action must be rejected.
	forbidResult := ScoreGoalAlignment("irreversible_financial_commitment", goals)
	if !forbidResult.RejectedByConstraints {
		t.Error("irreversible_financial_commitment must be rejected")
	}

	// Preferred action must pass.
	incomeResult := ScoreGoalAlignment("propose_income_action", goals)
	if incomeResult.RejectedByConstraints {
		t.Error("propose_income_action must not be rejected")
	}
	if incomeResult.AlignmentScore == 0 {
		t.Error("propose_income_action must have positive alignment score")
	}
}
