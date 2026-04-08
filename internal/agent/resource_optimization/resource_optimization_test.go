package resource_optimization

import (
	"testing"
)

// --- Test 1: Rolling averages update correctly ---

func TestComputeSignals_InsufficientSamples(t *testing.T) {
	profile := ResourceProfile{
		Mode:         "graph",
		GoalType:     "reduce_retry_rate",
		AvgLatencyMs: 3000,
		SampleCount:  2, // below MinSamplesForSignals
	}
	signals := ComputeSignals(profile)
	if signals.EfficiencyScore != 1.0 {
		t.Errorf("expected efficiency 1.0 for insufficient samples, got %f", signals.EfficiencyScore)
	}
	if signals.LatencyPenalty != 0 {
		t.Errorf("expected 0 latency penalty for insufficient samples, got %f", signals.LatencyPenalty)
	}
}

func TestComputeSignals_SufficientSamples(t *testing.T) {
	profile := ResourceProfile{
		Mode:              "graph",
		GoalType:          "reduce_retry_rate",
		AvgLatencyMs:      1000,
		AvgReasoningDepth: 2.0,
		AvgPathLength:     2.0,
		AvgTokenCost:      3.0,
		AvgExecutionCost:  2.0,
		SampleCount:       5,
	}
	signals := ComputeSignals(profile)

	if signals.LatencyPenalty <= 0 {
		t.Errorf("expected positive latency penalty for 1000ms, got %f", signals.LatencyPenalty)
	}
	if signals.CostPenalty <= 0 {
		t.Errorf("expected positive cost penalty for token_cost=3.0, got %f", signals.CostPenalty)
	}
	if signals.DepthPenalty <= 0 {
		t.Errorf("expected positive depth penalty for depth=2.0, got %f", signals.DepthPenalty)
	}
	if signals.EfficiencyScore <= 0 || signals.EfficiencyScore >= 1 {
		t.Errorf("expected efficiency in (0,1), got %f", signals.EfficiencyScore)
	}
}

// --- Test 2: Latency penalty increases with latency ---

func TestLatencyPenalty_IncreasesWithLatency(t *testing.T) {
	lowLatency := ResourceProfile{
		AvgLatencyMs: 600, AvgReasoningDepth: 1, AvgPathLength: 1, SampleCount: 10,
	}
	highLatency := ResourceProfile{
		AvgLatencyMs: 3000, AvgReasoningDepth: 1, AvgPathLength: 1, SampleCount: 10,
	}

	sigLow := ComputeSignals(lowLatency)
	sigHigh := ComputeSignals(highLatency)

	if sigHigh.LatencyPenalty <= sigLow.LatencyPenalty {
		t.Errorf("higher latency should have higher penalty: low=%f, high=%f",
			sigLow.LatencyPenalty, sigHigh.LatencyPenalty)
	}
}

func TestLatencyPenalty_ZeroBelowThreshold(t *testing.T) {
	profile := ResourceProfile{
		AvgLatencyMs: 400, SampleCount: 10,
	}
	signals := ComputeSignals(profile)
	if signals.LatencyPenalty != 0 {
		t.Errorf("expected 0 latency penalty below threshold, got %f", signals.LatencyPenalty)
	}
}

// --- Test 3: Cost penalty increases with cost ---

func TestCostPenalty_IncreasesWithCost(t *testing.T) {
	lowCost := ResourceProfile{
		AvgTokenCost: 2.0, AvgExecutionCost: 0.5, SampleCount: 10,
	}
	highCost := ResourceProfile{
		AvgTokenCost: 8.0, AvgExecutionCost: 7.0, SampleCount: 10,
	}

	sigLow := ComputeSignals(lowCost)
	sigHigh := ComputeSignals(highCost)

	if sigHigh.CostPenalty <= sigLow.CostPenalty {
		t.Errorf("higher cost should have higher penalty: low=%f, high=%f",
			sigLow.CostPenalty, sigHigh.CostPenalty)
	}
}

func TestCostPenalty_ZeroBelowThreshold(t *testing.T) {
	profile := ResourceProfile{
		AvgTokenCost: 0.5, AvgExecutionCost: 0.2, SampleCount: 10,
	}
	signals := ComputeSignals(profile)
	if signals.CostPenalty != 0 {
		t.Errorf("expected 0 cost penalty below threshold, got %f", signals.CostPenalty)
	}
}

