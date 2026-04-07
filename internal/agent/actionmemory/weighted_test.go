package actionmemory

import (
	"math"
	"testing"
	"time"
)

// --- Temporal Decay Tests ---

func TestRecencyWeight_Fresh(t *testing.T) {
	now := time.Now().UTC()
	w := RecencyWeight(now.Add(-30*time.Minute), now)
	assertFloatEqual(t, "fresh weight", w, 1.0)
}

func TestRecencyWeight_Recent(t *testing.T) {
	now := time.Now().UTC()
	w := RecencyWeight(now.Add(-12*time.Hour), now)
	assertFloatEqual(t, "recent weight", w, 0.80)
}

func TestRecencyWeight_Moderate(t *testing.T) {
	now := time.Now().UTC()
	w := RecencyWeight(now.Add(-3*24*time.Hour), now)
	assertFloatEqual(t, "moderate weight", w, 0.60)
}

func TestRecencyWeight_Stale(t *testing.T) {
	now := time.Now().UTC()
	w := RecencyWeight(now.Add(-14*24*time.Hour), now)
	assertFloatEqual(t, "stale weight", w, 0.40)
}

func TestRecencyWeight_Zero(t *testing.T) {
	now := time.Now().UTC()
	w := RecencyWeight(time.Time{}, now)
	assertFloatEqual(t, "zero time weight", w, 0.40)
}

// --- Sample Weight Tests ---

func TestSampleWeight_Tiny(t *testing.T) {
	w := SampleWeight(2)
	assertFloatEqual(t, "tiny sample", w, 0.30)
}

func TestSampleWeight_Low(t *testing.T) {
	w := SampleWeight(4)
	// Linear interp: 0.30 + (4-3)/(5-3)*0.30 = 0.30 + 0.5*0.30 = 0.45
	assertFloatNear(t, "low sample", w, 0.45, 0.001)
}

func TestSampleWeight_Medium(t *testing.T) {
	w := SampleWeight(7)
	// Linear interp: 0.60 + (7-5)/(10-5)*0.40 = 0.60 + 0.4*0.40 = 0.76
	assertFloatNear(t, "medium sample", w, 0.76, 0.001)
}

func TestSampleWeight_High(t *testing.T) {
	w := SampleWeight(10)
	assertFloatEqual(t, "high sample", w, 1.00)
}

func TestSampleWeight_VeryHigh(t *testing.T) {
	w := SampleWeight(100)
	assertFloatEqual(t, "very high sample", w, 1.00)
}

// --- Evidence Confidence Tests ---

func TestEvidenceConfidence_BothStrong(t *testing.T) {
	c := EvidenceConfidence(1.0, 1.0)
	assertFloatEqual(t, "both strong", c, 1.0)
}

func TestEvidenceConfidence_OneWeak(t *testing.T) {
	c := EvidenceConfidence(1.0, 0.30)
	expected := math.Sqrt(1.0 * 0.30) // ~0.5477
	assertFloatNear(t, "one weak", c, expected, 0.01)
}

func TestEvidenceConfidence_BothWeak(t *testing.T) {
	c := EvidenceConfidence(0.40, 0.30)
	expected := math.Sqrt(0.40 * 0.30) // ~0.3464
	assertFloatNear(t, "both weak", c, expected, 0.01)
}

// --- Spec Test 1: Recent record outranks stale record ---

func TestWeighted_RecentOutranksStale(t *testing.T) {
	now := time.Now().UTC()

	// Stale provider-exact: 30 samples, avoid, 10 days old.
	stale := BuildWeightedFeedback(
		ActionFeedback{ActionType: "retry_job", SampleSize: 30, FailureRate: 0.60, Recommendation: RecommendAvoidAction},
		SourceProviderExact,
		now.Add(-10*24*time.Hour),
		now,
	)

	// Fresh global: 8 samples, prefer, 30 minutes old.
	fresh := BuildWeightedFeedback(
		ActionFeedback{ActionType: "retry_job", SampleSize: 8, SuccessRate: 0.80, Recommendation: RecommendPreferAction},
		SourceGlobal,
		now.Add(-30*time.Minute),
		now,
	)

	// Fresh global should outrank stale provider-exact.
	if fresh.FinalWeight <= stale.FinalWeight {
		t.Errorf("fresh global (%.3f) should outrank stale provider-exact (%.3f)", fresh.FinalWeight, stale.FinalWeight)
	}
}

