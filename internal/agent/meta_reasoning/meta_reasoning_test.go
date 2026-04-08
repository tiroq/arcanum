package meta_reasoning

import (
	"testing"
)

// --- SelectMode Tests ---

func TestSelectMode_ConservativeSafeMode(t *testing.T) {
	input := MetaInput{
		GoalType:      "process_task",
		StabilityMode: "safe_mode",
		Confidence:    0.9,
		Risk:          0.1,
	}
	d := SelectMode(input)
	if d.Mode != ModeConservative {
		t.Errorf("expected conservative, got %s", d.Mode)
	}
	if d.Reason != "stability_safe_mode" {
		t.Errorf("expected stability_safe_mode reason, got %s", d.Reason)
	}
}

func TestSelectMode_ConservativeHighFailureRate(t *testing.T) {
	input := MetaInput{
		GoalType:    "process_task",
		FailureRate: 0.6,
		Confidence:  0.3,
	}
	d := SelectMode(input)
	if d.Mode != ModeConservative {
		t.Errorf("expected conservative, got %s", d.Mode)
	}
	if d.Reason != "high_failure_rate" {
		t.Errorf("expected high_failure_rate, got %s", d.Reason)
	}
}

func TestSelectMode_ConservativeFailureRateExact(t *testing.T) {
	input := MetaInput{
		GoalType:    "process_task",
		FailureRate: 0.5, // exactly at threshold — NOT conservative (must exceed)
	}
	d := SelectMode(input)
	if d.Mode == ModeConservative {
		t.Errorf("expected NOT conservative at exact threshold, got conservative")
	}
}

func TestSelectMode_DirectStrongSignal(t *testing.T) {
	input := MetaInput{
		GoalType:       "process_task",
		Confidence:     0.85,
		Risk:           0.1,
		PathSampleSize: 10,
	}
	d := SelectMode(input)
	if d.Mode != ModeDirect {
		t.Errorf("expected direct, got %s", d.Mode)
	}
	if d.Reason != "strong_signal_low_risk" {
		t.Errorf("expected strong_signal_low_risk, got %s", d.Reason)
	}
}

func TestSelectMode_DirectRequiresMinSamples(t *testing.T) {
	input := MetaInput{
		GoalType:       "process_task",
		Confidence:     0.9,
		Risk:           0.1,
		PathSampleSize: 2, // below MinPathSamplesForDirect
	}
	d := SelectMode(input)
	if d.Mode == ModeDirect {
		t.Errorf("expected NOT direct with insufficient samples, got direct")
	}
}

func TestSelectMode_DirectBlockedByHighRisk(t *testing.T) {
	input := MetaInput{
		GoalType:       "process_task",
		Confidence:     0.9,
		Risk:           0.3, // above LowRiskThreshold
		PathSampleSize: 10,
	}
	d := SelectMode(input)
	if d.Mode == ModeDirect {
		t.Errorf("expected NOT direct with high risk, got direct")
	}
}

func TestSelectMode_ExploratoryMissedWins(t *testing.T) {
	input := MetaInput{
		GoalType:       "process_task",
		MissedWinCount: 5,
		Confidence:     0.5,
	}
	d := SelectMode(input)
	if d.Mode != ModeExploratory {
		t.Errorf("expected exploratory, got %s", d.Mode)
	}
	if d.Reason != "high_missed_wins" {
		t.Errorf("expected high_missed_wins, got %s", d.Reason)
	}
}

func TestSelectMode_ExploratoryStagnationNoop(t *testing.T) {
	input := MetaInput{
		GoalType:       "process_task",
		RecentNoopRate: 0.7,
		Confidence:     0.5,
	}
	d := SelectMode(input)
	if d.Mode != ModeExploratory {
		t.Errorf("expected exploratory, got %s", d.Mode)
	}
	if d.Reason != "stagnation_noop_rate" {
		t.Errorf("expected stagnation_noop_rate, got %s", d.Reason)
	}
}

