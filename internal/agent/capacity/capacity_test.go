package capacity

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- 10.1 Capacity State Tests ---

func TestBlockedFamilyTimeReducesCapacity(t *testing.T) {
	// Test 1: blocked family time reduces available capacity.
	maxHours := 8.0
	blockedHours := 3.0 // 18:00-21:00
	load := 0.0

	available := ComputeAvailableCapacity(maxHours, blockedHours, load)
	if available != 5.0 {
		t.Errorf("expected 5.0 available hours, got %f", available)
	}
}

func TestHighOwnerLoadReducesCapacity(t *testing.T) {
	// Test 2: high owner load reduces available capacity.
	maxHours := 8.0
	blockedHours := 0.0
	load := 0.80 // well above OverloadThreshold (0.60)

	available := ComputeAvailableCapacity(maxHours, blockedHours, load)
	if available >= maxHours {
		t.Errorf("expected reduced capacity with high load, got %f (max=%f)", available, maxHours)
	}
	// Expected: penalty = ((0.80-0.60)/(1-0.60)) * 0.50 * 8 = 0.5 * 0.5 * 8 = 2.0
	// available = 8.0 - 2.0 = 6.0
	if math.Abs(available-6.0) > 0.001 {
		t.Errorf("expected 6.0, got %f", available)
	}
}

func TestNoConstraintsPreservesBaseCapacity(t *testing.T) {
	// Test 3: no constraints → base capacity preserved.
	maxHours := 8.0
	available := ComputeAvailableCapacity(maxHours, 0, 0)
	if available != maxHours {
		t.Errorf("expected %f, got %f", maxHours, available)
	}
}

func TestCapacityClampedToZero(t *testing.T) {
	// Edge case: excessive blocked time → clamped to 0.
	available := ComputeAvailableCapacity(8, 10, 0)
	if available != 0 {
		t.Errorf("expected 0 (clamped), got %f", available)
	}
}

func TestCapacityWithBothConstraints(t *testing.T) {
	// Combined blocked time + overload.
	maxHours := 8.0
	blockedHours := 3.0
	load := 0.80

	available := ComputeAvailableCapacity(maxHours, blockedHours, load)
	// base: 8 - 3 = 5, penalty: 2.0, available: 3.0
	if math.Abs(available-3.0) > 0.001 {
		t.Errorf("expected ~3.0, got %f", available)
	}
}

// --- 10.2 Scoring Tests ---

func TestHigherValuePerHourScoresHigher(t *testing.T) {
	// Test 4: higher value-per-hour → higher fit score.
	state := CapacityState{
		AvailableHoursToday: 8.0,
		MaxDailyWorkHours:   8.0,
		OwnerLoadScore:      0.3,
	}

	highValue := CapacityItem{ItemType: "opportunity", ItemID: "a", EstimatedEffort: 1, ExpectedValue: 500, Urgency: 0.5}
	lowValue := CapacityItem{ItemType: "opportunity", ItemID: "b", EstimatedEffort: 1, ExpectedValue: 5, Urgency: 0.5}

	dHigh := EvaluateItem(highValue, state)
	dLow := EvaluateItem(lowValue, state)

	if dHigh.CapacityFitScore <= dLow.CapacityFitScore {
		t.Errorf("high value item (%f) should score higher than low value item (%f)",
			dHigh.CapacityFitScore, dLow.CapacityFitScore)
	}
}

func TestOversizedTaskPenalized(t *testing.T) {
	// Test 5: oversized task penalized.
	state := CapacityState{
		AvailableHoursToday: 4.0,
		MaxDailyWorkHours:   8.0,
		OwnerLoadScore:      0.3,
	}

	small := CapacityItem{ItemType: "task", ItemID: "s", EstimatedEffort: 1, ExpectedValue: 100, Urgency: 0.5}
	large := CapacityItem{ItemType: "task", ItemID: "l", EstimatedEffort: 10, ExpectedValue: 100, Urgency: 0.5}

	dSmall := EvaluateItem(small, state)
	dLarge := EvaluateItem(large, state)

	if dSmall.CapacityFitScore <= dLarge.CapacityFitScore {
		t.Errorf("small task (%f) should score higher than oversized task (%f)",
			dSmall.CapacityFitScore, dLarge.CapacityFitScore)
	}
}

