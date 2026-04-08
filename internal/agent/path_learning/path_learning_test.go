package pathlearning

import (
	"testing"
	"time"
)

// --- Test 1: Path signature canonicalization ---

func TestBuildPathSignature_SingleAction(t *testing.T) {
	sig := BuildPathSignature([]string{"retry_job"})
	if sig != "retry_job" {
		t.Errorf("expected 'retry_job', got '%s'", sig)
	}
}

func TestBuildPathSignature_MultipleActions(t *testing.T) {
	sig := BuildPathSignature([]string{"retry_job", "log_recommendation"})
	if sig != "retry_job>log_recommendation" {
		t.Errorf("expected 'retry_job>log_recommendation', got '%s'", sig)
	}
}

func TestBuildPathSignature_ThreeActions(t *testing.T) {
	sig := BuildPathSignature([]string{"retry_job", "log_recommendation", "trigger_resync"})
	expected := "retry_job>log_recommendation>trigger_resync"
	if sig != expected {
		t.Errorf("expected '%s', got '%s'", expected, sig)
	}
}

func TestBuildPathSignature_Empty(t *testing.T) {
	sig := BuildPathSignature(nil)
	if sig != "" {
		t.Errorf("expected empty string, got '%s'", sig)
	}
}

func TestBuildPathSignature_Deterministic(t *testing.T) {
	actions := []string{"retry_job", "log_recommendation"}
	sig1 := BuildPathSignature(actions)
	sig2 := BuildPathSignature(actions)
	if sig1 != sig2 {
		t.Errorf("path signature not deterministic: '%s' vs '%s'", sig1, sig2)
	}
}

// --- Test 2: Transition key canonicalization ---

func TestBuildTransitionKey(t *testing.T) {
	key := BuildTransitionKey("retry_job", "log_recommendation")
	if key != "retry_job->log_recommendation" {
		t.Errorf("expected 'retry_job->log_recommendation', got '%s'", key)
	}
}

func TestBuildTransitionKey_Reversed(t *testing.T) {
	key := BuildTransitionKey("log_recommendation", "retry_job")
	if key != "log_recommendation->retry_job" {
		t.Errorf("expected 'log_recommendation->retry_job', got '%s'", key)
	}
}

// --- Test 3: Extract transitions ---

func TestExtractTransitions_TwoActions(t *testing.T) {
	pairs := ExtractTransitions([]string{"retry_job", "log_recommendation"})
	if len(pairs) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(pairs))
	}
	if pairs[0].From != "retry_job" || pairs[0].To != "log_recommendation" {
		t.Errorf("expected retry_job->log_recommendation, got %s->%s", pairs[0].From, pairs[0].To)
	}
}

func TestExtractTransitions_ThreeActions(t *testing.T) {
	pairs := ExtractTransitions([]string{"retry_job", "log_recommendation", "noop"})
	if len(pairs) != 2 {
		t.Fatalf("expected 2 transitions, got %d", len(pairs))
	}
	if pairs[0].From != "retry_job" || pairs[0].To != "log_recommendation" {
		t.Errorf("transition 0: expected retry_job->log_recommendation, got %s->%s", pairs[0].From, pairs[0].To)
	}
	if pairs[1].From != "log_recommendation" || pairs[1].To != "noop" {
		t.Errorf("transition 1: expected log_recommendation->noop, got %s->%s", pairs[1].From, pairs[1].To)
	}
}

func TestExtractTransitions_SingleAction(t *testing.T) {
	pairs := ExtractTransitions([]string{"retry_job"})
	if pairs != nil {
		t.Errorf("expected nil transitions for single action, got %d", len(pairs))
	}
}

func TestExtractTransitions_Empty(t *testing.T) {
	pairs := ExtractTransitions(nil)
	if pairs != nil {
		t.Errorf("expected nil transitions for empty list, got %d", len(pairs))
	}
}

// --- Test 4: Path outcome classification ---