// --- Test 4: Efficiency score bounded and deterministic ---

func TestEfficiencyScore_BoundedAndDeterministic(t *testing.T) {
	profiles := []ResourceProfile{
		{AvgLatencyMs: 0, AvgTokenCost: 0, AvgReasoningDepth: 0, SampleCount: 10},
		{AvgLatencyMs: 2000, AvgTokenCost: 5, AvgReasoningDepth: 3, SampleCount: 10},
		{AvgLatencyMs: 10000, AvgTokenCost: 20, AvgReasoningDepth: 10, SampleCount: 10},
	}

	for i, p := range profiles {
		signals := ComputeSignals(p)
		if signals.EfficiencyScore < 0 || signals.EfficiencyScore > 1 {
			t.Errorf("profile[%d]: efficiency %f out of range [0,1]", i, signals.EfficiencyScore)
		}

		// Deterministic: run again with same input.
		signals2 := ComputeSignals(p)
		if signals.EfficiencyScore != signals2.EfficiencyScore {
			t.Errorf("profile[%d]: non-deterministic efficiency: %f vs %f",
				i, signals.EfficiencyScore, signals2.EfficiencyScore)
		}
	}
}

func TestEfficiencyScore_MaxForZeroPenalties(t *testing.T) {
	profile := ResourceProfile{
		AvgLatencyMs:      0,
		AvgTokenCost:      0,
		AvgReasoningDepth: 0,
		SampleCount:       10,
	}
	signals := ComputeSignals(profile)
	if signals.EfficiencyScore != 1.0 {
		t.Errorf("expected max efficiency 1.0 for zero penalties, got %f", signals.EfficiencyScore)
	}
}

// --- Test 5: Direct mode receives boost under high confidence + low cost ---

func TestModeAdjustment_DirectBoostHighConfidence(t *testing.T) {
	profile := &ResourceProfile{
		AvgLatencyMs:      200,
		AvgTokenCost:      0.5,
		AvgReasoningDepth: 1,
		SampleCount:       10,
	}
	adj := ComputeModeAdjustment("direct", profile, 0.9, 0.8, "normal")
	if adj != ModeDirectBoost {
		t.Errorf("expected direct boost %f, got %f", ModeDirectBoost, adj)
	}
}

func TestModeAdjustment_DirectNoBoostLowConfidence(t *testing.T) {
	profile := &ResourceProfile{
		AvgLatencyMs:      200,
		AvgTokenCost:      0.5,
		AvgReasoningDepth: 1,
		SampleCount:       10,
	}
	adj := ComputeModeAdjustment("direct", profile, 0.5, 0.8, "normal")
	if adj != 0 {
		t.Errorf("expected no adjustment for low confidence, got %f", adj)
	}
}

// --- Test 6: Graph mode penalized when expensive and not justified ---

func TestModeAdjustment_GraphPenalizedHighCost(t *testing.T) {
	profile := &ResourceProfile{
		AvgLatencyMs:      4000,
		AvgTokenCost:      8.0,
		AvgExecutionCost:  6.0,
		AvgReasoningDepth: 3,
		SampleCount:       10,
	}
	adj := ComputeModeAdjustment("graph", profile, 0.6, 0.4, "normal")
	if adj >= 0 {
		t.Errorf("expected negative adjustment for expensive graph mode, got %f", adj)
	}
	if adj < -ModeGraphPenalty {
		t.Errorf("adjustment %f exceeds max penalty %f", adj, -ModeGraphPenalty)
	}
}

func TestModeAdjustment_GraphNoPenaltyHighSuccess(t *testing.T) {
	profile := &ResourceProfile{
		AvgLatencyMs:      4000,
		AvgTokenCost:      8.0,
		AvgReasoningDepth: 3,
		SampleCount:       10,
	}
	adj := ComputeModeAdjustment("graph", profile, 0.6, 0.8, "normal")
	if adj != 0 {
		t.Errorf("expected no penalty for graph with high success rate, got %f", adj)
	}
}

// --- Test 7: Resource pressure does not override stability ---

func TestModeAdjustment_ConservativeNotAdjusted(t *testing.T) {
	profile := &ResourceProfile{
		AvgLatencyMs:     5000,
		AvgTokenCost:     10,
		AvgExecutionCost: 8,
		SampleCount:      10,
	}
	adj := ComputeModeAdjustment("conservative", profile, 0.9, 0.9, "normal")
	if adj != 0 {
		t.Errorf("conservative mode should never be adjusted, got %f", adj)
	}
}

