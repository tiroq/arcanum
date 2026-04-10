package income

import (
	"context"
	"testing"
)

// --- Scoring tests ---

func TestScoreOpportunity_HighValueHighConfidence(t *testing.T) {
	o := IncomeOpportunity{
		EstimatedValue:  5000,
		EstimatedEffort: 0.3,
		Confidence:      0.9,
	}
	score := ScoreOpportunity(o)
	// value_score = 5000/10000 = 0.5
	// score = 0.5*0.40 + 0.9*0.30 - 0.3*0.20 = 0.20 + 0.27 - 0.06 = 0.41
	if score < 0.40 || score > 0.42 {
		t.Errorf("expected ~0.41, got %f", score)
	}
}

func TestScoreOpportunity_ZeroValue(t *testing.T) {
	o := IncomeOpportunity{
		EstimatedValue:  0,
		EstimatedEffort: 0.5,
		Confidence:      0.5,
	}
	score := ScoreOpportunity(o)
	// value_score = 0
	// score = 0*0.40 + 0.5*0.30 - 0.5*0.20 = 0.15 - 0.10 = 0.05
	if score < 0.04 || score > 0.06 {
		t.Errorf("expected ~0.05, got %f", score)
	}
}

func TestScoreOpportunity_MaxValue(t *testing.T) {
	o := IncomeOpportunity{
		EstimatedValue:  20000, // over max
		EstimatedEffort: 0,
		Confidence:      1.0,
	}
	score := ScoreOpportunity(o)
	// value_score = clamped to 1.0
	// score = 1.0*0.40 + 1.0*0.30 - 0*0.20 = 0.70
	if score < 0.69 || score > 0.71 {
		t.Errorf("expected ~0.70, got %f", score)
	}
}

func TestScoreOpportunity_ClampedToZero(t *testing.T) {
	o := IncomeOpportunity{
		EstimatedValue:  0,
		EstimatedEffort: 1.0,
		Confidence:      0,
	}
	score := ScoreOpportunity(o)
	// score = 0 + 0 - 1.0*0.20 = -0.20 → clamped to 0
	if score != 0 {
		t.Errorf("expected 0, got %f", score)
	}
}

func TestScoreOpportunity_ClampedToOne(t *testing.T) {
	// Extreme values shouldn't exceed 1
	o := IncomeOpportunity{
		EstimatedValue:  100000,
		EstimatedEffort: 0,
		Confidence:      1.0,
	}
	score := ScoreOpportunity(o)
	if score > 1.0 {
		t.Errorf("expected ≤1.0, got %f", score)
	}
}

// --- Mapper tests ---

func TestIsIncomeAction_Known(t *testing.T) {
	if !IsIncomeAction("propose_income_action") {
		t.Error("expected propose_income_action to be income action")
	}
	if !IsIncomeAction("analyze_opportunity") {
		t.Error("expected analyze_opportunity to be income action")
	}
	if !IsIncomeAction("schedule_work") {
		t.Error("expected schedule_work to be income action")
	}
}

func TestIsIncomeAction_Unknown(t *testing.T) {
	if IsIncomeAction("noop") {
		t.Error("expected noop to NOT be income action")
	}
	if IsIncomeAction("") {
		t.Error("expected empty string to NOT be income action")
	}
}

func TestMapOpportunityToActions_Consulting(t *testing.T) {
	actions := MapOpportunityToActions("consulting")
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}
	if actions[0] != "propose_income_action" || actions[1] != "schedule_work" {
		t.Errorf("unexpected actions: %v", actions)
	}
}

func TestMapOpportunityToActions_Automation(t *testing.T) {
	actions := MapOpportunityToActions("automation")
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}
	if actions[0] != "propose_income_action" || actions[1] != "analyze_opportunity" {
		t.Errorf("unexpected actions: %v", actions)
	}
}

func TestMapOpportunityToActions_Service(t *testing.T) {
	actions := MapOpportunityToActions("service")
	if len(actions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(actions))
	}
}

func TestMapOpportunityToActions_Content(t *testing.T) {
	actions := MapOpportunityToActions("content")
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0] != "propose_income_action" {
		t.Errorf("unexpected action: %s", actions[0])
	}
}

func TestMapOpportunityToActions_Unknown(t *testing.T) {
	actions := MapOpportunityToActions("unknown_type")
	if len(actions) != 1 {
		t.Fatalf("expected 1 fallback action, got %d", len(actions))
	}
	if actions[0] != "propose_income_action" {
		t.Errorf("expected propose_income_action fallback, got %s", actions[0])
	}
}

