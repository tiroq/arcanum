package autonomy

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock TaskOrchestratorRunner ---

type mockTaskOrchestrator struct {
	mu              sync.Mutex
	tasks           map[string]*mockOrcTask
	createCalled    int
	recomputeCalled int
	dispatchCalled  int
	createErr       error
	recomputeErr    error
	dispatchResult  DispatchResultInfo
	dispatchErr     error
}

type mockOrcTask struct {
	ID                  string
	Status              string
	Source              string
	Goal                string
	Urgency             float64
	ExpectedValue       float64
	RiskLevel           float64
	StrategyType        string
	ActuationDecisionID string
	ExecutionTaskID     string
	OutcomeType         string
	LastError           string
	AttemptCount        int
}

func newMockTaskOrchestrator() *mockTaskOrchestrator {
	return &mockTaskOrchestrator{
		tasks: make(map[string]*mockOrcTask),
		dispatchResult: DispatchResultInfo{
			DispatchedTaskIDs: make(map[string]string),
		},
	}
}

func (m *mockTaskOrchestrator) CreateTask(_ context.Context, source, goal string, urgency, expectedValue, riskLevel float64, strategyType string) (CreatedTaskInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createCalled++
	if m.createErr != nil {
		return CreatedTaskInfo{}, m.createErr
	}
	id := fmt.Sprintf("task-%d", m.createCalled)
	m.tasks[id] = &mockOrcTask{
		ID:            id,
		Status:        "pending",
		Source:        source,
		Goal:          goal,
		Urgency:       urgency,
		ExpectedValue: expectedValue,
		RiskLevel:     riskLevel,
		StrategyType:  strategyType,
	}
	return CreatedTaskInfo{ID: id}, nil
}

func (m *mockTaskOrchestrator) RecomputePriorities(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recomputeCalled++
	return m.recomputeErr
}

func (m *mockTaskOrchestrator) Dispatch(_ context.Context) (DispatchResultInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dispatchCalled++
	return m.dispatchResult, m.dispatchErr
}

func (m *mockTaskOrchestrator) FindByActuationDecision(_ context.Context, decisionID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, t := range m.tasks {
		if t.ActuationDecisionID == decisionID {
			return id, nil
		}
	}
	return "", nil
}

func (m *mockTaskOrchestrator) CompleteTask(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[id]; ok {
		t.Status = "completed"
	}
	return nil
}

func (m *mockTaskOrchestrator) FailTask(_ context.Context, id, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[id]; ok {
		t.Status = "failed"
		t.LastError = reason
	}
	return nil
}

func (m *mockTaskOrchestrator) PauseTask(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[id]; ok {
		t.Status = "paused"
	}
	return nil
}

func (m *mockTaskOrchestrator) SetActuationDecisionID(_ context.Context, taskID, decisionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[taskID]; ok {
		t.ActuationDecisionID = decisionID
	}
	return nil
}

func (m *mockTaskOrchestrator) SetExecutionTaskID(_ context.Context, taskID, execTaskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[taskID]; ok {
		t.ExecutionTaskID = execTaskID
	}
	return nil
}

func (m *mockTaskOrchestrator) SetOutcome(_ context.Context, taskID, outcomeType, lastError string, attemptCount int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[taskID]; ok {
		t.OutcomeType = outcomeType
		t.LastError = lastError
		t.AttemptCount = attemptCount
	}
	return nil
}

func (m *mockTaskOrchestrator) ListRunningTasks(_ context.Context, limit int) ([]RunningTaskInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []RunningTaskInfo
	for _, t := range m.tasks {
		if t.Status == "running" {
			result = append(result, RunningTaskInfo{
				ID:              t.ID,
				ExecutionTaskID: t.ExecutionTaskID,
				Goal:            t.Goal,
				AttemptCount:    t.AttemptCount,
			})
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (m *mockTaskOrchestrator) getTask(id string) *mockOrcTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tasks[id]
}

func (m *mockTaskOrchestrator) setTaskRunning(id, execTaskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[id]; ok {
		t.Status = "running"
		t.ExecutionTaskID = execTaskID
	}
}

func (m *mockTaskOrchestrator) getCreateCalled() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.createCalled
}

func (m *mockTaskOrchestrator) getRecomputeCalled() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.recomputeCalled
}

func (m *mockTaskOrchestrator) getDispatchCalled() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dispatchCalled
}

// --- Mock ExecutionLoopRunner ---

type mockExecutionLoop struct {
	mu    sync.Mutex
	tasks map[string]ExecTaskInfo
}

func newMockExecutionLoop() *mockExecutionLoop {
	return &mockExecutionLoop{tasks: make(map[string]ExecTaskInfo)}
}

func (m *mockExecutionLoop) GetTask(_ context.Context, id string) (ExecTaskInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[id]; ok {
		return t, nil
	}
	return ExecTaskInfo{}, fmt.Errorf("exec task not found: %s", id)
}

func (m *mockExecutionLoop) setTask(info ExecTaskInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[info.ID] = info
}

// --- Test helpers ---