// --- Spec Test 2: Tiny exact sample does not dominate strong global ---

func TestWeighted_TinyExactDoesNotDominateStrongGlobal(t *testing.T) {
	now := time.Now().UTC()

	// Tiny provider-exact: 2 samples, prefer, 1 hour old.
	tinyExact := BuildWeightedFeedback(
		ActionFeedback{ActionType: "retry_job", SampleSize: 2, SuccessRate: 1.0, Recommendation: RecommendPreferAction},
		SourceProviderExact,
		now.Add(-1*time.Hour),
		now,
	)

	// Strong global: 50 samples, avoid, 2 hours old.
	strongGlobal := BuildWeightedFeedback(
		ActionFeedback{ActionType: "retry_job", SampleSize: 50, FailureRate: 0.60, Recommendation: RecommendAvoidAction},
		SourceGlobal,
		now.Add(-2*time.Hour),
		now,
	)

	// Strong global should outrank tiny exact.
	if strongGlobal.FinalWeight <= tinyExact.FinalWeight {
		t.Errorf("strong global (%.3f) should outrank tiny exact (%.3f)", strongGlobal.FinalWeight, tinyExact.FinalWeight)
	}
}

// --- Spec Test 3: Provider-context still wins when fresh + sufficiently sampled ---

func TestWeighted_ProviderContextWinsWhenFreshAndSampled(t *testing.T) {
	now := time.Now().UTC()

	// Fresh provider-exact: 15 samples, avoid, 20 minutes old.
	freshProvider := BuildWeightedFeedback(
		ActionFeedback{ActionType: "retry_job", SampleSize: 15, FailureRate: 0.60, Recommendation: RecommendAvoidAction},
		SourceProviderExact,
		now.Add(-20*time.Minute),
		now,
	)

	// Fresh global: 15 samples, prefer, 20 minutes old (same recency+samples).
	freshGlobal := BuildWeightedFeedback(
		ActionFeedback{ActionType: "retry_job", SampleSize: 15, SuccessRate: 0.80, Recommendation: RecommendPreferAction},
		SourceGlobal,
		now.Add(-20*time.Minute),
		now,
	)

	// Provider-exact should win due to specificity bonus.
	if freshProvider.FinalWeight <= freshGlobal.FinalWeight {
		t.Errorf("fresh provider-exact (%.3f) should outrank fresh global (%.3f)", freshProvider.FinalWeight, freshGlobal.FinalWeight)
	}
}

// --- Spec Test 4: Weighted fallback behaves deterministically ---

func TestWeighted_DeterministicResolution(t *testing.T) {
	now := time.Now().UTC()

	candidates := []WeightedFeedback{
		BuildWeightedFeedback(
			ActionFeedback{ActionType: "retry_job", SampleSize: 10, FailureRate: 0.60, Recommendation: RecommendAvoidAction},
			SourceProviderExact,
			now.Add(-2*time.Hour),
			now,
		),
		BuildWeightedFeedback(
			ActionFeedback{ActionType: "retry_job", SampleSize: 20, SuccessRate: 0.80, Recommendation: RecommendPreferAction},
			SourceGlobal,
			now.Add(-1*time.Hour),
			now,
		),
	}

	best1, all1 := ResolveWeightedFeedback(candidates)
	best2, all2 := ResolveWeightedFeedback(candidates)

	if best1 == nil || best2 == nil {
		t.Fatal("expected non-nil best from both resolutions")
	}
	if best1.FinalWeight != best2.FinalWeight {
		t.Errorf("non-deterministic: run1=%.3f, run2=%.3f", best1.FinalWeight, best2.FinalWeight)
	}
	if best1.SourceLevel != best2.SourceLevel {
		t.Errorf("non-deterministic source: run1=%s, run2=%s", best1.SourceLevel, best2.SourceLevel)
	}
	if len(all1) != len(all2) {
		t.Errorf("non-deterministic candidate count: run1=%d, run2=%d", len(all1), len(all2))
	}
}

