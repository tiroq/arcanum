package decision_graph

import (
	"testing"
)

// --- Test 1: Graph builds correctly ---

func TestBuildGraph_Basic(t *testing.T) {
	input := BuildInput{
		GoalType:         "reduce_retry_rate",
		CandidateActions: []string{"retry_job", "log_recommendation", "noop"},
		Signals: map[string]ActionSignals{
			"retry_job":          {ExpectedValue: 0.7, Risk: 0.2, Confidence: 0.8},
			"log_recommendation": {ExpectedValue: 0.4, Risk: 0.05, Confidence: 0.9},
			"noop":               {ExpectedValue: 0.1, Risk: 0.0, Confidence: 1.0},
		},
		Config: DefaultGraphConfig(),
	}

	graph := BuildGraph(input)

	if graph.GoalType != "reduce_retry_rate" {
		t.Errorf("expected goal_type reduce_retry_rate, got %s", graph.GoalType)
	}

	// With depth 3 and transitions, should have depth-1, depth-2, depth-3 nodes.
	if len(graph.Nodes) < 3 {
		t.Errorf("expected at least 3 nodes (one per candidate), got %d", len(graph.Nodes))
	}

	// Should have edges for transitions.
	if len(graph.Edges) == 0 {
		t.Error("expected edges for multi-step transitions, got 0")
	}

	// Verify depth-1 nodes exist.
	depth1Count := 0
	for _, n := range graph.Nodes {
		if isDepth1(n.ID) {
			depth1Count++
		}
	}
	if depth1Count != 3 {
		t.Errorf("expected 3 depth-1 nodes, got %d", depth1Count)
	}
}

func TestBuildGraph_EmptyCandidates(t *testing.T) {
	input := BuildInput{
		GoalType:         "reduce_retry_rate",
		CandidateActions: nil,
		Signals:          nil,
		Config:           DefaultGraphConfig(),
	}

	graph := BuildGraph(input)

	if len(graph.Nodes) != 0 {
		t.Errorf("expected 0 nodes for empty candidates, got %d", len(graph.Nodes))
	}
}

func TestBuildGraph_SafeModeLimitsDepth(t *testing.T) {
	config := DefaultGraphConfig()
	config.StabilityMode = "safe_mode"

	input := BuildInput{
		GoalType:         "reduce_retry_rate",
		CandidateActions: []string{"retry_job", "log_recommendation", "noop"},
		Signals: map[string]ActionSignals{
			"retry_job":          {ExpectedValue: 0.7, Risk: 0.2, Confidence: 0.8},
			"log_recommendation": {ExpectedValue: 0.4, Risk: 0.05, Confidence: 0.9},
			"noop":               {ExpectedValue: 0.1, Risk: 0.0, Confidence: 1.0},
		},
		Config: config,
	}

	graph := BuildGraph(input)

	// safe_mode → depth 1 only. Should have exactly 3 nodes, no edges.
	if len(graph.Nodes) != 3 {
		t.Errorf("safe_mode: expected exactly 3 depth-1 nodes, got %d", len(graph.Nodes))
	}
	if len(graph.Edges) != 0 {
		t.Errorf("safe_mode: expected 0 edges, got %d", len(graph.Edges))
	}
}

// --- Test 2: Multiple paths evaluated ---

func TestEnumeratePaths_MultiplePaths(t *testing.T) {
	input := BuildInput{
		GoalType:         "reduce_retry_rate",
		CandidateActions: []string{"retry_job", "log_recommendation", "noop"},
		Signals: map[string]ActionSignals{
			"retry_job":          {ExpectedValue: 0.7, Risk: 0.2, Confidence: 0.8},
			"log_recommendation": {ExpectedValue: 0.4, Risk: 0.05, Confidence: 0.9},
			"noop":               {ExpectedValue: 0.1, Risk: 0.0, Confidence: 1.0},
		},
		Config: GraphConfig{MaxDepth: 2, StabilityMode: "normal"},
	}

	graph := BuildGraph(input)
	paths := EnumeratePaths(graph)

	// Should have multiple paths: single-node + multi-node combinations.
	if len(paths) < 3 {
		t.Errorf("expected at least 3 paths, got %d", len(paths))
	}

	// Verify there are both single-node and multi-node paths.
	hasSingle := false
	hasMulti := false
	for _, p := range paths {
		if len(p.Nodes) == 1 {
			hasSingle = true
		}
		if len(p.Nodes) > 1 {
			hasMulti = true
		}
	}
	if !hasSingle {
		t.Error("expected at least one single-node path")
	}
	if !hasMulti {
		t.Error("expected at least one multi-node path")
	}
}

// --- Test 3: Best path selected ---

func TestSelectBestPath_HighestScoreWins(t *testing.T) {
	paths := []DecisionPath{
		{
			Nodes:      []DecisionNode{{ID: "a", ActionType: "retry_job", ExpectedValue: 0.7, Risk: 0.2, Confidence: 0.8}},
			FinalScore: 0.6,
		},
		{
			Nodes:      []DecisionNode{{ID: "b", ActionType: "log_recommendation", ExpectedValue: 0.4, Risk: 0.05, Confidence: 0.9}},
			FinalScore: 0.45,
		},
		{
			Nodes:      []DecisionNode{{ID: "c", ActionType: "noop", ExpectedValue: 0.1, Risk: 0.0, Confidence: 1.0}},
			FinalScore: 0.1,
		},
	}

	selection := SelectBestPath(paths, DefaultGraphConfig())

	if selection.Selected == nil {
		t.Fatal("expected a selected path")
	}
	if selection.Selected.Nodes[0].ActionType != "retry_job" {
		t.Errorf("expected retry_job (highest score), got %s", selection.Selected.Nodes[0].ActionType)
	}
	if selection.Reason != "highest_final_score" {
		t.Errorf("expected reason highest_final_score, got %s", selection.Reason)
	}
}

