package portfolio

import (
	"context"
	"testing"
)

// ----------------------------------------------------------------
// 1. Opportunity maps to correct strategy
// ----------------------------------------------------------------

func TestMapOpportunityToStrategy(t *testing.T) {
	tests := []struct {
		oppType  string
		expected string
	}{
		// Spec-required mappings.
		{"consulting_lead", TypeConsulting},
		{"automation_candidate", TypeAutomationServices},
		{"product_feature_candidate", TypeProduct},
		{"content_opportunity", TypeContent},
		{"cost_saving_candidate", TypeCostEfficiency},
		// 2. Resale/repackage deterministic mapping.
		{"resale_or_repackage_candidate", TypeAutomationServices},
		// Backward-compatible short forms.
		{"consulting", TypeConsulting},
		{"automation", TypeAutomation},
		{"automation_services", TypeAutomationServices},
		{"service", TypeService},
		{"content", TypeContent},
		{"product", TypeProduct},
		{"cost_efficiency", TypeCostEfficiency},
		{"other", TypeOther},
		// 3. Unknown type handled safely.
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

// ----------------------------------------------------------------
// Allocation & normalisation
// ----------------------------------------------------------------

func TestNormaliseAllocations_Basic(t *testing.T) {
	scores := map[string]float64{
		"a": 0.6,
		"b": 0.4,
	}
	allocs, weights := NormaliseAllocations(scores, 40.0)

	total := 0.0
	for _, h := range allocs {
		total += h
	}
	if total < 39.9 || total > 40.1 {
		t.Errorf("total allocated = %.2f, want ~40.0", total)
	}
	wTotal := 0.0
	for _, w := range weights {
		wTotal += w
	}
	if wTotal < 0.99 || wTotal > 1.01 {
		t.Errorf("weights total = %.4f, want ~1.0", wTotal)
	}
}

// 9. Concentration cap enforced.
func TestNormaliseAllocations_MinMaxConstraints(t *testing.T) {
	scores := map[string]float64{
		"a": 0.90,
		"b": 0.01,
		"c": 0.01,
		"d": 0.01,
		"e": 0.01,
	}
	allocs, _ := NormaliseAllocations(scores, 100.0)

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
	allocs, weights := NormaliseAllocations(map[string]float64{}, 40.0)
	if len(allocs) != 0 || len(weights) != 0 {
		t.Errorf("expected empty allocations, got %d/%d", len(allocs), len(weights))
	}
}

func TestNormaliseAllocations_ZeroHours(t *testing.T) {
	allocs, _ := NormaliseAllocations(map[string]float64{"a": 0.5}, 0)
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
	allocs, _ := NormaliseAllocations(scores, 30.0)

	for id, hrs := range allocs {
		if hrs < 9.9 || hrs > 10.1 {
			t.Errorf("strategy %s expected ~10h, got %.2f", id, hrs)
		}
	}
}

func TestNormaliseAllocations_ZeroScores(t *testing.T) {
	scores := map[string]float64{
		"a": 0,
		"b": 0,
	}
	allocs, _ := NormaliseAllocations(scores, 20.0)
	for _, hrs := range allocs {
		if hrs < 9.9 || hrs > 10.1 {
			t.Errorf("expected equal distribution for zero scores, got %.2f", hrs)
		}
	}
}

// ----------------------------------------------------------------
// 5. ROI per hour computed correctly
// ----------------------------------------------------------------

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

// ----------------------------------------------------------------
// 6. Conversion rate computed correctly
// ----------------------------------------------------------------

func TestComputeConversionRate(t *testing.T) {
	tests := []struct {
		won, total int
		expected   float64
	}{
		{5, 10, 0.5},
		{0, 10, 0},
		{3, 0, 0},
		{10, 10, 1.0},
	}
	for _, tc := range tests {
		got := ComputeConversionRate(tc.won, tc.total)
		if got != tc.expected {
			t.Errorf("ComputeConversionRate(%d, %d) = %.4f, want %.4f", tc.won, tc.total, got, tc.expected)
		}
	}
}

// ----------------------------------------------------------------
// 4. Verified revenue contributes to performance (via ROI)
// ----------------------------------------------------------------

func TestVerifiedRevenueContributesToROI(t *testing.T) {
	roi := ComputeROI(5000, 50) // 100 $/hr
	if roi != 100 {
		t.Errorf("ROI from verified revenue: got %.2f, want 100", roi)
	}
}

// ----------------------------------------------------------------
// Strategy boost: low ROI penalised, high ROI boosted
// ----------------------------------------------------------------

func TestComputeStrategyBoost_LowROI(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, Status: StatusActive, ExpectedReturnPerHr: 30},
	}
	perfMap := map[string]StrategyPerformance{
		"s1": {StrategyID: "s1", TotalVerifiedRevenue: 25, TotalEstimatedHours: 10, ROIPerHour: 2.5},
	}

	boost := ComputeStrategyBoost(TypeConsulting, strategies, perfMap)
	if boost >= 0 {
		t.Errorf("expected negative boost for low ROI, got %.4f", boost)
	}
	if boost < -StrategyPenaltyMax {
		t.Errorf("penalty %.4f exceeds max %.4f", boost, StrategyPenaltyMax)
	}
}

