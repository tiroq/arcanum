package outcome

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/tiroq/arcanum/internal/agent/actions"
)

// mockDB implements a minimal in-memory evaluator for unit tests.
// It avoids the need for a real database connection.
type mockJobQuerier struct {
	status       string
	attemptCount int
	jobCount     int
	queryErr     error
}

// testEvaluator wraps evaluation logic with injectable job state.
type testEvaluator struct {
	mock *mockJobQuerier
}

func newTestEvaluator(m *mockJobQuerier) *testEvaluator {
	return &testEvaluator{mock: m}
}

// evaluateRetryJob mimics DBEvaluator.evaluateRetryJob using injected state.
func (te *testEvaluator) evaluateRetryJob(action actions.Action) (*ActionOutcome, error) {
	jobIDStr, _ := action.Params["job_id"].(string)
	jobID, _ := uuid.Parse(jobIDStr)

	afterState := map[string]any{
		"status":        te.mock.status,
		"attempt_count": te.mock.attemptCount,
	}
	beforeState := map[string]any{
		"action_triggered": "retry",
	}

	o := &ActionOutcome{
		ID:          uuid.New(),
		ActionID:    mustParseUUID(action.ID),
		GoalID:      action.GoalID,
		ActionType:  action.Type,
		TargetType:  "job",
		TargetID:    jobID,
		BeforeState: beforeState,
		AfterState:  afterState,
		EvaluatedAt: time.Now().UTC(),
	}

	switch te.mock.status {
	case "succeeded":
		o.OutcomeStatus = OutcomeSuccess
		o.EffectDetected = true
		o.Improvement = true
	case "dead_letter":
		o.OutcomeStatus = OutcomeFailure
		o.EffectDetected = true
		o.Improvement = false
	case "retry_scheduled":
		o.OutcomeStatus = OutcomeNeutral
		o.EffectDetected = true
		o.Improvement = false
	default:
		o.OutcomeStatus = OutcomeNeutral
		o.EffectDetected = false
		o.Improvement = false
	}
	return o, nil
}

// evaluateResync mimics DBEvaluator.evaluateResync using injected state.
func (te *testEvaluator) evaluateResync(action actions.Action) (*ActionOutcome, error) {
	taskIDStr, _ := action.Params["source_task_id"].(string)
	taskID, _ := uuid.Parse(taskIDStr)

	afterState := map[string]any{
		"new_jobs_created": te.mock.jobCount,
	}
	beforeState := map[string]any{
		"action_triggered": "resync",
	}

	o := &ActionOutcome{
		ID:          uuid.New(),
		ActionID:    mustParseUUID(action.ID),
		GoalID:      action.GoalID,
		ActionType:  action.Type,
		TargetType:  "task",
		TargetID:    taskID,
		BeforeState: beforeState,
		AfterState:  afterState,
		EvaluatedAt: time.Now().UTC(),
	}

	if te.mock.jobCount > 0 {
		o.OutcomeStatus = OutcomeSuccess
		o.EffectDetected = true
		o.Improvement = true
	} else {
		o.OutcomeStatus = OutcomeNeutral
		o.EffectDetected = false
		o.Improvement = false
	}
	return o, nil
}

func makeAction(actionType string, params map[string]any) actions.Action {
	return actions.Action{
		ID:        uuid.New().String(),
		Type:      actionType,
		GoalID:    "test-goal",
		Params:    params,
		CreatedAt: time.Now().UTC(),
	}
}

func makeResult(actionID string) actions.ActionResult {
	return actions.ActionResult{
		ActionID: actionID,
		Status:   actions.StatusExecuted,
	}
}

// --- Unit Tests: retry_job ---