// --- Test 4: Risk aggregation correct ---

func TestEvaluatePath_RiskAggregation(t *testing.T) {
	path := DecisionPath{
		Nodes: []DecisionNode{
			{ID: "a", ActionType: "retry_job", ExpectedValue: 0.6, Risk: 0.3, Confidence: 0.8},
			{ID: "b", ActionType: "log_recommendation", ExpectedValue: 0.4, Risk: 0.2, Confidence: 0.7},
		},
	}

	config := DefaultGraphConfig()
	scored := EvaluatePath(path, config)

	// Risk = 1 - (1-0.3)(1-0.2) = 1 - 0.7*0.8 = 1 - 0.56 = 0.44
	expectedRisk := 0.44
	if abs(scored.TotalRisk-expectedRisk) > 0.01 {
		t.Errorf("expected risk ~%.2f, got %.4f", expectedRisk, scored.TotalRisk)
	}

	// Risk should NOT be simple sum (0.5), should be compounding (0.44).
	if abs(scored.TotalRisk-0.5) < 0.01 {
		t.Error("risk appears to be a simple sum instead of aggregated")
	}
}

func TestEvaluatePath_SingleNodeRisk(t *testing.T) {
	path := DecisionPath{
		Nodes: []DecisionNode{
			{ID: "a", ActionType: "retry_job", ExpectedValue: 0.6, Risk: 0.3, Confidence: 0.8},
		},
	}

	scored := EvaluatePath(path, DefaultGraphConfig())

	// Single node: risk = 1 - (1-0.3) = 0.3
	if abs(scored.TotalRisk-0.3) > 0.001 {
		t.Errorf("expected risk 0.30, got %.4f", scored.TotalRisk)
	}
}

// --- Test 5: Confidence propagation correct ---

func TestEvaluatePath_ConfidenceMinimum(t *testing.T) {
	path := DecisionPath{
		Nodes: []DecisionNode{
			{ID: "a", ActionType: "retry_job", ExpectedValue: 0.6, Risk: 0.1, Confidence: 0.9},
			{ID: "b", ActionType: "log_recommendation", ExpectedValue: 0.4, Risk: 0.1, Confidence: 0.6},
			{ID: "c", ActionType: "noop", ExpectedValue: 0.1, Risk: 0.0, Confidence: 0.8},
		},
	}

	scored := EvaluatePath(path, DefaultGraphConfig())

	// TotalConfidence = min(0.9, 0.6, 0.8) = 0.6
	if abs(scored.TotalConfidence-0.6) > 0.001 {
		t.Errorf("expected confidence 0.6 (minimum), got %.4f", scored.TotalConfidence)
	}
}

// --- Test 6: Shorter path wins tie ---

func TestSelectBestPath_ShorterPathWinsTie(t *testing.T) {
	paths := []DecisionPath{
		{
			Nodes: []DecisionNode{
				{ID: "a", ActionType: "retry_job", ExpectedValue: 0.7, Risk: 0.2, Confidence: 0.8},
				{ID: "b", ActionType: "log_recommendation", ExpectedValue: 0.4, Risk: 0.1, Confidence: 0.7},
			},
			FinalScore: 0.5,
		},
		{
			Nodes: []DecisionNode{
				{ID: "c", ActionType: "retry_job", ExpectedValue: 0.7, Risk: 0.2, Confidence: 0.8},
			},
			FinalScore: 0.5,
		},
	}

	selection := SelectBestPath(paths, DefaultGraphConfig())

	if selection.Selected == nil {
		t.Fatal("expected a selected path")
	}
	if len(selection.Selected.Nodes) != 1 {
		t.Errorf("expected shorter path (1 node) on tie, got %d nodes", len(selection.Selected.Nodes))
	}
}

// --- Test 7: Safe mode limits depth ---

func TestBuildGraph_SafeMode_OnlyDepth1(t *testing.T) {
	config := GraphConfig{MaxDepth: 3, StabilityMode: "safe_mode"}

	input := BuildInput{
		GoalType:         "reduce_retry_rate",
		CandidateActions: []string{"retry_job", "log_recommendation"},
		Signals: map[string]ActionSignals{
			"retry_job":          {ExpectedValue: 0.7, Risk: 0.2, Confidence: 0.8},
			"log_recommendation": {ExpectedValue: 0.4, Risk: 0.05, Confidence: 0.9},
		},
		Config: config,
	}

	graph := BuildGraph(input)
	paths := EnumeratePaths(graph)

	// All paths should be depth=1 in safe_mode.
	for _, p := range paths {
		if len(p.Nodes) > 1 {
			t.Errorf("safe_mode: expected all paths depth=1, found path with %d nodes", len(p.Nodes))
		}
	}
}

// --- Test 8: Deterministic selection ---