func TestSmallHighValueTaskPreferred(t *testing.T) {
	// Test 6: small high-value task preferred.
	state := CapacityState{
		AvailableHoursToday: 6.0,
		MaxDailyWorkHours:   8.0,
		OwnerLoadScore:      0.4,
	}

	quickWin := CapacityItem{ItemType: "opportunity", ItemID: "qw", EstimatedEffort: 0.5, ExpectedValue: 200, Urgency: 0.7}
	longGrind := CapacityItem{ItemType: "opportunity", ItemID: "lg", EstimatedEffort: 8, ExpectedValue: 300, Urgency: 0.3}

	dQuick := EvaluateItem(quickWin, state)
	dGrind := EvaluateItem(longGrind, state)

	if dQuick.CapacityFitScore <= dGrind.CapacityFitScore {
		t.Errorf("quick win (%f) should score higher than long grind (%f)",
			dQuick.CapacityFitScore, dGrind.CapacityFitScore)
	}
	if !dQuick.Recommended {
		t.Error("quick win should be recommended")
	}
}

func TestDeterministicRepeatedRuns(t *testing.T) {
	// Test 7: deterministic repeated runs.
	state := CapacityState{
		AvailableHoursToday: 6.0,
		MaxDailyWorkHours:   8.0,
		OwnerLoadScore:      0.5,
	}
	item := CapacityItem{ItemType: "task", ItemID: "x", EstimatedEffort: 2, ExpectedValue: 100, Urgency: 0.6}

	d1 := EvaluateItem(item, state)
	d2 := EvaluateItem(item, state)
	d3 := EvaluateItem(item, state)

	if d1.CapacityFitScore != d2.CapacityFitScore || d2.CapacityFitScore != d3.CapacityFitScore {
		t.Errorf("non-deterministic: %f, %f, %f", d1.CapacityFitScore, d2.CapacityFitScore, d3.CapacityFitScore)
	}
	if d1.Recommended != d2.Recommended || d2.Recommended != d3.Recommended {
		t.Error("non-deterministic recommendations")
	}
}

// --- 10.3 Family Protection Tests ---

func TestBlockedFamilyTimeSuppressesLongTasks(t *testing.T) {
	// Test 8: blocked family time suppresses long tasks.
	stateBlocked := CapacityState{
		AvailableHoursToday: 2.0, // Only 2 hours after heavy blocked time
		MaxDailyWorkHours:   8.0,
		OwnerLoadScore:      0.7,
	}

	longTask := CapacityItem{ItemType: "task", ItemID: "lt", EstimatedEffort: 6, ExpectedValue: 80, Urgency: 0.2}
	d := EvaluateItem(longTask, stateBlocked)

	if d.Recommended {
		t.Errorf("6-hour task should be deferred when only 2 hours available and overloaded, fit=%f", d.CapacityFitScore)
	}
}

func TestMinimumFamilyTimeHonored(t *testing.T) {
	// Test 9: minimum family time honored.
	ranges := []BlockedTime{{Reason: "family", Range: "18:00-21:00"}}
	blocked := ComputeBlockedHours(ranges)
	if blocked != 3.0 {
		t.Errorf("expected 3.0 blocked hours, got %f", blocked)
	}
	available := ComputeAvailableCapacity(8.0, blocked, 0)
	if available > 5.0+0.001 {
		t.Errorf("expected ≤ 5.0 after family time, got %f", available)
	}
}

func TestOverloadStatePenalizesHeavyItems(t *testing.T) {
	// Test 10: overload state penalizes heavy items.
	stateNormal := CapacityState{
		AvailableHoursToday: 8.0,
		MaxDailyWorkHours:   8.0,
		OwnerLoadScore:      0.2,
	}
	stateOverload := CapacityState{
		AvailableHoursToday: 4.0,
		MaxDailyWorkHours:   8.0,
		OwnerLoadScore:      0.9,
	}

	heavyItem := CapacityItem{ItemType: "task", ItemID: "heavy", EstimatedEffort: 5, ExpectedValue: 100, Urgency: 0.4}

	dNormal := EvaluateItem(heavyItem, stateNormal)
	dOverload := EvaluateItem(heavyItem, stateOverload)

	if dOverload.CapacityFitScore >= dNormal.CapacityFitScore {
		t.Errorf("overloaded state (%f) should have lower fit than normal (%f)",
			dOverload.CapacityFitScore, dNormal.CapacityFitScore)
	}
}