func TestModeAdjustment_SafeModeNoAdjustment(t *testing.T) {
	profile := &ResourceProfile{
		AvgLatencyMs: 5000,
		AvgTokenCost: 10,
		SampleCount:  10,
	}
	adj := ComputeModeAdjustment("direct", profile, 0.9, 0.9, "safe_mode")
	if adj != 0 {
		t.Errorf("no adjustments in safe_mode, got %f", adj)
	}
}

// --- Test 8: Missing data → no change ---

func TestModeAdjustment_NilProfile(t *testing.T) {
	adj := ComputeModeAdjustment("graph", nil, 0.9, 0.9, "normal")
	if adj != 0 {
		t.Errorf("expected 0 for nil profile (fail-open), got %f", adj)
	}
}

func TestComputeSignalsFromProfile_Nil(t *testing.T) {
	signals := ComputeSignalsFromProfile(nil)
	if signals.EfficiencyScore != 1.0 {
		t.Errorf("expected efficiency 1.0 for nil profile, got %f", signals.EfficiencyScore)
	}
}

// --- Test 9: Path resource penalty ---

func TestPathPenalty_SingleStepNoPenalty(t *testing.T) {
	profile := &ResourceProfile{
		AvgLatencyMs:     4000,
		AvgTokenCost:     8,
		AvgExecutionCost: 6,
		SampleCount:      10,
	}
	penalty := ComputePathResourcePenalty(1, profile, "normal")
	if penalty != 0 {
		t.Errorf("expected 0 penalty for single-step path, got %f", penalty)
	}
}

func TestPathPenalty_LongerPathHigherPenalty(t *testing.T) {
	profile := &ResourceProfile{
		AvgLatencyMs:      3000,
		AvgTokenCost:      5,
		AvgExecutionCost:  4,
		AvgReasoningDepth: 3,
		SampleCount:       10,
	}
	penalty2 := ComputePathResourcePenalty(2, profile, "normal")
	penalty3 := ComputePathResourcePenalty(3, profile, "normal")

	if penalty2 <= 0 {
		t.Errorf("expected positive penalty for 2-node path, got %f", penalty2)
	}
	if penalty3 <= penalty2 {
		t.Errorf("expected 3-node penalty > 2-node penalty: %f vs %f", penalty3, penalty2)
	}
	if penalty3 > ResourcePenaltyWeight {
		t.Errorf("penalty %f exceeds max bound %f", penalty3, ResourcePenaltyWeight)
	}
}

func TestPathPenalty_SafeModeNoPenalty(t *testing.T) {
	profile := &ResourceProfile{
		AvgLatencyMs: 5000,
		AvgTokenCost: 10,
		SampleCount:  10,
	}
	penalty := ComputePathResourcePenalty(3, profile, "safe_mode")
	if penalty != 0 {
		t.Errorf("expected 0 penalty in safe_mode, got %f", penalty)
	}
}

func TestPathPenalty_NilProfile(t *testing.T) {
	penalty := ComputePathResourcePenalty(3, nil, "normal")
	if penalty != 0 {
		t.Errorf("expected 0 for nil profile (fail-open), got %f", penalty)
	}
}

// --- Test 10: Deterministic repeated runs ---

func TestDeterminism_RepeatedRuns(t *testing.T) {
	profile := ResourceProfile{
		Mode:              "graph",
		GoalType:          "optimize_cost",
		AvgLatencyMs:      1500,
		AvgReasoningDepth: 2.5,
		AvgPathLength:     2.0,
		AvgTokenCost:      4.0,
		AvgExecutionCost:  3.0,
		SampleCount:       20,
	}

	for i := 0; i < 100; i++ {
		signals := ComputeSignals(profile)
		adj := ComputeModeAdjustment("graph", &profile, 0.7, 0.5, "normal")
		penalty := ComputePathResourcePenalty(3, &profile, "normal")

		// All runs should be identical.
		expected := ComputeSignals(profile)
		if signals.EfficiencyScore != expected.EfficiencyScore {
			t.Fatalf("run %d: non-deterministic efficiency: %f vs %f", i, signals.EfficiencyScore, expected.EfficiencyScore)
		}

		expectedAdj := ComputeModeAdjustment("graph", &profile, 0.7, 0.5, "normal")
		if adj != expectedAdj {
			t.Fatalf("run %d: non-deterministic mode adjustment: %f vs %f", i, adj, expectedAdj)
		}

		expectedPenalty := ComputePathResourcePenalty(3, &profile, "normal")
		if penalty != expectedPenalty {
			t.Fatalf("run %d: non-deterministic path penalty: %f vs %f", i, penalty, expectedPenalty)
		}
	}
}