func TestSelectBestPath_Deterministic(t *testing.T) {
	input := BuildInput{
		GoalType:         "reduce_retry_rate",
		CandidateActions: []string{"retry_job", "log_recommendation", "noop"},
		Signals: map[string]ActionSignals{
			"retry_job":          {ExpectedValue: 0.7, Risk: 0.2, Confidence: 0.8},
			"log_recommendation": {ExpectedValue: 0.5, Risk: 0.1, Confidence: 0.85},
			"noop":               {ExpectedValue: 0.1, Risk: 0.0, Confidence: 1.0},
		},
		Config: GraphConfig{MaxDepth: 2, StabilityMode: "normal"},
	}

	// Run twice with identical inputs.
	sel1, act1 := Evaluate(input)
	sel2, act2 := Evaluate(input)

	if act1 != act2 {
		t.Errorf("determinism violated: first run selected %s, second selected %s", act1, act2)
	}
	if sel1.Reason != sel2.Reason {
		t.Errorf("determinism violated: reason mismatch %s vs %s", sel1.Reason, sel2.Reason)
	}
	if sel1.Selected != nil && sel2.Selected != nil {
		if sel1.Selected.FinalScore != sel2.Selected.FinalScore {
			t.Errorf("determinism violated: score mismatch %.4f vs %.4f",
				sel1.Selected.FinalScore, sel2.Selected.FinalScore)
		}
	}
}

// --- Test 9: Execution only first step ---

func TestEvaluate_ExecutesOnlyFirstStep(t *testing.T) {
	input := BuildInput{
		GoalType:         "reduce_retry_rate",
		CandidateActions: []string{"retry_job", "log_recommendation", "noop"},
		Signals: map[string]ActionSignals{
			"retry_job":          {ExpectedValue: 0.8, Risk: 0.1, Confidence: 0.9},
			"log_recommendation": {ExpectedValue: 0.3, Risk: 0.05, Confidence: 0.9},
			"noop":               {ExpectedValue: 0.1, Risk: 0.0, Confidence: 1.0},
		},
		Config: GraphConfig{MaxDepth: 2, StabilityMode: "normal"},
	}

	selection, actionType := Evaluate(input)

	// Should return a single action type (first step of the best path).
	if actionType == "" {
		t.Fatal("expected an action type to be selected")
	}

	// The selection may have a multi-node path, but only first node matters.
	if selection.Selected != nil && len(selection.Selected.Nodes) > 0 {
		if actionType != selection.Selected.Nodes[0].ActionType {
			t.Errorf("returned action type %s doesn't match first node %s",
				actionType, selection.Selected.Nodes[0].ActionType)
		}
	}
}

// --- Additional tests ---

func TestEvaluatePath_EmptyPath(t *testing.T) {
	path := DecisionPath{Nodes: nil}
	scored := EvaluatePath(path, DefaultGraphConfig())

	if scored.FinalScore != 0 {
		t.Errorf("expected 0 score for empty path, got %.4f", scored.FinalScore)
	}
}

func TestEvaluatePath_ThrottledPenalizesLongPaths(t *testing.T) {
	config := GraphConfig{
		MaxDepth:        3,
		StabilityMode:   "throttled",
		LongPathPenalty: 0.15,
	}

	shortPath := DecisionPath{
		Nodes: []DecisionNode{
			{ID: "a", ActionType: "retry_job", ExpectedValue: 0.7, Risk: 0.1, Confidence: 0.8},
		},
	}
	longPath := DecisionPath{
		Nodes: []DecisionNode{
			{ID: "a", ActionType: "retry_job", ExpectedValue: 0.7, Risk: 0.1, Confidence: 0.8},
			{ID: "b", ActionType: "log_recommendation", ExpectedValue: 0.7, Risk: 0.1, Confidence: 0.8},
		},
	}

	shortScored := EvaluatePath(shortPath, config)
	longScored := EvaluatePath(longPath, config)

	// Long path should have higher risk due to throttled penalty.
	if longScored.TotalRisk <= shortScored.TotalRisk {
		t.Errorf("throttled mode: long path risk (%.4f) should exceed short path risk (%.4f)",
			longScored.TotalRisk, shortScored.TotalRisk)
	}
}

func TestSelectBestPath_ExplorationOverride(t *testing.T) {
	paths := []DecisionPath{
		{
			Nodes:      []DecisionNode{{ID: "a", ActionType: "retry_job"}},
			FinalScore: 0.8,
		},
		{
			Nodes:      []DecisionNode{{ID: "b", ActionType: "log_recommendation"}},
			FinalScore: 0.6,
		},
	}

	config := GraphConfig{ShouldExplore: true}
	selection := SelectBestPath(paths, config)

	if !selection.ExplorationUsed {
		t.Error("expected exploration to be used")
	}
	if selection.Selected == nil {
		t.Fatal("expected a selected path")
	}
	if selection.Selected.Nodes[0].ActionType != "log_recommendation" {
		t.Errorf("exploration should select second-best, got %s", selection.Selected.Nodes[0].ActionType)
	}
	if selection.Reason != "exploration_override_second_best" {
		t.Errorf("expected exploration_override reason, got %s", selection.Reason)
	}
}

func TestSelectBestPath_AllBadFallbackNoop(t *testing.T) {
	paths := []DecisionPath{
		{
			Nodes:      []DecisionNode{{ID: "a", ActionType: "retry_job"}},
			FinalScore: 0,
		},
		{
			Nodes:      []DecisionNode{{ID: "b", ActionType: "noop"}},
			FinalScore: 0,
		},
	}

	selection := SelectBestPath(paths, DefaultGraphConfig())

	if selection.Selected == nil {
		t.Fatal("expected fallback to noop")
	}
	if selection.Selected.Nodes[0].ActionType != "noop" {
		t.Errorf("expected noop fallback, got %s", selection.Selected.Nodes[0].ActionType)
	}
}

func TestSelectBestPath_NoPaths(t *testing.T) {
	selection := SelectBestPath(nil, DefaultGraphConfig())

	if selection.Reason != "no_paths" {
		t.Errorf("expected reason no_paths, got %s", selection.Reason)
	}
	if selection.Selected != nil {
		t.Error("expected nil selection for empty paths")
	}
}