// --- Generator tests ---

func TestGenerateProposals_BelowThreshold(t *testing.T) {
	opp := IncomeOpportunity{
		ID:              "opp-1",
		Title:           "Test",
		OpportunityType: "consulting",
	}
	proposals := GenerateProposals(opp, 0.10, false) // below ScoreThreshold
	if len(proposals) != 0 {
		t.Errorf("expected no proposals below threshold, got %d", len(proposals))
	}
}

func TestGenerateProposals_AboveThreshold(t *testing.T) {
	opp := IncomeOpportunity{
		ID:              "opp-1",
		Title:           "Test",
		OpportunityType: "consulting",
		EstimatedValue:  1000,
		Confidence:      0.9,
		EstimatedEffort: 0.2,
	}
	proposals := GenerateProposals(opp, 0.50, false)
	if len(proposals) != 2 { // consulting maps to 2 actions
		t.Fatalf("expected 2 proposals, got %d", len(proposals))
	}
	for _, p := range proposals {
		if p.OpportunityID != "opp-1" {
			t.Errorf("expected opportunity_id opp-1, got %s", p.OpportunityID)
		}
		if p.Status != ProposalStatusPending {
			t.Errorf("expected pending status, got %s", p.Status)
		}
		if p.ID == "" {
			t.Error("expected non-empty ID")
		}
	}
}

func TestGenerateProposals_GovernanceFrozenRequiresReview(t *testing.T) {
	opp := IncomeOpportunity{
		ID:              "opp-1",
		Title:           "Frozen test",
		OpportunityType: "content",
		Confidence:      0.9,
	}
	proposals := GenerateProposals(opp, 0.50, true) // governance frozen
	if len(proposals) == 0 {
		t.Fatal("expected proposals even when frozen")
	}
	for _, p := range proposals {
		if !p.RequiresReview {
			t.Error("expected requires_review=true when governance is frozen")
		}
	}
}

func TestGenerateProposals_HighRiskRequiresReview(t *testing.T) {
	opp := IncomeOpportunity{
		ID:              "opp-1",
		Title:           "High risk",
		OpportunityType: "content",
		Confidence:      0.3,  // low confidence → high risk
		EstimatedEffort: 0.8,  // high effort → high risk
	}
	proposals := GenerateProposals(opp, 0.50, false)
	for _, p := range proposals {
		if p.RiskLevel != RiskHigh {
			t.Errorf("expected high risk, got %s", p.RiskLevel)
		}
		if !p.RequiresReview {
			t.Error("expected requires_review=true for high-risk proposals")
		}
	}
}

func TestGenerateProposals_BigTicketRequiresReview(t *testing.T) {
	opp := IncomeOpportunity{
		ID:              "opp-1",
		Title:           "Big ticket",
		OpportunityType: "consulting",
		EstimatedValue:  20000, // high value
		Confidence:      0.9,
		EstimatedEffort: 0.1,
	}
	proposals := GenerateProposals(opp, 0.80, false)
	// expected_value = 20000 * 0.80 = 16000 > BigTicketThreshold
	for _, p := range proposals {
		if !p.RequiresReview {
			t.Error("expected requires_review=true for big ticket proposal")
		}
	}
}

// --- Risk derivation tests ---

func TestDeriveRiskLevel_Low(t *testing.T) {
	risk := deriveRiskLevel(0.85, 0.2) // confidence > 0.80 and effort < 0.30
	if risk != RiskLow {
		t.Errorf("expected low, got %s", risk)
	}
}

func TestDeriveRiskLevel_Medium(t *testing.T) {
	risk := deriveRiskLevel(0.60, 0.5) // confidence >= 0.50 but not low
	if risk != RiskMedium {
		t.Errorf("expected medium, got %s", risk)
	}
}

func TestDeriveRiskLevel_High(t *testing.T) {
	risk := deriveRiskLevel(0.3, 0.8) // low confidence
	if risk != RiskHigh {
		t.Errorf("expected high, got %s", risk)
	}
}

// --- Clamp tests ---

func TestClamp01_InRange(t *testing.T) {
	if v := clamp01(0.5); v != 0.5 {
		t.Errorf("expected 0.5, got %f", v)
	}
}

func TestClamp01_Below(t *testing.T) {
	if v := clamp01(-0.5); v != 0 {
		t.Errorf("expected 0, got %f", v)
	}
}

func TestClamp01_Above(t *testing.T) {
	if v := clamp01(1.5); v != 1 {
		t.Errorf("expected 1, got %f", v)
	}
}

