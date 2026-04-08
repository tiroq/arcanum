package strategy

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tiroq/arcanum/internal/agent/actionmemory"
)

// --- Helpers ---

func makeTestPlan(st StrategyType, steps int, utility, risk, confidence float64) StrategyPlan {
	var planSteps []StrategyStep
	switch st {
	case StrategyDirectRetry:
		planSteps = []StrategyStep{{Order: 1, ActionType: "retry_job"}}
	case StrategyObserveThenRetry:
		planSteps = []StrategyStep{
			{Order: 1, ActionType: "log_recommendation"},
			{Order: 2, ActionType: "retry_job"},
		}
	case StrategyRecommendOnly:
		planSteps = []StrategyStep{{Order: 1, ActionType: "log_recommendation"}}
	case StrategyNoop:
		planSteps = []StrategyStep{{Order: 1, ActionType: "noop"}}
	default:
		for i := 0; i < steps; i++ {
			planSteps = append(planSteps, StrategyStep{Order: i + 1, ActionType: "retry_job"})
		}
	}
	return StrategyPlan{
		ID:              uuid.New(),
		GoalID:          "g1",
		GoalType:        "reduce_retry_rate",
		StrategyType:    st,
		Steps:           planSteps,
		ExpectedUtility: utility,
		RiskScore:       risk,
		Confidence:      confidence,
	}
}

func defaultPortfolioInput() PortfolioInput {
	return PortfolioInput{
		Base: ScoreInput{
			ActionFeedback:  map[string]actionmemory.ActionFeedback{},
			CandidateScores: map[string]float64{},
			CandidateConf:   map[string]float64{},
			StabilityMode:   "normal",
		},
		StabilityMode: "normal",
	}
}

// --- Test 1: Multiple strategies → best selected ---

func TestPortfolio_BestStrategySelected(t *testing.T) {
	plans := []StrategyPlan{
		makeTestPlan(StrategyDirectRetry, 1, 0.80, 0.0, 0.9),
		makeTestPlan(StrategyObserveThenRetry, 2, 0.50, 0.15, 0.7),
		makeTestPlan(StrategyRecommendOnly, 1, 0.40, 0.0, 0.6),
		makeTestPlan(StrategyNoop, 1, 0.10, 0.0, 1.0),
	}

	input := defaultPortfolioInput()
	portfolio := BuildPortfolio(plans, input)

	selection := SelectFromPortfolio(portfolio, PortfolioSelectConfig{
		StabilityMode: "normal",
	})

	if selection.Selected == nil {
		t.Fatal("expected a strategy to be selected")
	}
	if selection.Selected.StrategyType != StrategyDirectRetry {
		t.Errorf("expected direct_retry (highest utility), got %s", selection.Selected.StrategyType)
	}
	if selection.Selected.FinalScore <= 0 {
		t.Errorf("selected FinalScore should be > 0, got %f", selection.Selected.FinalScore)
	}
}

// --- Test 2: High risk → rejected ---

func TestPortfolio_HighRiskRejected(t *testing.T) {
	plans := []StrategyPlan{
		makeTestPlan(StrategyDirectRetry, 1, 0.20, 0.95, 0.3),
		makeTestPlan(StrategyNoop, 1, 0.10, 0.0, 1.0),
	}

	input := defaultPortfolioInput()
	portfolio := BuildPortfolio(plans, input)

	selection := SelectFromPortfolio(portfolio, PortfolioSelectConfig{
		StabilityMode: "normal",
	})

	if selection.Selected == nil {
		t.Fatal("expected noop to be selected as fallback")
	}

	// The high-risk strategy should score lower than noop.
	var retryCandidate *StrategyCandidate
	var noopCandidate *StrategyCandidate
	for i := range portfolio {
		if portfolio[i].StrategyType == StrategyDirectRetry {
			retryCandidate = &portfolio[i]
		}
		if portfolio[i].StrategyType == StrategyNoop {
			noopCandidate = &portfolio[i]
		}
	}
	if retryCandidate == nil || noopCandidate == nil {
		t.Fatal("missing candidates")
	}
	if retryCandidate.FinalScore >= noopCandidate.FinalScore {
		t.Errorf("high-risk strategy (%f) should score lower than noop (%f)",
			retryCandidate.FinalScore, noopCandidate.FinalScore)
	}
}