func TestClassifyTransitionHelpfulness_Helpful(t *testing.T) {
	// Step 1 failed, but final result is success → transition was helpful.
	result := classifyTransitionHelpfulness("failure", "success", "success", false)
	if result != "helpful" {
		t.Errorf("expected 'helpful', got '%s'", result)
	}
}

func TestClassifyTransitionHelpfulness_HelpfulWithImprovement(t *testing.T) {
	// Step 1 neutral, final neutral but improvement detected → helpful.
	result := classifyTransitionHelpfulness("neutral", "neutral", "neutral", true)
	if result != "helpful" {
		t.Errorf("expected 'helpful', got '%s'", result)
	}
}

func TestClassifyTransitionHelpfulness_HelpfulStep2Success(t *testing.T) {
	// Step 2 success + final success → helpful.
	result := classifyTransitionHelpfulness("success", "success", "success", false)
	if result != "helpful" {
		t.Errorf("expected 'helpful', got '%s'", result)
	}
}

func TestClassifyTransitionHelpfulness_Unhelpful(t *testing.T) {
	// Step 2 ran but result still failure, no improvement → unhelpful.
	result := classifyTransitionHelpfulness("failure", "failure", "failure", false)
	if result != "unhelpful" {
		t.Errorf("expected 'unhelpful', got '%s'", result)
	}
}

func TestClassifyTransitionHelpfulness_UnhelpfulNeutral(t *testing.T) {
	// Step 2 ran, result stayed neutral, no improvement → unhelpful.
	result := classifyTransitionHelpfulness("neutral", "neutral", "neutral", false)
	if result != "unhelpful" {
		t.Errorf("expected 'unhelpful', got '%s'", result)
	}
}

func TestClassifyTransitionHelpfulness_NeutralNoStep2(t *testing.T) {
	// No step 2 status → neutral.
	result := classifyTransitionHelpfulness("success", "", "success", false)
	if result != "neutral" {
		t.Errorf("expected 'neutral', got '%s'", result)
	}
}

// --- Test 5: Path feedback generation ---

func TestGeneratePathFeedback_PreferPath(t *testing.T) {
	record := &PathMemoryRecord{
		PathSignature: "retry_job>log_recommendation",
		GoalType:      "reduce_retry_rate",
		TotalRuns:     10,
		SuccessRuns:   8,
		FailureRuns:   1,
		NeutralRuns:   1,
		SuccessRate:   0.8,
		FailureRate:   0.1,
	}

	fb := GeneratePathFeedback(record)
	if fb.Recommendation != RecommendPreferPath {
		t.Errorf("expected prefer_path, got %s", fb.Recommendation)
	}
}

func TestGeneratePathFeedback_AvoidPath(t *testing.T) {
	record := &PathMemoryRecord{
		PathSignature: "retry_job>log_recommendation",
		GoalType:      "reduce_retry_rate",
		TotalRuns:     10,
		SuccessRuns:   2,
		FailureRuns:   6,
		NeutralRuns:   2,
		SuccessRate:   0.2,
		FailureRate:   0.6,
	}

	fb := GeneratePathFeedback(record)
	if fb.Recommendation != RecommendAvoidPath {
		t.Errorf("expected avoid_path, got %s", fb.Recommendation)
	}
}

func TestGeneratePathFeedback_Neutral(t *testing.T) {
	record := &PathMemoryRecord{
		PathSignature: "retry_job",
		GoalType:      "reduce_retry_rate",
		TotalRuns:     10,
		SuccessRuns:   5,
		FailureRuns:   3,
		NeutralRuns:   2,
		SuccessRate:   0.5,
		FailureRate:   0.3,
	}

	fb := GeneratePathFeedback(record)
	if fb.Recommendation != RecommendNeutralPath {
		t.Errorf("expected neutral, got %s", fb.Recommendation)
	}
}

