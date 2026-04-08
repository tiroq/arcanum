package arbitration

import (
	"testing"
)

// --- Test 1: Higher priority overrides lower ---

func TestHardOverride_StabilityOverridesPathLearning(t *testing.T) {
	signals := []Signal{
		{Type: SignalStability, Recommendation: RecommendAvoid, Adjustment: -0.20, Confidence: 0.9, Source: "stability"},
		{Type: SignalPathLearning, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.8, Source: "path_learning"},
	}

	result := ResolveSignals("test>path", signals, 0.8)

	if result.FinalAdjustment >= 0 {
		t.Errorf("expected negative adjustment (stability=avoid wins), got %.4f", result.FinalAdjustment)
	}
	if result.FinalAdjustment != -0.20 {
		t.Errorf("expected -0.20, got %.4f", result.FinalAdjustment)
	}

	// Path learning should be suppressed.
	found := false
	for _, s := range result.Trace.SuppressedSignals {
		if s.Signal.Type == SignalPathLearning {
			found = true
		}
	}
	if !found {
		t.Error("expected path_learning to be suppressed by stability hard override")
	}
}

func TestHardOverride_CalibrationOverridesComparative(t *testing.T) {
	signals := []Signal{
		{Type: SignalCalibration, Recommendation: RecommendAvoid, Adjustment: -0.15, Confidence: 0.7, Source: "calibration"},
		{Type: SignalComparative, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.6, Source: "comparative"},
	}

	result := ResolveSignals("test>path", signals, 0.8)

	if result.FinalAdjustment != -0.15 {
		t.Errorf("expected -0.15 (calibration wins), got %.4f", result.FinalAdjustment)
	}
}

// --- Test 2: Calibration suppresses learning signals ---

func TestConfidenceSuppression_LowConfidence(t *testing.T) {
	signals := []Signal{
		{Type: SignalStability, Recommendation: RecommendNeutral, Adjustment: 0, Confidence: 0.5, Source: "stability"},
		{Type: SignalPathLearning, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.8, Source: "path_learning"},
		{Type: SignalTransitionLearning, Recommendation: RecommendPrefer, Adjustment: 0.05, Confidence: 0.7, Source: "transition_learning"},
		{Type: SignalComparative, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.6, Source: "comparative"},
	}

	// Calibrated confidence below threshold (0.4).
	result := ResolveSignals("test>path", signals, 0.3)

	// All learning signals should be suppressed.
	suppressedTypes := map[SignalType]bool{}
	for _, s := range result.Trace.SuppressedSignals {
		suppressedTypes[s.Signal.Type] = true
	}
	if !suppressedTypes[SignalPathLearning] {
		t.Error("expected path_learning to be suppressed at low confidence")
	}
	if !suppressedTypes[SignalTransitionLearning] {
		t.Error("expected transition_learning to be suppressed at low confidence")
	}
	if !suppressedTypes[SignalComparative] {
		t.Error("expected comparative to be suppressed at low confidence")
	}

	// Only stability (neutral) should remain → adjustment = 0.
	if result.FinalAdjustment != 0 {
		t.Errorf("expected 0 adjustment (only neutral stability remains), got %.4f", result.FinalAdjustment)
	}
}

func TestConfidenceSuppression_HighConfidence_NoSuppression(t *testing.T) {
	signals := []Signal{
		{Type: SignalPathLearning, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.8, Source: "path_learning"},
	}

	result := ResolveSignals("test>path", signals, 0.8)

	if len(result.Trace.SuppressedSignals) != 0 {
		t.Error("expected no suppression at high confidence")
	}
	if result.FinalAdjustment != 0.10 {
		t.Errorf("expected 0.10, got %.4f", result.FinalAdjustment)
	}
}

// --- Test 3: Stability always wins conflicts ---