func TestSelectMode_ExploratoryStagnationLowValue(t *testing.T) {
	input := MetaInput{
		GoalType:           "process_task",
		RecentLowValueRate: 0.55,
		Confidence:         0.5,
	}
	d := SelectMode(input)
	if d.Mode != ModeExploratory {
		t.Errorf("expected exploratory, got %s", d.Mode)
	}
	if d.Reason != "stagnation_low_value_rate" {
		t.Errorf("expected stagnation_low_value_rate, got %s", d.Reason)
	}
}

func TestSelectMode_DefaultGraph(t *testing.T) {
	input := MetaInput{
		GoalType:   "process_task",
		Confidence: 0.5,
		Risk:       0.1,
	}
	d := SelectMode(input)
	if d.Mode != ModeGraph {
		t.Errorf("expected graph, got %s", d.Mode)
	}
	if d.Reason != "default_graph_mode" {
		t.Errorf("expected default_graph_mode, got %s", d.Reason)
	}
}

func TestSelectMode_ConservativeTakesPrecedenceOverDirect(t *testing.T) {
	// safe_mode overrides even when confidence is strong.
	input := MetaInput{
		GoalType:       "process_task",
		StabilityMode:  "safe_mode",
		Confidence:     0.9,
		Risk:           0.05,
		PathSampleSize: 10,
	}
	d := SelectMode(input)
	if d.Mode != ModeConservative {
		t.Errorf("expected conservative to override direct, got %s", d.Mode)
	}
}

func TestSelectMode_ConservativeTakesPrecedenceOverExploratory(t *testing.T) {
	input := MetaInput{
		GoalType:       "process_task",
		FailureRate:    0.6,
		MissedWinCount: 10,
	}
	d := SelectMode(input)
	if d.Mode != ModeConservative {
		t.Errorf("expected conservative to override exploratory, got %s", d.Mode)
	}
}

// --- ScoreMode Tests ---

func TestScoreMode_NilMemory(t *testing.T) {
	input := MetaInput{Confidence: 0.7, Risk: 0.1}
	s := ScoreMode(ModeGraph, nil, input)
	if s.MemoryRate != 0.5 {
		t.Errorf("expected 0.5 default memory rate, got %f", s.MemoryRate)
	}
	// 0.5*0.5 + 0.7*0.3 - 0.1*0.2 = 0.25 + 0.21 - 0.02 = 0.44
	expected := 0.44
	if diff := s.Score - expected; diff < -0.001 || diff > 0.001 {
		t.Errorf("expected score ~%f, got %f", expected, s.Score)
	}
}

func TestScoreMode_WithMemory(t *testing.T) {
	mem := &ModeMemoryRecord{
		SelectionCount: 10,
		SuccessRate:    0.8,
	}
	input := MetaInput{Confidence: 0.6, Risk: 0.2}
	s := ScoreMode(ModeGraph, mem, input)
	if s.MemoryRate != 0.8 {
		t.Errorf("expected 0.8 memory rate, got %f", s.MemoryRate)
	}
	// 0.8*0.5 + 0.6*0.3 - 0.2*0.2 = 0.4 + 0.18 - 0.04 = 0.54
	expected := 0.54
	if diff := s.Score - expected; diff < -0.001 || diff > 0.001 {
		t.Errorf("expected score ~%f, got %f", expected, s.Score)
	}
}

func TestScoreMode_ClampsToZero(t *testing.T) {
	mem := &ModeMemoryRecord{SelectionCount: 10, SuccessRate: 0.0}
	input := MetaInput{Confidence: 0.0, Risk: 1.0}
	s := ScoreMode(ModeGraph, mem, input)
	if s.Score != 0 {
		t.Errorf("expected score 0 (clamped), got %f", s.Score)
	}
}