func chainTestOrchestrator(cfg *AutonomyConfig) *Orchestrator {
	aud := &mockAuditor{}
	logger := testLogger()
	return NewOrchestrator(cfg, aud, logger)
}

func chainTestConfig() *AutonomyConfig {
	cfg := minimalConfig()
	cfg.Scheduler.Cycles.TaskRecomputeHours = 1
	cfg.Scheduler.Cycles.TaskDispatchHours = 1
	return cfg
}

// ====================================================================
// Chain 1: Actuation → Task Materialization
// ====================================================================

func TestMaterializeDecisionAsTask_SafeDecision(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	orch.WithTaskOrchestrator(to)

	d := ActuationDecisionInfo{
		ID:             "dec-1",
		Type:           "stabilize_income",
		Status:         "proposed",
		RequiresReview: false,
		Priority:       0.75,
	}

	taskID, created, err := orch.MaterializeDecisionAsTask(context.Background(), d)
	require.NoError(t, err)
	assert.True(t, created)
	assert.NotEmpty(t, taskID)

	// Verify task was created with correct parameters.
	task := to.getTask(taskID)
	require.NotNil(t, task)
	assert.Equal(t, "actuation", task.Source)
	assert.Equal(t, "stabilize income by prioritizing safe revenue actions", task.Goal)
	assert.Equal(t, 0.75, task.Urgency)
	assert.Equal(t, "consulting", task.StrategyType)
	assert.Equal(t, 0.3, task.RiskLevel) // non-review = low risk

	// Verify decision ID was linked.
	assert.Equal(t, "dec-1", task.ActuationDecisionID)
}

func TestMaterializeDecisionAsTask_DuplicatePrevention(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	orch.WithTaskOrchestrator(to)

	d := ActuationDecisionInfo{
		ID:       "dec-dup",
		Type:     "reduce_load",
		Status:   "proposed",
		Priority: 0.5,
	}

	// First call creates the task.
	taskID1, created1, err := orch.MaterializeDecisionAsTask(context.Background(), d)
	require.NoError(t, err)
	assert.True(t, created1)
	assert.NotEmpty(t, taskID1)

	// Second call should detect the duplicate.
	taskID2, created2, err := orch.MaterializeDecisionAsTask(context.Background(), d)
	require.NoError(t, err)
	assert.False(t, created2)
	assert.Equal(t, taskID1, taskID2) // returns existing task ID

	// Only one task should exist.
	assert.Equal(t, 1, to.getCreateCalled())
}

func TestMaterializeDecisionAsTask_ReviewRequiredBlockedInSupervised(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeSupervisedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	orch.WithTaskOrchestrator(to)

	d := ActuationDecisionInfo{
		ID:             "dec-review",
		Type:           "trigger_automation",
		Status:         "proposed",
		RequiresReview: true,
		Priority:       0.8,
	}

	taskID, created, err := orch.MaterializeDecisionAsTask(context.Background(), d)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Empty(t, taskID)
	assert.Equal(t, 0, to.getCreateCalled())

	// Audit event should exist.
	aud := orch.auditor.(*mockAuditor)
	assert.True(t, aud.hasEvent("actuation.task_skipped_review_required"))
}

func TestMaterializeDecisionAsTask_FrozenBlocks(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeFrozen
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	orch.WithTaskOrchestrator(to)

	d := ActuationDecisionInfo{
		ID:       "dec-frozen",
		Type:     "increase_discovery",
		Status:   "proposed",
		Priority: 0.6,
	}

	taskID, created, err := orch.MaterializeDecisionAsTask(context.Background(), d)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Empty(t, taskID)
	assert.Equal(t, 0, to.getCreateCalled())

	aud := orch.auditor.(*mockAuditor)
	assert.True(t, aud.hasEvent("actuation.task_blocked_by_governance"))
}

func TestMaterializeDecisionAsTask_ReviewRequiredHighRisk(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	orch.WithTaskOrchestrator(to)

	d := ActuationDecisionInfo{
		ID:             "dec-highrisk",
		Type:           "adjust_pricing",
		Status:         "proposed",
		RequiresReview: true,
		Priority:       0.9,
	}

	taskID, created, err := orch.MaterializeDecisionAsTask(context.Background(), d)
	require.NoError(t, err)
	assert.True(t, created)
	assert.NotEmpty(t, taskID)

	task := to.getTask(taskID)
	require.NotNil(t, task)
	assert.Equal(t, 0.6, task.RiskLevel) // review-required = higher risk
}

func TestMaterializeDecisionAsTask_NilTaskOrchestrator(t *testing.T) {
	cfg := chainTestConfig()
	orch := chainTestOrchestrator(cfg)
	// No WithTaskOrchestrator — should fail-open.

	d := ActuationDecisionInfo{
		ID:       "dec-nil",
		Type:     "reduce_load",
		Priority: 0.5,
	}

	taskID, created, err := orch.MaterializeDecisionAsTask(context.Background(), d)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Empty(t, taskID)
}