func TestStabilityAlwaysWins(t *testing.T) {
	signals := []Signal{
		{Type: SignalStability, Recommendation: RecommendAvoid, Adjustment: -0.30, Confidence: 0.9, Source: "stability"},
		{Type: SignalCalibration, Recommendation: RecommendPrefer, Adjustment: 0.15, Confidence: 0.8, Source: "calibration"},
		{Type: SignalCausal, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.7, Source: "causal"},
		{Type: SignalPathLearning, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.8, Source: "path_learning"},
		{Type: SignalExploration, Recommendation: RecommendPrefer, Adjustment: 0.05, Confidence: 0.5, Source: "exploration"},
	}

	result := ResolveSignals("test>path", signals, 0.8)

	// Stability=avoid should override all prefer signals below it.
	if result.FinalAdjustment > 0 {
		t.Errorf("expected negative or zero adjustment (stability should dominate), got %.4f", result.FinalAdjustment)
	}
	if result.FinalAdjustment != -0.30 {
		t.Errorf("expected -0.30, got %.4f", result.FinalAdjustment)
	}
}

// --- Test 4: Reinforcement works ---

func TestReinforcement_AgreeingSignals(t *testing.T) {
	signals := []Signal{
		{Type: SignalPathLearning, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.8, Source: "path_learning"},
		{Type: SignalTransitionLearning, Recommendation: RecommendPrefer, Adjustment: 0.05, Confidence: 0.7, Source: "transition_learning"},
		{Type: SignalComparative, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.6, Source: "comparative"},
	}

	result := ResolveSignals("test>path", signals, 0.8)

	expected := 0.25
	if abs(result.FinalAdjustment-expected) > 0.001 {
		t.Errorf("expected reinforcement total %.2f, got %.4f", expected, result.FinalAdjustment)
	}

	containsRule := false
	for _, r := range result.Trace.RulesApplied {
		if r == "reinforcement" {
			containsRule = true
		}
	}
	if !containsRule {
		t.Error("expected reinforcement rule in trace")
	}

	if result.Trace.Reason != "signals_reinforced" {
		t.Errorf("expected reason signals_reinforced, got %s", result.Trace.Reason)
	}
}

// --- Test 5: Conflict → neutralization ---

func TestConflictNeutralization(t *testing.T) {
	// Same priority level signals that conflict (after hard override can't resolve).
	// Both at same priority: Causal prefer + Causal avoid.
	// Actually use two signals at the same level that survive hard override.
	// Use signals where NO single signal has higher priority contradiction.
	signals := []Signal{
		{Type: SignalCausal, Recommendation: RecommendPrefer, Adjustment: 0.15, Confidence: 0.7, Source: "causal_a"},
		{Type: SignalCausal, Recommendation: RecommendAvoid, Adjustment: -0.10, Confidence: 0.6, Source: "causal_b"},
	}

	result := ResolveSignals("test>path", signals, 0.8)

	containsRule := false
	for _, r := range result.Trace.RulesApplied {
		if r == "conflict_neutralization" {
			containsRule = true
		}
	}
	if !containsRule {
		t.Error("expected conflict_neutralization rule in trace")
	}

	// Total = 0.15 + (-0.10) = 0.05; neutralized = 0.05 * (1 - 0.3) = 0.035
	expected := 0.05 * (1 - NeutralizationStrength)
	if abs(result.FinalAdjustment-expected) > 0.001 {
		t.Errorf("expected neutralized adjustment ~%.4f, got %.4f", expected, result.FinalAdjustment)
	}

	if result.Trace.Reason != "conflict_neutralized" {
		t.Errorf("expected reason conflict_neutralized, got %s", result.Trace.Reason)
	}
}

// --- Test 6: Exploration cannot override stability ---

