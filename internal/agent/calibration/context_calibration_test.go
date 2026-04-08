package calibration

import (
	"math"
	"testing"
)

// --- 1. Context override works ---

func TestApplyContextualCalibration_OverconfidentReduces(t *testing.T) {
	// calibration_error = avg_predicted - avg_actual = 0.8 - 0.5 = 0.3
	// But bounded to max +0.20
	// delta = 0.20
	// adjusted = 0.8 - 0.20 = 0.60
	adjusted := ApplyContextualCalibration(0.8, 0.3)
	if math.Abs(adjusted-0.60) > 1e-9 {
		t.Errorf("expected 0.60, got %v", adjusted)
	}
}

func TestApplyContextualCalibration_UnderconfidentIncreases(t *testing.T) {
	// calibration_error = avg_predicted - avg_actual = 0.4 - 0.7 = -0.3
	// Bounded to -0.20
	// delta = -0.20
	// adjusted = 0.4 - (-0.20) = 0.60
	adjusted := ApplyContextualCalibration(0.4, -0.3)
	if math.Abs(adjusted-0.60) > 1e-9 {
		t.Errorf("expected 0.60, got %v", adjusted)
	}
}

// --- 2. Small calibration error within bounds ---

func TestApplyContextualCalibration_SmallError(t *testing.T) {
	// calibration_error = 0.05 (within ±0.20)
	// adjusted = 0.7 - 0.05 = 0.65
	adjusted := ApplyContextualCalibration(0.7, 0.05)
	if math.Abs(adjusted-0.65) > 1e-9 {
		t.Errorf("expected 0.65, got %v", adjusted)
	}
}

// --- 3. No data → no change ---

func TestApplyContextualCalibration_ZeroError(t *testing.T) {
	adjusted := ApplyContextualCalibration(0.7, 0.0)
	if adjusted != 0.7 {
		t.Errorf("expected 0.7, got %v", adjusted)
	}
}

// --- 4. Adjustment bounded ---

func TestApplyContextualCalibration_BoundedPositive(t *testing.T) {
	// Large overconfidence error = 0.5, should be clamped to +0.20
	adjusted := ApplyContextualCalibration(0.8, 0.5)
	expected := 0.60 // 0.8 - 0.20
	if math.Abs(adjusted-expected) > 1e-9 {
		t.Errorf("expected %v, got %v", expected, adjusted)
	}
}

func TestApplyContextualCalibration_BoundedNegative(t *testing.T) {
	// Large underconfidence error = -0.5, should be clamped to -0.20
	adjusted := ApplyContextualCalibration(0.3, -0.5)
	expected := 0.50 // 0.3 - (-0.20)
	if math.Abs(adjusted-expected) > 1e-9 {
		t.Errorf("expected %v, got %v", expected, adjusted)
	}
}

// --- 5. Clamped to [0, 1] ---

func TestApplyContextualCalibration_ClampedToZero(t *testing.T) {
	// Raw confidence is very low, large positive error
	adjusted := ApplyContextualCalibration(0.05, 0.15)
	expected := 0.0 // 0.05 - 0.15 = -0.10 clamped to 0
	if adjusted != expected {
		t.Errorf("expected %v, got %v", expected, adjusted)
	}
}

func TestApplyContextualCalibration_ClampedToOne(t *testing.T) {
	// Raw confidence is very high, large negative error
	adjusted := ApplyContextualCalibration(0.95, -0.15)
	expected := 1.0 // 0.95 + 0.15 = 1.10 clamped to 1
	if adjusted != expected {
		t.Errorf("expected %v, got %v", expected, adjusted)
	}
}

// --- 6. Deterministic behavior ---

func TestApplyContextualCalibration_Deterministic(t *testing.T) {
	for i := 0; i < 100; i++ {
		a := ApplyContextualCalibration(0.7, 0.1)
		b := ApplyContextualCalibration(0.7, 0.1)
		if a != b {
			t.Fatalf("non-deterministic: got %v and %v on iteration %d", a, b, i)
		}
	}
}

// --- 7. Context key generation ---

func TestContextKeys_FullContext(t *testing.T) {
	ctx := CalibrationContext{
		GoalType:     "improve_code",
		ProviderName: "openai",
		StrategyType: "refactor",
	}
	keys := contextKeys(ctx)
	if len(keys) != 4 {
		t.Fatalf("expected 4 keys, got %d", len(keys))
	}
	// L0: full
	if keys[0].GoalType != "improve_code" || keys[0].ProviderName != "openai" || keys[0].StrategyType != "refactor" {
		t.Errorf("L0 key mismatch: %+v", keys[0])
	}
	// L1: goal + strategy
	if keys[1].GoalType != "improve_code" || keys[1].ProviderName != "" || keys[1].StrategyType != "refactor" {
		t.Errorf("L1 key mismatch: %+v", keys[1])
	}
	// L2: goal only
	if keys[2].GoalType != "improve_code" || keys[2].ProviderName != "" || keys[2].StrategyType != "" {
		t.Errorf("L2 key mismatch: %+v", keys[2])
	}
	// L3: global
	if keys[3].GoalType != "" || keys[3].ProviderName != "" || keys[3].StrategyType != "" {
		t.Errorf("L3 key mismatch: %+v", keys[3])
	}
}

func TestContextKeys_GoalOnly(t *testing.T) {
	ctx := CalibrationContext{GoalType: "deploy"}
	keys := contextKeys(ctx)
	// Should produce L2 + L3 only
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	if keys[0].GoalType != "deploy" {
		t.Errorf("L2 key mismatch: %+v", keys[0])
	}
	if keys[1].GoalType != "" {
		t.Errorf("L3 key mismatch: %+v", keys[1])
	}
}