func TestMaterializeDecisionAsTask_AllActuationTypes(t *testing.T) {
	for aType, expectedGoal := range actuationTypeToGoal {
		t.Run(aType, func(t *testing.T) {
			cfg := chainTestConfig()
			cfg.Mode = ModeAutonomous
			orch := chainTestOrchestrator(cfg)
			to := newMockTaskOrchestrator()
			orch.WithTaskOrchestrator(to)

			d := ActuationDecisionInfo{
				ID:       "dec-" + aType,
				Type:     aType,
				Status:   "proposed",
				Priority: 0.5,
			}

			_, created, err := orch.MaterializeDecisionAsTask(context.Background(), d)
			require.NoError(t, err)
			assert.True(t, created)

			task := to.getTask("task-1")
			require.NotNil(t, task)
			assert.Equal(t, expectedGoal, task.Goal)
			expectedStrategy := actuationTypeToStrategy[aType]
			assert.Equal(t, expectedStrategy, task.StrategyType)
		})
	}
}

// ====================================================================
// Chain 2: Autonomy → Task Recompute / Dispatch Cycles
// ====================================================================

func TestCycleTaskRecompute_Runs(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	el := newMockExecutionLoop()
	fs := NewInMemoryFeedbackStore()
	orch.WithTaskOrchestrator(to).WithExecutionLoop(el).WithFeedbackStore(fs)

	err := orch.cycleTaskRecompute(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 1, to.getRecomputeCalled())

	state := orch.GetState()
	assert.Equal(t, 1, state.TaskRecomputeCount)
}

func TestCycleTaskRecompute_FrozenSkips(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeFrozen
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	orch.WithTaskOrchestrator(to)

	err := orch.cycleTaskRecompute(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 0, to.getRecomputeCalled())
	aud := orch.auditor.(*mockAuditor)
	assert.True(t, aud.hasEvent("autonomy.task_recompute_skipped"))
}

func TestCycleTaskRecompute_NilOrchestrator(t *testing.T) {
	cfg := chainTestConfig()
	orch := chainTestOrchestrator(cfg)
	// No task orchestrator wired.
	err := orch.cycleTaskRecompute(context.Background())
	require.NoError(t, err)
}

func TestCycleTaskDispatch_Runs(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	to.dispatchResult = DispatchResultInfo{
		DispatchedCount:   2,
		SkippedCount:      1,
		BlockedCount:      0,
		DispatchedTaskIDs: map[string]string{"task-a": "exec-1", "task-b": "exec-2"},
	}
	orch.WithTaskOrchestrator(to)

	err := orch.cycleTaskDispatch(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 1, to.getDispatchCalled())

	state := orch.GetState()
	assert.Equal(t, 2, state.TaskDispatchCount)

	// Verify execution task IDs were linked.
	tA := to.getTask("task-a")
	tB := to.getTask("task-b")
	// Tasks were created by mock via dispatch result, not Create.
	// We need to create them first for SetExecutionTaskID to work.
	// Let's verify dispatch was called correctly instead.
	_ = tA // may be nil since there's no corresponding task in the mock store
	_ = tB
}

func TestCycleTaskDispatch_FrozenSkips(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeFrozen
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	orch.WithTaskOrchestrator(to)

	err := orch.cycleTaskDispatch(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 0, to.getDispatchCalled())
	aud := orch.auditor.(*mockAuditor)
	assert.True(t, aud.hasEvent("autonomy.task_dispatch_skipped"))
}

func TestCycleTaskDispatch_DispatchErrorFailOpen(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	to.dispatchErr = fmt.Errorf("max running tasks reached")
	orch.WithTaskOrchestrator(to)

	err := orch.cycleTaskDispatch(context.Background())
	require.NoError(t, err) // should fail-open

	state := orch.GetState()
	assert.Equal(t, 0, state.TaskDispatchCount)
}

func TestCycleTaskDispatch_LinksExecutionTaskIDs(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()

	// Pre-create tasks in mock so SetExecutionTaskID has something to update.
	to.tasks["task-x"] = &mockOrcTask{ID: "task-x", Status: "queued"}
	to.tasks["task-y"] = &mockOrcTask{ID: "task-y", Status: "queued"}

	to.dispatchResult = DispatchResultInfo{
		DispatchedCount:   2,
		DispatchedTaskIDs: map[string]string{"task-x": "exec-100", "task-y": "exec-200"},
	}
	orch.WithTaskOrchestrator(to)

	err := orch.cycleTaskDispatch(context.Background())
	require.NoError(t, err)

	// Verify execution task IDs were linked.
	assert.Equal(t, "exec-100", to.getTask("task-x").ExecutionTaskID)
	assert.Equal(t, "exec-200", to.getTask("task-y").ExecutionTaskID)
}

// ====================================================================
// Chain 3: Execution Loop → Task Lifecycle Closure
// ====================================================================