// --- Edge Cases ---

func TestEdgeCase_ZeroTokenCost(t *testing.T) {
	profile := ResourceProfile{
		AvgTokenCost:     0,
		AvgExecutionCost: 0,
		SampleCount:      10,
	}
	signals := ComputeSignals(profile)
	if signals.CostPenalty != 0 {
		t.Errorf("expected 0 cost penalty for zero costs, got %f", signals.CostPenalty)
	}
}

func TestEdgeCase_ExtremeLatencySpike(t *testing.T) {
	profile := ResourceProfile{
		AvgLatencyMs: 100000,
		SampleCount:  10,
	}
	signals := ComputeSignals(profile)
	if signals.LatencyPenalty > 1.0 {
		t.Errorf("latency penalty should be clamped to 1.0, got %f", signals.LatencyPenalty)
	}
	if signals.EfficiencyScore < 0 {
		t.Errorf("efficiency should be at least 0, got %f", signals.EfficiencyScore)
	}
}

func TestEdgeCase_VeryExpensiveButSuccessful(t *testing.T) {
	// Mode is expensive but has high success rate → no penalty.
	profile := &ResourceProfile{
		AvgLatencyMs:      4000,
		AvgTokenCost:      8.0,
		AvgExecutionCost:  6.0,
		AvgReasoningDepth: 3,
		SampleCount:       10,
	}
	adj := ComputeModeAdjustment("graph", profile, 0.6, 0.9, "normal")
	if adj < 0 {
		t.Errorf("expensive but successful graph should not be penalized, got %f", adj)
	}
}

func TestEdgeCase_VeryCheapButLowQuality(t *testing.T) {
	// Cheap mode shouldn't get boosted when confidence is low.
	profile := &ResourceProfile{
		AvgLatencyMs:      100,
		AvgTokenCost:      0.1,
		AvgReasoningDepth: 1,
		SampleCount:       10,
	}
	adj := ComputeModeAdjustment("direct", profile, 0.3, 0.2, "normal")
	if adj != 0 {
		t.Errorf("cheap but low confidence should not be boosted, got %f", adj)
	}
}

// --- Pressure Detection ---

func TestDetectPressure_None(t *testing.T) {
	profiles := []ResourceProfile{
		{AvgLatencyMs: 500, AvgExecutionCost: 1.0, SampleCount: 10},
		{AvgLatencyMs: 300, AvgExecutionCost: 0.5, SampleCount: 10},
	}
	pressure := DetectPressure(profiles)
	if pressure != "none" {
		t.Errorf("expected 'none', got %q", pressure)
	}
}

func TestDetectPressure_HighLatency(t *testing.T) {
	profiles := []ResourceProfile{
		{AvgLatencyMs: 3000, AvgExecutionCost: 1.0, SampleCount: 10},
	}
	pressure := DetectPressure(profiles)
	if pressure != "high_latency" {
		t.Errorf("expected 'high_latency', got %q", pressure)
	}
}

func TestDetectPressure_HighCost(t *testing.T) {
	profiles := []ResourceProfile{
		{AvgLatencyMs: 500, AvgExecutionCost: 6.0, SampleCount: 10},
	}
	pressure := DetectPressure(profiles)
	if pressure != "high_cost" {
		t.Errorf("expected 'high_cost', got %q", pressure)
	}
}

func TestDetectPressure_HighBoth(t *testing.T) {
	profiles := []ResourceProfile{
		{AvgLatencyMs: 3000, AvgExecutionCost: 6.0, SampleCount: 10},
	}
	pressure := DetectPressure(profiles)
	if pressure != "high_latency_and_cost" {
		t.Errorf("expected 'high_latency_and_cost', got %q", pressure)
	}
}

func TestDetectPressure_InsufficientSamplesIgnored(t *testing.T) {
	profiles := []ResourceProfile{
		{AvgLatencyMs: 10000, AvgExecutionCost: 20, SampleCount: 1},
	}
	pressure := DetectPressure(profiles)
	if pressure != "none" {
		t.Errorf("expected 'none' for insufficient samples, got %q", pressure)
	}
}