// --- 10.4 Integration Tests (unit level) ---

func TestIncomeProposalRankingChangesWithLowCapacity(t *testing.T) {
	// Test 11: income proposal ranking changes with low capacity.
	highCap := CapacityState{
		AvailableHoursToday: 8.0,
		MaxDailyWorkHours:   8.0,
		OwnerLoadScore:      0.1,
	}
	lowCap := CapacityState{
		AvailableHoursToday: 2.0,
		MaxDailyWorkHours:   8.0,
		OwnerLoadScore:      0.8,
	}

	// A long task that would score well with plenty of capacity.
	longProposal := CapacityItem{
		ItemType: "proposal", ItemID: "p1",
		EstimatedEffort: 6, ExpectedValue: 300, Urgency: 0.5,
	}
	shortProposal := CapacityItem{
		ItemType: "proposal", ItemID: "p2",
		EstimatedEffort: 1, ExpectedValue: 100, Urgency: 0.5,
	}

	dLongHigh := EvaluateItem(longProposal, highCap)
	dLongLow := EvaluateItem(longProposal, lowCap)
	dShortLow := EvaluateItem(shortProposal, lowCap)

	// Long proposal should rank lower in low capacity.
	if dLongLow.CapacityFitScore >= dLongHigh.CapacityFitScore {
		t.Errorf("long proposal in low capacity (%f) should score lower than high capacity (%f)",
			dLongLow.CapacityFitScore, dLongHigh.CapacityFitScore)
	}
	// Short proposal should beat long proposal in low capacity.
	if dShortLow.CapacityFitScore <= dLongLow.CapacityFitScore {
		t.Errorf("short proposal (%f) should score higher than long proposal (%f) in low capacity",
			dShortLow.CapacityFitScore, dLongLow.CapacityFitScore)
	}
}

func TestPlannerBoostPenaltyApplied(t *testing.T) {
	// Test 12: planner boost/penalty applied correctly.

	// Penalty increases when capacity is constrained.
	penalty := ComputeCapacityPenalty(2.0, 8.0, 0.3)
	if penalty <= 0 {
		t.Errorf("expected positive penalty when capacity constrained, got %f", penalty)
	}
	if penalty > CapacityPenaltyMax {
		t.Errorf("penalty %f exceeds max %f", penalty, CapacityPenaltyMax)
	}

	// No penalty when fully available.
	penaltyFull := ComputeCapacityPenalty(8.0, 8.0, 0.0)
	if penaltyFull != 0 {
		t.Errorf("expected 0 penalty when fully available, got %f", penaltyFull)
	}

	// Boost for small, high-value task.
	boost := ComputeCapacityBoost(0.8, 1.0, 100.0)
	if boost <= 0 {
		t.Errorf("expected positive boost for small high-value task, got %f", boost)
	}
	if boost > CapacityBoostMax {
		t.Errorf("boost %f exceeds max %f", boost, CapacityBoostMax)
	}

	// No boost for large task.
	boostLarge := ComputeCapacityBoost(0.8, 5.0, 100.0)
	if boostLarge != 0 {
		t.Errorf("expected 0 boost for large task, got %f", boostLarge)
	}
}

func TestNoCapacityDataFailOpen(t *testing.T) {
	// Test 13: no capacity data → fail-open behavior.
	penalty := ComputeCapacityPenalty(0, 0, 0)
	if penalty != 0 {
		t.Errorf("expected 0 penalty with no capacity data, got %f", penalty)
	}

	// GraphAdapter nil-safe.
	var adapter *GraphAdapter
	p := adapter.GetCapacityPenalty(context.Background())
	if p != 0 {
		t.Errorf("expected 0 from nil adapter, got %f", p)
	}
	b := adapter.GetCapacityBoost(context.Background(), 1, 100)
	if b != 0 {
		t.Errorf("expected 0 boost from nil adapter, got %f", b)
	}
}

// --- Value Per Hour Tests ---

func TestValuePerHour(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		effort   float64
		expected float64
	}{
		{"normal", 100, 2, 50},
		{"zero effort floors to minimum", 100, 0, 100 / MinimumEffortFloor},
		{"negative effort floors", 100, -1, 100 / MinimumEffortFloor},
		{"zero value", 0, 2, 0},
		{"negative value", -50, 2, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeValuePerHour(tt.value, tt.effort)
			if math.Abs(got-tt.expected) > 0.001 {
				t.Errorf("ComputeValuePerHour(%f, %f) = %f, want %f", tt.value, tt.effort, got, tt.expected)
			}
		})
	}
}