// --- Validation tests ---

func TestValidateOpportunity_Valid(t *testing.T) {
	o := IncomeOpportunity{
		Title:           "Test",
		OpportunityType: "consulting",
		EstimatedValue:  100,
		EstimatedEffort: 0.5,
		Confidence:      0.7,
	}
	if err := validateOpportunity(o); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateOpportunity_EmptyTitle(t *testing.T) {
	o := IncomeOpportunity{
		OpportunityType: "consulting",
	}
	if err := validateOpportunity(o); err == nil {
		t.Error("expected error for empty title")
	}
}

func TestValidateOpportunity_InvalidType(t *testing.T) {
	o := IncomeOpportunity{
		Title:           "Test",
		OpportunityType: "invalid",
	}
	if err := validateOpportunity(o); err == nil {
		t.Error("expected error for invalid opportunity type")
	}
}

func TestValidateOpportunity_NegativeValue(t *testing.T) {
	o := IncomeOpportunity{
		Title:           "Test",
		OpportunityType: "consulting",
		EstimatedValue:  -100,
	}
	if err := validateOpportunity(o); err == nil {
		t.Error("expected error for negative value")
	}
}

func TestValidateOpportunity_EffortOutOfRange(t *testing.T) {
	o := IncomeOpportunity{
		Title:           "Test",
		OpportunityType: "consulting",
		EstimatedEffort: 1.5,
	}
	if err := validateOpportunity(o); err == nil {
		t.Error("expected error for effort > 1")
	}
}

func TestValidateOpportunity_ConfidenceOutOfRange(t *testing.T) {
	o := IncomeOpportunity{
		Title:           "Test",
		OpportunityType: "consulting",
		Confidence:      -0.1,
	}
	if err := validateOpportunity(o); err == nil {
		t.Error("expected error for negative confidence")
	}
}

// --- Graph adapter tests ---

func TestGraphAdapter_NilEngine(t *testing.T) {
	adapter := NewGraphAdapter(nil, nil)
	score, count := adapter.GetIncomeSignal(context.Background())
	if score != 0 || count != 0 {
		t.Errorf("expected (0, 0) for nil engine, got (%f, %d)", score, count)
	}
}

func TestGraphAdapter_NilAdapter(t *testing.T) {
	var adapter *GraphAdapter
	score, count := adapter.GetIncomeSignal(context.Background())
	if score != 0 || count != 0 {
		t.Errorf("expected (0, 0) for nil adapter, got (%f, %d)", score, count)
	}
}

func TestGraphAdapter_IsIncomeRelated(t *testing.T) {
	adapter := NewGraphAdapter(nil, nil)
	if !adapter.IsIncomeRelated("propose_income_action") {
		t.Error("expected propose_income_action to be income related")
	}
	if adapter.IsIncomeRelated("noop") {
		t.Error("expected noop to NOT be income related")
	}
}

// --- IncomeSignal type tests ---

func TestIncomeSignal_ZeroValue(t *testing.T) {
	sig := IncomeSignal{}
	if sig.BestOpenScore != 0 || sig.OpenOpportunities != 0 {
		t.Error("expected zero-value IncomeSignal")
	}
}

// --- Constants sanity tests ---

func TestConstants_Bounds(t *testing.T) {
	if WeightValue+WeightConf < 0 || WeightValue+WeightConf > 1 {
		t.Error("WeightValue + WeightConf should sum to ≤1")
	}
	if IncomeSignalMaxBoost <= 0 || IncomeSignalMaxBoost > 0.30 {
		t.Errorf("IncomeSignalMaxBoost should be in (0, 0.30], got %f", IncomeSignalMaxBoost)
	}
	if ScoreThreshold < 0 || ScoreThreshold > 1 {
		t.Errorf("ScoreThreshold out of [0,1]: %f", ScoreThreshold)
	}
}

// --- Valid opportunity types test ---

func TestValidOpportunityTypes_AllPresent(t *testing.T) {
	expected := []string{"consulting", "automation", "service", "content", "other"}
	for _, typ := range expected {
		if !validOpportunityTypes[typ] {
			t.Errorf("expected %q to be a valid opportunity type", typ)
		}
	}
}

func TestValidOpportunityTypes_InvalidNotPresent(t *testing.T) {
	invalid := []string{"", "random", "unknown"}
	for _, typ := range invalid {
		if validOpportunityTypes[typ] {
			t.Errorf("expected %q to NOT be a valid opportunity type", typ)
		}
	}
}