func TestBuildGraph_MaxNodeCount(t *testing.T) {
	// Create many candidates to test node count limiting.
	candidates := make([]string, 25)
	signals := make(map[string]ActionSignals, 25)
	for i := 0; i < 25; i++ {
		name := "action_" + string(rune('a'+i))
		candidates[i] = name
		signals[name] = ActionSignals{ExpectedValue: 0.5, Risk: 0.1, Confidence: 0.8}
	}

	input := BuildInput{
		GoalType:         "reduce_retry_rate",
		CandidateActions: candidates,
		Signals:          signals,
		Config:           GraphConfig{MaxDepth: 3, StabilityMode: "normal"},
	}

	graph := BuildGraph(input)

	if len(graph.Nodes) > MaxNodeCount {
		t.Errorf("expected at most %d nodes, got %d", MaxNodeCount, len(graph.Nodes))
	}
}

func TestEvaluatePath_ScoreFormula(t *testing.T) {
	// Single node to verify exact formula.
	path := DecisionPath{
		Nodes: []DecisionNode{
			{ID: "a", ActionType: "retry_job", ExpectedValue: 0.8, Risk: 0.2, Confidence: 0.9},
		},
	}

	scored := EvaluatePath(path, DefaultGraphConfig())

	// TotalValue = 0.8, TotalRisk = 0.2, TotalConfidence = 0.9
	// FinalScore = 0.8*0.5 + 0.9*0.3 - 0.2*0.2 = 0.4 + 0.27 - 0.04 = 0.63
	expected := 0.63
	if abs(scored.FinalScore-expected) > 0.001 {
		t.Errorf("expected FinalScore %.3f, got %.4f", expected, scored.FinalScore)
	}
}

func TestDefaultTransitions_RetryFamily(t *testing.T) {
	transitions := defaultTransitions("reduce_retry_rate")

	retrySuccessors, ok := transitions["retry_job"]
	if !ok {
		t.Fatal("expected transitions for retry_job")
	}
	if len(retrySuccessors) == 0 {
		t.Error("expected at least one successor for retry_job")
	}
}

func TestDefaultTransitions_BacklogFamily(t *testing.T) {
	transitions := defaultTransitions("resolve_queue_backlog")

	resyncSuccessors, ok := transitions["trigger_resync"]
	if !ok {
		t.Fatal("expected transitions for trigger_resync")
	}
	if len(resyncSuccessors) == 0 {
		t.Error("expected at least one successor for trigger_resync")
	}
}

func TestEffectiveMaxDepth(t *testing.T) {
	tests := []struct {
		name     string
		config   GraphConfig
		expected int
	}{
		{"normal_default", GraphConfig{MaxDepth: 3, StabilityMode: "normal"}, 3},
		{"safe_mode", GraphConfig{MaxDepth: 3, StabilityMode: "safe_mode"}, 1},
		{"zero_depth", GraphConfig{MaxDepth: 0, StabilityMode: "normal"}, 3},
		{"custom_depth", GraphConfig{MaxDepth: 2, StabilityMode: "normal"}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.EffectiveMaxDepth()
			if got != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, got)
			}
		})
	}
}

func TestEvaluateAllPaths(t *testing.T) {
	paths := []DecisionPath{
		{
			Nodes: []DecisionNode{
				{ID: "a", ActionType: "retry_job", ExpectedValue: 0.8, Risk: 0.1, Confidence: 0.9},
			},
		},
		{
			Nodes: []DecisionNode{
				{ID: "b", ActionType: "noop", ExpectedValue: 0.1, Risk: 0.0, Confidence: 1.0},
			},
		},
	}

	scored := EvaluateAllPaths(paths, DefaultGraphConfig())

	if len(scored) != 2 {
		t.Fatalf("expected 2 scored paths, got %d", len(scored))
	}

	// First path (retry_job) should score higher than noop.
	if scored[0].FinalScore <= scored[1].FinalScore {
		t.Errorf("retry_job (%.4f) should score higher than noop (%.4f)",
			scored[0].FinalScore, scored[1].FinalScore)
	}
}

// --- Helper ---

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// --- Iteration 21: Path/Transition Learning Adjustment Tests ---

func TestApplyPathLearningAdjustments_PreferPathIncreasesScore(t *testing.T) {
	paths := []DecisionPath{
		{
			Nodes: []DecisionNode{
				{ID: "a", ActionType: "retry_job"},
				{ID: "b", ActionType: "log_recommendation"},
			},
			FinalScore: 0.5,
		},
	}

	signals := &PathLearningSignals{
		PathFeedback: map[string]string{
			"retry_job>log_recommendation": "prefer_path",
		},
		TransitionFeedback: map[string]string{},
	}

	adjusted := ApplyPathLearningAdjustments(paths, signals)

	if adjusted[0].FinalScore <= 0.5 {
		t.Errorf("prefer_path should increase score, got %.4f", adjusted[0].FinalScore)
	}
	expected := 0.5 + pathPreferAdjustment
	if abs(adjusted[0].FinalScore-expected) > 0.001 {
		t.Errorf("expected FinalScore %.4f, got %.4f", expected, adjusted[0].FinalScore)
	}
}