func TestComputeStrategyBoost_HighROI(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, Status: StatusActive, ExpectedReturnPerHr: 80},
	}
	perfMap := map[string]StrategyPerformance{
		"s1": {StrategyID: "s1", TotalVerifiedRevenue: 600, TotalEstimatedHours: 10, ROIPerHour: 60},
	}

	boost := ComputeStrategyBoost(TypeConsulting, strategies, perfMap)
	if boost <= 0 {
		t.Errorf("expected positive boost for high ROI, got %.4f", boost)
	}
	if boost > StrategyPriorityBoostMax {
		t.Errorf("boost %.4f exceeds max %.4f", boost, StrategyPriorityBoostMax)
	}
}

// 13. No portfolio data → no effect.
func TestComputeStrategyBoost_NoData(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, Status: StatusActive, ExpectedReturnPerHr: 30},
	}
	perfMap := map[string]StrategyPerformance{}

	boost := ComputeStrategyBoost(TypeConsulting, strategies, perfMap)
	if boost != 0 {
		t.Errorf("expected 0 boost without data and low expected return, got %.4f", boost)
	}
}

// 11. High-performing strategy yields bounded boost.
func TestComputeStrategyBoost_HighExpectedReturn(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeAutomation, Status: StatusActive, ExpectedReturnPerHr: 80},
	}
	perfMap := map[string]StrategyPerformance{}

	boost := ComputeStrategyBoost(TypeAutomation, strategies, perfMap)
	if boost <= 0 {
		t.Errorf("expected positive boost from high expected return, got %.4f", boost)
	}
	if boost > StrategyPriorityBoostMax {
		t.Errorf("boost %.4f exceeds max %.4f", boost, StrategyPriorityBoostMax)
	}
}

func TestComputeStrategyBoost_InactiveStrategy(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, Status: StatusPaused, ExpectedReturnPerHr: 200},
	}
	boost := ComputeStrategyBoost(TypeConsulting, strategies, nil)
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

func TestComputeStrategyBoost_BoundsCheck(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, Status: StatusActive, ExpectedReturnPerHr: 10000},
	}
	perfMap := map[string]StrategyPerformance{
		"s1": {StrategyID: "s1", TotalVerifiedRevenue: 100000, TotalEstimatedHours: 10},
	}

	boost := ComputeStrategyBoost(TypeConsulting, strategies, perfMap)
	if boost > StrategyPriorityBoostMax {
		t.Errorf("boost %.4f exceeds max %.4f", boost, StrategyPriorityBoostMax)
	}
	if boost < -StrategyPenaltyMax {
		t.Errorf("boost %.4f exceeds negative max %.4f", boost, StrategyPenaltyMax)
	}
}

// 12. Over-allocated weak strategy penalised.
func TestComputeStrategyBoost_OverAllocatedWeakPenalised(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, Status: StatusActive, ExpectedReturnPerHr: 5, StabilityScore: 0.3},
	}
	perfMap := map[string]StrategyPerformance{
		"s1": {StrategyID: "s1", TotalVerifiedRevenue: 20, TotalEstimatedHours: 10, ROIPerHour: 2.0},
	}
	// ROI = 2.0 < LowROIThreshold (10) → penalty.
	boost := ComputeStrategyBoost(TypeConsulting, strategies, perfMap)
	if boost >= 0 {
		t.Errorf("expected penalty for over-allocated weak strategy, got %.4f", boost)
	}
}

