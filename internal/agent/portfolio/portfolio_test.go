package portfolio

import (
	"context"
	"testing"
)

// --- 1. Opportunity maps to correct strategy ---

func TestMapOpportunityToStrategy(t *testing.T) {
	tests := []struct {
		oppType  string
		expected string
	}{
		{"consulting", TypeConsulting},
		{"automation", TypeAutomation},
		{"service", TypeService},
		{"content", TypeContent},
		{"product", TypeProduct},
		{"other", TypeOther},
		{"unknown_type", TypeOther},
		{"", TypeOther},
	}
	for _, tc := range tests {
		got := MapOpportunityToStrategy(tc.oppType)
		if got != tc.expected {
			t.Errorf("MapOpportunityToStrategy(%q) = %q, want %q", tc.oppType, got, tc.expected)
		}
	}
}

// --- 2. Allocation respects capacity ---

func TestNormaliseAllocations_Basic(t *testing.T) {
	scores := map[string]float64{
		"a": 0.6,
		"b": 0.4,
	}
	allocs := NormaliseAllocations(scores, 40.0)

	total := 0.0
	for _, h := range allocs {
		total += h
	}
	if total < 39.9 || total > 40.1 {
		t.Errorf("total allocated = %.2f, want ~40.0", total)
	}
}

func TestNormaliseAllocations_MinMaxConstraints(t *testing.T) {
	// 5 strategies: one very dominant, rest near zero.
	scores := map[string]float64{
		"a": 0.90,
		"b": 0.01,
		"c": 0.01,
		"d": 0.01,
		"e": 0.01,
	}
	allocs := NormaliseAllocations(scores, 100.0)

	for id, hrs := range allocs {
		minH := 100.0 * MinAllocationFraction
		maxH := 100.0 * MaxAllocationFraction
		if hrs < minH-0.01 {
			t.Errorf("strategy %s: %.2f hours < min %.2f", id, hrs, minH)
		}
		if hrs > maxH+0.01 {
			t.Errorf("strategy %s: %.2f hours > max %.2f", id, hrs, maxH)
		}
	}
}

func TestNormaliseAllocations_Empty(t *testing.T) {
	allocs := NormaliseAllocations(map[string]float64{}, 40.0)
	if len(allocs) != 0 {
		t.Errorf("expected empty allocations, got %d", len(allocs))
	}
}

func TestNormaliseAllocations_ZeroHours(t *testing.T) {
	allocs := NormaliseAllocations(map[string]float64{"a": 0.5}, 0)
	if len(allocs) != 0 {
		t.Errorf("expected empty allocations for 0 hours, got %d", len(allocs))
	}
}

func TestNormaliseAllocations_EqualScores(t *testing.T) {
	scores := map[string]float64{
		"a": 0.5,
		"b": 0.5,
		"c": 0.5,
	}
	allocs := NormaliseAllocations(scores, 30.0)

	for id, hrs := range allocs {
		if hrs < 9.9 || hrs > 10.1 {
			t.Errorf("strategy %s expected ~10h, got %.2f", id, hrs)
		}
	}
}

// --- 3. ROI calculated correctly ---

func TestComputeROI(t *testing.T) {
	tests := []struct {
		revenue, time, expected float64
	}{
		{1000, 10, 100},
		{0, 10, 0},
		{1000, 0, 0},
		{500, 25, 20},
	}
	for _, tc := range tests {
		got := ComputeROI(tc.revenue, tc.time)
		if got != tc.expected {
			t.Errorf("ComputeROI(%.0f, %.0f) = %.2f, want %.2f", tc.revenue, tc.time, got, tc.expected)
		}
	}
}

// --- 4. Low ROI strategy penalized ---