func TestApplyPathLearningAdjustments_AvoidPathDecreasesScore(t *testing.T) {
	paths := []DecisionPath{
		{
			Nodes: []DecisionNode{
				{ID: "a", ActionType: "retry_job"},
				{ID: "b", ActionType: "log_recommendation"},
			},
			FinalScore: 0.5,
		},
	}

	signals := &PathLearningSignals{
		PathFeedback: map[string]string{
			"retry_job>log_recommendation": "avoid_path",
		},
		TransitionFeedback: map[string]string{},
	}

	adjusted := ApplyPathLearningAdjustments(paths, signals)

	if adjusted[0].FinalScore >= 0.5 {
		t.Errorf("avoid_path should decrease score, got %.4f", adjusted[0].FinalScore)
	}
	expected := 0.5 + pathAvoidAdjustment // 0.5 - 0.20 = 0.30
	if abs(adjusted[0].FinalScore-expected) > 0.001 {
		t.Errorf("expected FinalScore %.4f, got %.4f", expected, adjusted[0].FinalScore)
	}
}

func TestApplyPathLearningAdjustments_PreferTransitionIncreasesScore(t *testing.T) {
	paths := []DecisionPath{
		{
			Nodes: []DecisionNode{
				{ID: "a", ActionType: "retry_job"},
				{ID: "b", ActionType: "log_recommendation"},
			},
			FinalScore: 0.5,
		},
	}

	signals := &PathLearningSignals{
		PathFeedback: map[string]string{},
		TransitionFeedback: map[string]string{
			"retry_job->log_recommendation": "prefer_transition",
		},
	}

	adjusted := ApplyPathLearningAdjustments(paths, signals)

	expected := 0.5 + transitionPreferAdjustment // 0.5 + 0.05 = 0.55
	if abs(adjusted[0].FinalScore-expected) > 0.001 {
		t.Errorf("expected FinalScore %.4f, got %.4f", expected, adjusted[0].FinalScore)
	}
}

func TestApplyPathLearningAdjustments_AvoidTransitionDecreasesScore(t *testing.T) {
	paths := []DecisionPath{
		{
			Nodes: []DecisionNode{
				{ID: "a", ActionType: "retry_job"},
				{ID: "b", ActionType: "log_recommendation"},
			},
			FinalScore: 0.5,
		},
	}

	signals := &PathLearningSignals{
		PathFeedback: map[string]string{},
		TransitionFeedback: map[string]string{
			"retry_job->log_recommendation": "avoid_transition",
		},
	}

	adjusted := ApplyPathLearningAdjustments(paths, signals)

	expected := 0.5 + transitionAvoidAdjustment // 0.5 - 0.10 = 0.40
	if abs(adjusted[0].FinalScore-expected) > 0.001 {
		t.Errorf("expected FinalScore %.4f, got %.4f", expected, adjusted[0].FinalScore)
	}
}

func TestApplyPathLearningAdjustments_CombinedAdjustments(t *testing.T) {
	paths := []DecisionPath{
		{
			Nodes: []DecisionNode{
				{ID: "a", ActionType: "retry_job"},
				{ID: "b", ActionType: "log_recommendation"},
			},
			FinalScore: 0.5,
		},
	}

	signals := &PathLearningSignals{
		PathFeedback: map[string]string{
			"retry_job>log_recommendation": "prefer_path",
		},
		TransitionFeedback: map[string]string{
			"retry_job->log_recommendation": "prefer_transition",
		},
	}

	adjusted := ApplyPathLearningAdjustments(paths, signals)

	expected := 0.5 + pathPreferAdjustment + transitionPreferAdjustment // 0.5 + 0.10 + 0.05 = 0.65
	if abs(adjusted[0].FinalScore-expected) > 0.001 {
		t.Errorf("expected FinalScore %.4f, got %.4f", expected, adjusted[0].FinalScore)
	}
}

func TestApplyPathLearningAdjustments_NilSignalsNoChange(t *testing.T) {
	paths := []DecisionPath{
		{
			Nodes: []DecisionNode{
				{ID: "a", ActionType: "retry_job"},
			},
			FinalScore: 0.5,
		},
	}

	adjusted := ApplyPathLearningAdjustments(paths, nil)

	if adjusted[0].FinalScore != 0.5 {
		t.Errorf("nil signals should leave score unchanged, got %.4f", adjusted[0].FinalScore)
	}
}

func TestApplyPathLearningAdjustments_NoMatchingSignalsNoChange(t *testing.T) {
	paths := []DecisionPath{
		{
			Nodes: []DecisionNode{
				{ID: "a", ActionType: "retry_job"},
			},
			FinalScore: 0.5,
		},
	}

	signals := &PathLearningSignals{
		PathFeedback:       map[string]string{"nonexistent_path": "prefer_path"},
		TransitionFeedback: map[string]string{"nonexistent_key": "prefer_transition"},
	}

	adjusted := ApplyPathLearningAdjustments(paths, signals)

	if adjusted[0].FinalScore != 0.5 {
		t.Errorf("non-matching signals should leave score unchanged, got %.4f", adjusted[0].FinalScore)
	}
}

func TestApplyPathLearningAdjustments_SingleNodeNoTransitionEffect(t *testing.T) {
	paths := []DecisionPath{
		{
			Nodes: []DecisionNode{
				{ID: "a", ActionType: "retry_job"},
			},
			FinalScore: 0.5,
		},
	}

	signals := &PathLearningSignals{
		PathFeedback: map[string]string{},
		TransitionFeedback: map[string]string{
			"retry_job->log_recommendation": "avoid_transition",
		},
	}

	adjusted := ApplyPathLearningAdjustments(paths, signals)

	// Single-node path has no transitions, so transition signals shouldn't apply.
	if adjusted[0].FinalScore != 0.5 {
		t.Errorf("single-node path should not be affected by transition signals, got %.4f", adjusted[0].FinalScore)
	}
}