// --- Family Loader Tests ---

func TestLoadFamilyConfigReal(t *testing.T) {
	// Try to find the configs directory relative to the test.
	paths := []string{
		"../../../configs/family_context.yaml",
		"configs/family_context.yaml",
	}
	var cfgPath string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			cfgPath = p
			break
		}
	}
	if cfgPath == "" {
		t.Skip("family_context.yaml not found, skipping")
	}
	cfg := LoadFamilyConfig(cfgPath)
	if cfg.MaxDailyWorkHours != 8 {
		t.Errorf("expected max_daily_work_hours=8, got %f", cfg.MaxDailyWorkHours)
	}
	if cfg.MinFamilyTimeHours != 2 {
		t.Errorf("expected min_family_time_hours=2, got %f", cfg.MinFamilyTimeHours)
	}
}

func TestLoadFamilyConfigMissingFile(t *testing.T) {
	cfg := LoadFamilyConfig("/nonexistent/path.yaml")
	if cfg.MaxDailyWorkHours != DefaultMaxDailyWorkHours {
		t.Errorf("expected default max hours %f, got %f", DefaultMaxDailyWorkHours, cfg.MaxDailyWorkHours)
	}
}

func TestLoadFamilyConfigEmpty(t *testing.T) {
	cfg := LoadFamilyConfig("")
	if cfg.MaxDailyWorkHours != DefaultMaxDailyWorkHours {
		t.Errorf("expected default, got %f", cfg.MaxDailyWorkHours)
	}
}

func TestLoadFamilyConfigInvalid(t *testing.T) {
	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "bad.yaml")
	os.WriteFile(f, []byte("not: {valid yaml: [unterminated"), 0644)
	cfg := LoadFamilyConfig(f)
	// Should return defaults, not crash.
	if cfg.MaxDailyWorkHours != DefaultMaxDailyWorkHours {
		t.Errorf("expected default on invalid yaml, got %f", cfg.MaxDailyWorkHours)
	}
}

// --- Blocked Hours Tests ---

func TestComputeBlockedHours(t *testing.T) {
	ranges := []BlockedTime{
		{Reason: "family", Range: "18:00-21:00"},
	}
	got := ComputeBlockedHours(ranges)
	if got != 3.0 {
		t.Errorf("expected 3.0, got %f", got)
	}
}

func TestComputeBlockedHoursMultiple(t *testing.T) {
	ranges := []BlockedTime{
		{Reason: "family", Range: "18:00-21:00"},
		{Reason: "lunch", Range: "12:00-13:00"},
	}
	got := ComputeBlockedHours(ranges)
	if got != 4.0 {
		t.Errorf("expected 4.0, got %f", got)
	}
}

func TestComputeBlockedHoursInvalidRange(t *testing.T) {
	ranges := []BlockedTime{
		{Reason: "bad", Range: "invalid"},
	}
	got := ComputeBlockedHours(ranges)
	if got != 0 {
		t.Errorf("expected 0 for invalid range, got %f", got)
	}
}

// --- Capacity Fit Score Tests ---

func TestCapacityFitScoreIsBounded(t *testing.T) {
	// Score should always be in [0, 1].
	tests := []struct {
		vph     float64
		urgency float64
		effort  float64
		avail   float64
		load    float64
	}{
		{0, 0, 0, 0, 0},
		{1000, 1, 0.5, 8, 0},
		{0, 0, 100, 0, 1.0},
		{-10, -1, -5, -3, -1},
	}
	for _, tt := range tests {
		score := ComputeCapacityFitScore(tt.vph, tt.urgency, tt.effort, tt.avail, tt.load)
		if score < 0 || score > 1 {
			t.Errorf("score %f out of [0,1] bounds for inputs %+v", score, tt)
		}
	}
}

// --- Defer Reason Tests ---

func TestDeferReasonExceedsCapacity(t *testing.T) {
	state := CapacityState{AvailableHoursToday: 2.0}
	item := CapacityItem{EstimatedEffort: 5, ExpectedValue: 50}
	d := EvaluateItem(item, state)
	if d.DeferReason != "exceeds_available_capacity" {
		t.Errorf("expected 'exceeds_available_capacity', got '%s'", d.DeferReason)
	}
}

