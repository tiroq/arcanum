package exploration

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/planning"
)

// --- Test Helpers ---

func assertFloatNear(t *testing.T, label string, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Errorf("%s: got %.4f, want %.4f (tol %.4f)", label, got, want, tolerance)
	}
}

type mockStability struct {
	mode StabilityMode
}

func (m *mockStability) GetMode(_ context.Context) StabilityMode { return m.mode }

type mockAudit struct {
	events []auditCall
}

type auditCall struct {
	entityType string
	eventType  string
	payload    any
}

func (m *mockAudit) RecordEvent(_ context.Context, entityType string, _ uuid.UUID, eventType, _, _ string, payload any) error {
	m.events = append(m.events, auditCall{entityType: entityType, eventType: eventType, payload: payload})
	return nil
}

func highConfidenceDecision() planning.PlanningDecision {
	return planning.PlanningDecision{
		GoalType:           "reduce_retry_rate",
		SelectedActionType: "retry_job",
		Candidates: []planning.PlannedActionCandidate{
			{ActionType: "retry_job", GoalType: "reduce_retry_rate", Score: 0.90, Confidence: 0.85},
			{ActionType: "log_recommendation", GoalType: "reduce_retry_rate", Score: 0.30, Confidence: 0.70},
			{ActionType: "noop", GoalType: "reduce_retry_rate", Score: 0.10, Confidence: 1.0},
		},
	}
}

func lowConfidenceDecision() planning.PlanningDecision {
	return planning.PlanningDecision{
		GoalType:           "reduce_retry_rate",
		SelectedActionType: "retry_job",
		Candidates: []planning.PlannedActionCandidate{
			{ActionType: "retry_job", GoalType: "reduce_retry_rate", Score: 0.55, Confidence: 0.40},
			{ActionType: "log_recommendation", GoalType: "reduce_retry_rate", Score: 0.50, Confidence: 0.35},
			{ActionType: "noop", GoalType: "reduce_retry_rate", Score: 0.10, Confidence: 1.0},
		},
	}
}

func noFeedback() map[string]actionmemory.ActionFeedback {
	return map[string]actionmemory.ActionFeedback{}
}

func newTestEngine(mode StabilityMode) (*Engine, *mockAudit) {
	a := &mockAudit{}
	s := &mockStability{mode: mode}
	e := NewEngine(nil, s, a, nil)
	return e, a
}

// Test 1: No exploration when confidence high

func TestNoExplorationWhenConfidenceHigh(t *testing.T) {
	engine, audit := newTestEngine(StabilityNormal)
	now := time.Now().UTC()
	d := engine.Evaluate(context.Background(), highConfidenceDecision(), noFeedback(), now)
	if d.Chosen {
		t.Fatal("exploration should NOT be chosen when confidence is high")
	}
	if d.ChosenActionType != "" {
		t.Fatalf("chosen action type should be empty, got %q", d.ChosenActionType)
	}
	if !d.Enabled {
		t.Fatal("exploration should be enabled in normal mode")
	}
	foundConsidered := false
	for _, e := range audit.events {
		if e.eventType == "exploration.chosen" {
			t.Fatal("should not have chosen audit event")
		}
		if e.eventType == "exploration.considered" {
			foundConsidered = true
		}
	}
	if !foundConsidered {
		t.Fatal("expected exploration.considered audit event")
	}
}

// Test 2: Exploration allowed when confidence low and budget available

func TestExplorationAllowedWhenConfidenceLow(t *testing.T) {
	engine, audit := newTestEngine(StabilityNormal)
	now := time.Now().UTC()
	d := engine.Evaluate(context.Background(), lowConfidenceDecision(), noFeedback(), now)
	if !d.Chosen {
		t.Fatalf("exploration should be chosen when confidence is low; reason=%s", d.DecisionReason)
	}
	if d.ChosenActionType == "" {
		t.Fatal("chosen action type should not be empty")
	}
	if d.ChosenActionType == "noop" {
		t.Fatal("should not explore noop")
	}
	if d.ChosenActionType != "log_recommendation" {
		t.Fatalf("expected log_recommendation, got %q", d.ChosenActionType)
	}
	foundChosen := false
	for _, e := range audit.events {
		if e.eventType == "exploration.chosen" {
			foundChosen = true
		}
	}
	if !foundChosen {
		t.Fatal("expected exploration.chosen audit event")
	}
}