func TestDetectPressure_EmptyProfiles(t *testing.T) {
	pressure := DetectPressure(nil)
	if pressure != "none" {
		t.Errorf("expected 'none' for nil profiles, got %q", pressure)
	}
}

// --- ModeComplexityWeight ---

func TestModeComplexityWeight(t *testing.T) {
	tests := []struct {
		mode     string
		expected float64
	}{
		{"direct", 1.0},
		{"conservative", 0.5},
		{"exploratory", 2.0},
		{"graph", 3.0},
		{"unknown", 1.0},
	}
	for _, tt := range tests {
		got := ModeComplexityWeight(tt.mode)
		if got != tt.expected {
			t.Errorf("ModeComplexityWeight(%q) = %f, want %f", tt.mode, got, tt.expected)
		}
	}
}

// --- clamp01 ---

func TestClamp01(t *testing.T) {
	tests := []struct {
		input    float64
		expected float64
	}{
		{-0.5, 0},
		{0, 0},
		{0.5, 0.5},
		{1.0, 1.0},
		{1.5, 1.0},
	}
	for _, tt := range tests {
		got := clamp01(tt.input)
		if got != tt.expected {
			t.Errorf("clamp01(%f) = %f, want %f", tt.input, got, tt.expected)
		}
	}
}

// --- Depth Penalty ---

func TestDepthPenalty_IncreasesWithDepth(t *testing.T) {
	low := ResourceProfile{AvgReasoningDepth: 1.5, SampleCount: 10}
	high := ResourceProfile{AvgReasoningDepth: 4.0, SampleCount: 10}

	sigLow := ComputeSignals(low)
	sigHigh := ComputeSignals(high)

	if sigHigh.DepthPenalty <= sigLow.DepthPenalty {
		t.Errorf("higher depth should have higher penalty: low=%f, high=%f",
			sigLow.DepthPenalty, sigHigh.DepthPenalty)
	}
}

func TestDepthPenalty_ZeroBelowThreshold(t *testing.T) {
	profile := ResourceProfile{AvgReasoningDepth: 0.5, SampleCount: 10}
	signals := ComputeSignals(profile)
	if signals.DepthPenalty != 0 {
		t.Errorf("expected 0 depth penalty below threshold, got %f", signals.DepthPenalty)
	}
}

// --- RecordDecision + GetRecentDecisions ---

func TestRecentDecisions_FIFO(t *testing.T) {
	// Clear the global state.
	recentDecisions = nil

	for i := 0; i < MaxRecentDecisions+10; i++ {
		RecordDecision(ResourceDecisionRecord{
			Mode:     "graph",
			GoalType: "test",
		})
	}

	decisions := GetRecentDecisions()
	if len(decisions) != MaxRecentDecisions {
		t.Errorf("expected %d recent decisions, got %d", MaxRecentDecisions, len(decisions))
	}
}

func TestGetRecentDecisions_Empty(t *testing.T) {
	recentDecisions = nil
	decisions := GetRecentDecisions()
	if decisions == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(decisions) != 0 {
		t.Errorf("expected 0 decisions, got %d", len(decisions))
	}
}

// --- Exploratory mode penalty ---

func TestModeAdjustment_ExploratoryPenalizedWhenVeryExpensive(t *testing.T) {
	profile := &ResourceProfile{
		AvgLatencyMs:      4500,
		AvgTokenCost:      9.0,
		AvgExecutionCost:  8.0,
		AvgReasoningDepth: 4,
		SampleCount:       10,
	}
	adj := ComputeModeAdjustment("exploratory", profile, 0.5, 0.3, "normal")
	if adj >= 0 {
		t.Errorf("expected negative adjustment for very expensive exploratory mode, got %f", adj)
	}
}

func TestModeAdjustment_ExploratoryNoPenaltyWhenCheap(t *testing.T) {
	profile := &ResourceProfile{
		AvgLatencyMs:      300,
		AvgTokenCost:      1.0,
		AvgReasoningDepth: 1,
		SampleCount:       10,
	}
	adj := ComputeModeAdjustment("exploratory", profile, 0.5, 0.5, "normal")
	if adj != 0 {
		t.Errorf("expected 0 for cheap exploratory mode, got %f", adj)
	}
}