func TestExplorationIsolation_StabilityPresent(t *testing.T) {
	signals := []Signal{
		{Type: SignalStability, Recommendation: RecommendAvoid, Adjustment: -0.20, Confidence: 0.9, Source: "stability"},
		{Type: SignalExploration, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.5, Source: "exploration"},
	}

	result := ResolveSignals("test>path", signals, 0.8)

	// Exploration must be suppressed.
	explorationSuppressed := false
	for _, s := range result.Trace.SuppressedSignals {
		if s.Signal.Type == SignalExploration && s.Rule == "exploration_isolation" {
			explorationSuppressed = true
		}
	}
	if !explorationSuppressed {
		t.Error("expected exploration to be suppressed by exploration_isolation rule")
	}

	// Only stability should apply.
	if result.FinalAdjustment != -0.20 {
		t.Errorf("expected -0.20, got %.4f", result.FinalAdjustment)
	}
}

func TestExplorationIsolation_CalibrationPresent(t *testing.T) {
	signals := []Signal{
		{Type: SignalCalibration, Recommendation: RecommendAvoid, Adjustment: -0.10, Confidence: 0.8, Source: "calibration"},
		{Type: SignalExploration, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.5, Source: "exploration"},
	}

	result := ResolveSignals("test>path", signals, 0.8)

	explorationSuppressed := false
	for _, s := range result.Trace.SuppressedSignals {
		if s.Signal.Type == SignalExploration {
			explorationSuppressed = true
		}
	}
	if !explorationSuppressed {
		t.Error("expected exploration to be suppressed when calibration has directional intent")
	}
}

func TestExplorationIsolation_NoBlocker(t *testing.T) {
	signals := []Signal{
		{Type: SignalPathLearning, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.8, Source: "path_learning"},
		{Type: SignalExploration, Recommendation: RecommendPrefer, Adjustment: 0.05, Confidence: 0.5, Source: "exploration"},
	}

	result := ResolveSignals("test>path", signals, 0.8)

	// No stability/calibration blocker → exploration should remain.
	if len(result.Trace.SuppressedSignals) != 0 {
		t.Error("expected no suppression when no stability/calibration blocker")
	}
	if abs(result.FinalAdjustment-0.15) > 0.001 {
		t.Errorf("expected ~0.15 (sum), got %.4f", result.FinalAdjustment)
	}
}

// --- Test 7: Deterministic output (repeat runs identical) ---

func TestDeterministic_RepeatRuns(t *testing.T) {
	signals := []Signal{
		{Type: SignalStability, Recommendation: RecommendAvoid, Adjustment: -0.20, Confidence: 0.9, Source: "stability"},
		{Type: SignalCalibration, Recommendation: RecommendPrefer, Adjustment: 0.15, Confidence: 0.8, Source: "calibration"},
		{Type: SignalPathLearning, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.7, Source: "path_learning"},
		{Type: SignalExploration, Recommendation: RecommendPrefer, Adjustment: 0.05, Confidence: 0.5, Source: "exploration"},
	}

	first := ResolveSignals("test>path", signals, 0.8)
	for i := 0; i < 100; i++ {
		run := ResolveSignals("test>path", signals, 0.8)
		if run.FinalAdjustment != first.FinalAdjustment {
			t.Fatalf("non-deterministic: run %d got %.4f, expected %.4f", i, run.FinalAdjustment, first.FinalAdjustment)
		}
		if run.Trace.Reason != first.Trace.Reason {
			t.Fatalf("non-deterministic reason: run %d got %s, expected %s", i, run.Trace.Reason, first.Trace.Reason)
		}
		if len(run.Trace.SuppressedSignals) != len(first.Trace.SuppressedSignals) {
			t.Fatalf("non-deterministic suppressed count: run %d got %d, expected %d", i, len(run.Trace.SuppressedSignals), len(first.Trace.SuppressedSignals))
		}
	}
}

// --- Test 8: Fail-open works ---

func TestFailOpen_EmptySignals(t *testing.T) {
	result := ResolveSignals("test>path", nil, 0.8)

	if result.FinalAdjustment != 0 {
		t.Errorf("expected 0 adjustment for nil signals, got %.4f", result.FinalAdjustment)
	}
	if result.Trace.Reason != "no_signals" {
		t.Errorf("expected reason no_signals, got %s", result.Trace.Reason)
	}
}