func TestGeneratePathFeedback_InsufficientData(t *testing.T) {
	record := &PathMemoryRecord{
		PathSignature: "retry_job",
		GoalType:      "reduce_retry_rate",
		TotalRuns:     3,
		SuccessRuns:   3,
		SuccessRate:   1.0,
	}

	fb := GeneratePathFeedback(record)
	// Even with 100% success rate, insufficient data → neutral.
	if fb.Recommendation != RecommendNeutralPath {
		t.Errorf("expected neutral for insufficient data, got %s", fb.Recommendation)
	}
}

// --- Test 6: Transition feedback generation ---

func TestGenerateTransitionFeedback_PreferTransition(t *testing.T) {
	record := &TransitionMemoryRecord{
		TransitionKey: "retry_job->log_recommendation",
		GoalType:      "reduce_retry_rate",
		TotalUses:     10,
		HelpfulUses:   7,
		UnhelpfulUses: 2,
		NeutralUses:   1,
		HelpfulRate:   0.7,
		UnhelpfulRate: 0.2,
	}

	fb := GenerateTransitionFeedback(record)
	if fb.Recommendation != RecommendPreferTransition {
		t.Errorf("expected prefer_transition, got %s", fb.Recommendation)
	}
}

func TestGenerateTransitionFeedback_AvoidTransition(t *testing.T) {
	record := &TransitionMemoryRecord{
		TransitionKey: "retry_job->log_recommendation",
		GoalType:      "reduce_retry_rate",
		TotalUses:     10,
		HelpfulUses:   2,
		UnhelpfulUses: 6,
		NeutralUses:   2,
		HelpfulRate:   0.2,
		UnhelpfulRate: 0.6,
	}

	fb := GenerateTransitionFeedback(record)
	if fb.Recommendation != RecommendAvoidTransition {
		t.Errorf("expected avoid_transition, got %s", fb.Recommendation)
	}
}

func TestGenerateTransitionFeedback_Neutral(t *testing.T) {
	record := &TransitionMemoryRecord{
		TransitionKey: "retry_job->log_recommendation",
		GoalType:      "reduce_retry_rate",
		TotalUses:     10,
		HelpfulUses:   4,
		UnhelpfulUses: 3,
		NeutralUses:   3,
		HelpfulRate:   0.4,
		UnhelpfulRate: 0.3,
	}

	fb := GenerateTransitionFeedback(record)
	if fb.Recommendation != RecommendNeutralTransition {
		t.Errorf("expected neutral, got %s", fb.Recommendation)
	}
}

func TestGenerateTransitionFeedback_InsufficientData(t *testing.T) {
	record := &TransitionMemoryRecord{
		TransitionKey: "retry_job->log_recommendation",
		GoalType:      "reduce_retry_rate",
		TotalUses:     3,
		HelpfulUses:   3,
		HelpfulRate:   1.0,
	}

	fb := GenerateTransitionFeedback(record)
	if fb.Recommendation != RecommendNeutralTransition {
		t.Errorf("expected neutral for insufficient data, got %s", fb.Recommendation)
	}
}

// --- Test 7: Outcome increment helpers ---

func TestOutcomeIncrements(t *testing.T) {
	tests := []struct {
		status  string
		s, f, n int
	}{
		{"success", 1, 0, 0},
		{"failure", 0, 1, 0},
		{"neutral", 0, 0, 1},
		{"unknown", 0, 0, 1}, // default to neutral
	}

	for _, tt := range tests {
		s, f, n := outcomeIncrements(tt.status)
		if s != tt.s || f != tt.f || n != tt.n {
			t.Errorf("outcomeIncrements(%s): expected (%d,%d,%d), got (%d,%d,%d)",
				tt.status, tt.s, tt.f, tt.n, s, f, n)
		}
	}
}

func TestHelpfulnessIncrements(t *testing.T) {
	tests := []struct {
		help    string
		h, u, n int
	}{
		{"helpful", 1, 0, 0},
		{"unhelpful", 0, 1, 0},
		{"neutral", 0, 0, 1},
		{"unknown", 0, 0, 1}, // default to neutral
	}

	for _, tt := range tests {
		h, u, n := helpfulnessIncrements(tt.help)
		if h != tt.h || u != tt.u || n != tt.n {
			t.Errorf("helpfulnessIncrements(%s): expected (%d,%d,%d), got (%d,%d,%d)",
				tt.help, tt.h, tt.u, tt.n, h, u, n)
		}
	}
}