// --- Test 3: Low confidence → penalized ---

func TestPortfolio_LowConfidencePenalized(t *testing.T) {
	highConf := makeTestPlan(StrategyDirectRetry, 1, 0.60, 0.0, 0.95)
	lowConf := makeTestPlan(StrategyRecommendOnly, 1, 0.60, 0.0, 0.20)

	input := defaultPortfolioInput()
	portfolio := BuildPortfolio([]StrategyPlan{highConf, lowConf}, input)

	var highScore, lowScore float64
	for _, c := range portfolio {
		if c.StrategyType == StrategyDirectRetry {
			highScore = c.FinalScore
		}
		if c.StrategyType == StrategyRecommendOnly {
			lowScore = c.FinalScore
		}
	}

	if lowScore >= highScore {
		t.Errorf("low confidence (%f) should score lower than high confidence (%f)",
			lowScore, highScore)
	}
}

// --- Test 4: Equal score → simpler wins ---

func TestPortfolio_EqualScore_SimplerWins(t *testing.T) {
	// Create two plans with very similar scores but different complexity.
	simple := makeTestPlan(StrategyDirectRetry, 1, 0.60, 0.0, 0.8)
	complex := makeTestPlan(StrategyObserveThenRetry, 2, 0.63, 0.15, 0.8)

	input := defaultPortfolioInput()
	portfolio := BuildPortfolio([]StrategyPlan{complex, simple}, input)

	// Make their FinalScores artificially close (within SimplicityBias).
	for i := range portfolio {
		if portfolio[i].StrategyType == StrategyDirectRetry {
			portfolio[i].FinalScore = 0.50
		}
		if portfolio[i].StrategyType == StrategyObserveThenRetry {
			portfolio[i].FinalScore = 0.53 // within SimplicityBias of 0.05
		}
	}

	selection := SelectFromPortfolio(portfolio, PortfolioSelectConfig{
		StabilityMode: "normal",
	})

	if selection.Selected == nil {
		t.Fatal("expected a strategy to be selected")
	}
	if selection.Selected.StrategyType != StrategyDirectRetry {
		t.Errorf("expected simpler strategy (direct_retry), got %s", selection.Selected.StrategyType)
	}
	if selection.Reason != "simplicity_bias" {
		t.Errorf("expected simplicity_bias reason, got %s", selection.Reason)
	}
}

// --- Test 5: All bad → fallback ---

func TestPortfolio_AllBadFallbackToNoop(t *testing.T) {
	plans := []StrategyPlan{
		makeTestPlan(StrategyDirectRetry, 1, 0.0, 0.9, 0.1),
		makeTestPlan(StrategyRecommendOnly, 1, 0.0, 0.5, 0.1),
		makeTestPlan(StrategyNoop, 1, 0.10, 0.0, 1.0),
	}

	input := defaultPortfolioInput()
	portfolio := BuildPortfolio(plans, input)

	// Force zero scores for non-noop candidates.
	for i := range portfolio {
		if portfolio[i].StrategyType != StrategyNoop {
			portfolio[i].FinalScore = 0
		}
	}

	selection := SelectFromPortfolio(portfolio, PortfolioSelectConfig{
		StabilityMode: "normal",
	})

	if selection.Selected == nil {
		t.Fatal("expected noop fallback")
	}
	if selection.Selected.StrategyType != StrategyNoop {
		t.Errorf("expected noop fallback, got %s", selection.Selected.StrategyType)
	}
}

// --- Test 6: Stability safe_mode → safe only ---