func TestPropagateExecutionResults_CompletedTask(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	el := newMockExecutionLoop()
	fs := NewInMemoryFeedbackStore()
	orch.WithTaskOrchestrator(to).WithExecutionLoop(el).WithFeedbackStore(fs)

	// Setup: a running task with a linked execution task that completed.
	to.tasks["task-1"] = &mockOrcTask{
		ID:              "task-1",
		Status:          "running",
		ExecutionTaskID: "exec-1",
		Goal:            "test goal",
		AttemptCount:    0,
	}
	el.setTask(ExecTaskInfo{
		ID:             "exec-1",
		Status:         "completed",
		IterationCount: 3,
		StepsExecuted:  5,
		StepsFailed:    0,
	})

	completed, failed, paused, err := orch.PropagateExecutionResults(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, completed)
	assert.Equal(t, 0, failed)
	assert.Equal(t, 0, paused)

	// Verify task status was updated.
	assert.Equal(t, "completed", to.getTask("task-1").Status)

	// Verify feedback was stored.
	allFb := fs.AllFeedback()
	require.Len(t, allFb, 1)
	assert.Equal(t, "completed", allFb[0].OutcomeType)
	assert.True(t, allFb[0].Success)
	assert.Equal(t, "safe_action_succeeded", allFb[0].SemanticSignal)
	assert.Equal(t, 5, allFb[0].StepsExecuted)
	assert.Equal(t, 0, allFb[0].StepsFailed)
}

func TestPropagateExecutionResults_FailedTask(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	el := newMockExecutionLoop()
	fs := NewInMemoryFeedbackStore()
	orch.WithTaskOrchestrator(to).WithExecutionLoop(el).WithFeedbackStore(fs)

	to.tasks["task-2"] = &mockOrcTask{
		ID:              "task-2",
		Status:          "running",
		ExecutionTaskID: "exec-2",
		AttemptCount:    0,
	}
	el.setTask(ExecTaskInfo{
		ID:            "exec-2",
		Status:        "failed",
		AbortReason:   "step execution error",
		StepsExecuted: 3,
		StepsFailed:   2,
	})

	completed, failed, paused, err := orch.PropagateExecutionResults(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, completed)
	assert.Equal(t, 1, failed)
	assert.Equal(t, 0, paused)

	assert.Equal(t, "failed", to.getTask("task-2").Status)

	allFb := fs.AllFeedback()
	require.Len(t, allFb, 1)
	assert.Equal(t, "failed", allFb[0].OutcomeType)
	assert.False(t, allFb[0].Success)
	assert.Equal(t, "execution_failure", allFb[0].SemanticSignal)
}

func TestPropagateExecutionResults_RepeatedFailure(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	el := newMockExecutionLoop()
	fs := NewInMemoryFeedbackStore()
	orch.WithTaskOrchestrator(to).WithExecutionLoop(el).WithFeedbackStore(fs)

	to.tasks["task-rf"] = &mockOrcTask{
		ID:              "task-rf",
		Status:          "running",
		ExecutionTaskID: "exec-rf",
		AttemptCount:    2, // >= 2 triggers "repeated_failure"
	}
	el.setTask(ExecTaskInfo{
		ID:          "exec-rf",
		Status:      "failed",
		AbortReason: "another failure",
	})

	_, _, _, err := orch.PropagateExecutionResults(context.Background())
	require.NoError(t, err)

	allFb := fs.AllFeedback()
	require.Len(t, allFb, 1)
	assert.Equal(t, "repeated_failure", allFb[0].SemanticSignal)
}

func TestPropagateExecutionResults_AbortedManual_Pauses(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	el := newMockExecutionLoop()
	fs := NewInMemoryFeedbackStore()
	orch.WithTaskOrchestrator(to).WithExecutionLoop(el).WithFeedbackStore(fs)

	to.tasks["task-abort"] = &mockOrcTask{
		ID:              "task-abort",
		Status:          "running",
		ExecutionTaskID: "exec-abort",
	}
	el.setTask(ExecTaskInfo{
		ID:          "exec-abort",
		Status:      "aborted",
		AbortReason: "manual abort",
	})

	completed, failed, paused, err := orch.PropagateExecutionResults(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, completed)
	assert.Equal(t, 0, failed)
	assert.Equal(t, 1, paused)

	assert.Equal(t, "paused", to.getTask("task-abort").Status)

	allFb := fs.AllFeedback()
	require.Len(t, allFb, 1)
	assert.Equal(t, "execution_aborted", allFb[0].SemanticSignal)
}

func TestPropagateExecutionResults_AbortedObjectivePenalty_Fails(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	el := newMockExecutionLoop()
	fs := NewInMemoryFeedbackStore()
	orch.WithTaskOrchestrator(to).WithExecutionLoop(el).WithFeedbackStore(fs)

	to.tasks["task-objpen"] = &mockOrcTask{
		ID:              "task-objpen",
		Status:          "running",
		ExecutionTaskID: "exec-objpen",
	}
	el.setTask(ExecTaskInfo{
		ID:          "exec-objpen",
		Status:      "aborted",
		AbortReason: "objective penalty threshold exceeded",
	})

	_, failed, _, err := orch.PropagateExecutionResults(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, failed)
	assert.Equal(t, "failed", to.getTask("task-objpen").Status)

	allFb := fs.AllFeedback()
	require.Len(t, allFb, 1)
	assert.Equal(t, "objective_penalty_abort", allFb[0].SemanticSignal)
}