// --- Test 8: Deterministic behavior across repeated runs ---

func TestPathFeedback_Deterministic(t *testing.T) {
	record := &PathMemoryRecord{
		PathSignature: "retry_job>log_recommendation",
		GoalType:      "reduce_retry_rate",
		TotalRuns:     10,
		SuccessRuns:   8,
		SuccessRate:   0.8,
		FailureRate:   0.1,
	}

	fb1 := GeneratePathFeedback(record)
	fb2 := GeneratePathFeedback(record)

	if fb1.Recommendation != fb2.Recommendation {
		t.Errorf("feedback not deterministic: %s vs %s", fb1.Recommendation, fb2.Recommendation)
	}
	if fb1.SuccessRate != fb2.SuccessRate {
		t.Errorf("success rate not deterministic: %.4f vs %.4f", fb1.SuccessRate, fb2.SuccessRate)
	}
}

func TestTransitionFeedback_Deterministic(t *testing.T) {
	record := &TransitionMemoryRecord{
		TransitionKey: "retry_job->log_recommendation",
		GoalType:      "reduce_retry_rate",
		TotalUses:     10,
		HelpfulUses:   7,
		HelpfulRate:   0.7,
		UnhelpfulRate: 0.2,
	}

	fb1 := GenerateTransitionFeedback(record)
	fb2 := GenerateTransitionFeedback(record)

	if fb1.Recommendation != fb2.Recommendation {
		t.Errorf("feedback not deterministic: %s vs %s", fb1.Recommendation, fb2.Recommendation)
	}
}

// --- Test 9: Threshold boundary conditions ---

func TestPathFeedback_AtThreshold(t *testing.T) {
	// Exactly at prefer threshold.
	record := &PathMemoryRecord{
		PathSignature: "retry_job",
		GoalType:      "reduce_retry_rate",
		TotalRuns:     5,
		SuccessRuns:   4,
		SuccessRate:   0.7, // Exactly at threshold.
		FailureRate:   0.1,
	}

	fb := GeneratePathFeedback(record)
	if fb.Recommendation != RecommendPreferPath {
		t.Errorf("expected prefer_path at threshold, got %s", fb.Recommendation)
	}
}

func TestPathFeedback_BelowThreshold(t *testing.T) {
	record := &PathMemoryRecord{
		PathSignature: "retry_job",
		GoalType:      "reduce_retry_rate",
		TotalRuns:     5,
		SuccessRuns:   3,
		SuccessRate:   0.69, // Just below threshold.
		FailureRate:   0.1,
	}

	fb := GeneratePathFeedback(record)
	if fb.Recommendation != RecommendNeutralPath {
		t.Errorf("expected neutral below threshold, got %s", fb.Recommendation)
	}
}

func TestTransitionFeedback_AtThreshold(t *testing.T) {
	record := &TransitionMemoryRecord{
		TransitionKey: "retry_job->log_recommendation",
		GoalType:      "reduce_retry_rate",
		TotalUses:     5,
		HelpfulRate:   0.6, // Exactly at threshold.
		UnhelpfulRate: 0.2,
	}

	fb := GenerateTransitionFeedback(record)
	if fb.Recommendation != RecommendPreferTransition {
		t.Errorf("expected prefer_transition at threshold, got %s", fb.Recommendation)
	}
}

// --- Test 10: PathOutcome model ---