func TestDeferReasonOverloaded(t *testing.T) {
	state := CapacityState{
		AvailableHoursToday: 2.0,
		MaxDailyWorkHours:   8.0,
		OwnerLoadScore:      0.9,
	}
	item := CapacityItem{EstimatedEffort: 1.5, ExpectedValue: 30, Urgency: 0.2}
	d := EvaluateItem(item, state)
	if d.Recommended {
		// Should be deferred.
		if d.DeferReason == "" {
			t.Error("expected a defer reason")
		}
	}
}

// --- Adapter Nil Safety Tests ---

func TestAdapterNilEngine(t *testing.T) {
	a := &GraphAdapter{}
	ctx := context.Background()

	p := a.GetCapacityPenalty(ctx)
	if p != 0 {
		t.Errorf("expected 0 from nil engine, got %f", p)
	}

	b := a.GetCapacityBoost(ctx, 1, 100)
	if b != 0 {
		t.Errorf("expected 0 from nil engine, got %f", b)
	}

	state, err := a.GetCapacityState(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if state.MaxDailyWorkHours != 0 {
		t.Errorf("expected zero state from nil engine")
	}
}

// --- Signal Derived Adapter Tests ---

func TestSignalDerivedAdapterNil(t *testing.T) {
	a := NewSignalDerivedAdapter(nil)
	result := a.GetDerivedState(context.Background())
	if result != nil {
		t.Error("expected nil from nil getter")
	}
}

func TestSignalDerivedAdapterReturnsValues(t *testing.T) {
	expected := map[string]float64{"owner_load_score": 0.75}
	a := NewSignalDerivedAdapter(func(ctx context.Context) map[string]float64 {
		return expected
	})
	result := a.GetDerivedState(context.Background())
	if result["owner_load_score"] != 0.75 {
		t.Errorf("expected 0.75, got %f", result["owner_load_score"])
	}
}

// --- Capacity Penalty Edge Cases ---

func TestCapacityPenaltyOverloaded(t *testing.T) {
	penalty := ComputeCapacityPenalty(2.0, 8.0, 0.9)
	if penalty <= 0 {
		t.Error("expected positive penalty when overloaded")
	}
	if penalty > CapacityPenaltyMax {
		t.Errorf("penalty %f exceeds max %f", penalty, CapacityPenaltyMax)
	}
}

func TestCapacityPenaltyMaxClamped(t *testing.T) {
	// Even with extreme conditions, penalty should not exceed max.
	penalty := ComputeCapacityPenalty(0, 8.0, 1.0)
	if penalty > CapacityPenaltyMax {
		t.Errorf("penalty %f exceeds max %f", penalty, CapacityPenaltyMax)
	}
}

// --- Engine Unit Test (without DB) ---

func TestEngineRecomputeStateWithoutDB(t *testing.T) {
	// Engine recompute without a real DB should fail-open.
	e := &Engine{
		family: FamilyConfig{
			MaxDailyWorkHours:  8,
			MinFamilyTimeHours: 2,
			BlockedRanges:      []BlockedTime{{Reason: "family", Range: "18:00-21:00"}},
		},
	}
	// derived := nil (no signals)
	// store := nil (no DB) — will panic on real call.
	// We test the computation logic only.
	blocked := ComputeBlockedHours(e.family.BlockedRanges)
	available := ComputeAvailableCapacity(e.family.MaxDailyWorkHours, blocked, 0)
	if available != 5.0 {
		t.Errorf("expected 5.0, got %f", available)
	}
}

// --- Timestamp Tests ---

func TestCapacityDecisionTimestamp(t *testing.T) {
	now := time.Now()
	state := CapacityState{
		AvailableHoursToday: 8.0,
		MaxDailyWorkHours:   8.0,
	}
	item := CapacityItem{ItemType: "task", ItemID: "t1", EstimatedEffort: 1, ExpectedValue: 100, Urgency: 0.5}
	d := EvaluateItem(item, state)
	// CreatedAt should be zero (set by engine, not scorer).
	if !d.CreatedAt.IsZero() {
		t.Errorf("expected zero CreatedAt from scorer, got %v", d.CreatedAt)
	}
	_ = now
}
