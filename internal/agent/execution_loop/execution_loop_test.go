package executionloop

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// --- Test helpers ---

type mockAuditor struct {
	events []mockAuditEvent
}

type mockAuditEvent struct {
	eventType string
	payload   any
}

func (m *mockAuditor) RecordEvent(_ context.Context, _ string, _ uuid.UUID, eventType, _, _ string, payload any) error {
	m.events = append(m.events, mockAuditEvent{eventType: eventType, payload: payload})
	return nil
}

var _ audit.AuditRecorder = (*mockAuditor)(nil)

type mockGovernance struct {
	mode string
}

func (m *mockGovernance) GetMode(_ interface{}) string { return m.mode }

type mockObjective struct {
	signalType string
	strength   float64
}

func (m *mockObjective) GetSignalType(_ interface{}) string      { return m.signalType }
func (m *mockObjective) GetSignalStrength(_ interface{}) float64 { return m.strength }

type mockExternalActions struct {
	results []ExecutorResult
	callIdx int
	calls   []mockEACall
}

type mockEACall struct {
	actionType    string
	payload       json.RawMessage
	opportunityID string
}

func (m *mockExternalActions) CreateAndExecute(_ interface{}, actionType string, payload json.RawMessage, opportunityID string) (ExecutorResult, error) {
	m.calls = append(m.calls, mockEACall{actionType: actionType, payload: payload, opportunityID: opportunityID})
	if m.callIdx < len(m.results) {
		r := m.results[m.callIdx]
		m.callIdx++
		return r, nil
	}
	return ExecutorResult{Success: true, ActionID: "default-action"}, nil
}

type failingExternalActions struct{}

func (f *failingExternalActions) CreateAndExecute(_ interface{}, _ string, _ json.RawMessage, _ string) (ExecutorResult, error) {
	return ExecutorResult{}, fmt.Errorf("connector unavailable")
}

func newTestEngine(auditor audit.AuditRecorder) *Engine {
	taskStore := NewInMemoryTaskStore()
	obsStore := NewInMemoryObservationStore()
	logger := zap.NewNop()
	return NewEngine(taskStore, obsStore, auditor, logger)
}

// --- Tests ---