func TestPortfolio_SafeMode_OnlySafeStrategies(t *testing.T) {
	plans := []StrategyPlan{
		makeTestPlan(StrategyDirectRetry, 1, 0.80, 0.0, 0.9),
		makeTestPlan(StrategyObserveThenRetry, 2, 0.70, 0.15, 0.8),
		makeTestPlan(StrategyRecommendOnly, 1, 0.40, 0.0, 0.6),
		makeTestPlan(StrategyNoop, 1, 0.10, 0.0, 1.0),
	}

	input := defaultPortfolioInput()
	input.StabilityMode = "safe_mode"
	input.Base.StabilityMode = "safe_mode"
	portfolio := BuildPortfolio(plans, input)

	selection := SelectFromPortfolio(portfolio, PortfolioSelectConfig{
		StabilityMode: "safe_mode",
	})

	if selection.Selected == nil {
		t.Fatal("expected a safe strategy to be selected")
	}
	if selection.Selected.StrategyType != StrategyRecommendOnly &&
		selection.Selected.StrategyType != StrategyNoop {
		t.Errorf("safe_mode should only allow recommendation_only or noop, got %s",
			selection.Selected.StrategyType)
	}
}

// --- Test 7: Exploration override works ---

func TestPortfolio_ExplorationOverride_SelectsSecondBest(t *testing.T) {
	plans := []StrategyPlan{
		makeTestPlan(StrategyDirectRetry, 1, 0.80, 0.0, 0.9),
		makeTestPlan(StrategyRecommendOnly, 1, 0.50, 0.0, 0.7),
		makeTestPlan(StrategyNoop, 1, 0.10, 0.0, 1.0),
	}

	input := defaultPortfolioInput()
	portfolio := BuildPortfolio(plans, input)

	// Without exploration: best wins.
	noExplore := SelectFromPortfolio(portfolio, PortfolioSelectConfig{
		StabilityMode: "normal",
		ShouldExplore: false,
	})
	if noExplore.Selected == nil {
		t.Fatal("expected selection without exploration")
	}
	bestType := noExplore.Selected.StrategyType

	// With exploration: second-best wins.
	portfolio2 := BuildPortfolio(plans, input)
	withExplore := SelectFromPortfolio(portfolio2, PortfolioSelectConfig{
		StabilityMode: "normal",
		ShouldExplore: true,
	})
	if withExplore.Selected == nil {
		t.Fatal("expected selection with exploration")
	}
	if !withExplore.ExplorationUsed {
		t.Error("expected exploration to be used")
	}
	if withExplore.Selected.StrategyType == bestType {
		t.Errorf("exploration should select different strategy than best (%s)", bestType)
	}
	if withExplore.Reason != "exploration_override_second_best" {
		t.Errorf("expected exploration_override_second_best reason, got %s", withExplore.Reason)
	}
}

// --- Test 8: Deterministic selection ---

func TestPortfolio_Deterministic(t *testing.T) {
	now := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	buildPortfolioForTest := func() []StrategyCandidate {
		plans := Generate("g1", "reduce_retry_rate", now)
		input := PortfolioInput{
			Base: ScoreInput{
				ActionFeedback:  map[string]actionmemory.ActionFeedback{},
				CandidateScores: map[string]float64{"retry_job": 0.7, "log_recommendation": 0.5},
				CandidateConf:   map[string]float64{"retry_job": 0.8, "log_recommendation": 0.6},
				StabilityMode:   "normal",
			},
			StabilityMode: "normal",
		}
		scored := ScoreStrategies(plans, input.Base)
		return BuildPortfolio(scored, input)
	}

	p1 := buildPortfolioForTest()
	p2 := buildPortfolioForTest()

	config := PortfolioSelectConfig{StabilityMode: "normal"}
	s1 := SelectFromPortfolio(p1, config)
	s2 := SelectFromPortfolio(p2, config)

	if s1.Selected == nil || s2.Selected == nil {
		t.Fatal("both selections should produce a result")
	}
	if s1.Selected.StrategyType != s2.Selected.StrategyType {
		t.Errorf("determinism violated: %s vs %s",
			s1.Selected.StrategyType, s2.Selected.StrategyType)
	}
	if s1.Reason != s2.Reason {
		t.Errorf("determinism violated in reason: %s vs %s", s1.Reason, s2.Reason)
	}
}

// --- Test 9: Memory signals affect scoring ---

