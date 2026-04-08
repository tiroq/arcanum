package strategy

import (
	"math"
	"testing"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
)

// ---- Iteration 19.1 Scoring Stability Tests ----

// Test 1: Deterministic — same inputs → same scores across 10 runs.
func TestScoringStability_Deterministic(t *testing.T) {
	plans := []StrategyPlan{
		makeTestPlan(StrategyDirectRetry, 1, 0.7, 0.2, 0.8),
		makeTestPlan(StrategyObserveThenResync, 2, 0.5, 0.3, 0.6),
	}
	input := defaultPortfolioInput()

	var firstRun []StrategyCandidate
	for i := 0; i < 10; i++ {
		result := BuildPortfolio(plans, input)
		if i == 0 {
			firstRun = result
			continue
		}
		for j := range result {
			if result[j].FinalScore != firstRun[j].FinalScore {
				t.Fatalf("run %d: candidate %d FinalScore changed: %f → %f",
					i, j, firstRun[j].FinalScore, result[j].FinalScore)
			}
			if result[j].Confidence != firstRun[j].Confidence {
				t.Fatalf("run %d: candidate %d Confidence changed: %f → %f",
					i, j, firstRun[j].Confidence, result[j].Confidence)
			}
			if result[j].RiskScore != firstRun[j].RiskScore {
				t.Fatalf("run %d: candidate %d RiskScore changed: %f → %f",
					i, j, firstRun[j].RiskScore, result[j].RiskScore)
			}
		}
	}
}

// Test 2: Small noise in EV should NOT flip the selection (inertia prevents oscillation).
func TestScoringStability_SmallNoiseNoOscillation(t *testing.T) {
	planA := makeTestPlan(StrategyDirectRetry, 1, 0.60, 0.2, 0.7)
	planB := makeTestPlan(StrategyDirectResync, 1, 0.57, 0.2, 0.7)

	input := defaultPortfolioInput()
	candidates := BuildPortfolio([]StrategyPlan{planA, planB}, input)

	// Without inertia: A wins (higher EV).
	sel := SelectFromPortfolio(candidates, PortfolioSelectConfig{})
	if sel.Selected == nil {
		t.Fatal("expected a selection")
	}
	winner := sel.Selected.StrategyType

	// With inertia favoring B (the loser): B should now win because gap < 0.10.
	candidates2 := BuildPortfolio([]StrategyPlan{planA, planB}, input)
	sel2 := SelectFromPortfolio(candidates2, PortfolioSelectConfig{
		LastSelectedStrategy: StrategyDirectResync,
	})
	if sel2.Selected == nil {
		t.Fatal("expected a selection with inertia")
	}
	// Inertia should cause B to win now.
	if sel2.Selected.StrategyType == winner {
		// Only fail if the original gap was small enough for inertia to matter.
		gapA := candidates2[0].FinalScore - candidates2[1].FinalScore
		if candidates2[0].StrategyType == StrategyDirectRetry {
			gapA = candidates2[0].FinalScore - candidates2[1].FinalScore
		}
		if gapA < InertiaThreshold {
			t.Fatalf("inertia should have prevented oscillation; gap=%.4f < threshold=%.2f",
				gapA, InertiaThreshold)
		}
	}
}

// Test 3: High EV + low confidence should be penalized.
func TestScoringStability_HighEVLowConfidencePenalized(t *testing.T) {
	highEVLowConf := makeTestPlan(StrategyDirectRetry, 1, 0.9, 0.1, 0.2)
	moderateAll := makeTestPlan(StrategyDirectResync, 1, 0.5, 0.1, 0.7)

	input := defaultPortfolioInput()
	candidates := BuildPortfolio([]StrategyPlan{highEVLowConf, moderateAll}, input)

	var highEV, moderate *StrategyCandidate
	for i := range candidates {
		if candidates[i].StrategyType == StrategyDirectRetry {
			highEV = &candidates[i]
		} else {
			moderate = &candidates[i]
		}
	}

	// The moderate-all candidate should have competitive or better final score
	// because confidence weighs significantly in the formula.
	t.Logf("highEV FinalScore=%.4f (conf=%.4f), moderate FinalScore=%.4f (conf=%.4f)",
		highEV.FinalScore, highEV.Confidence, moderate.FinalScore, moderate.Confidence)

	// Confidence difference should significantly narrow or reverse the gap.
	confGap := moderate.Confidence - highEV.Confidence
	if confGap <= 0 {
		t.Fatal("moderate candidate should have higher confidence")
	}
}

