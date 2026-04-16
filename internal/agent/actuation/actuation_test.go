package actuation

import (
	"context"
	"testing"
)

// --- Mock providers ---

type mockReflectionProvider struct {
	signals []ReflectionSignalInput
	err     error
}

func (m *mockReflectionProvider) GetReflectionSignals(_ context.Context) ([]ReflectionSignalInput, error) {
	return m.signals, m.err
}

type mockObjectiveProvider struct {
	netUtility    float64
	utilityScore  float64
	riskScore     float64
	financialRisk float64
	overloadRisk  float64
}

func (m *mockObjectiveProvider) GetNetUtility(_ context.Context) float64    { return m.netUtility }
func (m *mockObjectiveProvider) GetUtilityScore(_ context.Context) float64  { return m.utilityScore }
func (m *mockObjectiveProvider) GetRiskScore(_ context.Context) float64     { return m.riskScore }
func (m *mockObjectiveProvider) GetFinancialRisk(_ context.Context) float64 { return m.financialRisk }
func (m *mockObjectiveProvider) GetOverloadRisk(_ context.Context) float64  { return m.overloadRisk }

// --- Rule evaluation tests ---

func TestEvaluateRules_NoSignals_NoActions(t *testing.T) {
	inputs := ActuationInputs{}
	proposals := EvaluateRules(inputs)
	if len(proposals) != 0 {
		t.Errorf("expected 0 proposals, got %d", len(proposals))
	}
}

func TestEvaluateRules_LowEfficiency(t *testing.T) {
	inputs := ActuationInputs{
		ReflectionSignals: []ReflectionSignalInput{
			{SignalType: "low_efficiency", Strength: 0.6},
		},
		NetUtility: 0.60,
	}
	proposals := EvaluateRules(inputs)

	types := proposalTypes(proposals)
	if !types[ActIncreaseDiscovery] {
		t.Error("expected ActIncreaseDiscovery")
	}
	if !types[ActTriggerAutomation] {
		t.Error("expected ActTriggerAutomation")
	}
}

func TestEvaluateRules_PricingMisalignment(t *testing.T) {
	inputs := ActuationInputs{
		ReflectionSignals: []ReflectionSignalInput{
			{SignalType: "pricing_misalignment", Strength: 0.7},
		},
	}
	proposals := EvaluateRules(inputs)

	types := proposalTypes(proposals)
	if !types[ActAdjustPricing] {
		t.Error("expected ActAdjustPricing")
	}
}

func TestEvaluateRules_OverloadRisk(t *testing.T) {
	inputs := ActuationInputs{
		ReflectionSignals: []ReflectionSignalInput{
			{SignalType: "overload_risk", Strength: 0.8},
		},
	}
	proposals := EvaluateRules(inputs)

	types := proposalTypes(proposals)
	if !types[ActReduceLoad] {
		t.Error("expected ActReduceLoad")
	}
	if !types[ActShiftScheduling] {
		t.Error("expected ActShiftScheduling")
	}
}

func TestEvaluateRules_IncomeInstability(t *testing.T) {
	inputs := ActuationInputs{
		ReflectionSignals: []ReflectionSignalInput{
			{SignalType: "income_instability", Strength: 0.5},
		},
	}
	proposals := EvaluateRules(inputs)

	types := proposalTypes(proposals)
	if !types[ActStabilizeIncome] {
		t.Error("expected ActStabilizeIncome")
	}
}

func TestEvaluateRules_AutomationOpportunity(t *testing.T) {
	inputs := ActuationInputs{
		ReflectionSignals: []ReflectionSignalInput{
			{SignalType: "automation_opportunity", Strength: 0.5},
		},
	}
	proposals := EvaluateRules(inputs)

	types := proposalTypes(proposals)
	if !types[ActTriggerAutomation] {
		t.Error("expected ActTriggerAutomation")
	}
}