func TestPathOutcome_Fields(t *testing.T) {
	now := time.Now().UTC()
	outcome := PathOutcome{
		PathID:           "test-path-1",
		GoalType:         "reduce_retry_rate",
		PathSignature:    "retry_job>log_recommendation",
		PathLength:       2,
		FirstStepAction:  "retry_job",
		FirstStepStatus:  "success",
		ContinuationUsed: false,
		FinalStatus:      "success",
		Improvement:      true,
		EvaluatedAt:      now,
	}

	if outcome.PathSignature != "retry_job>log_recommendation" {
		t.Errorf("unexpected path signature: %s", outcome.PathSignature)
	}
	if outcome.PathLength != 2 {
		t.Errorf("unexpected path length: %d", outcome.PathLength)
	}
	if outcome.FirstStepAction != "retry_job" {
		t.Errorf("unexpected first step action: %s", outcome.FirstStepAction)
	}
}

// --- Test 11: Neutral fallbacks ---

func TestNeutralPathFeedback(t *testing.T) {
	fb := neutralPathFeedback("retry_job", "reduce_retry_rate")
	if fb.Recommendation != RecommendNeutralPath {
		t.Errorf("expected neutral, got %s", fb.Recommendation)
	}
	if fb.PathSignature != "retry_job" {
		t.Errorf("expected path_signature 'retry_job', got '%s'", fb.PathSignature)
	}
}

func TestNeutralTransitionFeedback(t *testing.T) {
	fb := neutralTransitionFeedback("retry_job->log_recommendation", "reduce_retry_rate")
	if fb.Recommendation != RecommendNeutralTransition {
		t.Errorf("expected neutral, got %s", fb.Recommendation)
	}
}

// --- Test 12: firstStepAction helper ---

func TestFirstStepAction(t *testing.T) {
	if firstStepAction([]string{"retry_job", "log_recommendation"}) != "retry_job" {
		t.Error("expected retry_job as first step")
	}
	if firstStepAction(nil) != "" {
		t.Error("expected empty string for nil actions")
	}
	if firstStepAction([]string{}) != "" {
		t.Error("expected empty string for empty actions")
	}
}

// --- Test 13: Transition memory NOT updated when transition not executed ---

func TestClassifyTransitionHelpfulness_NoStep2Status(t *testing.T) {
	// When step2Status is empty, transition was not executed → always neutral.
	result := classifyTransitionHelpfulness("success", "", "success", true)
	if result != "neutral" {
		t.Errorf("expected 'neutral' for unexecuted transition, got '%s'", result)
	}
}

func TestClassifyTransitionHelpfulness_NoStep2Status_Failure(t *testing.T) {
	result := classifyTransitionHelpfulness("failure", "", "failure", false)
	if result != "neutral" {
		t.Errorf("expected 'neutral' for unexecuted transition, got '%s'", result)
	}
}

// --- Test 14: Order sensitivity ---

func TestBuildPathSignature_OrderMatters(t *testing.T) {
	sig1 := BuildPathSignature([]string{"retry_job", "log_recommendation"})
	sig2 := BuildPathSignature([]string{"log_recommendation", "retry_job"})

	if sig1 == sig2 {
		t.Error("different action orders should produce different signatures")
	}
}

func TestBuildTransitionKey_OrderMatters(t *testing.T) {
	key1 := BuildTransitionKey("retry_job", "log_recommendation")
	key2 := BuildTransitionKey("log_recommendation", "retry_job")

	if key1 == key2 {
		t.Error("different transition directions should produce different keys")
	}
}

// --- Test 15: Score adjustment constants ---

func TestScoreAdjustmentConstants(t *testing.T) {
	// Verify constants are within expected bounds.
	if PathPreferAdjustment != 0.10 {
		t.Errorf("expected PathPreferAdjustment=0.10, got %.2f", PathPreferAdjustment)
	}
	if PathAvoidAdjustment != -0.20 {
		t.Errorf("expected PathAvoidAdjustment=-0.20, got %.2f", PathAvoidAdjustment)
	}
	if TransitionPreferAdjustment != 0.05 {
		t.Errorf("expected TransitionPreferAdjustment=0.05, got %.2f", TransitionPreferAdjustment)
	}
	if TransitionAvoidAdjustment != -0.10 {
		t.Errorf("expected TransitionAvoidAdjustment=-0.10, got %.2f", TransitionAvoidAdjustment)
	}
}