func TestComputeStrategyBoost_LowROI(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, Status: StatusActive, ExpectedReturnPerHr: 30},
	}
	perfMap := map[string]StrategyPerformance{
		"s1": {StrategyID: "s1", TotalRevenue: 25, TotalTimeSpent: 10, ROI: 2.5}, // ROI = 2.5 $/hr < 10
	}

	boost := ComputeStrategyBoost(TypeConsulting, strategies, perfMap)
	if boost >= 0 {
		t.Errorf("expected negative boost for low ROI, got %.4f", boost)
	}
	if boost < -StrategyPenaltyMax {
		t.Errorf("penalty %.4f exceeds max %.4f", boost, StrategyPenaltyMax)
	}
}

// --- 5. High ROI boosted ---

func TestComputeStrategyBoost_HighROI(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, Status: StatusActive, ExpectedReturnPerHr: 80},
	}
	perfMap := map[string]StrategyPerformance{
		"s1": {StrategyID: "s1", TotalRevenue: 600, TotalTimeSpent: 10, ROI: 60}, // ROI = 60 $/hr > 50
	}

	boost := ComputeStrategyBoost(TypeConsulting, strategies, perfMap)
	if boost <= 0 {
		t.Errorf("expected positive boost for high ROI, got %.4f", boost)
	}
	if boost > StrategyPriorityBoostMax {
		t.Errorf("boost %.4f exceeds max %.4f", boost, StrategyPriorityBoostMax)
	}
}

func TestComputeStrategyBoost_NoData(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, Status: StatusActive, ExpectedReturnPerHr: 30},
	}
	perfMap := map[string]StrategyPerformance{} // no performance

	boost := ComputeStrategyBoost(TypeConsulting, strategies, perfMap)
	// Expected return 30 < HighROIThreshold (50), so no boost from expected either.
	if boost != 0 {
		t.Errorf("expected 0 boost without data and low expected return, got %.4f", boost)
	}
}

func TestComputeStrategyBoost_HighExpectedReturn(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeAutomation, Status: StatusActive, ExpectedReturnPerHr: 80},
	}
	perfMap := map[string]StrategyPerformance{} // no real data

	boost := ComputeStrategyBoost(TypeAutomation, strategies, perfMap)
	if boost <= 0 {
		t.Errorf("expected positive boost from high expected return, got %.4f", boost)
	}
}

func TestComputeStrategyBoost_InactiveStrategy(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, Status: StatusPaused, ExpectedReturnPerHr: 200},
	}
	perfMap := map[string]StrategyPerformance{}

	boost := ComputeStrategyBoost(TypeConsulting, strategies, perfMap)
	if boost != 0 {
		t.Errorf("expected 0 boost for paused strategy, got %.4f", boost)
	}
}

func TestComputeStrategyBoost_UnknownType(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, Status: StatusActive, ExpectedReturnPerHr: 80},
	}
	boost := ComputeStrategyBoost("nonexistent", strategies, nil)
	if boost != 0 {
		t.Errorf("expected 0 boost for unknown type, got %.4f", boost)
	}
}

// --- 6. Diversification enforced ---

func TestComputeDiversificationIndex_Perfect(t *testing.T) {
	allocs := map[string]float64{
		"a": 10,
		"b": 10,
		"c": 10,
	}
	idx := ComputeDiversificationIndex(allocs)
	if idx < 0.99 {
		t.Errorf("diversification index for equal allocs = %.4f, want ~1.0", idx)
	}
}

func TestComputeDiversificationIndex_Concentrated(t *testing.T) {
	allocs := map[string]float64{
		"a": 100,
		"b": 0,
	}
	idx := ComputeDiversificationIndex(allocs)
	if idx > 0.01 {
		t.Errorf("diversification index for concentrated allocs = %.4f, want ~0.0", idx)
	}
}

func TestComputeDiversificationIndex_Single(t *testing.T) {
	allocs := map[string]float64{"a": 40}
	idx := ComputeDiversificationIndex(allocs)
	if idx != 0 {
		t.Errorf("diversification index for single strategy = %.4f, want 0", idx)
	}
}