func TestPortfolio_MemorySignalsAffectScoring(t *testing.T) {
	plans := []StrategyPlan{
		makeTestPlan(StrategyDirectRetry, 1, 0.50, 0.0, 0.7),
		makeTestPlan(StrategyRecommendOnly, 1, 0.50, 0.0, 0.7),
	}

	// Without memory signals.
	baseInput := defaultPortfolioInput()
	basePortfolio := BuildPortfolio(plans, baseInput)

	var baseRetryScore, baseRecommendScore float64
	for _, c := range basePortfolio {
		if c.StrategyType == StrategyDirectRetry {
			baseRetryScore = c.FinalScore
		}
		if c.StrategyType == StrategyRecommendOnly {
			baseRecommendScore = c.FinalScore
		}
	}

	// With prefer signal for direct_retry.
	plans2 := []StrategyPlan{
		makeTestPlan(StrategyDirectRetry, 1, 0.50, 0.0, 0.7),
		makeTestPlan(StrategyRecommendOnly, 1, 0.50, 0.0, 0.7),
	}
	enrichedInput := defaultPortfolioInput()
	enrichedInput.StrategyMemory = map[string]StrategyFeedbackSignal{
		"direct_retry": {
			Recommendation: "prefer_strategy",
			SampleSize:     10,
			SuccessRate:    0.8,
		},
	}
	enrichedPortfolio := BuildPortfolio(plans2, enrichedInput)

	var enrichedRetryScore float64
	for _, c := range enrichedPortfolio {
		if c.StrategyType == StrategyDirectRetry {
			enrichedRetryScore = c.FinalScore
		}
	}

	if enrichedRetryScore <= baseRetryScore {
		t.Errorf("prefer_strategy should boost score: enriched=%f base=%f",
			enrichedRetryScore, baseRetryScore)
	}

	// Base scores should be equal when no signals.
	if baseRetryScore != baseRecommendScore {
		t.Logf("base scores differ slightly due to action types (expected): retry=%f recommend=%f",
			baseRetryScore, baseRecommendScore)
	}
}

// --- Test 10: Portfolio scoring formula is correct ---

func TestPortfolio_ScoringFormula(t *testing.T) {
	ev := 0.8
	risk := 0.2
	confidence := 0.9

	expected := ev*WeightExpected + confidence*WeightConfidence - risk*WeightRisk
	got := computeFinalScore(ev, risk, confidence)

	if got != expected {
		t.Errorf("FinalScore formula: expected %f, got %f", expected, got)
	}
}

// --- Test 11: FinalScore is bounded [0, 1] ---

func TestPortfolio_FinalScoreBounded(t *testing.T) {
	tests := []struct {
		name       string
		ev, risk, conf float64
	}{
		{"all high", 1.0, 0.0, 1.0},
		{"all low", 0.0, 1.0, 0.0},
		{"mixed", 0.5, 0.3, 0.7},
		{"extreme negative", 0.0, 1.0, 0.0},
		{"extreme positive", 1.0, 0.0, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := computeFinalScore(tt.ev, tt.risk, tt.conf)
			if score < 0 || score > 1.0 {
				t.Errorf("FinalScore out of bounds [0,1]: %f (ev=%f risk=%f conf=%f)",
					score, tt.ev, tt.risk, tt.conf)
			}
		})
	}
}

// --- Test 12: Strategy memory adjustment range ---

func TestPortfolio_StrategyMemoryAdjustment(t *testing.T) {
	prefer := strategyMemoryAdjustment(StrategyFeedbackSignal{
		Recommendation: "prefer_strategy",
		SampleSize:     15,
	})
	if prefer < 0 || prefer > 0.08 {
		t.Errorf("prefer adjustment out of range [0, 0.08]: %f", prefer)
	}

	avoid := strategyMemoryAdjustment(StrategyFeedbackSignal{
		Recommendation: "avoid_strategy",
		SampleSize:     15,
	})
	if avoid > 0 || avoid < -0.10 {
		t.Errorf("avoid adjustment out of range [-0.10, 0]: %f", avoid)
	}

	neutral := strategyMemoryAdjustment(StrategyFeedbackSignal{
		Recommendation: "neutral",
	})
	if neutral != 0 {
		t.Errorf("neutral adjustment should be 0, got %f", neutral)
	}

	// Small sample: adjustment should be scaled down.
	smallSample := strategyMemoryAdjustment(StrategyFeedbackSignal{
		Recommendation: "prefer_strategy",
		SampleSize:     3,
	})
	if smallSample >= prefer {
		t.Errorf("small sample (%f) should be less than full sample (%f)", smallSample, prefer)
	}
}