func TestEvaluateRules_PriorityEscalation_LowUtility(t *testing.T) {
	inputs := ActuationInputs{
		ReflectionSignals: []ReflectionSignalInput{
			{SignalType: "low_efficiency", Strength: 0.5},
		},
		NetUtility: 0.30, // Below LowUtilityThreshold
	}
	proposals := EvaluateRules(inputs)

	for _, p := range proposals {
		// With escalation, priority should be higher than base
		if p.Priority <= 0 {
			t.Errorf("expected positive priority after escalation, got %f", p.Priority)
		}
	}
}

func TestEvaluateRules_HighFinancialRisk(t *testing.T) {
	inputs := ActuationInputs{
		FinancialRisk: 0.85,
	}
	proposals := EvaluateRules(inputs)

	types := proposalTypes(proposals)
	if !types[ActStabilizeIncome] {
		t.Error("expected ActStabilizeIncome from high financial risk")
	}
}

func TestEvaluateRules_HighOverloadRisk(t *testing.T) {
	inputs := ActuationInputs{
		OverloadRisk: 0.80,
	}
	proposals := EvaluateRules(inputs)

	types := proposalTypes(proposals)
	if !types[ActReduceLoad] {
		t.Error("expected ActReduceLoad from high overload risk")
	}
}

func TestEvaluateRules_Deterministic(t *testing.T) {
	inputs := ActuationInputs{
		ReflectionSignals: []ReflectionSignalInput{
			{SignalType: "low_efficiency", Strength: 0.6},
			{SignalType: "overload_risk", Strength: 0.7},
		},
		NetUtility:    0.35,
		FinancialRisk: 0.75,
		OverloadRisk:  0.80,
	}

	// Run multiple times — must produce identical results.
	first := EvaluateRules(inputs)
	for i := 0; i < 10; i++ {
		again := EvaluateRules(inputs)
		if len(again) != len(first) {
			t.Fatalf("non-deterministic: run %d produced %d proposals vs %d", i, len(again), len(first))
		}
		for j := range first {
			if first[j].Type != again[j].Type {
				t.Errorf("non-deterministic: run %d proposal %d type mismatch: %s vs %s", i, j, first[j].Type, again[j].Type)
			}
			if first[j].Priority != again[j].Priority {
				t.Errorf("non-deterministic: run %d proposal %d priority mismatch: %f vs %f", i, j, first[j].Priority, again[j].Priority)
			}
		}
	}
}

func TestEvaluateRules_MaxBounded(t *testing.T) {
	// Even with many signals, output is bounded at MaxDecisionsPerRun.
	var signals []ReflectionSignalInput
	for i := 0; i < 20; i++ {
		signals = append(signals, ReflectionSignalInput{
			SignalType: "low_efficiency",
			Strength:   0.5,
		})
	}
	inputs := ActuationInputs{
		ReflectionSignals: signals,
		FinancialRisk:     0.9,
		OverloadRisk:      0.9,
	}

	proposals := EvaluateRules(inputs)
	if len(proposals) > MaxDecisionsPerRun {
		t.Errorf("exceeded max decisions: got %d, max %d", len(proposals), MaxDecisionsPerRun)
	}
}

func TestEvaluateRules_BelowMinStrength_Ignored(t *testing.T) {
	inputs := ActuationInputs{
		ReflectionSignals: []ReflectionSignalInput{
			{SignalType: "low_efficiency", Strength: 0.05}, // Below MinSignalStrength
		},
	}
	proposals := EvaluateRules(inputs)
	if len(proposals) != 0 {
		t.Errorf("expected 0 proposals for weak signal, got %d", len(proposals))
	}
}