// ----------------------------------------------------------------
// Diversification
// ----------------------------------------------------------------

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

// ----------------------------------------------------------------
// 7. High ROI strategy gets more allocation
// ----------------------------------------------------------------

func TestComputeAllocationScores_HighROIGetsMore(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, ExpectedReturnPerHr: 100, StabilityScore: 0.8, TimeToFirstValue: 10},
		{ID: "s2", Type: TypeContent, ExpectedReturnPerHr: 20, StabilityScore: 0.5, TimeToFirstValue: 50},
	}
	scores := ComputeAllocationScores(strategies, nil, 0.0, false)
	if scores["s1"] <= scores["s2"] {
		t.Errorf("s1 (high ROI) should score > s2 (low ROI): s1=%.4f s2=%.4f", scores["s1"], scores["s2"])
	}
}

// 8. High stability favored under pressure.
func TestComputeAllocationScores_HighStabilityFavoredUnderPressure(t *testing.T) {
	strategies := []Strategy{
		{ID: "stable", Type: TypeConsulting, ExpectedReturnPerHr: 60, StabilityScore: 0.95, TimeToFirstValue: 10},
		{ID: "risky", Type: TypeContent, ExpectedReturnPerHr: 60, StabilityScore: 0.2, TimeToFirstValue: 10},
	}
	scores := ComputeAllocationScores(strategies, nil, 0.9, true)
	if scores["stable"] <= scores["risky"] {
		t.Errorf("stable strategy should score higher under pressure: stable=%.4f risky=%.4f", scores["stable"], scores["risky"])
	}
}

func TestComputeAllocationScores_WithRealPerformance(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, ExpectedReturnPerHr: 50, StabilityScore: 0.7, TimeToFirstValue: 20},
	}
	perfMap := map[string]StrategyPerformance{
		"s1": {StrategyID: "s1", TotalVerifiedRevenue: 500, TotalEstimatedHours: 10},
	}

	scores := ComputeAllocationScores(strategies, perfMap, 0.0, false)
	if scores["s1"] <= 0 {
		t.Errorf("expected positive score with real performance, got %.4f", scores["s1"])
	}
}

func TestComputeAllocationScores_HighPressure(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, ExpectedReturnPerHr: 100, StabilityScore: 0.8, TimeToFirstValue: 10},
		{ID: "s2", Type: TypeContent, ExpectedReturnPerHr: 20, StabilityScore: 0.9, TimeToFirstValue: 100},
	}

	lowPressure := ComputeAllocationScores(strategies, nil, 0.1, false)
	highPressure := ComputeAllocationScores(strategies, nil, 0.9, false)

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

// 10. Low capacity shifts allocation to short-cycle strategies.
func TestComputeAllocationScores_LowCapacityFavorsShortCycle(t *testing.T) {
	strategies := []Strategy{
		{ID: "fast", Type: TypeConsulting, ExpectedReturnPerHr: 60, StabilityScore: 0.8, TimeToFirstValue: 5},
		{ID: "slow", Type: TypeProduct, ExpectedReturnPerHr: 60, StabilityScore: 0.8, TimeToFirstValue: 150},
	}
	// Under high pressure with family priority, short-cycle should dominate.
	scores := ComputeAllocationScores(strategies, nil, 0.8, true)
	if scores["fast"] <= scores["slow"] {
		t.Errorf("fast strategy should score higher under low capacity: fast=%.4f slow=%.4f", scores["fast"], scores["slow"])
	}
}

// Family-safe: slow speculative penalised under family constraint.
func TestComputeAllocationScores_FamilySafePenalisesSpeculative(t *testing.T) {
	strategies := []Strategy{
		{ID: "safe", Type: TypeConsulting, ExpectedReturnPerHr: 50, StabilityScore: 0.9, TimeToFirstValue: 10},
		{ID: "spec", Type: TypeProduct, ExpectedReturnPerHr: 50, StabilityScore: 0.2, TimeToFirstValue: 150},
	}
	// Family priority high → speculative penalised.
	scores := ComputeAllocationScores(strategies, nil, 0.7, true)
	if scores["safe"] <= scores["spec"] {
		t.Errorf("safe strategy should beat speculative under family priority: safe=%.4f spec=%.4f", scores["safe"], scores["spec"])
	}
}