// --- Spec Test 5: Planner decisions differ when recency changes ---

func TestWeighted_PlannerScoreChangesWithRecency(t *testing.T) {
	now := time.Now().UTC()

	// Fresh avoid signal.
	freshAvoid := BuildWeightedFeedback(
		ActionFeedback{ActionType: "retry_job", SampleSize: 10, FailureRate: 0.60, Recommendation: RecommendAvoidAction},
		SourceGlobal,
		now.Add(-30*time.Minute),
		now,
	)
	adjFresh, _ := WeightedScoreAdjustment(&freshAvoid, 0.40, 0.25)

	// Same signal but stale.
	staleAvoid := BuildWeightedFeedback(
		ActionFeedback{ActionType: "retry_job", SampleSize: 10, FailureRate: 0.60, Recommendation: RecommendAvoidAction},
		SourceGlobal,
		now.Add(-14*24*time.Hour),
		now,
	)
	adjStale, _ := WeightedScoreAdjustment(&staleAvoid, 0.40, 0.25)

	// Fresh should have stronger penalty than stale.
	if math.Abs(adjFresh) <= math.Abs(adjStale) {
		t.Errorf("fresh penalty (%.3f) should be stronger than stale (%.3f)", adjFresh, adjStale)
	}
}

// --- Spec Test 6: No regression when no timestamps ---

func TestWeighted_NoRegressionWithoutTimestamps(t *testing.T) {
	// When LastUpdated is zero, should still produce valid WeightedFeedback
	// with stale recency weight but no panic or invalid state.
	now := time.Now().UTC()

	wf := BuildWeightedFeedback(
		ActionFeedback{ActionType: "retry_job", SampleSize: 10, FailureRate: 0.60, Recommendation: RecommendAvoidAction},
		SourceGlobal,
		time.Time{}, // zero
		now,
	)

	if wf.RecencyWeight != decayWeightStale {
		t.Errorf("zero-time recency should be stale (%.2f), got %.2f", decayWeightStale, wf.RecencyWeight)
	}
	if wf.FinalWeight <= 0 {
		t.Errorf("final weight should be positive, got %.3f", wf.FinalWeight)
	}
	if wf.Recommendation != RecommendAvoidAction {
		t.Errorf("recommendation should be preserved, got %s", wf.Recommendation)
	}

	adj, reason := WeightedScoreAdjustment(&wf, 0.40, 0.25)
	if adj >= 0 {
		t.Errorf("avoid adjustment should be negative, got %.3f", adj)
	}
	if reason == "" {
		t.Error("should have non-empty reasoning")
	}
}

// --- GatherWeightedCandidates Tests ---

func TestGatherWeightedCandidates_AllLayers(t *testing.T) {
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
		BacklogBucket: "low",
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
			SampleSize:     20,
			SuccessRate:    0.75,
			FailureRate:    0.25,
			Recommendation: RecommendPreferAction,
			LastUpdated:    now.Add(-6 * time.Hour),
		},
	}

	candidates := GatherWeightedCandidates(
		provRecords, ctxRecords, globalFb,
		"retry_job", "reduce_retry_rate",
		"ollama", "fast",
		"low", "low",
		now,
	)

	// Should have: provider-exact, provider-partial, context-exact, context-partial, global = 5
	if len(candidates) != 5 {
		t.Errorf("expected 5 candidates, got %d", len(candidates))
		for i, c := range candidates {
			t.Logf("candidate %d: %s (%.3f)", i, c.SourceLevel, c.FinalWeight)
		}
	}
}