func TestScoreMode_ClampsToOne(t *testing.T) {
	mem := &ModeMemoryRecord{SelectionCount: 10, SuccessRate: 1.0}
	input := MetaInput{Confidence: 1.0, Risk: 0.0}
	s := ScoreMode(ModeGraph, mem, input)
	// Max formula: 1.0*0.5 + 1.0*0.3 - 0.0*0.2 = 0.8
	if s.Score != 0.8 {
		t.Errorf("expected score 0.8, got %f", s.Score)
	}
}

// --- ApplyInertia Tests ---

func TestApplyInertia_NoLastMode(t *testing.T) {
	scores := []ModeScore{
		{Mode: ModeGraph, Score: 0.5},
		{Mode: ModeDirect, Score: 0.6},
	}
	result := ApplyInertia(scores, nil)
	if result[0].Score != 0.5 || result[1].Score != 0.6 {
		t.Errorf("expected no change when lastMode is nil, got %v", result)
	}
}

func TestApplyInertia_BoostsLastMode(t *testing.T) {
	lastMode := ModeGraph
	scores := []ModeScore{
		{Mode: ModeGraph, Score: 0.50},
		{Mode: ModeDirect, Score: 0.55}, // gap = 0.05, below InertiaThreshold
	}
	result := ApplyInertia(scores, &lastMode)
	// Graph should get +0.07 boost: 0.50 + 0.07 = 0.57
	if result[0].Score < 0.56 || result[0].Score > 0.58 {
		t.Errorf("expected graph score ~0.57 after inertia, got %f", result[0].Score)
	}
	if result[1].Score != 0.55 {
		t.Errorf("expected direct unchanged at 0.55, got %f", result[1].Score)
	}
}

func TestApplyInertia_NoBoostWhenGapLarge(t *testing.T) {
	lastMode := ModeGraph
	scores := []ModeScore{
		{Mode: ModeGraph, Score: 0.30},
		{Mode: ModeDirect, Score: 0.60}, // gap = 0.30, above InertiaThreshold
	}
	result := ApplyInertia(scores, &lastMode)
	if result[0].Score != 0.30 {
		t.Errorf("expected no boost when gap is large, got %f", result[0].Score)
	}
}

func TestApplyInertia_NoBoostWhenAlreadyBest(t *testing.T) {
	lastMode := ModeGraph
	scores := []ModeScore{
		{Mode: ModeGraph, Score: 0.70},
		{Mode: ModeDirect, Score: 0.50}, // graph is already best, gap negative
	}
	result := ApplyInertia(scores, &lastMode)
	if result[0].Score != 0.70 {
		t.Errorf("expected no boost when already best, got %f", result[0].Score)
	}
}

// --- SelectModeWithScoring Tests ---

func TestSelectModeWithScoring_HardRuleOverridesScoring(t *testing.T) {
	input := MetaInput{
		GoalType:      "process_task",
		StabilityMode: "safe_mode",
		Confidence:    0.9,
	}
	d := SelectModeWithScoring(input, nil)
	if d.Mode != ModeConservative {
		t.Errorf("expected conservative from hard rule, got %s", d.Mode)
	}
}

func TestSelectModeWithScoring_DefaultGraphWithNoMemory(t *testing.T) {
	input := MetaInput{
		GoalType:   "process_task",
		Confidence: 0.5,
		Risk:       0.1,
	}
	d := SelectModeWithScoring(input, nil)
	if d.Mode != ModeGraph {
		t.Errorf("expected graph default, got %s", d.Mode)
	}
}

func TestSelectModeWithScoring_ScoringOverrideWhenSignificant(t *testing.T) {
	// Direct has very high historical success, enough to override graph.
	memoryByMode := map[DecisionMode]*ModeMemoryRecord{
		ModeDirect: {
			SelectionCount: 20,
			SuccessRate:    1.0, // perfect history
		},
		ModeGraph: {
			SelectionCount: 20,
			SuccessRate:    0.2, // poor history
		},
	}
	input := MetaInput{
		GoalType:   "process_task",
		Confidence: 0.7,
		Risk:       0.1,
	}
	d := SelectModeWithScoring(input, memoryByMode)
	// Direct score: 1.0*0.5 + 0.7*0.3 - 0.1*0.2 = 0.5 + 0.21 - 0.02 = 0.69
	// Graph score:  0.2*0.5 + 0.7*0.3 - 0.1*0.2 = 0.1 + 0.21 - 0.02 = 0.29
	// Gap: 0.40 > InertiaThreshold → override
	if d.Mode != ModeDirect {
		t.Errorf("expected direct via scoring override, got %s (reason: %s)", d.Mode, d.Reason)
	}
}