// Test 3: No exploration when budget exhausted

func TestNoExplorationWhenBudgetExhausted(t *testing.T) {
	now := time.Now().UTC()

	// Cycle budget: 1 consume exhausts it
	b := DefaultBudget()
	b.Consume(now)
	if b.HasCycleBudget() {
		t.Fatal("cycle budget should be exhausted after 1 consume")
	}

	// Hourly budget: 3 consumes exhausts it
	b2 := DefaultBudget()
	b2.Consume(now)
	b2.Consume(now)
	b2.Consume(now)
	if b2.HasWindowBudget(now) {
		t.Fatal("hourly budget should be exhausted after 3 consumes")
	}
}

// Test 4: No exploration in safe_mode

func TestNoExplorationInSafeMode(t *testing.T) {
	engine, audit := newTestEngine(StabilitySafeMode)
	now := time.Now().UTC()
	d := engine.Evaluate(context.Background(), lowConfidenceDecision(), noFeedback(), now)
	if d.Chosen {
		t.Fatal("exploration must not be chosen in safe_mode")
	}
	if d.Enabled {
		t.Fatal("exploration should be disabled in safe_mode")
	}
	if d.DecisionReason != "stability_mode_safe_mode" {
		t.Fatalf("expected stability_mode_safe_mode reason, got %q", d.DecisionReason)
	}
	foundSkipped := false
	for _, e := range audit.events {
		if e.eventType == "exploration.skipped" {
			foundSkipped = true
		}
	}
	if !foundSkipped {
		t.Fatal("expected exploration.skipped audit event")
	}
}

// Test 5: No exploration in throttled mode

func TestNoExplorationInThrottledMode(t *testing.T) {
	engine, _ := newTestEngine(StabilityThrottled)
	now := time.Now().UTC()
	d := engine.Evaluate(context.Background(), lowConfidenceDecision(), noFeedback(), now)
	if d.Chosen {
		t.Fatal("exploration must not be chosen in throttled mode")
	}
	if d.Enabled {
		t.Fatal("exploration should be disabled in throttled mode")
	}
	if d.DecisionReason != "stability_mode_throttled" {
		t.Fatalf("expected stability_mode_throttled reason, got %q", d.DecisionReason)
	}
}

// Test 6: Exploration prefers underexplored safe action over noop

func TestExplorationPrefersUnderexploredSafeAction(t *testing.T) {
	decision := planning.PlanningDecision{
		GoalType:           "reduce_retry_rate",
		SelectedActionType: "retry_job",
		Candidates: []planning.PlannedActionCandidate{
			{ActionType: "retry_job", GoalType: "reduce_retry_rate", Score: 0.55, Confidence: 0.40},
			{ActionType: "log_recommendation", GoalType: "reduce_retry_rate", Score: 0.50, Confidence: 0.35},
			{ActionType: "trigger_resync", GoalType: "reduce_retry_rate", Score: 0.45, Confidence: 0.30},
			{ActionType: "noop", GoalType: "reduce_retry_rate", Score: 0.10, Confidence: 1.0},
		},
	}

	feedback := map[string]actionmemory.ActionFeedback{
		"log_recommendation": {
			ActionType:     "log_recommendation",
			SampleSize:     20,
			SuccessRate:    0.70,
			Recommendation: actionmemory.RecommendPreferAction,
		},
	}

	candidates := ScoreExplorationCandidates(decision, feedback)
	if len(candidates) < 2 {
		t.Fatalf("expected at least 2 candidates, got %d", len(candidates))
	}
	top := candidates[0]
	if top.ActionType != "trigger_resync" {
		t.Fatalf("expected trigger_resync to rank first (underexplored), got %q", top.ActionType)
	}
	if top.NoveltyScore < 0.8 {
		t.Fatalf("expected high novelty for unexplored action, got %.2f", top.NoveltyScore)
	}
	if top.ActionType == "noop" {
		t.Fatal("noop must never be chosen for exploration")
	}
}

// Test 7: Exploratory action tagged in audit payload