func TestGatherWeightedCandidates_NoProvider(t *testing.T) {
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

	candidates := GatherWeightedCandidates(
		nil, ctxRecords, globalFb,
		"retry_job", "reduce_retry_rate",
		"", "", // no provider
		"low", "low",
		now,
	)

	// Should have: context-exact, context-partial, global = 3
	if len(candidates) != 3 {
		t.Errorf("expected 3 candidates, got %d", len(candidates))
	}
}

// --- ResolveWeightedFeedback Tests ---

func TestResolveWeightedFeedback_Empty(t *testing.T) {
	best, all := ResolveWeightedFeedback(nil)
	if best != nil {
		t.Error("expected nil best for empty candidates")
	}
	if all != nil {
		t.Error("expected nil all for empty candidates")
	}
}

func TestResolveWeightedFeedback_AllInsufficientData(t *testing.T) {
	now := time.Now().UTC()
	candidates := []WeightedFeedback{
		BuildWeightedFeedback(
			ActionFeedback{ActionType: "retry_job", SampleSize: 2, Recommendation: RecommendInsufficientData},
			SourceGlobal,
			now,
			now,
		),
	}
	best, all := ResolveWeightedFeedback(candidates)
	if best != nil {
		t.Error("expected nil best when all insufficient")
	}
	if len(all) != 1 {
		t.Errorf("expected all candidates returned, got %d", len(all))
	}
}

// --- WeightedScoreAdjustment Tests ---

func TestWeightedScoreAdjustment_Nil(t *testing.T) {
	adj, reason := WeightedScoreAdjustment(nil, 0.40, 0.25)
	assertFloatEqual(t, "nil adjustment", adj, 0)
	assertEqual(t, "nil reason", reason, "")
}

func TestWeightedScoreAdjustment_ScaledByConfidence(t *testing.T) {
	now := time.Now().UTC()

	wf := BuildWeightedFeedback(
		ActionFeedback{ActionType: "retry_job", SampleSize: 10, FailureRate: 0.60, Recommendation: RecommendAvoidAction},
		SourceGlobal,
		now.Add(-30*time.Minute),
		now,
	)

	adj, _ := WeightedScoreAdjustment(&wf, 0.40, 0.25)

	// Raw adj = -0.40. Confidence = sqrt(1.0 * 1.0) = 1.0. Scaled = -0.40.
	assertFloatNear(t, "full confidence avoid", adj, -0.40, 0.01)
}

func TestWeightedScoreAdjustment_ReducedByLowConfidence(t *testing.T) {
	now := time.Now().UTC()

	wf := BuildWeightedFeedback(
		ActionFeedback{ActionType: "retry_job", SampleSize: 3, FailureRate: 0.60, Recommendation: RecommendAvoidAction},
		SourceGlobal,
		now.Add(-10*24*time.Hour),
		now,
	)

	adj, _ := WeightedScoreAdjustment(&wf, 0.40, 0.25)

	// Recency for >7d = 0.40, SampleWeight for 3 = 0.30.
	// Confidence = sqrt(0.40 * 0.30) ≈ 0.346.
	// Scaled adj = -0.40 * 0.346 ≈ -0.138.
	if adj >= -0.20 {
		t.Logf("adj=%.3f, confidence=%.3f, recency=%.3f, sample=%.3f",
			adj, wf.Confidence, wf.RecencyWeight, wf.SampleWeight)
	}
	if math.Abs(adj) >= 0.40 {
		t.Errorf("low confidence should reduce penalty from 0.40, got %.3f", adj)
	}
	if adj >= 0 {
		t.Errorf("avoid should still be negative, got %.3f", adj)
	}
}

// --- Helpers ---

func assertFloatEqual(t *testing.T, name string, got, want float64) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %.4f, want %.4f", name, got, want)
	}
}

func assertFloatNear(t *testing.T, name string, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Errorf("%s: got %.4f, want %.4f (tolerance %.4f)", name, got, want, tolerance)
	}
}