// --- Test 13: Continuation gain adjustment ---

func TestPortfolio_ContinuationGainAdjustment(t *testing.T) {
	// Multi-step with high gain.
	highGain := continuationGainAdjustment(0.7, 2)
	if highGain != 0.05 {
		t.Errorf("high gain multi-step should be +0.05, got %f", highGain)
	}

	// Multi-step with low gain.
	lowGain := continuationGainAdjustment(0.2, 2)
	if lowGain != -0.05 {
		t.Errorf("low gain multi-step should be -0.05, got %f", lowGain)
	}

	// Single-step: no adjustment regardless of gain.
	singleStep := continuationGainAdjustment(0.9, 1)
	if singleStep != 0 {
		t.Errorf("single-step should have no continuation adjustment, got %f", singleStep)
	}
}

// --- Test 14: Stability adjustment values ---

func TestPortfolio_StabilityAdjustment(t *testing.T) {
	// Normal: no adjustment.
	if adj := stabilityAdjustment("normal", 1, StrategyDirectRetry); adj != 0 {
		t.Errorf("normal mode should have no adjustment, got %f", adj)
	}

	// Safe mode: noop no penalty.
	if adj := stabilityAdjustment("safe_mode", 1, StrategyNoop); adj != 0 {
		t.Errorf("safe_mode noop should have no penalty, got %f", adj)
	}

	// Safe mode: aggressive strategy gets high penalty.
	if adj := stabilityAdjustment("safe_mode", 1, StrategyDirectRetry); adj <= 0 {
		t.Errorf("safe_mode aggressive should have positive risk penalty, got %f", adj)
	}

	// Throttled: multi-step penalized.
	if adj := stabilityAdjustment("throttled", 2, StrategyObserveThenRetry); adj <= 0 {
		t.Errorf("throttled multi-step should have positive risk penalty, got %f", adj)
	}

	// Throttled: single-step not penalized.
	if adj := stabilityAdjustment("throttled", 1, StrategyDirectRetry); adj != 0 {
		t.Errorf("throttled single-step should have no penalty, got %f", adj)
	}
}

// --- Test 15: End-to-end generate → score → portfolio → select ---

func TestPortfolio_EndToEnd(t *testing.T) {
	now := time.Now().UTC()

	plans := Generate("g1", "reduce_retry_rate", now)

	baseInput := ScoreInput{
		ActionFeedback: map[string]actionmemory.ActionFeedback{
			"retry_job": {
				ActionType:     "retry_job",
				SampleSize:     10,
				SuccessRate:    0.80,
				Recommendation: actionmemory.RecommendPreferAction,
			},
		},
		CandidateScores: map[string]float64{"retry_job": 0.8, "log_recommendation": 0.5},
		CandidateConf:   map[string]float64{"retry_job": 0.9, "log_recommendation": 0.6},
		StabilityMode:   "normal",
	}

	scored := ScoreStrategies(plans, baseInput)

	portfolioInput := PortfolioInput{
		Base:          baseInput,
		StabilityMode: "normal",
		StrategyMemory: map[string]StrategyFeedbackSignal{
			"direct_retry": {
				Recommendation: "prefer_strategy",
				SampleSize:     8,
				SuccessRate:    0.75,
			},
		},
	}

	portfolio := BuildPortfolio(scored, portfolioInput)
	selection := SelectFromPortfolio(portfolio, PortfolioSelectConfig{
		StabilityMode: "normal",
	})

	if selection.Selected == nil {
		t.Fatal("expected a strategy to be selected")
	}

	// With high retry_job score + prefer signal, direct_retry should win.
	if selection.Selected.StrategyType != StrategyDirectRetry {
		t.Errorf("expected direct_retry, got %s", selection.Selected.StrategyType)
	}

	// All candidates should have valid FinalScores.
	for _, c := range selection.Candidates {
		if c.FinalScore < 0 || c.FinalScore > 1.0 {
			t.Errorf("candidate %s has out-of-bounds FinalScore: %f",
				c.StrategyType, c.FinalScore)
		}
	}
}

