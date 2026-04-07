package actionmemory

import (
	"math"
	"testing"
	"time"
)

// --- Hierarchy Level Tests ---

func TestHierarchyLevel_String(t *testing.T) {
	tests := []struct {
		level HierarchyLevel
		want  string
	}{
		{HierarchyExact, "L0_exact"},
		{HierarchyReduced, "L1_reduced"},
		{HierarchyGeneralized, "L2_generalized"},
		{HierarchyGlobal, "L3_global"},
		{HierarchyLevel(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("HierarchyLevel(%d).String() = %s, want %s", tt.level, got, tt.want)
		}
	}
}

func TestHierarchySpecificityBonus(t *testing.T) {
	if hierarchySpecificityBonus(HierarchyExact) != hierBonusExact {
		t.Error("L0 bonus wrong")
	}
	if hierarchySpecificityBonus(HierarchyReduced) != hierBonusReduced {
		t.Error("L1 bonus wrong")
	}
	if hierarchySpecificityBonus(HierarchyGeneralized) != hierBonusGeneralized {
		t.Error("L2 bonus wrong")
	}
	if hierarchySpecificityBonus(HierarchyGlobal) != hierBonusGlobal {
		t.Error("L3 bonus wrong")
	}
	// Bonuses must be strictly decreasing.
	if hierBonusExact <= hierBonusReduced || hierBonusReduced <= hierBonusGeneralized || hierBonusGeneralized <= hierBonusGlobal {
		t.Error("hierarchy bonuses must be strictly decreasing")
	}
}

// --- Record Aggregator Tests ---

func TestRecordAggregator_Empty(t *testing.T) {
	var agg recordAggregator
	now := time.Now().UTC()
	c := agg.build(HierarchyReduced, "retry_job", []string{"action_type"}, now)
	if c != nil {
		t.Error("empty aggregator should return nil")
	}
}

func TestRecordAggregator_SingleProvider(t *testing.T) {
	now := time.Now().UTC()
	var agg recordAggregator
	agg.addProvider(&ProviderContextMemoryRecord{
		ActionType:  "retry_job",
		TotalRuns:   10,
		SuccessRuns: 8,
		FailureRuns: 1,
		NeutralRuns: 1,
		LastUpdated: now.Add(-1 * time.Hour),
	})
	c := agg.build(HierarchyExact, "retry_job", []string{"action_type"}, now)
	if c == nil {
		t.Fatal("expected non-nil candidate")
	}
	if c.SampleSize != 10 {
		t.Errorf("sample size = %d, want 10", c.SampleSize)
	}
	if c.Level != HierarchyExact {
		t.Errorf("level = %d, want L0", c.Level)
	}
}

func TestRecordAggregator_MultipleContextRecords(t *testing.T) {
	now := time.Now().UTC()
	var agg recordAggregator
	agg.addContext(&ContextMemoryRecord{
		TotalRuns: 5, SuccessRuns: 3, FailureRuns: 1, NeutralRuns: 1,
		LastUpdated: now.Add(-2 * time.Hour),
	})
	agg.addContext(&ContextMemoryRecord{
		TotalRuns: 10, SuccessRuns: 7, FailureRuns: 2, NeutralRuns: 1,
		LastUpdated: now.Add(-30 * time.Minute),
	})
	c := agg.build(HierarchyReduced, "retry_job", []string{"action_type", "goal_type"}, now)
	if c == nil {
		t.Fatal("expected non-nil candidate")
	}
	if c.SampleSize != 15 {
		t.Errorf("sample size = %d, want 15", c.SampleSize)
	}
	// Latest update should be the more recent one.
	assertFloatNear(t, "recency_weight", c.RecencyWeight, 1.0, 0.01)
}

// --- GatherHierarchicalCandidates Tests ---

func TestGatherHierarchical_AllLevels(t *testing.T) {
	now := time.Now().UTC()

	provRecords := []ProviderContextMemoryRecord{{
		ActionType:    "retry_job",
		GoalType:      "reduce_retry_rate",
		ProviderName:  "ollama",
		ModelRole:     "fast",
		FailureBucket: "low",
		BacklogBucket: "low",
		TotalRuns:     10,
		SuccessRuns:   8,
		FailureRuns:   2,
		SuccessRate:   0.80,
		FailureRate:   0.20,
		LastUpdated:   now.Add(-1 * time.Hour),
	}}

	ctxRecords := []ContextMemoryRecord{{
		ActionType:    "retry_job",
		GoalType:      "reduce_retry_rate",
		FailureBucket: "low",
		BacklogBucket: "medium",
		TotalRuns:     15,
		SuccessRuns:   12,
		FailureRuns:   3,
		SuccessRate:   0.80,
		FailureRate:   0.20,
		LastUpdated:   now.Add(-2 * time.Hour),
	}}

	globalFb := map[string]ActionFeedback{
		"retry_job": {
			ActionType:     "retry_job",
			SampleSize:     30,
			SuccessRate:    0.75,
			FailureRate:    0.25,
			Recommendation: RecommendPreferAction,
			LastUpdated:    now.Add(-6 * time.Hour),
		},
	}

	candidates := GatherHierarchicalCandidates(
		provRecords, ctxRecords, globalFb,
		"retry_job", "reduce_retry_rate",
		"ollama", "fast",
		"low", "low",
		now,
	)

	// Should have L0, L1, L2, L3 = 4 candidates.
	if len(candidates) != 4 {
		t.Errorf("expected 4 candidates, got %d", len(candidates))
		for i, c := range candidates {
			t.Logf("candidate %d: %s conf=%.3f samples=%d", i, c.LevelName, c.Confidence, c.SampleSize)
		}
	}

	// Verify levels are in order.
	levels := make(map[HierarchyLevel]bool)
	for _, c := range candidates {
		levels[c.Level] = true
	}
	for _, l := range []HierarchyLevel{HierarchyExact, HierarchyReduced, HierarchyGeneralized, HierarchyGlobal} {
		if !levels[l] {
			t.Errorf("missing level %s", l)
		}
	}
}

func TestGatherHierarchical_NoProvider(t *testing.T) {
	now := time.Now().UTC()

	ctxRecords := []ContextMemoryRecord{{
		ActionType:    "retry_job",
		GoalType:      "reduce_retry_rate",
		FailureBucket: "low",
		BacklogBucket: "low",
		TotalRuns:     10,
		SuccessRuns:   7,
		FailureRuns:   3,
		SuccessRate:   0.70,
		FailureRate:   0.30,
		LastUpdated:   now.Add(-1 * time.Hour),
	}}

	globalFb := map[string]ActionFeedback{
		"retry_job": {
			ActionType:     "retry_job",
			SampleSize:     20,
			SuccessRate:    0.75,
			FailureRate:    0.25,
			Recommendation: RecommendPreferAction,
			LastUpdated:    now.Add(-6 * time.Hour),
		},
	}

	candidates := GatherHierarchicalCandidates(
		nil, ctxRecords, globalFb,
		"retry_job", "reduce_retry_rate",
		"", "", // no provider
		"low", "low",
		now,
	)

	// L0 (context exact), L1 (context with failure), L2 (action+goal), L3 (global) = 4.
	if len(candidates) != 4 {
		t.Errorf("expected 4 candidates, got %d", len(candidates))
		for i, c := range candidates {
			t.Logf("candidate %d: %s conf=%.3f samples=%d", i, c.LevelName, c.Confidence, c.SampleSize)
		}
	}
}

func TestGatherHierarchical_NoExactMatch(t *testing.T) {
	now := time.Now().UTC()

	// Records don't match the exact query dims but match reduced/generalized.
	ctxRecords := []ContextMemoryRecord{{
		ActionType:    "retry_job",
		GoalType:      "reduce_retry_rate",
		FailureBucket: "high", // different from query "low"
		BacklogBucket: "medium",
		TotalRuns:     20,
		SuccessRuns:   4,
		FailureRuns:   14,
		SuccessRate:   0.20,
		FailureRate:   0.70,
		LastUpdated:   now.Add(-30 * time.Minute),
	}}

	globalFb := map[string]ActionFeedback{
		"retry_job": {
			ActionType:     "retry_job",
			SampleSize:     30,
			SuccessRate:    0.25,
			FailureRate:    0.55,
			Recommendation: RecommendAvoidAction,
			LastUpdated:    now.Add(-1 * time.Hour),
		},
	}

	candidates := GatherHierarchicalCandidates(
		nil, ctxRecords, globalFb,
		"retry_job", "reduce_retry_rate",
		"", "",
		"low", "low", // no exact match for failure=low
		now,
	)

	// No L0 (no exact match), no L1 (failure_bucket doesn't match "low"),
	// L2 (action+goal matches), L3 (global).
	hasL0 := false
	for _, c := range candidates {
		if c.Level == HierarchyExact {
			hasL0 = true
		}
	}
	if hasL0 {
		t.Error("should not have L0 candidate when no exact match")
	}
	if len(candidates) < 2 {
		t.Errorf("expected at least L2+L3 candidates, got %d", len(candidates))
	}
}

// --- L1 Aggregation Tests ---

func TestGatherHierarchical_L1Aggregates(t *testing.T) {
	now := time.Now().UTC()

	// Two records with same action+goal+failure but different backlog/model.
	provRecords := []ProviderContextMemoryRecord{
		{
			ActionType: "retry_job", GoalType: "reduce_retry_rate",
			ProviderName: "ollama", ModelRole: "fast",
			FailureBucket: "high", BacklogBucket: "low",
			TotalRuns: 8, SuccessRuns: 2, FailureRuns: 5, NeutralRuns: 1,
			SuccessRate: 0.25, FailureRate: 0.625,
			LastUpdated: now.Add(-1 * time.Hour),
		},
		{
			ActionType: "retry_job", GoalType: "reduce_retry_rate",
			ProviderName: "ollama", ModelRole: "default",
			FailureBucket: "high", BacklogBucket: "medium",
			TotalRuns: 12, SuccessRuns: 3, FailureRuns: 8, NeutralRuns: 1,
			SuccessRate: 0.25, FailureRate: 0.667,
			LastUpdated: now.Add(-30 * time.Minute),
		},
	}

	candidates := GatherHierarchicalCandidates(
		provRecords, nil, nil,
		"retry_job", "reduce_retry_rate",
		"ollama", "fast",
		"high", "low",
		now,
	)

	// L1 should aggregate both records (both match action+goal+failure=high).
	var l1 *HierarchicalCandidate
	for i := range candidates {
		if candidates[i].Level == HierarchyReduced {
			l1 = &candidates[i]
		}
	}
	if l1 == nil {
		t.Fatal("expected L1 candidate")
	}
	if l1.SampleSize != 20 {
		t.Errorf("L1 sample size = %d, want 20 (8+12)", l1.SampleSize)
	}
}

// --- Resolution Tests ---

func TestResolveHierarchical_Empty(t *testing.T) {
	best, all := ResolveHierarchicalFeedback(nil)
	if best != nil || all != nil {
		t.Error("expected nil for empty candidates")
	}
}

func TestResolveHierarchical_AllInsufficientData(t *testing.T) {
	candidates := []HierarchicalCandidate{
		{Level: HierarchyExact, Recommendation: RecommendInsufficientData, FinalScore: 0.5},
		{Level: HierarchyGlobal, Recommendation: RecommendNeutral, FinalScore: 0.4},
	}
	best, all := ResolveHierarchicalFeedback(candidates)
	if best != nil {
		t.Error("expected nil best when all non-actionable")
	}
	if len(all) != 2 {
		t.Errorf("expected all candidates returned, got %d", len(all))
	}
}

// --- Spec Test 1: Weak exact vs strong generalized → generalized wins ---

func TestHierarchical_WeakExactVsStrongGeneralized(t *testing.T) {
	now := time.Now().UTC()

	// L0: exact match but tiny sample, stale.
	// L2: generalized with strong sample, fresh.
	provRecords := []ProviderContextMemoryRecord{
		{
			ActionType: "retry_job", GoalType: "reduce_retry_rate",
			ProviderName: "ollama", ModelRole: "fast",
			FailureBucket: "high", BacklogBucket: "low",
			TotalRuns: 2, SuccessRuns: 1, FailureRuns: 1, NeutralRuns: 0,
			SuccessRate: 0.50, FailureRate: 0.50,
			LastUpdated: now.Add(-10 * 24 * time.Hour), // stale
		},
		{
			ActionType: "retry_job", GoalType: "reduce_retry_rate",
			ProviderName: "openrouter", ModelRole: "default",
			FailureBucket: "medium", BacklogBucket: "medium",
			TotalRuns: 30, SuccessRuns: 6, FailureRuns: 21, NeutralRuns: 3,
			SuccessRate: 0.20, FailureRate: 0.70,
			LastUpdated: now.Add(-30 * time.Minute), // fresh
		},
	}

	candidates := GatherHierarchicalCandidates(
		provRecords, nil, nil,
		"retry_job", "reduce_retry_rate",
		"ollama", "fast",
		"high", "low",
		now,
	)

	best, _ := ResolveHierarchicalFeedback(candidates)
	if best == nil {
		t.Fatal("expected non-nil best")
	}

	// The generalized (L2) candidate should win because it has strong
	// evidence (30 samples, fresh) while the exact match is tiny and stale.
	if best.Level == HierarchyExact {
		t.Errorf("weak exact should NOT win over strong generalized; got level=%s conf=%.3f", best.LevelName, best.Confidence)
	}
	// The winning level should have higher confidence.
	if best.Confidence < 0.5 {
		t.Errorf("winning candidate should have reasonable confidence, got %.3f", best.Confidence)
	}
}

// --- Spec Test 2: Strong exact vs weak generalized → exact wins ---

func TestHierarchical_StrongExactVsWeakGeneralized(t *testing.T) {
	now := time.Now().UTC()

	// L0: exact match with strong avoid signal (fresh, many samples).
	// Other records at L2 level dilute aggregate to neutral → filtered out.
	// L3: weak global prefer (stale, small sample).
	// Expected: specific level wins (not global).
	provRecords := []ProviderContextMemoryRecord{
		{
			ActionType: "retry_job", GoalType: "reduce_retry_rate",
			ProviderName: "ollama", ModelRole: "fast",
			FailureBucket: "high", BacklogBucket: "low",
			TotalRuns: 20, SuccessRuns: 4, FailureRuns: 14, NeutralRuns: 2,
			SuccessRate: 0.20, FailureRate: 0.70,
			LastUpdated: now.Add(-20 * time.Minute), // fresh
		},
		{
			// Different provider/model/failure: dilutes L2 aggregate to neutral.
			ActionType: "retry_job", GoalType: "reduce_retry_rate",
			ProviderName: "openrouter", ModelRole: "default",
			FailureBucket: "low", BacklogBucket: "medium",
			TotalRuns: 30, SuccessRuns: 18, FailureRuns: 5, NeutralRuns: 7,
			SuccessRate: 0.60, FailureRate: 0.17,
			LastUpdated: now.Add(-1 * time.Hour),
		},
	}

	globalFb := map[string]ActionFeedback{
		"retry_job": {
			ActionType:     "retry_job",
			SampleSize:     6,
			SuccessRate:    0.83,
			FailureRate:    0.10,
			Recommendation: RecommendPreferAction,
			LastUpdated:    now.Add(-10 * 24 * time.Hour), // stale
		},
	}

	candidates := GatherHierarchicalCandidates(
		provRecords, nil, globalFb,
		"retry_job", "reduce_retry_rate",
		"ollama", "fast",
		"high", "low",
		now,
	)

	best, _ := ResolveHierarchicalFeedback(candidates)
	if best == nil {
		t.Fatal("expected non-nil best")
	}

	// The specific levels (L0/L1) have strong avoid signal. L2 aggregate is
	// diluted (neutral → filtered). L3 is weak prefer. Best should be the
	// strong specific avoid, not the weak global prefer.
	if best.Level == HierarchyGlobal {
		t.Errorf("strong specific should beat weak global; got level=%s conf=%.3f",
			best.LevelName, best.Confidence)
	}
	if best.Recommendation != RecommendAvoidAction {
		t.Errorf("expected avoid_action from specific evidence; got %s at level=%s",
			best.Recommendation, best.LevelName)
	}
}

// --- Spec Test 3: No exact → generalized used instead of global ---

func TestHierarchical_NoExactUsesGeneralized(t *testing.T) {
	now := time.Now().UTC()

	// No records matching exact dims. L2 (action+goal) has strong data, L3 has stale data.
	ctxRecords := []ContextMemoryRecord{
		{
			ActionType:    "retry_job",
			GoalType:      "reduce_retry_rate",
			FailureBucket: "medium", // different from query "low"
			BacklogBucket: "high",
			TotalRuns:     20,
			SuccessRuns:   4,
			FailureRuns:   14,
			NeutralRuns:   2,
			SuccessRate:   0.20,
			FailureRate:   0.70,
			LastUpdated:   now.Add(-1 * time.Hour), // recent
		},
	}

	globalFb := map[string]ActionFeedback{
		"retry_job": {
			ActionType:     "retry_job",
			SampleSize:     10,
			SuccessRate:    0.30,
			FailureRate:    0.50,
			Recommendation: RecommendAvoidAction,
			LastUpdated:    now.Add(-10 * 24 * time.Hour), // stale
		},
	}

	candidates := GatherHierarchicalCandidates(
		nil, ctxRecords, globalFb,
		"retry_job", "reduce_retry_rate",
		"", "",
		"low", "low", // no exact match
		now,
	)

	best, _ := ResolveHierarchicalFeedback(candidates)
	if best == nil {
		t.Fatal("expected non-nil best")
	}

	// L2 (generalized, action+goal) should be chosen over L3 (global)
	// because L2 is fresher and has strong samples.
	if best.Level == HierarchyGlobal {
		t.Errorf("generalized should be preferred over stale global; got level=%s conf=%.3f", best.LevelName, best.Confidence)
	}
	if best.Level == HierarchyExact {
		t.Error("should not have exact match")
	}
}

// --- Spec Test 4: Hierarchical resolution is deterministic ---

func TestHierarchical_DeterministicResolution(t *testing.T) {
	now := time.Now().UTC()

	provRecords := []ProviderContextMemoryRecord{{
		ActionType: "retry_job", GoalType: "reduce_retry_rate",
		ProviderName: "ollama", ModelRole: "fast",
		FailureBucket: "high", BacklogBucket: "low",
		TotalRuns: 15, SuccessRuns: 3, FailureRuns: 10, NeutralRuns: 2,
		SuccessRate: 0.20, FailureRate: 0.67,
		LastUpdated: now.Add(-2 * time.Hour),
	}}

	globalFb := map[string]ActionFeedback{
		"retry_job": {
			ActionType:     "retry_job",
			SampleSize:     25,
			SuccessRate:    0.30,
			FailureRate:    0.55,
			Recommendation: RecommendAvoidAction,
			LastUpdated:    now.Add(-1 * time.Hour),
		},
	}

	for run := 0; run < 10; run++ {
		candidates := GatherHierarchicalCandidates(
			provRecords, nil, globalFb,
			"retry_job", "reduce_retry_rate",
			"ollama", "fast",
			"high", "low",
			now,
		)

		best1, all1 := ResolveHierarchicalFeedback(candidates)
		best2, all2 := ResolveHierarchicalFeedback(candidates)

		if best1 == nil || best2 == nil {
			t.Fatal("expected non-nil best from both resolutions")
		}
		if best1.FinalScore != best2.FinalScore {
			t.Errorf("run %d: non-deterministic score: %.3f vs %.3f", run, best1.FinalScore, best2.FinalScore)
		}
		if best1.Level != best2.Level {
			t.Errorf("run %d: non-deterministic level: %s vs %s", run, best1.LevelName, best2.LevelName)
		}
		if len(all1) != len(all2) {
			t.Errorf("run %d: non-deterministic candidate count: %d vs %d", run, len(all1), len(all2))
		}
	}
}

// --- Spec Test 5: No regression when hierarchy disabled (no temporal data) ---

func TestHierarchical_NoRegressionWithoutTimestamps(t *testing.T) {
	now := time.Now().UTC()

	// All records have zero timestamps — the planner should use categorical fallback,
	// not the hierarchical path. Verify hierarchical functions still produce valid output.
	provRecords := []ProviderContextMemoryRecord{{
		ActionType: "retry_job", GoalType: "reduce_retry_rate",
		ProviderName: "ollama", ModelRole: "fast",
		FailureBucket: "high", BacklogBucket: "low",
		TotalRuns: 20, SuccessRuns: 4, FailureRuns: 14, NeutralRuns: 2,
		SuccessRate: 0.20, FailureRate: 0.70,
		// LastUpdated: zero → stale weight
	}}

	candidates := GatherHierarchicalCandidates(
		provRecords, nil, nil,
		"retry_job", "reduce_retry_rate",
		"ollama", "fast",
		"high", "low",
		now,
	)

	// Should still produce valid candidates.
	if len(candidates) == 0 {
		t.Fatal("expected at least one candidate even with zero timestamps")
	}

	for _, c := range candidates {
		if c.RecencyWeight != decayWeightStale {
			t.Errorf("zero-time recency should be stale (%.2f), got %.2f at level %s",
				decayWeightStale, c.RecencyWeight, c.LevelName)
		}
		if c.FinalScore <= 0 {
			t.Errorf("final score should be positive, got %.3f at level %s", c.FinalScore, c.LevelName)
		}
	}

	// Resolution should still work.
	best, _ := ResolveHierarchicalFeedback(candidates)
	if best == nil {
		t.Fatal("expected non-nil best even with zero timestamps")
	}

	// Score adjustment should work without panic.
	adj, reason := HierarchicalScoreAdjustment(best, 0.40, 0.25)
	if adj >= 0 {
		t.Errorf("avoid adjustment should be negative, got %.3f", adj)
	}
	if reason == "" {
		t.Error("should have non-empty reasoning")
	}
}

// --- Similarity Threshold Tests ---

func TestHierarchical_SimilarConfidencePrefersSimpler(t *testing.T) {
	// When L0 and L2 have confidence within the similarity threshold
	// and the same recommendation, the simpler level (L2) should be preferred.
	candidates := []HierarchicalCandidate{
		{
			Level:          HierarchyExact,
			LevelName:      HierarchyExact.String(),
			Recommendation: RecommendAvoidAction,
			Confidence:     0.90,
			FinalScore:     0.90 + hierBonusExact, // 0.93
		},
		{
			Level:          HierarchyGeneralized,
			LevelName:      HierarchyGeneralized.String(),
			Recommendation: RecommendAvoidAction,
			Confidence:     0.88,                        // within 0.05 of 0.90
			FinalScore:     0.88 + hierBonusGeneralized, // 0.89
		},
	}

	best, _ := ResolveHierarchicalFeedback(candidates)
	if best == nil {
		t.Fatal("expected non-nil best")
	}
	if best.Level != HierarchyGeneralized {
		t.Errorf("similar confidence should prefer simpler level; got %s", best.LevelName)
	}
}

func TestHierarchical_LargeConfidenceGapExactWins(t *testing.T) {
	// When L0 has much higher confidence, it should win.
	candidates := []HierarchicalCandidate{
		{
			Level:          HierarchyExact,
			LevelName:      HierarchyExact.String(),
			Recommendation: RecommendAvoidAction,
			Confidence:     0.95,
			FinalScore:     0.95 + hierBonusExact, // 0.98
		},
		{
			Level:          HierarchyGeneralized,
			LevelName:      HierarchyGeneralized.String(),
			Recommendation: RecommendAvoidAction,
			Confidence:     0.50,                        // far from 0.95
			FinalScore:     0.50 + hierBonusGeneralized, // 0.51
		},
	}

	best, _ := ResolveHierarchicalFeedback(candidates)
	if best == nil {
		t.Fatal("expected non-nil best")
	}
	if best.Level != HierarchyExact {
		t.Errorf("large confidence gap should favor exact; got %s", best.LevelName)
	}
}

func TestHierarchical_ConflictingRecommendationsSpecificWins(t *testing.T) {
	// When recommendations conflict and confidence is similar,
	// the specificity bonus gives the more specific level the edge.
	candidates := []HierarchicalCandidate{
		{
			Level:          HierarchyExact,
			LevelName:      HierarchyExact.String(),
			Recommendation: RecommendAvoidAction,
			Confidence:     0.90,
			FinalScore:     0.90 + hierBonusExact, // 0.93
		},
		{
			Level:          HierarchyGeneralized,
			LevelName:      HierarchyGeneralized.String(),
			Recommendation: RecommendPreferAction, // contradicts L0
			Confidence:     0.90,
			FinalScore:     0.90 + hierBonusGeneralized, // 0.91
		},
	}

	best, _ := ResolveHierarchicalFeedback(candidates)
	if best == nil {
		t.Fatal("expected non-nil best")
	}
	// With conflicting recommendations, the "prefer simpler" rule does NOT apply.
	// FinalScore decides: L0 (0.93) > L2 (0.91).
	if best.Level != HierarchyExact {
		t.Errorf("conflicting recommendations should let specificity bonus decide; got %s", best.LevelName)
	}
}

// --- HierarchicalScoreAdjustment Tests ---

func TestHierarchicalScoreAdjustment_Nil(t *testing.T) {
	adj, reason := HierarchicalScoreAdjustment(nil, 0.40, 0.25)
	if adj != 0 {
		t.Errorf("nil adjustment should be 0, got %.3f", adj)
	}
	if reason != "" {
		t.Errorf("nil reason should be empty, got %q", reason)
	}
}

func TestHierarchicalScoreAdjustment_ScaledByConfidence(t *testing.T) {
	hc := &HierarchicalCandidate{
		Level:          HierarchyReduced,
		LevelName:      HierarchyReduced.String(),
		Recommendation: RecommendAvoidAction,
		Confidence:     0.80,
		FinalScore:     0.82,
		SampleSize:     15,
	}

	adj, reason := HierarchicalScoreAdjustment(hc, 0.40, 0.25)

	// Raw = -0.40, scaled by confidence 0.80 → -0.32.
	expectedAdj := -0.40 * 0.80
	assertFloatNear(t, "adjustment", adj, expectedAdj, 0.001)
	if reason == "" {
		t.Error("should have non-empty reasoning")
	}
}

func TestHierarchicalScoreAdjustment_PreferBoost(t *testing.T) {
	hc := &HierarchicalCandidate{
		Level:          HierarchyGlobal,
		LevelName:      HierarchyGlobal.String(),
		Recommendation: RecommendPreferAction,
		Confidence:     1.0,
		FinalScore:     1.0,
		SampleSize:     20,
	}

	adj, _ := HierarchicalScoreAdjustment(hc, 0.40, 0.25)
	assertFloatNear(t, "prefer boost", adj, 0.25, 0.001)
}

// --- hierShouldPrefer Tests ---

func TestHierShouldPrefer_HigherFinalScore(t *testing.T) {
	c := HierarchicalCandidate{Confidence: 0.90, FinalScore: 0.93, Level: HierarchyExact}
	best := HierarchicalCandidate{Confidence: 0.50, FinalScore: 0.51, Level: HierarchyGlobal}
	if !hierShouldPrefer(c, best) {
		t.Error("higher FinalScore with large confidence gap should be preferred")
	}
}

func TestHierShouldPrefer_SimplerWhenSimilar(t *testing.T) {
	c := HierarchicalCandidate{Confidence: 0.88, FinalScore: 0.89, Level: HierarchyGeneralized, Recommendation: RecommendAvoidAction}
	best := HierarchicalCandidate{Confidence: 0.90, FinalScore: 0.93, Level: HierarchyExact, Recommendation: RecommendAvoidAction}
	if !hierShouldPrefer(c, best) {
		t.Error("simpler level should be preferred when confidence is similar and recommendations agree")
	}
}

func TestHierShouldPrefer_NotSimplerWhenGap(t *testing.T) {
	c := HierarchicalCandidate{Confidence: 0.50, FinalScore: 0.51, Level: HierarchyGeneralized}
	best := HierarchicalCandidate{Confidence: 0.90, FinalScore: 0.93, Level: HierarchyExact}
	if hierShouldPrefer(c, best) {
		t.Error("simpler level should NOT be preferred when confidence gap is large")
	}
}

func TestHierShouldPrefer_SameLevelHigherScore(t *testing.T) {
	c := HierarchicalCandidate{Confidence: 0.90, FinalScore: 0.92, Level: HierarchyReduced}
	best := HierarchicalCandidate{Confidence: 0.89, FinalScore: 0.91, Level: HierarchyReduced}
	if !hierShouldPrefer(c, best) {
		t.Error("same level with higher FinalScore should be preferred")
	}
}

// --- Integration: end-to-end from records to adjustment ---

func TestHierarchical_EndToEnd_GeneralizedWinsAndAdjusts(t *testing.T) {
	now := time.Now().UTC()

	// Weak exact + strong generalized data.
	provRecords := []ProviderContextMemoryRecord{
		{
			ActionType: "retry_job", GoalType: "reduce_retry_rate",
			ProviderName: "ollama", ModelRole: "fast",
			FailureBucket: "high", BacklogBucket: "low",
			TotalRuns: 3, SuccessRuns: 2, FailureRuns: 1, NeutralRuns: 0,
			SuccessRate: 0.67, FailureRate: 0.33,
			LastUpdated: now.Add(-8 * 24 * time.Hour), // stale
		},
		{
			ActionType: "retry_job", GoalType: "reduce_retry_rate",
			ProviderName: "openrouter", ModelRole: "default",
			FailureBucket: "high", BacklogBucket: "high",
			TotalRuns: 40, SuccessRuns: 8, FailureRuns: 28, NeutralRuns: 4,
			SuccessRate: 0.20, FailureRate: 0.70,
			LastUpdated: now.Add(-20 * time.Minute), // fresh
		},
	}

	candidates := GatherHierarchicalCandidates(
		provRecords, nil, nil,
		"retry_job", "reduce_retry_rate",
		"ollama", "fast",
		"high", "low",
		now,
	)

	best, _ := ResolveHierarchicalFeedback(candidates)
	if best == nil {
		t.Fatal("expected non-nil best")
	}

	adj, reason := HierarchicalScoreAdjustment(best, 0.40, 0.25)
	if reason == "" {
		t.Error("should have reasoning")
	}

	// The generalized level should dominate (strong evidence, fresh),
	// producing an avoid signal scaled by confidence.
	if adj >= 0 {
		t.Errorf("strong avoid evidence should produce negative adjustment, got %.3f", adj)
	}
	if math.Abs(adj) < 0.10 {
		t.Errorf("adjustment should be meaningful, got %.3f", adj)
	}
}