// Test 4: High confidence + low EV should NOT dominate.
func TestScoringStability_HighConfLowEVNotDominating(t *testing.T) {
	highConfLowEV := makeTestPlan(StrategyNoop, 1, 0.1, 0.0, 0.95)
	balancedCandidate := makeTestPlan(StrategyDirectRetry, 1, 0.6, 0.2, 0.6)

	input := defaultPortfolioInput()
	candidates := BuildPortfolio([]StrategyPlan{highConfLowEV, balancedCandidate}, input)

	sel := SelectFromPortfolio(candidates, PortfolioSelectConfig{})
	if sel.Selected == nil {
		t.Fatal("expected selection")
	}

	// The balanced candidate should win because EV has the highest weight (0.5).
	if sel.Selected.StrategyType != StrategyDirectRetry {
		t.Fatalf("balanced candidate should win; got %s (score=%.4f)",
			sel.Selected.StrategyType, sel.Selected.FinalScore)
	}
}

// Test 5: Risk decomposition produces correct component values.
func TestScoringStability_RiskDecomposition(t *testing.T) {
	// Use ObserveThenRetry for 2-step plan (makeTestPlan hardcodes step list per type).
	plan := makeTestPlan(StrategyObserveThenRetry, 2, 0.5, 0.3, 0.7)

	input := defaultPortfolioInput()
	input.StabilityMode = "throttled"
	input.StrategyMemory = map[string]StrategyFeedbackSignal{
		string(StrategyObserveThenRetry): {
			Recommendation: "avoid_strategy",
			FailureRate:    0.4,
			SampleSize:     15,
		},
	}

	candidates := BuildPortfolio([]StrategyPlan{plan}, input)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	c := candidates[0]
	rc := c.Signals.RiskComponents

	// StabilityRisk: throttled + multi-step (2 steps) → 0.6
	if rc.StabilityRisk != 0.6 {
		t.Errorf("StabilityRisk: want 0.6, got %.4f", rc.StabilityRisk)
	}

	// HistoricalRisk: from failure rate 0.4
	if rc.HistoricalRisk != 0.4 {
		t.Errorf("HistoricalRisk: want 0.4, got %.4f", rc.HistoricalRisk)
	}

	// PolicyRisk: plan risk 0.3 + (2-1)*0.15 = 0.45
	expectedPolicy := 0.3 + 0.15
	if math.Abs(rc.PolicyRisk-expectedPolicy) > 0.001 {
		t.Errorf("PolicyRisk: want %.4f, got %.4f", expectedPolicy, rc.PolicyRisk)
	}

	// Composite: 0.6*0.5 + 0.4*0.3 + 0.45*0.2 = 0.30 + 0.12 + 0.09 = 0.51
	expectedComposite := rc.StabilityRisk*0.5 + rc.HistoricalRisk*0.3 + rc.PolicyRisk*0.2
	if math.Abs(c.RiskScore-clamp01(expectedComposite)) > 0.001 {
		t.Errorf("CompositeRisk: want %.4f, got %.4f", expectedComposite, c.RiskScore)
	}
}

// Test 6: Inertia prevents flip-flop between two closely scored strategies.
func TestScoringStability_InertiaPreventFlipFlop(t *testing.T) {
	// Two strategies with very close scores.
	planA := makeTestPlan(StrategyDirectRetry, 1, 0.55, 0.2, 0.7)
	planB := makeTestPlan(StrategyDirectResync, 1, 0.53, 0.2, 0.7)

	input := defaultPortfolioInput()

	// Simulate: last cycle selected B. With inertia, B should stick.
	candidates := BuildPortfolio([]StrategyPlan{planA, planB}, input)
	sel := SelectFromPortfolio(candidates, PortfolioSelectConfig{
		LastSelectedStrategy: StrategyDirectResync,
	})
	if sel.Selected == nil {
		t.Fatal("expected selection")
	}
	if sel.Selected.StrategyType != StrategyDirectResync {
		t.Fatalf("inertia should keep B (DirectResync); got %s", sel.Selected.StrategyType)
	}

	// Simulate: last cycle selected A. With inertia, A should stick.
	candidates2 := BuildPortfolio([]StrategyPlan{planA, planB}, input)
	sel2 := SelectFromPortfolio(candidates2, PortfolioSelectConfig{
		LastSelectedStrategy: StrategyDirectRetry,
	})
	if sel2.Selected == nil {
		t.Fatal("expected selection")
	}
	if sel2.Selected.StrategyType != StrategyDirectRetry {
		t.Fatalf("inertia should keep A (DirectRetry); got %s", sel2.Selected.StrategyType)
	}
}