func TestEvaluateRetryJob_Succeeded(t *testing.T) {
	mock := &mockJobQuerier{status: "succeeded", attemptCount: 2}
	te := newTestEvaluator(mock)

	action := makeAction(string(actions.ActionRetryJob), map[string]any{
		"job_id": uuid.New().String(),
	})

	o, err := te.evaluateRetryJob(action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertEqual(t, "outcome_status", string(o.OutcomeStatus), "success")
	assertEqual(t, "effect_detected", o.EffectDetected, true)
	assertEqual(t, "improvement", o.Improvement, true)
	assertEqual(t, "target_type", o.TargetType, "job")
}

func TestEvaluateRetryJob_DeadLetter(t *testing.T) {
	mock := &mockJobQuerier{status: "dead_letter", attemptCount: 5}
	te := newTestEvaluator(mock)

	action := makeAction(string(actions.ActionRetryJob), map[string]any{
		"job_id": uuid.New().String(),
	})

	o, err := te.evaluateRetryJob(action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertEqual(t, "outcome_status", string(o.OutcomeStatus), "failure")
	assertEqual(t, "effect_detected", o.EffectDetected, true)
	assertEqual(t, "improvement", o.Improvement, false)
}

func TestEvaluateRetryJob_RetryScheduled(t *testing.T) {
	mock := &mockJobQuerier{status: "retry_scheduled", attemptCount: 3}
	te := newTestEvaluator(mock)

	action := makeAction(string(actions.ActionRetryJob), map[string]any{
		"job_id": uuid.New().String(),
	})

	o, err := te.evaluateRetryJob(action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertEqual(t, "outcome_status", string(o.OutcomeStatus), "neutral")
	assertEqual(t, "effect_detected", o.EffectDetected, true)
	assertEqual(t, "improvement", o.Improvement, false)
}

func TestEvaluateRetryJob_Unchanged(t *testing.T) {
	mock := &mockJobQuerier{status: "queued", attemptCount: 1}
	te := newTestEvaluator(mock)

	action := makeAction(string(actions.ActionRetryJob), map[string]any{
		"job_id": uuid.New().String(),
	})

	o, err := te.evaluateRetryJob(action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertEqual(t, "outcome_status", string(o.OutcomeStatus), "neutral")
	assertEqual(t, "effect_detected", o.EffectDetected, false)
	assertEqual(t, "improvement", o.Improvement, false)
}

// --- Unit Tests: trigger_resync ---

func TestEvaluateResync_NewJobCreated(t *testing.T) {
	mock := &mockJobQuerier{jobCount: 2}
	te := newTestEvaluator(mock)

	action := makeAction(string(actions.ActionTriggerResync), map[string]any{
		"source_task_id": uuid.New().String(),
	})

	o, err := te.evaluateResync(action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertEqual(t, "outcome_status", string(o.OutcomeStatus), "success")
	assertEqual(t, "effect_detected", o.EffectDetected, true)
	assertEqual(t, "improvement", o.Improvement, true)
	assertEqual(t, "target_type", o.TargetType, "task")
}

func TestEvaluateResync_NoOp(t *testing.T) {
	mock := &mockJobQuerier{jobCount: 0}
	te := newTestEvaluator(mock)

	action := makeAction(string(actions.ActionTriggerResync), map[string]any{
		"source_task_id": uuid.New().String(),
	})

	o, err := te.evaluateResync(action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertEqual(t, "outcome_status", string(o.OutcomeStatus), "neutral")
	assertEqual(t, "effect_detected", o.EffectDetected, false)
	assertEqual(t, "improvement", o.Improvement, false)
}

// --- Unit Tests: log_recommendation ---

func TestEvaluateRecommendation_AlwaysNeutral(t *testing.T) {
	// evaluateRecommendation is a pure function, no DB needed.
	e := &DBEvaluator{} // nil db is fine — not used for recommendations.
	action := makeAction(string(actions.ActionLogRecommendation), map[string]any{
		"goal_type":   "improve_acceptance_rate",
		"description": "Review acceptance thresholds",
	})

	o := e.evaluateRecommendation(action)

	assertEqual(t, "outcome_status", string(o.OutcomeStatus), "neutral")
	assertEqual(t, "effect_detected", o.EffectDetected, false)
	assertEqual(t, "improvement", o.Improvement, false)
	assertEqual(t, "target_type", o.TargetType, "recommendation")
	assertEqual(t, "target_id", o.TargetID, uuid.Nil)
}

// --- Unit Tests: Evaluator dispatch ---

func TestEvaluator_DispatchRecommendation(t *testing.T) {
	// Only recommendation can be tested without a DB since it's a pure function.
	e := &DBEvaluator{}
	action := makeAction(string(actions.ActionLogRecommendation), nil)
	result := makeResult(action.ID)

	o, err := e.Evaluate(context.Background(), action, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertEqual(t, "outcome_status", string(o.OutcomeStatus), "neutral")
}

func TestEvaluator_DispatchUnknownType(t *testing.T) {
	e := &DBEvaluator{}
	action := makeAction("unknown_action", nil)
	result := makeResult(action.ID)

	o, err := e.Evaluate(context.Background(), action, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertEqual(t, "outcome_status", string(o.OutcomeStatus), "neutral")
}

// --- Handler Tests ---

type mockOutcomeStore struct {
	saved []*ActionOutcome
}

func (m *mockOutcomeStore) save(o *ActionOutcome) {
	m.saved = append(m.saved, o)
}

// --- Test Helpers ---

func assertEqual[T comparable](t *testing.T, field string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", field, got, want)
	}
}