func TestFailOpen_EmptySlice(t *testing.T) {
	result := ResolveSignals("test>path", []Signal{}, 0.8)

	if result.FinalAdjustment != 0 {
		t.Errorf("expected 0 adjustment for empty signals, got %.4f", result.FinalAdjustment)
	}
	if result.Trace.Reason != "no_signals" {
		t.Errorf("expected reason no_signals, got %s", result.Trace.Reason)
	}
}

// --- Test 9: No regression vs previous behavior ---

func TestNoRegression_SinglePathLearning(t *testing.T) {
	// Single path learning signal with no conflicts should produce same adjustment.
	signals := []Signal{
		{Type: SignalPathLearning, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.8, Source: "path_learning"},
	}

	result := ResolveSignals("retry_job", signals, 0.8)

	if result.FinalAdjustment != 0.10 {
		t.Errorf("regression: expected 0.10 for single prefer, got %.4f", result.FinalAdjustment)
	}
}

func TestNoRegression_SingleComparativeAvoid(t *testing.T) {
	signals := []Signal{
		{Type: SignalComparative, Recommendation: RecommendAvoid, Adjustment: -0.20, Confidence: 0.6, Source: "comparative"},
	}

	result := ResolveSignals("retry_job", signals, 0.8)

	if result.FinalAdjustment != -0.20 {
		t.Errorf("regression: expected -0.20 for single avoid, got %.4f", result.FinalAdjustment)
	}
}

// --- Test 10: Trace always populated ---

func TestTraceAlwaysPopulated(t *testing.T) {
	tests := []struct {
		name    string
		signals []Signal
		conf    float64
	}{
		{"empty", nil, 0.8},
		{"single", []Signal{{Type: SignalPathLearning, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.8, Source: "pl"}}, 0.8},
		{"conflicting", []Signal{
			{Type: SignalCausal, Recommendation: RecommendPrefer, Adjustment: 0.15, Confidence: 0.7, Source: "c1"},
			{Type: SignalCausal, Recommendation: RecommendAvoid, Adjustment: -0.10, Confidence: 0.6, Source: "c2"},
		}, 0.8},
		{"suppressed", []Signal{
			{Type: SignalPathLearning, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.8, Source: "pl"},
		}, 0.2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ResolveSignals("test>path", tc.signals, tc.conf)

			if result.Trace.PathSignature != "test>path" {
				t.Errorf("trace missing path signature")
			}
			if result.Trace.Reason == "" {
				t.Errorf("trace missing reason")
			}
			if result.Trace.FinalAdjustment != result.FinalAdjustment {
				t.Errorf("trace final adjustment mismatch: trace=%.4f result=%.4f",
					result.Trace.FinalAdjustment, result.FinalAdjustment)
			}
		})
	}
}

// --- Edge case: All signals conflicting ---

func TestEdgeCase_AllConflicting(t *testing.T) {
	// Use same-priority signals so hard_override doesn't resolve the conflict.
	signals := []Signal{
		{Type: SignalCausal, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.8, Source: "causal_a"},
		{Type: SignalCausal, Recommendation: RecommendAvoid, Adjustment: -0.10, Confidence: 0.7, Source: "causal_b"},
	}

	result := ResolveSignals("test>path", signals, 0.8)

	// Same priority, conflict → neutralization.
	if result.Trace.Reason != "conflict_neutralized" {
		t.Errorf("expected conflict_neutralized, got %s", result.Trace.Reason)
	}
	// Total=0.0, neutralized=0.0.
	if abs(result.FinalAdjustment) > 0.001 {
		t.Errorf("expected ~0 neutralized adjustment, got %.4f", result.FinalAdjustment)
	}
}

// --- Edge case: Low confidence + multiple signals ---