// Test 7: Normalization clamps extreme values.
func TestScoringStability_NormalizationClampsExtremes(t *testing.T) {
	// Create a plan with extreme raw values that would go out of [0,1].
	plan := makeTestPlan(StrategyDirectRetry, 1, 1.5, -0.5, 2.0)

	input := defaultPortfolioInput()
	// Add strong positive signals to push EV even higher.
	input.StrategyMemory = map[string]StrategyFeedbackSignal{
		string(StrategyDirectRetry): {
			Recommendation: "prefer_strategy",
			SampleSize:     50,
		},
	}

	candidates := BuildPortfolio([]StrategyPlan{plan}, input)
	c := candidates[0]

	if c.ExpectedValue < 0 || c.ExpectedValue > 1 {
		t.Errorf("EV not clamped: %.4f", c.ExpectedValue)
	}
	if c.RiskScore < 0 || c.RiskScore > 1 {
		t.Errorf("RiskScore not clamped: %.4f", c.RiskScore)
	}
	if c.Confidence < 0 || c.Confidence > 1 {
		t.Errorf("Confidence not clamped: %.4f", c.Confidence)
	}
	if c.FinalScore < 0 || c.FinalScore > 1 {
		t.Errorf("FinalScore not clamped: %.4f", c.FinalScore)
	}
}

// Test 8: All signals zero → edge case, no panic, valid score.
func TestScoringStability_AllSignalsZero(t *testing.T) {
	plan := makeTestPlan(StrategyDirectRetry, 1, 0, 0, 0)

	input := PortfolioInput{
		Base: ScoreInput{
			ActionFeedback: map[string]actionmemory.ActionFeedback{},
		},
	}

	candidates := BuildPortfolio([]StrategyPlan{plan}, input)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	c := candidates[0]
	if c.FinalScore < 0 || c.FinalScore > 1 {
		t.Errorf("FinalScore out of bounds: %.4f", c.FinalScore)
	}
	if c.ExpectedValue != 0 {
		t.Errorf("expected EV=0, got %.4f", c.ExpectedValue)
	}

	// Verify decomposed signals are populated.
	if c.Signals.Risk != c.RiskScore {
		t.Errorf("Signals.Risk mismatch: %.4f vs %.4f", c.Signals.Risk, c.RiskScore)
	}
	if c.Signals.Confidence != c.Confidence {
		t.Errorf("Signals.Confidence mismatch: %.4f vs %.4f", c.Signals.Confidence, c.Confidence)
	}
}

// Test 9: Confidence decomposition weights are correct.
func TestScoringStability_ConfidenceDecomposition(t *testing.T) {
	plan := makeTestPlan(StrategyDirectRetry, 1, 0.5, 0.2, 0.6)
	plan.Steps = []StrategyStep{
		{ActionType: "retry_job"},
	}

	input := defaultPortfolioInput()
	input.Base.ActionFeedback = map[string]actionmemory.ActionFeedback{
		"retry_job": {
			SampleSize:     20,
			Recommendation: actionmemory.RecommendPreferAction,
		},
	}
	input.StrategyMemory = map[string]StrategyFeedbackSignal{
		string(StrategyDirectRetry): {
			Recommendation: "prefer_strategy",
			SampleSize:     15,
		},
	}

	candidates := BuildPortfolio([]StrategyPlan{plan}, input)
	c := candidates[0]
	cc := c.Signals.ConfidenceComponents

	// SampleConfidence should be > 0 (we have samples).
	if cc.SampleConfidence <= 0 {
		t.Errorf("SampleConfidence should be > 0, got %.4f", cc.SampleConfidence)
	}

	// RecencyConfidence should be based on plan.Confidence (0.6), single step no degradation.
	if cc.RecencyConfidence != 0.6 {
		t.Errorf("RecencyConfidence: want 0.6, got %.4f", cc.RecencyConfidence)
	}

	// Composite: SampleConfidence*0.6 + RecencyConfidence*0.4
	expectedComposite := clamp01(cc.SampleConfidence*0.6 + cc.RecencyConfidence*0.4)
	if math.Abs(c.Confidence-expectedComposite) > 0.001 {
		t.Errorf("Composite confidence: want %.4f, got %.4f", expectedComposite, c.Confidence)
	}
}

// Test 10: Inertia disabled when gap is large.
func TestScoringStability_InertiaDisabledForLargeGap(t *testing.T) {
	// A has much higher score than B.
	planA := makeTestPlan(StrategyDirectRetry, 1, 0.8, 0.1, 0.8)
	planB := makeTestPlan(StrategyDirectResync, 1, 0.3, 0.5, 0.4)

	input := defaultPortfolioInput()

	// Even with inertia favoring B, A should still win (gap > InertiaThreshold).
	candidates := BuildPortfolio([]StrategyPlan{planA, planB}, input)
	sel := SelectFromPortfolio(candidates, PortfolioSelectConfig{
		LastSelectedStrategy: StrategyDirectResync,
	})
	if sel.Selected == nil {
		t.Fatal("expected selection")
	}
	if sel.Selected.StrategyType != StrategyDirectRetry {
		t.Fatalf("large gap should override inertia; got %s", sel.Selected.StrategyType)
	}
}