func TestComputeDiversificationIndex_Empty(t *testing.T) {
	idx := ComputeDiversificationIndex(map[string]float64{})
	if idx != 0 {
		t.Errorf("diversification index for empty = %.4f, want 0", idx)
	}
}

// --- 7. Portfolio rebalance / allocation scores ---

func TestComputeAllocationScores_BasicFlow(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, ExpectedReturnPerHr: 100, Volatility: 0.2},
		{ID: "s2", Type: TypeContent, ExpectedReturnPerHr: 30, Volatility: 0.8},
	}
	perfMap := map[string]StrategyPerformance{}

	scores := ComputeAllocationScores(strategies, perfMap, 0.5, true)
	if len(scores) != 2 {
		t.Fatalf("expected 2 scores, got %d", len(scores))
	}

	// Higher expected return + lower volatility should score higher.
	if scores["s1"] <= scores["s2"] {
		t.Errorf("s1 (high return, low vol) should score > s2 (low return, high vol): s1=%.4f s2=%.4f", scores["s1"], scores["s2"])
	}
}

func TestComputeAllocationScores_WithRealPerformance(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, ExpectedReturnPerHr: 50, Volatility: 0.3},
	}
	perfMap := map[string]StrategyPerformance{
		"s1": {StrategyID: "s1", TotalRevenue: 500, TotalTimeSpent: 10}, // real ROI = 50
	}

	scores := ComputeAllocationScores(strategies, perfMap, 0.0, false)
	if scores["s1"] <= 0 {
		t.Errorf("expected positive score with real performance, got %.4f", scores["s1"])
	}
}

func TestComputeAllocationScores_HighPressure(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, ExpectedReturnPerHr: 100, Volatility: 0.2},
		{ID: "s2", Type: TypeContent, ExpectedReturnPerHr: 20, Volatility: 0.1},
	}

	lowPressure := ComputeAllocationScores(strategies, nil, 0.1, false)
	highPressure := ComputeAllocationScores(strategies, nil, 0.9, false)

	// Under high pressure, the gap between high and low ROI strategies should be larger.
	gapLow := lowPressure["s1"] - lowPressure["s2"]
	gapHigh := highPressure["s1"] - highPressure["s2"]
	if gapHigh <= gapLow {
		t.Errorf("expected higher gap under pressure: low=%.4f high=%.4f", gapLow, gapHigh)
	}
}

func TestComputeAllocationScores_Empty(t *testing.T) {
	scores := ComputeAllocationScores(nil, nil, 0, false)
	if len(scores) != 0 {
		t.Errorf("expected empty scores, got %d", len(scores))
	}
}

// --- 8. Strategic signals ---

func TestDetectSignals_Underperforming(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, Status: StatusActive, ExpectedReturnPerHr: 30},
	}
	perfMap := map[string]StrategyPerformance{
		"s1": {StrategyID: "s1", TotalRevenue: 30, TotalTimeSpent: 10}, // ROI = 3
	}
	allocs := map[string]float64{"s1": 10}

	signals := DetectSignals(strategies, perfMap, allocs, 40)
	found := false
	for _, s := range signals {
		if s.SignalType == "underperforming" && s.StrategyID == "s1" {
			found = true
		}
	}
	if !found {
		t.Error("expected underperforming signal for s1")
	}
}

func TestDetectSignals_HighPotential(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, Status: StatusActive, ExpectedReturnPerHr: 80, Volatility: 0.3},
	}
	signals := DetectSignals(strategies, nil, nil, 40)
	found := false
	for _, s := range signals {
		if s.SignalType == "high_potential" && s.StrategyID == "s1" {
			found = true
		}
	}
	if !found {
		t.Error("expected high_potential signal for s1")
	}
}