func TestApplyPathLearningAdjustments_ScoreClampedToRange(t *testing.T) {
	paths := []DecisionPath{
		{
			Nodes: []DecisionNode{
				{ID: "a", ActionType: "retry_job"},
				{ID: "b", ActionType: "log_recommendation"},
			},
			FinalScore: 0.05, // Very low score.
		},
	}

	signals := &PathLearningSignals{
		PathFeedback: map[string]string{
			"retry_job>log_recommendation": "avoid_path",
		},
		TransitionFeedback: map[string]string{
			"retry_job->log_recommendation": "avoid_transition",
		},
	}

	adjusted := ApplyPathLearningAdjustments(paths, signals)

	// 0.05 - 0.20 - 0.10 = -0.25, should be clamped to 0.
	if adjusted[0].FinalScore < 0 {
		t.Errorf("score should be clamped to 0, got %.4f", adjusted[0].FinalScore)
	}
	if adjusted[0].FinalScore != 0 {
		t.Errorf("expected score 0 (clamped), got %.4f", adjusted[0].FinalScore)
	}
}

func TestApplyPathLearningAdjustments_MultipleTransitions(t *testing.T) {
	paths := []DecisionPath{
		{
			Nodes: []DecisionNode{
				{ID: "a", ActionType: "retry_job"},
				{ID: "b", ActionType: "log_recommendation"},
				{ID: "c", ActionType: "trigger_resync"},
			},
			FinalScore: 0.5,
		},
	}

	signals := &PathLearningSignals{
		PathFeedback: map[string]string{},
		TransitionFeedback: map[string]string{
			"retry_job->log_recommendation":      "prefer_transition",
			"log_recommendation->trigger_resync": "avoid_transition",
		},
	}

	adjusted := ApplyPathLearningAdjustments(paths, signals)

	// 0.5 + 0.05 - 0.10 = 0.45
	expected := 0.5 + transitionPreferAdjustment + transitionAvoidAdjustment
	if abs(adjusted[0].FinalScore-expected) > 0.001 {
		t.Errorf("expected FinalScore %.4f, got %.4f", expected, adjusted[0].FinalScore)
	}
}

func TestPathSignatureFromNodes(t *testing.T) {
	nodes := []DecisionNode{
		{ActionType: "retry_job"},
		{ActionType: "log_recommendation"},
	}
	sig := pathSignatureFromNodes(nodes)
	if sig != "retry_job>log_recommendation" {
		t.Errorf("expected 'retry_job>log_recommendation', got '%s'", sig)
	}
}

func TestPathSignatureFromNodes_Empty(t *testing.T) {
	sig := pathSignatureFromNodes(nil)
	if sig != "" {
		t.Errorf("expected empty string, got '%s'", sig)
	}
}

func TestPathSignatureFromNodes_Single(t *testing.T) {
	nodes := []DecisionNode{{ActionType: "retry_job"}}
	sig := pathSignatureFromNodes(nodes)
	if sig != "retry_job" {
		t.Errorf("expected 'retry_job', got '%s'", sig)
	}
}

// --- Comparative Learning Adjustment Tests (Iteration 22) ---

func TestComparativeAdjustments_PreferIncreasesScore(t *testing.T) {
	paths := []DecisionPath{
		{Nodes: []DecisionNode{{ActionType: "retry_job"}}, FinalScore: 0.5},
	}
	signals := &ComparativeLearningSignals{
		ComparativeFeedback: map[string]string{
			"retry_job": "prefer_path",
		},
	}
	adjusted := ApplyComparativeLearningAdjustments(paths, signals)
	expected := 0.5 + comparativePreferAdjustment
	if abs(adjusted[0].FinalScore-expected) > 0.001 {
		t.Errorf("expected FinalScore %.4f, got %.4f", expected, adjusted[0].FinalScore)
	}
}

func TestComparativeAdjustments_AvoidDecreasesScore(t *testing.T) {
	paths := []DecisionPath{
		{Nodes: []DecisionNode{{ActionType: "log_recommendation"}}, FinalScore: 0.6},
	}
	signals := &ComparativeLearningSignals{
		ComparativeFeedback: map[string]string{
			"log_recommendation": "avoid_path",
		},
	}
	adjusted := ApplyComparativeLearningAdjustments(paths, signals)
	expected := 0.6 + comparativeAvoidAdjustment
	if abs(adjusted[0].FinalScore-expected) > 0.001 {
		t.Errorf("expected FinalScore %.4f, got %.4f", expected, adjusted[0].FinalScore)
	}
}

func TestComparativeAdjustments_UnderexploredBoost(t *testing.T) {
	paths := []DecisionPath{
		{Nodes: []DecisionNode{{ActionType: "trigger_resync"}}, FinalScore: 0.4},
	}
	signals := &ComparativeLearningSignals{
		ComparativeFeedback: map[string]string{
			"trigger_resync": "underexplored_path",
		},
	}
	adjusted := ApplyComparativeLearningAdjustments(paths, signals)
	expected := 0.4 + comparativeUnderexploredAdjustment
	if abs(adjusted[0].FinalScore-expected) > 0.001 {
		t.Errorf("expected FinalScore %.4f, got %.4f", expected, adjusted[0].FinalScore)
	}
}

func TestComparativeAdjustments_NilSignalsNoChange(t *testing.T) {
	paths := []DecisionPath{
		{Nodes: []DecisionNode{{ActionType: "retry_job"}}, FinalScore: 0.5},
	}
	adjusted := ApplyComparativeLearningAdjustments(paths, nil)
	if abs(adjusted[0].FinalScore-0.5) > 0.001 {
		t.Errorf("expected FinalScore 0.5000, got %.4f", adjusted[0].FinalScore)
	}
}