func TestContextKeys_EmptyContext(t *testing.T) {
	ctx := CalibrationContext{}
	keys := contextKeys(ctx)
	// Global only
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0].GoalType != "" {
		t.Errorf("L3 key should be empty: %+v", keys[0])
	}
}

func TestContextKeys_GoalAndProvider(t *testing.T) {
	ctx := CalibrationContext{GoalType: "test", ProviderName: "ollama"}
	keys := contextKeys(ctx)
	// No L0 (needs all 3), no L1 (needs goal+strategy)
	// L2: goal only + L3: global
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

func TestContextKeys_GoalAndStrategy(t *testing.T) {
	ctx := CalibrationContext{GoalType: "test", StrategyType: "fast"}
	keys := contextKeys(ctx)
	// L1: goal + strategy, L2: goal only, L3: global
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
	if keys[0].GoalType != "test" || keys[0].StrategyType != "fast" {
		t.Errorf("L1 key mismatch: %+v", keys[0])
	}
}

// --- 8. Multiple contexts produce different confidence ---

func TestApplyContextualCalibration_DifferentContextsDifferentResults(t *testing.T) {
	rawConfidence := 0.7

	// Context A: overconfident (error = 0.15)
	adjustedA := ApplyContextualCalibration(rawConfidence, 0.15)

	// Context B: underconfident (error = -0.10)
	adjustedB := ApplyContextualCalibration(rawConfidence, -0.10)

	// Context C: well-calibrated (error = 0.0)
	adjustedC := ApplyContextualCalibration(rawConfidence, 0.0)

	if adjustedA == adjustedB {
		t.Error("different calibration errors should produce different results")
	}
	if adjustedB == adjustedC {
		t.Error("different calibration errors should produce different results")
	}
	if adjustedA >= rawConfidence {
		t.Error("overconfident should reduce confidence")
	}
	if adjustedB <= rawConfidence {
		t.Error("underconfident should increase confidence")
	}
	if adjustedC != rawConfidence {
		t.Error("zero error should not change confidence")
	}
}

// --- 9. Context levels match contextKeys ---

func TestContextLevels_MatchedToKeys(t *testing.T) {
	ctx := CalibrationContext{
		GoalType:     "g",
		ProviderName: "p",
		StrategyType: "s",
	}
	keys := contextKeys(ctx)
	levels := contextLevels(ctx)
	if len(keys) != len(levels) {
		t.Fatalf("keys (%d) and levels (%d) length mismatch", len(keys), len(levels))
	}
	expectedLevels := []string{ContextLevelL0, ContextLevelL1, ContextLevelL2, ContextLevelL3}
	for i, l := range levels {
		if l != expectedLevels[i] {
			t.Errorf("level %d: expected %s, got %s", i, expectedLevels[i], l)
		}
	}
}

// --- 10. ContextGraphAdapter nil safety ---

func TestContextGraphAdapter_NilCalibrator(t *testing.T) {
	adapter := NewContextGraphAdapter(nil, nil)
	result := adapter.CalibrateConfidenceForContext(nil, 0.75, "goal", "provider", "strategy")
	if result != 0.75 {
		t.Errorf("expected 0.75 with nil calibrator, got %v", result)
	}
}

// --- 11. ContextOutcomeAdapter nil safety ---

func TestContextOutcomeAdapter_NilCalibrator(t *testing.T) {
	adapter := NewContextOutcomeAdapter(nil, nil)
	err := adapter.RecordContextCalibrationOutcome(nil, "goal", "provider", "strategy", 0.8, "success")
	if err != nil {
		t.Errorf("expected nil error with nil calibrator, got %v", err)
	}
}

// --- 12. Extreme values ---

func TestApplyContextualCalibration_ExtremeValues(t *testing.T) {
	tests := []struct {
		name       string
		raw        float64
		error_     float64
		wantMin    float64
		wantMax    float64
	}{
		{"raw=0, error=0", 0.0, 0.0, 0.0, 0.0},
		{"raw=1, error=0", 1.0, 0.0, 1.0, 1.0},
		{"raw=0, large positive error", 0.0, 1.0, 0.0, 0.0},
		{"raw=1, large negative error", 1.0, -1.0, 1.0, 1.0},
		{"raw=0.5, max positive", 0.5, 0.20, 0.30, 0.30},
		{"raw=0.5, max negative", 0.5, -0.20, 0.70, 0.70},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ApplyContextualCalibration(tt.raw, tt.error_)
			if result < tt.wantMin-1e-9 || result > tt.wantMax+1e-9 {
				t.Errorf("ApplyContextualCalibration(%v, %v) = %v, want [%v, %v]",
					tt.raw, tt.error_, result, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// --- 13. No regression: zero calibration error = identical behavior ---

func TestApplyContextualCalibration_NoRegressionZeroError(t *testing.T) {
	confidences := []float64{0.0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0}
	for _, conf := range confidences {
		adjusted := ApplyContextualCalibration(conf, 0.0)
		if adjusted != conf {
			t.Errorf("with zero error, confidence %v should be unchanged, got %v", conf, adjusted)
		}
	}
}

// --- 14. Constants are correct ---

func TestContextCalibrationConstants(t *testing.T) {
	if ContextMinSamples != 5 {
		t.Errorf("ContextMinSamples = %d, want 5", ContextMinSamples)
	}
	if ContextMaxAdjustment != 0.20 {
		t.Errorf("ContextMaxAdjustment = %v, want 0.20", ContextMaxAdjustment)
	}
}