func TestPlanGenerationValid(t *testing.T) {
	p := NewPlanner([]string{"external_action"})
	steps, err := p.GeneratePlan(PlannerInput{
		Goal:        "test goal",
		Context:     PlannerContext{OpportunityID: "opp-1"},
		Constraints: PlannerConstraints{MaxSteps: 5},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(steps) == 0 {
		t.Fatal("expected at least one step")
	}
	if steps[0].Tool != "external_action" {
		t.Fatalf("expected tool external_action, got %s", steps[0].Tool)
	}
	if steps[0].Status != StepStatusPending {
		t.Fatalf("expected pending status, got %s", steps[0].Status)
	}
}

func TestPlanRespectsMaxSteps(t *testing.T) {
	p := NewPlanner([]string{"external_action"})
	output := PlannerOutput{
		Steps: make([]PlannerStep, 6),
	}
	for i := range output.Steps {
		output.Steps[i] = PlannerStep{
			Description: fmt.Sprintf("step %d", i),
			Tool:        "external_action",
			Payload:     json.RawMessage(`{}`),
		}
	}
	_, err := p.GeneratePlanFromOutput(output)
	if err != ErrPlanTooLong {
		t.Fatalf("expected ErrPlanTooLong, got %v", err)
	}
}

func TestStepExecutesSuccessfully(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	ea := &mockExternalActions{
		results: []ExecutorResult{{Success: true, ActionID: "act-1"}},
	}
	eng.WithExternalActions(ea)

	ctx := context.Background()
	task, err := eng.CreateTask(ctx, "opp-1", "do something")
	if err != nil {
		t.Fatal(err)
	}
	result, err := eng.RunLoop(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != TaskStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
	if len(ea.calls) == 0 {
		t.Fatal("expected at least one external action call")
	}
}

func TestStepFailureHandled(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	ea := &mockExternalActions{
		results: []ExecutorResult{
			{Success: false, Error: "network error"},
			{Success: true, ActionID: "act-2"},
		},
	}
	eng.WithExternalActions(ea)

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-1", "retry test")
	result, err := eng.RunLoop(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != TaskStatusCompleted {
		t.Fatalf("expected completed after retry, got %s", result.Status)
	}
}

func TestRetryLimitEnforced(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	ea := &mockExternalActions{
		results: []ExecutorResult{
			{Success: false, Error: "fail-1"},
			{Success: false, Error: "fail-2"},
			{Success: false, Error: "fail-3"},
		},
	}
	eng.WithExternalActions(ea)

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-1", "fail test")
	result, err := eng.RunLoop(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Task should abort after consecutive failures or max iterations.
	if result.Status != TaskStatusAborted {
		t.Fatalf("expected aborted, got %s", result.Status)
	}
}

func TestIterationLimitEnforced(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	// Always fail but with different errors to avoid consecutive failure abort.
	callCount := 0
	ea := &mockExternalActions{
		results: func() []ExecutorResult {
			var rs []ExecutorResult
			for i := 0; i < 20; i++ {
				rs = append(rs, ExecutorResult{Success: false, Error: fmt.Sprintf("err-%d", i)})
			}
			return rs
		}(),
	}
	_ = callCount
	eng.WithExternalActions(ea)

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-1", "iter limit")
	result, err := eng.RunLoop(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != TaskStatusAborted {
		t.Fatalf("expected aborted, got %s", result.Status)
	}
	if result.IterationCount > MaxIterations {
		t.Fatalf("exceeded max iterations: %d > %d", result.IterationCount, MaxIterations)
	}
}

func TestTaskCompletesWhenGoalReached(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	eng.WithExternalActions(&mockExternalActions{
		results: []ExecutorResult{{Success: true, ActionID: "done"}},
	})

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-1", "simple goal")
	result, _ := eng.RunLoop(ctx, task.ID)
	if result.Status != TaskStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
}

func TestTaskAbortsOnRepeatedFailure(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	ea := &mockExternalActions{
		results: []ExecutorResult{
			{Success: false, Error: "same error"},
			{Success: false, Error: "same error"},
			{Success: false, Error: "same error"},
		},
	}
	eng.WithExternalActions(ea)

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-1", "repeat fail")
	result, _ := eng.RunLoop(ctx, task.ID)
	if result.Status != TaskStatusAborted {
		t.Fatalf("expected aborted, got %s", result.Status)
	}
}

func TestGovernanceBlocksExecution(t *testing.T) {
	for _, mode := range []string{"frozen", "rollback_only"} {
		t.Run(mode, func(t *testing.T) {
			aud := &mockAuditor{}
			eng := newTestEngine(aud)
			eng.WithGovernance(&mockGovernance{mode: mode})
			eng.WithExternalActions(&mockExternalActions{
				results: []ExecutorResult{{Success: true}},
			})

			ctx := context.Background()
			task, _ := eng.CreateTask(ctx, "opp-1", "blocked")
			result, _ := eng.RunLoop(ctx, task.ID)
			if result.Status != TaskStatusAborted {
				t.Fatalf("expected aborted for mode %s, got %s", mode, result.Status)
			}
		})
	}
}

func TestObjectivePenaltyAbortsTask(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	eng.WithObjective(&mockObjective{signalType: "penalty", strength: 0.10})
	eng.WithExternalActions(&mockExternalActions{
		results: []ExecutorResult{{Success: true}},
	})

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-1", "penalised")
	result, _ := eng.RunLoop(ctx, task.ID)
	if result.Status != TaskStatusAborted {
		t.Fatalf("expected aborted due to penalty, got %s", result.Status)
	}
	if result.AbortReason == "" {
		t.Fatal("expected abort reason")
	}
}

func TestObserverDetectsNoProgress(t *testing.T) {
	obsStore := NewInMemoryObservationStore()
	logger := zap.NewNop()
	observer := NewObserver(obsStore, logger)

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_ = obsStore.Insert(ctx, ExecutionObservation{
			StepID:  fmt.Sprintf("step-%d", i),
			TaskID:  "task-1",
			Success: false,
			Error:   "failed",
		})
	}

	if !observer.DetectNoProgress(ctx, "task-1", 3) {
		t.Fatal("expected no progress detected")
	}
}

func TestPlannerAdaptsPlan(t *testing.T) {
	p := NewPlanner([]string{"external_action"})
	steps := []ExecutionStep{
		{ID: "s1", Status: StepStatusFailed, AttemptCount: 0},
		{ID: "s2", Status: StepStatusPending},
	}
	adapted := p.AdaptPlan(steps, "s1", "some error")
	if adapted[0].AttemptCount != 1 {
		t.Fatalf("expected attempt count 1, got %d", adapted[0].AttemptCount)
	}
	if adapted[0].Status != StepStatusPending {
		t.Fatalf("expected pending after adapt, got %s", adapted[0].Status)
	}

	// After max retries, step should be blocked.
	adapted = p.AdaptPlan(adapted, "s1", "again")
	if adapted[0].Status != StepStatusBlocked {
		t.Fatalf("expected blocked after max retries, got %s", adapted[0].Status)
	}
}

func TestInvalidPlanRejected(t *testing.T) {
	p := NewPlanner([]string{"external_action"})
	_, err := p.GeneratePlanFromOutput(PlannerOutput{Steps: []PlannerStep{}})
	if err != ErrEmptyPlan {
		t.Fatalf("expected ErrEmptyPlan, got %v", err)
	}
}

func TestEmptyPlanHandled(t *testing.T) {
	p := NewPlanner([]string{"external_action"})
	_, err := p.GeneratePlan(PlannerInput{Goal: ""})
	if err == nil {
		t.Fatal("expected error for empty goal")
	}
}

func TestExecutionIdempotency(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	eng.WithExternalActions(&mockExternalActions{
		results: []ExecutorResult{{Success: true, ActionID: "act-1"}},
	})

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-1", "idempotent")
	result1, _ := eng.RunLoop(ctx, task.ID)
	if result1.Status != TaskStatusCompleted {
		t.Fatalf("expected completed, got %s", result1.Status)
	}

	// Second run on completed task should return it unchanged.
	result2, err := eng.RunLoop(ctx, task.ID)
	if err == nil {
		t.Fatal("expected error running completed task")
	}
	_ = result2
}

func TestConcurrentTasksSafe(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	eng.WithExternalActions(&mockExternalActions{
		results: func() []ExecutorResult {
			var rs []ExecutorResult
			for i := 0; i < 10; i++ {
				rs = append(rs, ExecutorResult{Success: true, ActionID: fmt.Sprintf("act-%d", i)})
			}
			return rs
		}(),
	})

	ctx := context.Background()
	task1, _ := eng.CreateTask(ctx, "opp-1", "task-1")
	task2, _ := eng.CreateTask(ctx, "opp-2", "task-2")

	r1, _ := eng.RunLoop(ctx, task1.ID)
	r2, _ := eng.RunLoop(ctx, task2.ID)

	if r1.Status != TaskStatusCompleted {
		t.Fatalf("task1 expected completed, got %s", r1.Status)
	}
	if r2.Status != TaskStatusCompleted {
		t.Fatalf("task2 expected completed, got %s", r2.Status)
	}
}

func TestAdapterNilSafe(t *testing.T) {
	var a *GraphAdapter
	ctx := context.Background()

	task, err := a.CreateTask(ctx, "opp", "goal")
	if err != nil {
		t.Fatal("nil adapter should not error")
	}
	if task.ID != "" {
		t.Fatal("nil adapter should return zero value")
	}

	tasks, err := a.ListTasks(ctx, 10)
	if err != nil || tasks != nil {
		t.Fatal("nil adapter should return nil, nil")
	}

	_, err = a.RunLoop(ctx, "some-id")
	if err != nil {
		t.Fatal("nil adapter should not error")
	}

	_, err = a.AbortTask(ctx, "some-id")
	if err != nil {
		t.Fatal("nil adapter should not error")
	}
}

func TestAuditEventsEmitted(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	eng.WithExternalActions(&mockExternalActions{
		results: []ExecutorResult{{Success: true, ActionID: "a1"}},
	})

	ctx := context.Background()
	eng.CreateTask(ctx, "opp-1", "audit test")
	foundCreated := false
	for _, ev := range aud.events {
		if ev.eventType == "execution.task_created" {
			foundCreated = true
		}
	}
	if !foundCreated {
		t.Fatal("expected execution.task_created audit event")
	}
}

func TestDryRunPathWorks(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	eng.WithExternalActions(&mockExternalActions{
		results: []ExecutorResult{{Success: true, DryRun: true, ActionID: "dry-1"}},
	})

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-1", "dry run")
	result, _ := eng.RunLoop(ctx, task.ID)
	if result.Status != TaskStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
}

func TestReviewRequiredPathWorks(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	// All calls return requires_review — no progress possible.
	eng.WithExternalActions(&mockExternalActions{
		results: func() []ExecutorResult {
			var rs []ExecutorResult
			for i := 0; i < 20; i++ {
				rs = append(rs, ExecutorResult{Success: false, RequiresReview: true})
			}
			return rs
		}(),
	})

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-1", "review needed")
	result, _ := eng.RunLoop(ctx, task.ID)
	// Task should abort because step is pending_review and no progress can be made.
	if result.Status != TaskStatusAborted {
		t.Fatalf("expected aborted (pending review), got %s", result.Status)
	}
}

func TestExternalActionIntegrationWorks(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	ea := &mockExternalActions{
		results: []ExecutorResult{{Success: true, ActionID: "ext-act-1", Output: json.RawMessage(`{"result":"ok"}`)}},
	}
	eng.WithExternalActions(ea)

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-ext", "external integration")
	result, _ := eng.RunLoop(ctx, task.ID)
	if result.Status != TaskStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
	if len(ea.calls) != 1 {
		t.Fatalf("expected 1 external action call, got %d", len(ea.calls))
	}
	if ea.calls[0].opportunityID != "opp-ext" {
		t.Fatalf("expected opportunity_id opp-ext, got %s", ea.calls[0].opportunityID)
	}
}

func TestTimeoutEnforced(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)

	// Override nowUTC to simulate time passing rapidly.
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	callCount := 0
	origNow := nowUTC
	nowUTC = func() time.Time {
		callCount++
		// After a few calls, jump past the deadline.
		if callCount > 5 {
			return baseTime.Add(time.Duration(MaxExecutionTimeSec+10) * time.Second)
		}
		return baseTime
	}
	defer func() { nowUTC = origNow }()

	// Return failures to keep loop running until timeout.
	ea := &mockExternalActions{
		results: func() []ExecutorResult {
			var rs []ExecutorResult
			for i := 0; i < 20; i++ {
				rs = append(rs, ExecutorResult{Success: false, Error: fmt.Sprintf("err-%d", i)})
			}
			return rs
		}(),
	}
	eng.WithExternalActions(ea)

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-1", "timeout test")
	result, _ := eng.RunLoop(ctx, task.ID)
	if result.Status != TaskStatusAborted {
		t.Fatalf("expected aborted due to timeout, got %s", result.Status)
	}
}

func TestExecutionOrderPreserved(t *testing.T) {
	p := NewPlanner([]string{"external_action"})
	output := PlannerOutput{
		Steps: []PlannerStep{
			{Description: "first", Tool: "external_action", Payload: json.RawMessage(`{"order":1}`)},
			{Description: "second", Tool: "external_action", Payload: json.RawMessage(`{"order":2}`)},
			{Description: "third", Tool: "external_action", Payload: json.RawMessage(`{"order":3}`)},
		},
	}
	steps, err := p.GeneratePlanFromOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].Description != "first" || steps[1].Description != "second" || steps[2].Description != "third" {
		t.Fatal("step order not preserved")
	}
}

func TestStepSkippingWorks(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)

	// First step will be blocked, second should still execute.
	ea := &mockExternalActions{
		results: []ExecutorResult{
			{Success: false, Error: "blocked"},
			{Success: false, Error: "blocked"},
			{Success: true, ActionID: "step-2-done"},
		},
	}
	eng.WithExternalActions(ea)

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-1", "skip test")

	// Pre-populate plan with 2 steps.
	p := NewPlanner([]string{"external_action"})
	output := PlannerOutput{
		Steps: []PlannerStep{
			{Description: "will-block", Tool: "external_action", Payload: json.RawMessage(`{}`)},
			{Description: "will-succeed", Tool: "external_action", Payload: json.RawMessage(`{}`)},
		},
	}
	steps, _ := p.GeneratePlanFromOutput(output)
	task.Plan = steps
	task.UpdatedAt = nowUTC()
	_ = eng.tasks.Update(ctx, task)

	result, _ := eng.RunLoop(ctx, task.ID)
	// Should complete because second step succeeds.
	if result.Status != TaskStatusCompleted {
		t.Fatalf("expected completed after skipping blocked step, got %s (reason: %s)", result.Status, result.AbortReason)
	}
}

func TestDeterministicBehaviorAcrossRuns(t *testing.T) {
	for i := 0; i < 3; i++ {
		aud := &mockAuditor{}
		eng := newTestEngine(aud)
		eng.WithExternalActions(&mockExternalActions{
			results: []ExecutorResult{{Success: true, ActionID: "det-1"}},
		})

		ctx := context.Background()
		task, _ := eng.CreateTask(ctx, "opp-det", "deterministic")
		result, _ := eng.RunLoop(ctx, task.ID)
		if result.Status != TaskStatusCompleted {
			t.Fatalf("run %d: expected completed, got %s", i, result.Status)
		}
		if result.IterationCount != 1 {
			t.Fatalf("run %d: expected 1 iteration, got %d", i, result.IterationCount)
		}
	}
}

func TestTaskStatusTransitions(t *testing.T) {
	tests := []struct {
		from  TaskStatus
		to    TaskStatus
		valid bool
	}{
		{TaskStatusPending, TaskStatusRunning, true},
		{TaskStatusPending, TaskStatusAborted, true},
		{TaskStatusRunning, TaskStatusCompleted, true},
		{TaskStatusRunning, TaskStatusFailed, true},
		{TaskStatusRunning, TaskStatusAborted, true},
		{TaskStatusCompleted, TaskStatusRunning, false},
		{TaskStatusFailed, TaskStatusRunning, false},
		{TaskStatusAborted, TaskStatusRunning, false},
		{TaskStatusPending, TaskStatusCompleted, false},
	}
	for _, tt := range tests {
		result := ValidateTaskTransition(tt.from, tt.to)
		if result != tt.valid {
			t.Errorf("transition %s→%s: expected %v, got %v", tt.from, tt.to, tt.valid, result)
		}
	}
}

func TestManualAbort(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-1", "abort me")
	result, err := eng.AbortTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != TaskStatusAborted {
		t.Fatalf("expected aborted, got %s", result.Status)
	}
	if result.AbortReason != "manual abort" {
		t.Fatalf("expected 'manual abort', got %s", result.AbortReason)
	}
}

func TestAbortCompletedTaskFails(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	eng.WithExternalActions(&mockExternalActions{
		results: []ExecutorResult{{Success: true, ActionID: "done"}},
	})

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-1", "done")
	eng.RunLoop(ctx, task.ID)

	_, err := eng.AbortTask(ctx, task.ID)
	if err == nil {
		t.Fatal("expected error aborting completed task")
	}
}

func TestNoExternalActionsProvider(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	// Do NOT wire external actions.

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-1", "no provider")
	result, _ := eng.RunLoop(ctx, task.ID)
	// Should abort because executor returns failure.
	if result.Status != TaskStatusAborted {
		t.Fatalf("expected aborted, got %s", result.Status)
	}
}

func TestGovernanceSafeHoldRequiresReview(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	eng.WithGovernance(&mockGovernance{mode: "safe_hold"})
	eng.WithExternalActions(&mockExternalActions{
		results: []ExecutorResult{{Success: true}},
	})

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-1", "safe hold")
	result, _ := eng.RunLoop(ctx, task.ID)
	// In safe_hold, executor returns requires_review, so task will eventually abort.
	if result.Status != TaskStatusAborted {
		t.Fatalf("expected aborted in safe_hold, got %s", result.Status)
	}
}

func TestDisallowedToolRejected(t *testing.T) {
	p := NewPlanner([]string{"external_action"})
	output := PlannerOutput{
		Steps: []PlannerStep{
			{Description: "bad tool", Tool: "shell_exec", Payload: json.RawMessage(`{}`)},
		},
	}
	_, err := p.GeneratePlanFromOutput(output)
	if err == nil {
		t.Fatal("expected error for disallowed tool")
	}
}

func TestObserverConsecutiveFailures(t *testing.T) {
	obsStore := NewInMemoryObservationStore()
	logger := zap.NewNop()
	observer := NewObserver(obsStore, logger)

	ctx := context.Background()
	// Record 2 failures then 1 success then 3 failures.
	for i := 0; i < 2; i++ {
		_ = obsStore.Insert(ctx, ExecutionObservation{StepID: "s1", TaskID: "t1", Success: false})
	}
	_ = obsStore.Insert(ctx, ExecutionObservation{StepID: "s2", TaskID: "t1", Success: true})
	for i := 0; i < 3; i++ {
		_ = obsStore.Insert(ctx, ExecutionObservation{StepID: "s3", TaskID: "t1", Success: false})
	}

	shouldAbort, _ := observer.ShouldAbort(ctx, "t1")
	if !shouldAbort {
		t.Fatal("expected shouldAbort=true after 3 consecutive failures")
	}
}

func TestObjectiveBelowThresholdAllowsExecution(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	// Penalty strength below threshold (0.05).
	eng.WithObjective(&mockObjective{signalType: "penalty", strength: 0.03})
	eng.WithExternalActions(&mockExternalActions{
		results: []ExecutorResult{{Success: true, ActionID: "ok"}},
	})

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-1", "low penalty")
	result, _ := eng.RunLoop(ctx, task.ID)
	if result.Status != TaskStatusCompleted {
		t.Fatalf("expected completed with low penalty, got %s", result.Status)
	}
}

func TestBoostSignalAllowsExecution(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	eng.WithObjective(&mockObjective{signalType: "boost", strength: 0.08})
	eng.WithExternalActions(&mockExternalActions{
		results: []ExecutorResult{{Success: true, ActionID: "boosted"}},
	})

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-1", "boost signal")
	result, _ := eng.RunLoop(ctx, task.ID)
	if result.Status != TaskStatusCompleted {
		t.Fatalf("expected completed with boost signal, got %s", result.Status)
	}
}

func TestFailingExternalActionsProvider(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	eng.WithExternalActions(&failingExternalActions{})

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-1", "failing ea")
	result, _ := eng.RunLoop(ctx, task.ID)
	if result.Status != TaskStatusAborted {
		t.Fatalf("expected aborted, got %s", result.Status)
	}
}

func TestGovernanceNormalAllowsExecution(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	eng.WithGovernance(&mockGovernance{mode: "normal"})
	eng.WithExternalActions(&mockExternalActions{
		results: []ExecutorResult{{Success: true, ActionID: "ok"}},
	})

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-1", "normal gov")
	result, _ := eng.RunLoop(ctx, task.ID)
	if result.Status != TaskStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
}

func TestAuditEventTypes(t *testing.T) {
	aud := &mockAuditor{}
	eng := newTestEngine(aud)
	eng.WithExternalActions(&mockExternalActions{
		results: []ExecutorResult{{Success: true, ActionID: "a1"}},
	})

	ctx := context.Background()
	task, _ := eng.CreateTask(ctx, "opp-1", "audit events")
	eng.RunLoop(ctx, task.ID)

	expectedEvents := map[string]bool{
		"execution.task_created":   false,
		"execution.plan_generated": false,
		"execution.step_executed":  false,
		"execution.task_completed": false,
	}
	for _, ev := range aud.events {
		if _, ok := expectedEvents[ev.eventType]; ok {
			expectedEvents[ev.eventType] = true
		}
	}
	for evType, found := range expectedEvents {
		if !found {
			t.Errorf("missing audit event: %s", evType)
		}
	}
}