func TestComparativeAdjustments_EmptyFeedbackNoChange(t *testing.T) {
	paths := []DecisionPath{
		{Nodes: []DecisionNode{{ActionType: "retry_job"}}, FinalScore: 0.5},
	}
	signals := &ComparativeLearningSignals{
		ComparativeFeedback: map[string]string{},
	}
	adjusted := ApplyComparativeLearningAdjustments(paths, signals)
	if abs(adjusted[0].FinalScore-0.5) > 0.001 {
		t.Errorf("expected FinalScore 0.5000, got %.4f", adjusted[0].FinalScore)
	}
}

func TestComparativeAdjustments_NeutralNoChange(t *testing.T) {
	paths := []DecisionPath{
		{Nodes: []DecisionNode{{ActionType: "retry_job"}}, FinalScore: 0.5},
	}
	signals := &ComparativeLearningSignals{
		ComparativeFeedback: map[string]string{
			"retry_job": "neutral",
		},
	}
	adjusted := ApplyComparativeLearningAdjustments(paths, signals)
	if abs(adjusted[0].FinalScore-0.5) > 0.001 {
		t.Errorf("expected FinalScore 0.5000, got %.4f", adjusted[0].FinalScore)
	}
}

func TestComparativeAdjustments_ClampToZero(t *testing.T) {
	paths := []DecisionPath{
		{Nodes: []DecisionNode{{ActionType: "retry_job"}}, FinalScore: 0.1},
	}
	signals := &ComparativeLearningSignals{
		ComparativeFeedback: map[string]string{
			"retry_job": "avoid_path",
		},
	}
	adjusted := ApplyComparativeLearningAdjustments(paths, signals)
	// 0.1 + (-0.20) = -0.1 → clamped to 0
	if adjusted[0].FinalScore != 0 {
		t.Errorf("expected FinalScore 0 (clamped), got %.4f", adjusted[0].FinalScore)
	}
}

func TestComparativeAdjustments_ClampToOne(t *testing.T) {
	paths := []DecisionPath{
		{Nodes: []DecisionNode{{ActionType: "retry_job"}}, FinalScore: 0.95},
	}
	signals := &ComparativeLearningSignals{
		ComparativeFeedback: map[string]string{
			"retry_job": "prefer_path",
		},
	}
	adjusted := ApplyComparativeLearningAdjustments(paths, signals)
	// 0.95 + 0.10 = 1.05 → clamped to 1
	if adjusted[0].FinalScore != 1 {
		t.Errorf("expected FinalScore 1 (clamped), got %.4f", adjusted[0].FinalScore)
	}
}

func TestComparativeAdjustments_MultiplePaths(t *testing.T) {
	paths := []DecisionPath{
		{Nodes: []DecisionNode{{ActionType: "retry_job"}}, FinalScore: 0.5},
		{Nodes: []DecisionNode{{ActionType: "log_recommendation"}}, FinalScore: 0.6},
		{Nodes: []DecisionNode{{ActionType: "noop"}}, FinalScore: 0.3},
	}
	signals := &ComparativeLearningSignals{
		ComparativeFeedback: map[string]string{
			"retry_job":          "prefer_path",
			"log_recommendation": "avoid_path",
			// noop has no feedback
		},
	}
	adjusted := ApplyComparativeLearningAdjustments(paths, signals)

	if abs(adjusted[0].FinalScore-(0.5+comparativePreferAdjustment)) > 0.001 {
		t.Errorf("retry_job: expected %.4f, got %.4f", 0.5+comparativePreferAdjustment, adjusted[0].FinalScore)
	}
	if abs(adjusted[1].FinalScore-(0.6+comparativeAvoidAdjustment)) > 0.001 {
		t.Errorf("log_recommendation: expected %.4f, got %.4f", 0.6+comparativeAvoidAdjustment, adjusted[1].FinalScore)
	}
	if abs(adjusted[2].FinalScore-0.3) > 0.001 {
		t.Errorf("noop: expected 0.3000, got %.4f", adjusted[2].FinalScore)
	}
}

func TestComparativeAdjustments_NoMatchingSignals(t *testing.T) {
	paths := []DecisionPath{
		{Nodes: []DecisionNode{{ActionType: "retry_job"}}, FinalScore: 0.5},
	}
	signals := &ComparativeLearningSignals{
		ComparativeFeedback: map[string]string{
			"other_action": "prefer_path",
		},
	}
	adjusted := ApplyComparativeLearningAdjustments(paths, signals)
	if abs(adjusted[0].FinalScore-0.5) > 0.001 {
		t.Errorf("expected FinalScore 0.5000, got %.4f", adjusted[0].FinalScore)
	}
}

func TestComparativeAdjustments_MultiNodePathSignature(t *testing.T) {
	paths := []DecisionPath{
		{
			Nodes: []DecisionNode{
				{ActionType: "retry_job"},
				{ActionType: "log_recommendation"},
			},
			FinalScore: 0.5,
		},
	}
	signals := &ComparativeLearningSignals{
		ComparativeFeedback: map[string]string{
			"retry_job>log_recommendation": "prefer_path",
		},
	}
	adjusted := ApplyComparativeLearningAdjustments(paths, signals)
	expected := 0.5 + comparativePreferAdjustment
	if abs(adjusted[0].FinalScore-expected) > 0.001 {
		t.Errorf("expected FinalScore %.4f, got %.4f", expected, adjusted[0].FinalScore)
	}
}