// ----------------------------------------------------------------
// Strategic signals
// ----------------------------------------------------------------

func TestDetectSignals_Underperforming(t *testing.T) {
	strategies := []Strategy{
		{ID: "s1", Type: TypeConsulting, Status: StatusActive, ExpectedReturnPerHr: 30},
	}
	perfMap := map[string]StrategyPerformance{
		"s1": {StrategyID: "s1", TotalVerifiedRevenue: 30, TotalEstimatedHours: 10},
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
		{ID: "s1", Type: TypeConsulting, Status: StatusActive, ExpectedReturnPerHr: 80, StabilityScore: 0.7},
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
	perfMap := map[string]StrategyPerformance{
		"s1": {StrategyID: "s1", TotalVerifiedRevenue: 5, TotalEstimatedHours: 2},
	}
	allocs := map[string]float64{"s1": 10}

	signals := DetectSignals(strategies, perfMap, allocs, 40)
	for _, s := range signals {
		if s.SignalType == "underperforming" {
			t.Error("should not produce underperforming signal with insufficient data")
		}
	}
}

// ----------------------------------------------------------------
// Graph adapter nil-safety
// ----------------------------------------------------------------

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

	allocs, err := a.GetAllocations(context.Background())
	if err != nil || allocs != nil {
		t.Error("nil adapter: expected nil allocations and no error")
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

// ----------------------------------------------------------------
// Validation
// ----------------------------------------------------------------

func TestValidStrategyTypes(t *testing.T) {
	expected := []string{
		"consulting", "automation", "automation_services",
		"product", "content", "cost_efficiency", "service", "other",
	}
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

// ----------------------------------------------------------------
// PortfolioSummary fields
// ----------------------------------------------------------------

func TestPortfolioSummary_Fields(t *testing.T) {
	s := PortfolioSummary{
		TotalActiveStrategies: 3,
		TotalAllocatedHours:   30,
		DominantStrategyID:    "s1",
		DiversificationScore:  0.85,
	}
	if s.TotalActiveStrategies != 3 {
		t.Errorf("TotalActiveStrategies = %d, want 3", s.TotalActiveStrategies)
	}
	if s.DominantStrategyID != "s1" {
		t.Errorf("DominantStrategyID = %q, want s1", s.DominantStrategyID)
	}
}

// ----------------------------------------------------------------
// AllocationWeight propagation
// ----------------------------------------------------------------

func TestAllocationWeight_ComputedByNormalise(t *testing.T) {
	// Use 4 strategies so min/max constraints don't force equal weights.
	scores := map[string]float64{
		"a": 0.6,
		"b": 0.3,
		"c": 0.05,
		"d": 0.05,
	}
	_, weights := NormaliseAllocations(scores, 40.0)
	if weights["a"] <= weights["b"] {
		t.Errorf("expected a weight > b weight: a=%.4f b=%.4f", weights["a"], weights["b"])
	}
	total := 0.0
	for _, w := range weights {
		total += w
	}
	if total < 0.99 || total > 1.01 {
		t.Errorf("weights should sum to ~1.0, got %.4f", total)
	}
}

// ----------------------------------------------------------------
// Speed component (time-to-first-value)
// ----------------------------------------------------------------

func TestSpeedComponent_FasterIsBetter(t *testing.T) {
	strategies := []Strategy{
		{ID: "fast", Type: TypeConsulting, ExpectedReturnPerHr: 50, StabilityScore: 0.5, TimeToFirstValue: 10},
		{ID: "slow", Type: TypeConsulting, ExpectedReturnPerHr: 50, StabilityScore: 0.5, TimeToFirstValue: 180},
	}
	scores := ComputeAllocationScores(strategies, nil, 0.0, false)
	if scores["fast"] <= scores["slow"] {
		t.Errorf("faster TTF should score higher: fast=%.4f slow=%.4f", scores["fast"], scores["slow"])
	}
}

// ----------------------------------------------------------------
// Cost efficiency strategy type
// ----------------------------------------------------------------

func TestCostEfficiencyMapping(t *testing.T) {
	got := MapOpportunityToStrategy("cost_saving_candidate")
	if got != TypeCostEfficiency {
		t.Errorf("cost_saving_candidate should map to cost_efficiency, got %q", got)
	}
}