func TestEvaluateRules_Deduplication(t *testing.T) {
	inputs := ActuationInputs{
		ReflectionSignals: []ReflectionSignalInput{
			{SignalType: "low_efficiency", Strength: 0.5},
			{SignalType: "automation_opportunity", Strength: 0.9},
		},
	}
	proposals := EvaluateRules(inputs)

	// Both produce ActTriggerAutomation — should be deduplicated.
	count := 0
	for _, p := range proposals {
		if p.Type == ActTriggerAutomation {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 ActTriggerAutomation after dedup, got %d", count)
	}
}

// --- State machine tests ---

func TestValidateTransition_Valid(t *testing.T) {
	cases := []struct {
		from, to DecisionStatus
	}{
		{StatusProposed, StatusApproved},
		{StatusProposed, StatusRejected},
		{StatusApproved, StatusExecuted},
	}
	for _, c := range cases {
		if !ValidateTransition(c.from, c.to) {
			t.Errorf("expected valid transition: %s → %s", c.from, c.to)
		}
	}
}

func TestValidateTransition_Invalid(t *testing.T) {
	cases := []struct {
		from, to DecisionStatus
	}{
		{StatusRejected, StatusApproved},
		{StatusExecuted, StatusProposed},
		{StatusProposed, StatusExecuted},
		{StatusRejected, StatusExecuted},
		{StatusApproved, StatusProposed},
		{StatusApproved, StatusRejected},
	}
	for _, c := range cases {
		if ValidateTransition(c.from, c.to) {
			t.Errorf("expected invalid transition: %s → %s", c.from, c.to)
		}
	}
}

// --- Routing target tests ---

func TestRoutingTargets(t *testing.T) {
	expected := map[ActuationType]string{
		ActRebalancePortfolio: "portfolio",
		ActAdjustPricing:      "pricing",
		ActShiftScheduling:    "scheduling",
		ActIncreaseDiscovery:  "discovery",
		ActTriggerAutomation:  "self_extension",
		ActReduceLoad:         "scheduling",
		ActStabilizeIncome:    "portfolio+pricing",
	}
	for actType, expectedTarget := range expected {
		got, ok := RoutingTarget[actType]
		if !ok {
			t.Errorf("missing routing target for %s", actType)
			continue
		}
		if got != expectedTarget {
			t.Errorf("routing target for %s: expected %s, got %s", actType, expectedTarget, got)
		}
	}
}

// --- Review required tests ---

func TestReviewRequired(t *testing.T) {
	if !ReviewRequired(ActTriggerAutomation) {
		t.Error("expected ActTriggerAutomation to require review")
	}
	if !ReviewRequired(ActAdjustPricing) {
		t.Error("expected ActAdjustPricing to require review")
	}
	if ReviewRequired(ActReduceLoad) {
		t.Error("expected ActReduceLoad to NOT require review")
	}
	if ReviewRequired(ActRebalancePortfolio) {
		t.Error("expected ActRebalancePortfolio to NOT require review")
	}
}

// --- Nil provider safety tests ---

func TestGatherInputs_NilProviders(t *testing.T) {
	e := &Engine{}
	inputs := e.gatherInputs(context.Background())
	if len(inputs.ReflectionSignals) != 0 {
		t.Error("expected empty signals with nil providers")
	}
	if inputs.NetUtility != 0 {
		t.Error("expected zero net utility with nil providers")
	}
}

// --- Adapter nil-safety tests ---

func TestGraphAdapter_NilEngine(t *testing.T) {
	var a *GraphAdapter
	ctx := context.Background()

	result, err := a.Run(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(result.Decisions) != 0 {
		t.Error("expected empty decisions")
	}

	decisions, err := a.ListDecisions(ctx, 10)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(decisions) != 0 {
		t.Error("expected empty decisions")
	}

	d, err := a.ApproveDecision(ctx, "test")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if d.ID != "" {
		t.Error("expected empty decision")
	}

	d, err = a.RejectDecision(ctx, "test")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if d.ID != "" {
		t.Error("expected empty decision")
	}

	d, err = a.ExecuteDecision(ctx, "test")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if d.ID != "" {
		t.Error("expected empty decision")
	}
}

// --- Clamp tests ---

func TestClamp01(t *testing.T) {
	cases := []struct {
		input, expected float64
	}{
		{-0.5, 0},
		{0, 0},
		{0.5, 0.5},
		{1.0, 1.0},
		{1.5, 1.0},
	}
	for _, c := range cases {
		got := clamp01(c.input)
		if got != c.expected {
			t.Errorf("clamp01(%f) = %f, expected %f", c.input, got, c.expected)
		}
	}
}

// --- Combined signal + objective test ---

func TestEvaluateRules_CombinedSignalsAndObjective(t *testing.T) {
	inputs := ActuationInputs{
		ReflectionSignals: []ReflectionSignalInput{
			{SignalType: "low_efficiency", Strength: 0.5},
			{SignalType: "income_instability", Strength: 0.6},
		},
		NetUtility:    0.30, // Below LowUtilityThreshold — escalates all
		FinancialRisk: 0.80, // Above HighFinancialRiskThreshold — adds ActStabilizeIncome
		OverloadRisk:  0.75, // Above HighOverloadRiskThreshold — adds ActReduceLoad
	}

	proposals := EvaluateRules(inputs)
	types := proposalTypes(proposals)

	// From low_efficiency signal
	if !types[ActIncreaseDiscovery] {
		t.Error("expected ActIncreaseDiscovery")
	}
	// From income_instability + high financial risk (deduplicated)
	if !types[ActStabilizeIncome] {
		t.Error("expected ActStabilizeIncome")
	}
	// From high overload risk
	if !types[ActReduceLoad] {
		t.Error("expected ActReduceLoad")
	}

	// Verify priority escalation: all priorities should be boosted
	for _, p := range proposals {
		if p.Priority < PriorityEscalationBoost {
			t.Errorf("expected priority >= %f after escalation, got %f for %s", PriorityEscalationBoost, p.Priority, p.Type)
		}
	}
}

// --- Unknown signal type → no action ---

func TestEvaluateRules_UnknownSignal_NoAction(t *testing.T) {
	inputs := ActuationInputs{
		ReflectionSignals: []ReflectionSignalInput{
			{SignalType: "unknown_signal_type", Strength: 0.9},
		},
	}
	proposals := EvaluateRules(inputs)
	if len(proposals) != 0 {
		t.Errorf("expected 0 proposals for unknown signal, got %d", len(proposals))
	}
}

// --- Valid actuation types ---

func TestValidActuationTypes(t *testing.T) {
	allTypes := []ActuationType{
		ActRebalancePortfolio,
		ActAdjustPricing,
		ActShiftScheduling,
		ActIncreaseDiscovery,
		ActTriggerAutomation,
		ActReduceLoad,
		ActStabilizeIncome,
	}
	for _, at := range allTypes {
		if !ValidActuationTypes[at] {
			t.Errorf("expected %s to be valid", at)
		}
	}
}

// --- Priority clamping test ---

func TestEvaluateRules_PriorityClamped(t *testing.T) {
	// Very strong signal + low utility → priority should still be clamped to [0,1]
	inputs := ActuationInputs{
		ReflectionSignals: []ReflectionSignalInput{
			{SignalType: "overload_risk", Strength: 1.0},
		},
		NetUtility:   0.10,
		OverloadRisk: 0.95,
	}

	proposals := EvaluateRules(inputs)
	for _, p := range proposals {
		if p.Priority < 0 || p.Priority > 1 {
			t.Errorf("priority out of bounds [0,1]: %f for %s", p.Priority, p.Type)
		}
		if p.Confidence < 0 || p.Confidence > 1 {
			t.Errorf("confidence out of bounds [0,1]: %f for %s", p.Confidence, p.Type)
		}
	}
}

// --- Helper ---

func proposalTypes(proposals []proposedAction) map[ActuationType]bool {
	m := make(map[ActuationType]bool)
	for _, p := range proposals {
		m[p.Type] = true
	}
	return m
}

// --- Causal proof: vector changes actuation behavior ---

func TestCausalProof_VectorChangesIncomePriority(t *testing.T) {
	inputs := ActuationInputs{
		ReflectionSignals: []ReflectionSignalInput{
			{SignalType: "income_instability", Strength: 0.60},
		},
		NetUtility:    0.50,
		FinancialRisk: 0.40,
		OverloadRisk:  0.30,
	}

	// Baseline: no vector.
	baseProposals := EvaluateRules(inputs)
	var basePriority float64
	for _, p := range baseProposals {
		if p.Type == ActStabilizeIncome {
			basePriority = p.Priority
		}
	}

	// With high income priority vector.
	highIncome := VectorRulesParams{
		HumanReviewStrictness: VectorBaselineHumanReviewStrictness,
		RiskTolerance:         VectorBaselineRiskTolerance,
		IncomePriority:        1.00,
	}
	vecProposals := EvaluateRulesWithVector(inputs, highIncome)
	var vecPriority float64
	for _, p := range vecProposals {
		if p.Type == ActStabilizeIncome {
			vecPriority = p.Priority
		}
	}

	if vecPriority <= basePriority {
		t.Errorf("CAUSAL FAILURE: high income vector should boost income stabilization priority: base=%.4f vec=%.4f",
			basePriority, vecPriority)
	}

	t.Logf("Causal proof: basePriority=%.4f vecPriority=%.4f", basePriority, vecPriority)
}

func TestCausalProof_VectorChangesReviewRequirement(t *testing.T) {
	// At baseline (0.80): adjust_pricing requires review.
	baseReview := ReviewRequired(ActAdjustPricing)
	if !baseReview {
		t.Fatal("baseline: adjust_pricing should require review")
	}

	// At low strictness (0.30): adjust_pricing does NOT require review.
	lowReview := ReviewRequiredWithVector(ActAdjustPricing, 0.30)
	if lowReview {
		t.Error("CAUSAL FAILURE: low review strictness should not require review for adjust_pricing")
	}

	// At high strictness (0.95): even rebalance_portfolio requires review.
	highReview := ReviewRequiredWithVector(ActRebalancePortfolio, 0.95)
	if !highReview {
		t.Error("CAUSAL FAILURE: high review strictness should require review for rebalance_portfolio")
	}

	// trigger_automation always requires review regardless.
	if !ReviewRequiredWithVector(ActTriggerAutomation, 0.10) {
		t.Error("trigger_automation should always require review")
	}

	t.Logf("Causal proof: base(adjust_pricing)=%v low(adjust_pricing)=%v high(rebalance)=%v",
		baseReview, lowReview, highReview)
}

func TestCausalProof_VectorDampensRiskActions(t *testing.T) {
	inputs := ActuationInputs{
		ReflectionSignals: []ReflectionSignalInput{
			{SignalType: "overload_risk", Strength: 0.80},
		},
		NetUtility:    0.50,
		FinancialRisk: 0.30,
		OverloadRisk:  0.80,
	}

	// Baseline.
	baseProposals := EvaluateRules(inputs)
	var baseReduceLoadPriority float64
	for _, p := range baseProposals {
		if p.Type == ActReduceLoad {
			baseReduceLoadPriority = p.Priority
		}
	}

	// High risk tolerance: should dampen reduce_load priority.
	highRT := VectorRulesParams{
		HumanReviewStrictness: VectorBaselineHumanReviewStrictness,
		RiskTolerance:         0.90,
		IncomePriority:        VectorBaselineIncomePriority,
	}
	vecProposals := EvaluateRulesWithVector(inputs, highRT)
	var vecReduceLoadPriority float64
	for _, p := range vecProposals {
		if p.Type == ActReduceLoad {
			vecReduceLoadPriority = p.Priority
		}
	}

	if vecReduceLoadPriority >= baseReduceLoadPriority {
		t.Errorf("CAUSAL FAILURE: high risk tolerance should dampen reduce_load priority: base=%.4f vec=%.4f",
			baseReduceLoadPriority, vecReduceLoadPriority)
	}

	t.Logf("Causal proof: baseReduceLoad=%.4f vecReduceLoad=%.4f", baseReduceLoadPriority, vecReduceLoadPriority)
}