func TestComparativeAdjustments_CombinedWithPathLearning(t *testing.T) {
	// Simulate path learning already applied, then comparative adjustments on top.
	paths := []DecisionPath{
		{Nodes: []DecisionNode{{ActionType: "retry_job"}}, FinalScore: 0.6}, // already +0.10 from path learning
	}
	signals := &ComparativeLearningSignals{
		ComparativeFeedback: map[string]string{
			"retry_job": "prefer_path",
		},
	}
	adjusted := ApplyComparativeLearningAdjustments(paths, signals)
	expected := 0.6 + comparativePreferAdjustment // 0.70
	if abs(adjusted[0].FinalScore-expected) > 0.001 {
		t.Errorf("expected FinalScore %.4f, got %.4f", expected, adjusted[0].FinalScore)
	}
}

// --- Counterfactual Adjustment Tests ---

func TestApplyCounterfactualAdjustments_Nil(t *testing.T) {
	paths := []DecisionPath{
		{Nodes: []DecisionNode{{ActionType: "retry_job"}}, FinalScore: 0.50},
	}
	result := ApplyCounterfactualAdjustments(paths, nil)
	if result[0].FinalScore != 0.50 {
		t.Errorf("expected unchanged score 0.50, got %.4f", result[0].FinalScore)
	}
}

func TestApplyCounterfactualAdjustments_Empty(t *testing.T) {
	paths := []DecisionPath{
		{Nodes: []DecisionNode{{ActionType: "retry_job"}}, FinalScore: 0.50},
	}
	preds := &CounterfactualPredictions{
		Predictions: map[string]float64{},
		Confidences: map[string]float64{},
	}
	result := ApplyCounterfactualAdjustments(paths, preds)
	if result[0].FinalScore != 0.50 {
		t.Errorf("expected unchanged score 0.50, got %.4f", result[0].FinalScore)
	}
}

func TestApplyCounterfactualAdjustments_HighPrediction(t *testing.T) {
	paths := []DecisionPath{
		{Nodes: []DecisionNode{{ActionType: "retry_job"}}, FinalScore: 0.50},
	}
	preds := &CounterfactualPredictions{
		Predictions: map[string]float64{"retry_job": 0.80},
		Confidences: map[string]float64{"retry_job": 0.50},
	}
	result := ApplyCounterfactualAdjustments(paths, preds)
	// AdjustedScore = 0.50 + (0.80 - 0.50) * 0.20 = 0.50 + 0.06 = 0.56
	expected := 0.56
	if abs(result[0].FinalScore-expected) > 0.001 {
		t.Errorf("expected FinalScore %.4f, got %.4f", expected, result[0].FinalScore)
	}
}

func TestApplyCounterfactualAdjustments_LowPrediction(t *testing.T) {
	paths := []DecisionPath{
		{Nodes: []DecisionNode{{ActionType: "log_recommendation"}}, FinalScore: 0.70},
	}
	preds := &CounterfactualPredictions{
		Predictions: map[string]float64{"log_recommendation": 0.20},
		Confidences: map[string]float64{"log_recommendation": 0.50},
	}
	result := ApplyCounterfactualAdjustments(paths, preds)
	// AdjustedScore = 0.70 + (0.20 - 0.70) * 0.20 = 0.70 - 0.10 = 0.60
	expected := 0.60
	if abs(result[0].FinalScore-expected) > 0.001 {
		t.Errorf("expected FinalScore %.4f, got %.4f", expected, result[0].FinalScore)
	}
}

func TestApplyCounterfactualAdjustments_LowConfidenceIgnored(t *testing.T) {
	paths := []DecisionPath{
		{Nodes: []DecisionNode{{ActionType: "retry_job"}}, FinalScore: 0.50},
	}
	preds := &CounterfactualPredictions{
		Predictions: map[string]float64{"retry_job": 0.90},
		Confidences: map[string]float64{"retry_job": 0.005}, // below threshold
	}
	result := ApplyCounterfactualAdjustments(paths, preds)
	if result[0].FinalScore != 0.50 {
		t.Errorf("expected unchanged score 0.50, got %.4f", result[0].FinalScore)
	}
}

func TestApplyCounterfactualAdjustments_ClampedToZeroOne(t *testing.T) {
	paths := []DecisionPath{
		{Nodes: []DecisionNode{{ActionType: "retry_job"}}, FinalScore: 0.98},
	}
	preds := &CounterfactualPredictions{
		Predictions: map[string]float64{"retry_job": 1.50}, // artificially high
		Confidences: map[string]float64{"retry_job": 0.50},
	}
	result := ApplyCounterfactualAdjustments(paths, preds)
	if result[0].FinalScore > 1.0 {
		t.Errorf("expected clamped to 1.0, got %.4f", result[0].FinalScore)
	}
}

func TestApplyCounterfactualAdjustments_UnpredictedPathUnchanged(t *testing.T) {
	paths := []DecisionPath{
		{Nodes: []DecisionNode{{ActionType: "retry_job"}}, FinalScore: 0.50},
		{Nodes: []DecisionNode{{ActionType: "noop"}}, FinalScore: 0.30},
	}
	preds := &CounterfactualPredictions{
		Predictions: map[string]float64{"retry_job": 0.80},
		Confidences: map[string]float64{"retry_job": 0.50},
	}
	result := ApplyCounterfactualAdjustments(paths, preds)
	if result[1].FinalScore != 0.30 {
		t.Errorf("expected noop unchanged at 0.30, got %.4f", result[1].FinalScore)
	}
}