func TestPropagateExecutionResults_AbortedGovernance(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	el := newMockExecutionLoop()
	fs := NewInMemoryFeedbackStore()
	orch.WithTaskOrchestrator(to).WithExecutionLoop(el).WithFeedbackStore(fs)

	to.tasks["task-gov"] = &mockOrcTask{
		ID:              "task-gov",
		Status:          "running",
		ExecutionTaskID: "exec-gov",
	}
	el.setTask(ExecTaskInfo{
		ID:          "exec-gov",
		Status:      "aborted",
		AbortReason: "governance blocked execution",
	})

	_, failed, _, err := orch.PropagateExecutionResults(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, failed)

	allFb := fs.AllFeedback()
	require.Len(t, allFb, 1)
	assert.Equal(t, "blocked_by_governance", allFb[0].SemanticSignal)
}

func TestPropagateExecutionResults_AbortedConsecutiveFailures(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	el := newMockExecutionLoop()
	fs := NewInMemoryFeedbackStore()
	orch.WithTaskOrchestrator(to).WithExecutionLoop(el).WithFeedbackStore(fs)

	to.tasks["task-consec"] = &mockOrcTask{
		ID:              "task-consec",
		Status:          "running",
		ExecutionTaskID: "exec-consec",
	}
	el.setTask(ExecTaskInfo{
		ID:          "exec-consec",
		Status:      "aborted",
		AbortReason: "too many consecutive failures reached limit",
	})

	_, failed, _, err := orch.PropagateExecutionResults(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, failed)

	allFb := fs.AllFeedback()
	require.Len(t, allFb, 1)
	assert.Equal(t, "repeated_failure", allFb[0].SemanticSignal)
}

func TestPropagateExecutionResults_ReviewBlock_Pauses(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	el := newMockExecutionLoop()
	fs := NewInMemoryFeedbackStore()
	orch.WithTaskOrchestrator(to).WithExecutionLoop(el).WithFeedbackStore(fs)

	to.tasks["task-review"] = &mockOrcTask{
		ID:              "task-review",
		Status:          "running",
		ExecutionTaskID: "exec-review",
	}
	el.setTask(ExecTaskInfo{
		ID:             "exec-review",
		Status:         "running",
		HasReviewBlock: true,
		StepsExecuted:  2,
		StepsFailed:    0,
	})

	completed, failed, paused, err := orch.PropagateExecutionResults(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, completed)
	assert.Equal(t, 0, failed)
	assert.Equal(t, 1, paused)

	assert.Equal(t, "paused", to.getTask("task-review").Status)

	allFb := fs.AllFeedback()
	require.Len(t, allFb, 1)
	assert.Equal(t, "blocked_by_review", allFb[0].SemanticSignal)
}

func TestPropagateExecutionResults_SkipsNoExecutionTaskID(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	el := newMockExecutionLoop()
	fs := NewInMemoryFeedbackStore()
	orch.WithTaskOrchestrator(to).WithExecutionLoop(el).WithFeedbackStore(fs)

	// Task with no execution task ID should be skipped.
	to.tasks["task-noid"] = &mockOrcTask{
		ID:     "task-noid",
		Status: "running",
		// No ExecutionTaskID
	}

	completed, failed, paused, err := orch.PropagateExecutionResults(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, completed)
	assert.Equal(t, 0, failed)
	assert.Equal(t, 0, paused)

	// Task status should remain "running".
	assert.Equal(t, "running", to.getTask("task-noid").Status)
	assert.Empty(t, fs.AllFeedback())
}

func TestPropagateExecutionResults_NilProviders(t *testing.T) {
	cfg := chainTestConfig()
	orch := chainTestOrchestrator(cfg)
	// No providers wired — should fail-open.

	completed, failed, paused, err := orch.PropagateExecutionResults(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, completed)
	assert.Equal(t, 0, failed)
	assert.Equal(t, 0, paused)
}

func TestPropagateExecutionResults_StillRunning_NoAction(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	el := newMockExecutionLoop()
	fs := NewInMemoryFeedbackStore()
	orch.WithTaskOrchestrator(to).WithExecutionLoop(el).WithFeedbackStore(fs)

	to.tasks["task-running"] = &mockOrcTask{
		ID:              "task-running",
		Status:          "running",
		ExecutionTaskID: "exec-running",
	}
	el.setTask(ExecTaskInfo{
		ID:             "exec-running",
		Status:         "running",
		HasReviewBlock: false, // no block, still running
	})

	completed, failed, paused, err := orch.PropagateExecutionResults(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, completed)
	assert.Equal(t, 0, failed)
	assert.Equal(t, 0, paused)

	// Task remains running.
	assert.Equal(t, "running", to.getTask("task-running").Status)
	assert.Empty(t, fs.AllFeedback())
}

// ====================================================================
// Chain 4: Structured Feedback
// ====================================================================