func TestDetectSignals_NoSignalsForColdStart(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, Status: StatusActive, ExpectedReturnPerHr: 30},
	}
	// Only 2 hours of data — below MinSamplesForPerformance (5).
	perfMap := map[string]StrategyPerformance{
		"s1": {StrategyID: "s1", TotalRevenue: 5, TotalTimeSpent: 2},
	}
	allocs := map[string]float64{"s1": 10}

	signals := DetectSignals(strategies, perfMap, allocs, 40)
	for _, s := range signals {
		if s.SignalType == "underperforming" {
			t.Error("should not produce underperforming signal with insufficient data")
		}
	}
}

// --- Graph adapter nil-safety ---

func TestGraphAdapter_NilSafe(t *testing.T) {
	var a *GraphAdapter

	boost := a.GetStrategyBoost(context.Background(), "consulting")
	if boost != 0 {
		t.Errorf("nil adapter: expected 0 boost, got %.4f", boost)
	}

	related := a.IsStrategyRelated("propose_income_action")
	if related {
		t.Error("nil adapter: expected false for IsStrategyRelated")
	}

	p := a.GetPortfolio(context.Background())
	if len(p.Entries) != 0 {
		t.Error("nil adapter: expected empty portfolio")
	}
}

func TestGraphAdapter_IsStrategyRelated(t *testing.T) {
	a := &GraphAdapter{}

	related := []string{"propose_income_action", "analyze_opportunity", "schedule_work"}
	for _, action := range related {
		if !a.IsStrategyRelated(action) {
			t.Errorf("expected %q to be strategy-related", action)
		}
	}

	unrelated := []string{"summarize_state", "generate_plan", "noop", "retry_job"}
	for _, action := range unrelated {
		if a.IsStrategyRelated(action) {
			t.Errorf("expected %q to NOT be strategy-related", action)
		}
	}
}

// --- Validation ---

func TestValidStrategyTypes(t *testing.T) {
	expected := []string{"consulting", "automation", "product", "content", "service", "other"}
	for _, tp := range expected {
		if !ValidStrategyTypes[tp] {
			t.Errorf("expected %q to be valid", tp)
		}
	}
	if ValidStrategyTypes["fantasy"] {
		t.Error("expected 'fantasy' to be invalid")
	}
}

func TestValidStatuses(t *testing.T) {
	expected := []string{"active", "paused", "abandoned"}
	for _, s := range expected {
		if !ValidStatuses[s] {
			t.Errorf("expected %q to be valid", s)
		}
	}
}

// --- Score bounds ---

func TestClamp01(t *testing.T) {
	tests := []struct {
		in, out float64
	}{
		{-0.5, 0},
		{0, 0},
		{0.5, 0.5},
		{1.0, 1.0},
		{1.5, 1.0},
	}
	for _, tc := range tests {
		if got := clamp01(tc.in); got != tc.out {
			t.Errorf("clamp01(%.1f) = %.1f, want %.1f", tc.in, got, tc.out)
		}
	}
}

// --- Boost bounds ---

func TestComputeStrategyBoost_BoundsCheck(t *testing.T) {
	// Even extreme values should stay within bounds.
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, Status: StatusActive, ExpectedReturnPerHr: 10000},
	}
	perfMap := map[string]StrategyPerformance{
		"s1": {StrategyID: "s1", TotalRevenue: 100000, TotalTimeSpent: 10},
	}

	boost := ComputeStrategyBoost(TypeConsulting, strategies, perfMap)
	if boost > StrategyPriorityBoostMax {
		t.Errorf("boost %.4f exceeds max %.4f", boost, StrategyPriorityBoostMax)
	}
	if boost < -StrategyPenaltyMax {
		t.Errorf("boost %.4f exceeds negative max %.4f", boost, StrategyPenaltyMax)
	}
}

// --- Allocation with zero scores ---

func TestNormaliseAllocations_ZeroScores(t *testing.T) {
	scores := map[string]float64{
		"a": 0,
		"b": 0,
	}
	allocs := NormaliseAllocations(scores, 20.0)
	for _, hrs := range allocs {
		if hrs < 9.9 || hrs > 10.1 {
			t.Errorf("expected equal distribution for zero scores, got %.2f", hrs)
		}
	}
}