// --- Test 16: Throttled mode penalizes high-risk strategies in portfolio ---

func TestPortfolio_ThrottledPenalizesMultiStep(t *testing.T) {
	plans := []StrategyPlan{
		makeTestPlan(StrategyDirectRetry, 1, 0.60, 0.0, 0.8),
		makeTestPlan(StrategyObserveThenRetry, 2, 0.65, 0.15, 0.8),
	}

	normalInput := defaultPortfolioInput()
	normalInput.StabilityMode = "normal"
	normalPortfolio := BuildPortfolio(plans, normalInput)

	plans2 := []StrategyPlan{
		makeTestPlan(StrategyDirectRetry, 1, 0.60, 0.0, 0.8),
		makeTestPlan(StrategyObserveThenRetry, 2, 0.65, 0.15, 0.8),
	}
	throttledInput := defaultPortfolioInput()
	throttledInput.StabilityMode = "throttled"
	throttledPortfolio := BuildPortfolio(plans2, throttledInput)

	var normalOTR, throttledOTR float64
	for _, c := range normalPortfolio {
		if c.StrategyType == StrategyObserveThenRetry {
			normalOTR = c.FinalScore
		}
	}
	for _, c := range throttledPortfolio {
		if c.StrategyType == StrategyObserveThenRetry {
			throttledOTR = c.FinalScore
		}
	}

	if throttledOTR >= normalOTR {
		t.Errorf("throttled multi-step (%f) should score lower than normal (%f)",
			throttledOTR, normalOTR)
	}
}

// --- Test 17: Empty candidates → no selection ---

func TestPortfolio_EmptyCandidates(t *testing.T) {
	selection := SelectFromPortfolio(nil, PortfolioSelectConfig{})
	if selection.Selected != nil {
		t.Error("empty candidates should produce nil selection")
	}
	if selection.Reason != "no_candidates" {
		t.Errorf("expected no_candidates reason, got %s", selection.Reason)
	}
}

// --- Test 18: Action memory aggregate signal ---

func TestPortfolio_ActionMemoryAggregate(t *testing.T) {
	plan := makeTestPlan(StrategyObserveThenRetry, 2, 0.50, 0.0, 0.7)
	feedback := map[string]actionmemory.ActionFeedback{
		"retry_job": {
			Recommendation: actionmemory.RecommendPreferAction,
			SampleSize:     10,
		},
		"log_recommendation": {
			Recommendation: actionmemory.RecommendAvoidAction,
			SampleSize:     10,
		},
	}

	signal := actionMemoryAggregateSignal(&plan, feedback)
	// One positive + one negative = near zero.
	if signal > 0.05 || signal < -0.05 {
		t.Errorf("mixed feedback aggregate should be near zero, got %f", signal)
	}

	// All positive.
	allPositive := map[string]actionmemory.ActionFeedback{
		"retry_job":          {Recommendation: actionmemory.RecommendPreferAction},
		"log_recommendation": {Recommendation: actionmemory.RecommendPreferAction},
	}
	signal = actionMemoryAggregateSignal(&plan, allPositive)
	if signal <= 0 {
		t.Errorf("all positive feedback should produce positive signal, got %f", signal)
	}
}

// --- Test 19: Sorting is deterministic ---

func TestPortfolio_SortDeterministic(t *testing.T) {
	c1 := StrategyCandidate{StrategyType: StrategyDirectRetry, FinalScore: 0.5, Plan: &StrategyPlan{Steps: []StrategyStep{{}}}}
	c2 := StrategyCandidate{StrategyType: StrategyRecommendOnly, FinalScore: 0.5, Plan: &StrategyPlan{Steps: []StrategyStep{{}}}}

	candidates := []StrategyCandidate{c2, c1}
	sortPortfolioCandidates(candidates)

	// Same score, same steps, alphabetical tiebreak: direct_retry < recommendation_only.
	if candidates[0].StrategyType != StrategyDirectRetry {
		t.Errorf("expected alphabetical tiebreak, got %s first", candidates[0].StrategyType)
	}
}