func TestExploratoryActionTaggedInAuditPayload(t *testing.T) {
	engine, audit := newTestEngine(StabilityNormal)
	now := time.Now().UTC()
	d := engine.Evaluate(context.Background(), lowConfidenceDecision(), noFeedback(), now)
	if !d.Chosen {
		t.Fatal("expected exploration to be chosen")
	}
	foundChosen := false
	for _, e := range audit.events {
		if e.eventType == "exploration.chosen" {
			foundChosen = true
			p, ok := e.payload.(map[string]any)
			if !ok {
				t.Fatal("payload should be map[string]any")
			}
			if p["chosen_action_type"] != d.ChosenActionType {
				t.Fatalf("audit payload action type mismatch: got %v, want %s",
					p["chosen_action_type"], d.ChosenActionType)
			}
			if p["chosen"] != true {
				t.Fatal("audit payload should have chosen=true")
			}
			if e.entityType != "exploration" {
				t.Fatalf("audit entity type should be 'exploration', got %q", e.entityType)
			}
		}
	}
	if !foundChosen {
		t.Fatal("expected exploration.chosen audit event")
	}
}

// Test 8: Deterministic decision across repeated runs

func TestDeterministicDecision(t *testing.T) {
	now := time.Now().UTC()
	decision := lowConfidenceDecision()
	feedback := noFeedback()

	var results []ExplorationDecision
	for i := 0; i < 10; i++ {
		engine, _ := newTestEngine(StabilityNormal)
		d := engine.Evaluate(context.Background(), decision, feedback, now)
		results = append(results, d)
	}

	first := results[0]
	for i := 1; i < len(results); i++ {
		r := results[i]
		if r.Chosen != first.Chosen {
			t.Fatalf("run %d: Chosen=%v, want %v", i, r.Chosen, first.Chosen)
		}
		if r.ChosenActionType != first.ChosenActionType {
			t.Fatalf("run %d: ChosenActionType=%q, want %q", i, r.ChosenActionType, first.ChosenActionType)
		}
		if r.DecisionReason != first.DecisionReason {
			t.Fatalf("run %d: DecisionReason=%q, want %q", i, r.DecisionReason, first.DecisionReason)
		}
		if len(r.Candidates) != len(first.Candidates) {
			t.Fatalf("run %d: %d candidates, want %d", i, len(r.Candidates), len(first.Candidates))
		}
		for j, c := range r.Candidates {
			if c.ActionType != first.Candidates[j].ActionType {
				t.Fatalf("run %d cand %d: %q != %q", i, j, c.ActionType, first.Candidates[j].ActionType)
			}
			assertFloatNear(t, "exploration_score", c.ExplorationScore, first.Candidates[j].ExplorationScore, 1e-9)
		}
	}
}

// Test 9: No regression when exploration disabled (nil provider)

func TestNoRegressionWhenExplorationDisabled(t *testing.T) {
	engine := NewEngine(nil, nil, nil, nil)
	now := time.Now().UTC()

	d := engine.Evaluate(context.Background(), highConfidenceDecision(), noFeedback(), now)
	if d.Chosen {
		t.Fatal("should not explore with high confidence")
	}

	d2 := engine.Evaluate(context.Background(), lowConfidenceDecision(), noFeedback(), now)
	if !d2.Chosen {
		t.Fatalf("should explore with low confidence and nil components; reason=%s", d2.DecisionReason)
	}
}

// --- Additional: Trigger detection ---

func TestShouldExplore_TooFewCandidates(t *testing.T) {
	d := planning.PlanningDecision{
		Candidates: []planning.PlannedActionCandidate{
			{ActionType: "retry_job", Score: 0.50, Confidence: 0.30},
		},
	}
	triggered, reason := ShouldExplore(d)
	if triggered {
		t.Fatal("should not trigger with < 2 candidates")
	}
	if reason != "too_few_candidates" {
		t.Fatalf("expected too_few_candidates, got %q", reason)
	}
}

func TestShouldExplore_NoViableAlternative(t *testing.T) {
	d := planning.PlanningDecision{
		SelectedActionType: "retry_job",
		Candidates: []planning.PlannedActionCandidate{
			{ActionType: "retry_job", Score: 0.50, Confidence: 0.30},
			{ActionType: "noop", Score: 0.10, Confidence: 1.0},
		},
	}
	triggered, reason := ShouldExplore(d)
	if triggered {
		t.Fatal("should not trigger when only alternative is noop")
	}
	if reason != "no_viable_alternative" {
		t.Fatalf("expected no_viable_alternative, got %q", reason)
	}
}