func TestSelectModeWithScoring_InertiaPreventsModeSwitch(t *testing.T) {
	// Difference is marginal — inertia should hold graph.
	memoryByMode := map[DecisionMode]*ModeMemoryRecord{
		ModeDirect: {
			SelectionCount: 10,
			SuccessRate:    0.65,
		},
		ModeGraph: {
			SelectionCount: 10,
			SuccessRate:    0.55,
		},
	}
	lastGraph := ModeGraph
	input := MetaInput{
		GoalType:   "process_task",
		Confidence: 0.5,
		Risk:       0.1,
		LastMode:   &lastGraph,
	}
	d := SelectModeWithScoring(input, memoryByMode)
	// Direct score: 0.65*0.5 + 0.5*0.3 - 0.1*0.2 = 0.325 + 0.15 - 0.02 = 0.455
	// Graph score:  0.55*0.5 + 0.5*0.3 - 0.1*0.2 = 0.275 + 0.15 - 0.02 = 0.405
	// Gap = 0.05 < InertiaThreshold → inertia applies to graph: 0.405 + 0.07 = 0.475
	// Now gap = 0.455 - 0.475 = -0.02 < InertiaThreshold → no override → graph stays
	if d.Mode != ModeGraph {
		t.Errorf("expected graph retained via inertia, got %s", d.Mode)
	}
}

// --- DecisionMode Tests ---

func TestDecisionMode_IsValid(t *testing.T) {
	for _, m := range AllModes {
		if !m.IsValid() {
			t.Errorf("expected %s to be valid", m)
		}
	}
	if DecisionMode("unknown").IsValid() {
		t.Error("expected 'unknown' to be invalid")
	}
}

// --- Determinism Tests ---

func TestSelectMode_Deterministic(t *testing.T) {
	input := MetaInput{
		GoalType:       "process_task",
		Confidence:     0.6,
		Risk:           0.15,
		FailureRate:    0.3,
		MissedWinCount: 1,
		PathSampleSize: 3,
	}
	first := SelectMode(input)
	for i := 0; i < 100; i++ {
		result := SelectMode(input)
		if result.Mode != first.Mode || result.Reason != first.Reason {
			t.Fatalf("non-deterministic result at iteration %d: %+v vs %+v", i, first, result)
		}
	}
}

func TestScoreAllModes_Deterministic(t *testing.T) {
	input := MetaInput{Confidence: 0.6, Risk: 0.2}
	first := ScoreAllModes(nil, input)
	for i := 0; i < 100; i++ {
		result := ScoreAllModes(nil, input)
		for j, s := range result {
			if s.Score != first[j].Score {
				t.Fatalf("non-deterministic score at iteration %d, mode %s: %f vs %f", i, s.Mode, first[j].Score, s.Score)
			}
		}
	}
}

// --- FallbackToGraph Tests ---

func TestSelectMode_FallbackToGraphOnNormalConditions(t *testing.T) {
	// No triggers fire — should default to graph.
	input := MetaInput{
		GoalType:       "process_task",
		Confidence:     0.6,
		Risk:           0.15,
		FailureRate:    0.2,
		MissedWinCount: 1,
		PathSampleSize: 3,
		RecentNoopRate: 0.1,
	}
	d := SelectMode(input)
	if d.Mode != ModeGraph {
		t.Errorf("expected graph fallback, got %s", d.Mode)
	}
}