func TestFeedbackRecorded_CounterIncremented(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	el := newMockExecutionLoop()
	fs := NewInMemoryFeedbackStore()
	orch.WithTaskOrchestrator(to).WithExecutionLoop(el).WithFeedbackStore(fs)

	to.tasks["task-fb"] = &mockOrcTask{
		ID:              "task-fb",
		Status:          "running",
		ExecutionTaskID: "exec-fb",
	}
	el.setTask(ExecTaskInfo{
		ID:            "exec-fb",
		Status:        "completed",
		StepsExecuted: 2,
	})

	_, _, _, err := orch.PropagateExecutionResults(context.Background())
	require.NoError(t, err)

	state := orch.GetState()
	assert.Equal(t, 1, state.FeedbackRecorded)
}

func TestReflectionFeedback_ReturnsSignals(t *testing.T) {
	cfg := chainTestConfig()
	orch := chainTestOrchestrator(cfg)
	fs := NewInMemoryFeedbackStore()
	orch.WithFeedbackStore(fs)

	// Insert feedback directly.
	now := time.Now().UTC()
	_ = fs.Insert(context.Background(), ExecutionFeedback{
		ID:             "fb-1",
		TaskID:         "task-1",
		OutcomeType:    "completed",
		Success:        true,
		SemanticSignal: "safe_action_succeeded",
		CreatedAt:      now.Add(-1 * time.Hour),
	})
	_ = fs.Insert(context.Background(), ExecutionFeedback{
		ID:             "fb-2",
		TaskID:         "task-2",
		OutcomeType:    "failed",
		Success:        false,
		SemanticSignal: "repeated_failure",
		CreatedAt:      now.Add(-2 * time.Hour),
	})

	signals, err := orch.GetReflectionFeedback(context.Background())
	require.NoError(t, err)
	require.Len(t, signals, 2)

	// Check signal content.
	signalTypes := make(map[string]bool)
	for _, s := range signals {
		signalTypes[s.Signal] = true
	}
	assert.True(t, signalTypes["safe_action_succeeded"])
	assert.True(t, signalTypes["repeated_failure"])
}

func TestReflectionFeedback_FiltersOldSignals(t *testing.T) {
	cfg := chainTestConfig()
	orch := chainTestOrchestrator(cfg)
	fs := NewInMemoryFeedbackStore()
	orch.WithFeedbackStore(fs)

	now := time.Now().UTC()
	_ = fs.Insert(context.Background(), ExecutionFeedback{
		ID:             "fb-recent",
		TaskID:         "task-1",
		SemanticSignal: "safe_action_succeeded",
		CreatedAt:      now.Add(-1 * time.Hour),
	})
	_ = fs.Insert(context.Background(), ExecutionFeedback{
		ID:             "fb-old",
		TaskID:         "task-2",
		SemanticSignal: "old_signal",
		CreatedAt:      now.Add(-48 * time.Hour), // older than 24h
	})

	signals, err := orch.GetReflectionFeedback(context.Background())
	require.NoError(t, err)
	assert.Len(t, signals, 1) // only recent signal
	assert.Equal(t, "safe_action_succeeded", signals[0].Signal)
}

func TestReflectionFeedback_NilStore(t *testing.T) {
	cfg := chainTestConfig()
	orch := chainTestOrchestrator(cfg)
	// No feedback store.
	signals, err := orch.GetReflectionFeedback(context.Background())
	require.NoError(t, err)
	assert.Nil(t, signals)
}

func TestObjectiveFeedback_ReturnsMetrics(t *testing.T) {
	cfg := chainTestConfig()
	orch := chainTestOrchestrator(cfg)
	fs := NewInMemoryFeedbackStore()
	orch.WithFeedbackStore(fs)

	now := time.Now().UTC()
	_ = fs.Insert(context.Background(), ExecutionFeedback{
		ID: "fb-c1", OutcomeType: "completed", SemanticSignal: "safe_action_succeeded", CreatedAt: now,
	})
	_ = fs.Insert(context.Background(), ExecutionFeedback{
		ID: "fb-c2", OutcomeType: "completed", SemanticSignal: "safe_action_succeeded", CreatedAt: now,
	})
	_ = fs.Insert(context.Background(), ExecutionFeedback{
		ID: "fb-f1", OutcomeType: "failed", SemanticSignal: "repeated_failure", CreatedAt: now,
	})

	metrics, err := orch.GetObjectiveFeedback(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, metrics.CompletedCount)
	assert.Equal(t, 1, metrics.FailedCount)
	assert.Equal(t, 0, metrics.AbortedCount)
	assert.Equal(t, 0, metrics.BlockedCount)
	assert.Equal(t, 3, metrics.TotalExecutions)
	assert.InDelta(t, 2.0/3.0, metrics.SuccessRate, 0.01)
	assert.Equal(t, 1, metrics.RepeatedFailures)
}

func TestObjectiveFeedback_NilStore(t *testing.T) {
	cfg := chainTestConfig()
	orch := chainTestOrchestrator(cfg)
	// No feedback store.
	metrics, err := orch.GetObjectiveFeedback(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, metrics.TotalExecutions)
	assert.Equal(t, 0.0, metrics.SuccessRate)
}