func TestEdgeCase_LowConfidenceMultipleSignals(t *testing.T) {
	signals := []Signal{
		{Type: SignalStability, Recommendation: RecommendPrefer, Adjustment: 0.05, Confidence: 0.9, Source: "stability"},
		{Type: SignalCalibration, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.7, Source: "calibration"},
		{Type: SignalPathLearning, Recommendation: RecommendAvoid, Adjustment: -0.20, Confidence: 0.8, Source: "path_learning"},
		{Type: SignalComparative, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.6, Source: "comparative"},
	}

	// Low calibrated confidence → learning signals suppressed.
	result := ResolveSignals("test>path", signals, 0.3)

	// Only stability + calibration should remain (both prefer → reinforcement).
	expected := 0.05 + 0.10
	if abs(result.FinalAdjustment-expected) > 0.001 {
		t.Errorf("expected %.2f (stability+calibration), got %.4f", expected, result.FinalAdjustment)
	}
}

// --- Edge case: Identical scores ---

func TestEdgeCase_IdenticalScores(t *testing.T) {
	signals := []Signal{
		{Type: SignalPathLearning, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.8, Source: "pl"},
		{Type: SignalComparative, Recommendation: RecommendPrefer, Adjustment: 0.10, Confidence: 0.8, Source: "comp"},
	}

	result := ResolveSignals("test>path", signals, 0.8)

	if result.FinalAdjustment != 0.20 {
		t.Errorf("expected 0.20, got %.4f", result.FinalAdjustment)
	}
}

// --- Priority tests ---

func TestPriority_Order(t *testing.T) {
	expected := []struct {
		signal   SignalType
		priority int
	}{
		{SignalStability, 1},
		{SignalCalibration, 2},
		{SignalCausal, 3},
		{SignalComparative, 4},
		{SignalPathLearning, 5},
		{SignalTransitionLearning, 6},
		{SignalExploration, 7},
	}

	for _, e := range expected {
		if Priority(e.signal) != e.priority {
			t.Errorf("expected %s priority %d, got %d", e.signal, e.priority, Priority(e.signal))
		}
	}
}

func TestHigherPriority(t *testing.T) {
	if !HigherPriority(SignalStability, SignalExploration) {
		t.Error("stability should be higher priority than exploration")
	}
	if HigherPriority(SignalExploration, SignalStability) {
		t.Error("exploration should NOT be higher priority than stability")
	}
	if HigherPriority(SignalStability, SignalStability) {
		t.Error("same signal should NOT be higher priority than itself")
	}
}

func TestIsLearningSignal(t *testing.T) {
	if !IsLearningSignal(SignalPathLearning) {
		t.Error("path_learning should be a learning signal")
	}
	if !IsLearningSignal(SignalTransitionLearning) {
		t.Error("transition_learning should be a learning signal")
	}
	if !IsLearningSignal(SignalComparative) {
		t.Error("comparative should be a learning signal")
	}
	if IsLearningSignal(SignalStability) {
		t.Error("stability should NOT be a learning signal")
	}
	if IsLearningSignal(SignalCalibration) {
		t.Error("calibration should NOT be a learning signal")
	}
}

func TestSignalType_String(t *testing.T) {
	s := SignalStability
	if s.String() != "stability" {
		t.Errorf("expected stability, got %s", s.String())
	}
}

func TestRecommendation_String(t *testing.T) {
	r := RecommendPrefer
	if r.String() != "prefer" {
		t.Errorf("expected prefer, got %s", r.String())
	}
}

// --- Neutrals only ---

func TestNeutralsOnly(t *testing.T) {
	signals := []Signal{
		{Type: SignalStability, Recommendation: RecommendNeutral, Adjustment: 0, Confidence: 0.9, Source: "stability"},
		{Type: SignalCalibration, Recommendation: RecommendNeutral, Adjustment: 0, Confidence: 0.8, Source: "calibration"},
	}

	result := ResolveSignals("test>path", signals, 0.8)

	if result.FinalAdjustment != 0 {
		t.Errorf("expected 0 for neutral-only signals, got %.4f", result.FinalAdjustment)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