func TestShouldExplore_SmallGapTrigger(t *testing.T) {
	d := planning.PlanningDecision{
		SelectedActionType: "retry_job",
		Candidates: []planning.PlannedActionCandidate{
			{ActionType: "retry_job", Score: 0.60, Confidence: 0.85},
			{ActionType: "log_recommendation", Score: 0.55, Confidence: 0.70},
			{ActionType: "noop", Score: 0.10, Confidence: 1.0},
		},
	}
	triggered, reason := ShouldExplore(d)
	if !triggered {
		t.Fatal("should trigger when score gap is small")
	}
	if reason != "small_score_gap" {
		t.Fatalf("expected small_score_gap, got %q", reason)
	}
}

// --- Additional: Scoring ---

func TestComputeNovelty_NoData(t *testing.T) {
	n := computeNovelty(false, actionmemory.ActionFeedback{})
	assertFloatNear(t, "no data novelty", n, 1.0, 0.001)
}

func TestComputeNovelty_HighSamples(t *testing.T) {
	n := computeNovelty(true, actionmemory.ActionFeedback{SampleSize: 10})
	assertFloatNear(t, "high sample novelty", n, 0.0, 0.001)
}

func TestComputeNovelty_LowSamples(t *testing.T) {
	n := computeNovelty(true, actionmemory.ActionFeedback{SampleSize: 1})
	assertFloatNear(t, "low sample novelty", n, 0.84, 0.001)
}

func TestComputeSafety_NoData(t *testing.T) {
	s := computeSafety(false, actionmemory.ActionFeedback{}, planning.PlannedActionCandidate{})
	assertFloatNear(t, "no data safety", s, 0.8, 0.001)
}

func TestComputeSafety_Avoid(t *testing.T) {
	fb := actionmemory.ActionFeedback{
		Recommendation: actionmemory.RecommendAvoidAction,
		SampleSize:     20,
	}
	s := computeSafety(true, fb, planning.PlannedActionCandidate{})
	assertFloatNear(t, "avoid safety", s, 0.0, 0.001)
}

func TestComputeSafety_Prefer(t *testing.T) {
	fb := actionmemory.ActionFeedback{
		Recommendation: actionmemory.RecommendPreferAction,
		SampleSize:     10,
	}
	s := computeSafety(true, fb, planning.PlannedActionCandidate{})
	assertFloatNear(t, "prefer safety", s, 1.0, 0.001)
}

func TestComputeUncertainty_HighConfidence(t *testing.T) {
	u := computeUncertainty(planning.PlannedActionCandidate{Confidence: 0.90})
	assertFloatNear(t, "high conf uncertainty", u, 0.0, 0.001)
}

func TestComputeUncertainty_LowConfidence(t *testing.T) {
	u := computeUncertainty(planning.PlannedActionCandidate{Confidence: 0.20})
	assertFloatNear(t, "low conf uncertainty", u, 1.0, 0.001)
}

func TestComputeUncertainty_MidConfidence(t *testing.T) {
	u := computeUncertainty(planning.PlannedActionCandidate{Confidence: 0.50})
	assertFloatNear(t, "mid conf uncertainty", u, 0.50, 0.001)
}

// --- Additional: Budget ---

func TestBudgetWindowRolls(t *testing.T) {
	b := DefaultBudget()
	now := time.Now().UTC()
	b.WindowStart = now
	b.Consume(now)
	b.Consume(now)
	b.Consume(now)
	if b.HasWindowBudget(now) {
		t.Fatal("should be exhausted after 3 consumes")
	}
	future := now.Add(61 * time.Minute)
	if !b.HasWindowBudget(future) {
		t.Fatal("budget should reset after window rolls")
	}
}

// --- Additional: Sort determinism ---

func TestSortCandidatesDeterministic(t *testing.T) {
	cs := []ExplorationCandidate{
		{ActionType: "z_action", ExplorationScore: 0.5},
		{ActionType: "a_action", ExplorationScore: 0.5},
		{ActionType: "m_action", ExplorationScore: 0.8},
	}
	sortCandidates(cs)
	if cs[0].ActionType != "m_action" {
		t.Fatalf("expected m_action first, got %q", cs[0].ActionType)
	}
	if cs[1].ActionType != "a_action" {
		t.Fatalf("expected a_action second (tie-break), got %q", cs[1].ActionType)
	}
	if cs[2].ActionType != "z_action" {
		t.Fatalf("expected z_action third, got %q", cs[2].ActionType)
	}
}