// ====================================================================
// End-to-End: Full Chain Integration
// ====================================================================

func TestEndToEnd_ActuationToFeedback(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	el := newMockExecutionLoop()
	fs := NewInMemoryFeedbackStore()
	orch.WithTaskOrchestrator(to).WithExecutionLoop(el).WithFeedbackStore(fs)

	// Step 1: Materialize actuation decision as task.
	d := ActuationDecisionInfo{
		ID:       "e2e-dec-1",
		Type:     "stabilize_income",
		Status:   "proposed",
		Priority: 0.7,
	}
	taskID, created, err := orch.MaterializeDecisionAsTask(context.Background(), d)
	require.NoError(t, err)
	require.True(t, created)
	require.NotEmpty(t, taskID)

	// Step 2: Simulate dispatch: set task to running with exec task ID.
	to.setTaskRunning(taskID, "exec-e2e-1")
	el.setTask(ExecTaskInfo{
		ID:             "exec-e2e-1",
		Status:         "completed",
		IterationCount: 3,
		StepsExecuted:  4,
		StepsFailed:    0,
	})

	// Step 3: Propagate execution results.
	completed, failed, paused, err := orch.PropagateExecutionResults(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, completed)
	assert.Equal(t, 0, failed)
	assert.Equal(t, 0, paused)

	// Step 4: Verify task lifecycle closed.
	assert.Equal(t, "completed", to.getTask(taskID).Status)

	// Step 5: Verify feedback was recorded.
	allFb := fs.AllFeedback()
	require.Len(t, allFb, 1)
	assert.Equal(t, "completed", allFb[0].OutcomeType)
	assert.True(t, allFb[0].Success)
	assert.Equal(t, "safe_action_succeeded", allFb[0].SemanticSignal)

	// Step 6: Verify reflection can consume feedback.
	signals, err := orch.GetReflectionFeedback(context.Background())
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "safe_action_succeeded", signals[0].Signal)

	// Step 7: Verify objective can consume feedback.
	metrics, err := orch.GetObjectiveFeedback(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, metrics.CompletedCount)
	assert.Equal(t, 1.0, metrics.SuccessRate)

	// Step 8: Verify state counters.
	state := orch.GetState()
	assert.Equal(t, 1, state.FeedbackRecorded)
	// Note: TasksCreatedFromActuation is incremented in cycleActuation, not MaterializeDecisionAsTask.
	// Direct calls don't update the cycle counter.
}

func TestEndToEnd_FailureChain(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	el := newMockExecutionLoop()
	fs := NewInMemoryFeedbackStore()
	orch.WithTaskOrchestrator(to).WithExecutionLoop(el).WithFeedbackStore(fs)

	// Create task from actuation.
	d := ActuationDecisionInfo{
		ID:       "e2e-fail-dec",
		Type:     "trigger_automation",
		Status:   "proposed",
		Priority: 0.6,
	}
	taskID, created, err := orch.MaterializeDecisionAsTask(context.Background(), d)
	require.NoError(t, err)
	require.True(t, created)

	// Simulate execution failure.
	to.setTaskRunning(taskID, "exec-e2e-fail")
	el.setTask(ExecTaskInfo{
		ID:            "exec-e2e-fail",
		Status:        "failed",
		AbortReason:   "connector execution error",
		StepsExecuted: 2,
		StepsFailed:   1,
	})

	_, failed, _, err := orch.PropagateExecutionResults(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, failed)

	// Verify failure feedback.
	allFb := fs.AllFeedback()
	require.Len(t, allFb, 1)
	assert.Equal(t, "failed", allFb[0].OutcomeType)
	assert.Equal(t, "execution_failure", allFb[0].SemanticSignal)

	// Verify objective sees the failure.
	metrics, err := orch.GetObjectiveFeedback(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, metrics.FailedCount)
	assert.Equal(t, 0.0, metrics.SuccessRate)
}

func TestEndToEnd_MixedOutcomes(t *testing.T) {
	cfg := chainTestConfig()
	cfg.Mode = ModeBoundedAutonomy
	orch := chainTestOrchestrator(cfg)
	to := newMockTaskOrchestrator()
	el := newMockExecutionLoop()
	fs := NewInMemoryFeedbackStore()
	orch.WithTaskOrchestrator(to).WithExecutionLoop(el).WithFeedbackStore(fs)

	// Task 1: completed.
	to.tasks["t-mix-1"] = &mockOrcTask{ID: "t-mix-1", Status: "running", ExecutionTaskID: "ex-1"}
	el.setTask(ExecTaskInfo{ID: "ex-1", Status: "completed", StepsExecuted: 3})

	// Task 2: failed.
	to.tasks["t-mix-2"] = &mockOrcTask{ID: "t-mix-2", Status: "running", ExecutionTaskID: "ex-2"}
	el.setTask(ExecTaskInfo{ID: "ex-2", Status: "failed", AbortReason: "timeout"})

	// Task 3: review block.
	to.tasks["t-mix-3"] = &mockOrcTask{ID: "t-mix-3", Status: "running", ExecutionTaskID: "ex-3"}
	el.setTask(ExecTaskInfo{ID: "ex-3", Status: "running", HasReviewBlock: true})

	completed, failed, paused, err := orch.PropagateExecutionResults(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, completed)
	assert.Equal(t, 1, failed)
	assert.Equal(t, 1, paused)

	allFb := fs.AllFeedback()
	assert.Len(t, allFb, 3) // all three produce feedback

	// Verify objective metrics reflect mixed outcomes.
	metrics, err := orch.GetObjectiveFeedback(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, metrics.CompletedCount)
	assert.Equal(t, 1, metrics.FailedCount)
	assert.Equal(t, 1, metrics.BlockedCount)
	assert.Equal(t, 3, metrics.TotalExecutions)
}

// ====================================================================
// InMemoryFeedbackStore tests
// ====================================================================

func TestInMemoryFeedbackStore_InsertAndList(t *testing.T) {
	store := NewInMemoryFeedbackStore()
	ctx := context.Background()

	now := time.Now().UTC()
	_ = store.Insert(ctx, ExecutionFeedback{ID: "fb-1", OutcomeType: "completed", CreatedAt: now})
	_ = store.Insert(ctx, ExecutionFeedback{ID: "fb-2", OutcomeType: "failed", CreatedAt: now.Add(time.Second)})
	_ = store.Insert(ctx, ExecutionFeedback{ID: "fb-3", OutcomeType: "completed", CreatedAt: now.Add(2 * time.Second)})

	// ListRecent orders by most recent first.
	recent, err := store.ListRecent(ctx, 10)
	require.NoError(t, err)
	require.Len(t, recent, 3)
	assert.Equal(t, "fb-3", recent[0].ID)
	assert.Equal(t, "fb-2", recent[1].ID)
	assert.Equal(t, "fb-1", recent[2].ID)

	// With limit.
	limited, err := store.ListRecent(ctx, 2)
	require.NoError(t, err)
	require.Len(t, limited, 2)
}

func TestInMemoryFeedbackStore_CountByOutcome(t *testing.T) {
	store := NewInMemoryFeedbackStore()
	ctx := context.Background()
	now := time.Now().UTC()

	_ = store.Insert(ctx, ExecutionFeedback{ID: "1", OutcomeType: "completed", CreatedAt: now})
	_ = store.Insert(ctx, ExecutionFeedback{ID: "2", OutcomeType: "completed", CreatedAt: now})
	_ = store.Insert(ctx, ExecutionFeedback{ID: "3", OutcomeType: "failed", CreatedAt: now})
	_ = store.Insert(ctx, ExecutionFeedback{ID: "4", OutcomeType: "completed", CreatedAt: now.Add(-48 * time.Hour)}) // old

	count, err := store.CountByOutcome(ctx, "completed", now.Add(-24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	failCount, err := store.CountByOutcome(ctx, "failed", now.Add(-24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, failCount)
}

func TestInMemoryFeedbackStore_CountBySignal(t *testing.T) {
	store := NewInMemoryFeedbackStore()
	ctx := context.Background()
	now := time.Now().UTC()

	_ = store.Insert(ctx, ExecutionFeedback{ID: "1", SemanticSignal: "repeated_failure", CreatedAt: now})
	_ = store.Insert(ctx, ExecutionFeedback{ID: "2", SemanticSignal: "safe_action_succeeded", CreatedAt: now})
	_ = store.Insert(ctx, ExecutionFeedback{ID: "3", SemanticSignal: "repeated_failure", CreatedAt: now})

	count, err := store.CountBySignal(ctx, "repeated_failure", now.Add(-1*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

// ====================================================================
// Helpers / edge cases
// ====================================================================

func TestClamp01Orch(t *testing.T) {
	assert.Equal(t, 0.0, clamp01Orch(-0.5))
	assert.Equal(t, 0.0, clamp01Orch(0.0))
	assert.Equal(t, 0.5, clamp01Orch(0.5))
	assert.Equal(t, 1.0, clamp01Orch(1.0))
	assert.Equal(t, 1.0, clamp01Orch(1.5))
}

func TestContains(t *testing.T) {
	assert.True(t, contains("objective penalty exceeded", "objective penalty"))
	assert.True(t, contains("consecutive failures limit", "consecutive failures"))
	assert.True(t, contains("governance blocked it", "governance"))
	assert.False(t, contains("something else", "objective penalty"))
	assert.True(t, contains("x", ""))
}

func TestActuationTypeGoalMapping(t *testing.T) {
	// Verify all 7 types are mapped.
	assert.Len(t, actuationTypeToGoal, 7)
	for k, v := range actuationTypeToGoal {
		assert.NotEmpty(t, v, "goal for type %s should not be empty", k)
	}
}

func TestActuationTypeStrategyMapping(t *testing.T) {
	assert.Equal(t, "consulting", actuationTypeToStrategy["stabilize_income"])
	assert.Equal(t, "automation_services", actuationTypeToStrategy["trigger_automation"])
	assert.Equal(t, "", actuationTypeToStrategy["reduce_load"]) // intentionally empty
}
