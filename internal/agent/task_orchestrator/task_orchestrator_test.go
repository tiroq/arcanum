package taskorchestrator

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// --- Mock providers ---

type mockObjectiveProvider struct {
	signalType     string
	signalStrength float64
}

func (m *mockObjectiveProvider) GetSignalType(_ context.Context) string      { return m.signalType }
func (m *mockObjectiveProvider) GetSignalStrength(_ context.Context) float64 { return m.signalStrength }

type mockGovernanceProvider struct {
	mode string
}

func (m *mockGovernanceProvider) GetMode(_ context.Context) string { return m.mode }

type mockCapacityProvider struct {
	load float64
}

func (m *mockCapacityProvider) GetLoad(_ context.Context) float64 { return m.load }

type mockPortfolioProvider struct {
	boosts map[string]float64
}

func (m *mockPortfolioProvider) GetStrategyBoost(_ context.Context, strategyType string) float64 {
	if m.boosts == nil {
		return 0
	}
	return m.boosts[strategyType]
}

type mockExecutionLoopProvider struct {
	calls    []string
	failNext bool
}

func (m *mockExecutionLoopProvider) CreateAndRun(_ context.Context, goal string) (string, error) {
	m.calls = append(m.calls, goal)
	if m.failNext {
		m.failNext = false
		return "", ErrGovernanceFrozen
	}
	return "exec-" + goal, nil
}

// --- Test helpers ---

func newTestEngine() (*Engine, *InMemoryTaskStore, *InMemoryQueueStore) {
	ts := NewInMemoryTaskStore()
	qs := NewInMemoryQueueStore()
	logger := zap.NewNop()
	auditor := &audit.NoOpAuditRecorder{}
	engine := NewEngine(ts, qs, auditor, logger)
	return engine, ts, qs
}

func fixedTime(t time.Time) func() {
	old := nowUTC
	nowUTC = func() time.Time { return t }
	return func() { nowUTC = old }
}

// ============================
// Priority Scoring Tests
// ============================

func TestComputePriority_BaseCase(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	task := OrchestratedTask{
		Urgency:       0.5,
		ExpectedValue: 500,
		RiskLevel:     0.3,
		CreatedAt:     now,
	}
	input := ScoringInput{}
	priority := ComputePriority(task, input, 0, now)

	// objective=1.0*0.30 + value=0.5*0.25 + urgency=0.5*0.20 + recency=0*0.10 - risk=0.3*0.15
	// = 0.30 + 0.125 + 0.10 + 0 - 0.045 = 0.48
	if priority < 0.47 || priority > 0.49 {
		t.Errorf("expected priority ~0.48, got %f", priority)
	}
}

func TestComputePriority_ObjectivePenalty(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	task := OrchestratedTask{
		Urgency:       0.5,
		ExpectedValue: 500,
		RiskLevel:     0.3,
		CreatedAt:     now,
	}
	input := ScoringInput{
		ObjectiveSignalType:     "penalty",
		ObjectiveSignalStrength: 0.5,
	}
	priority := ComputePriority(task, input, 0, now)

	// objective=(1-0.5)=0.5*0.30 = 0.15 instead of 0.30
	// total = 0.15 + 0.125 + 0.10 + 0 - 0.045 = 0.33
	if priority < 0.32 || priority > 0.34 {
		t.Errorf("expected priority ~0.33, got %f", priority)
	}
}

func TestComputePriority_HighRiskCap(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	task := OrchestratedTask{
		Urgency:       1.0,
		ExpectedValue: 1000,
		RiskLevel:     0.75,
		CreatedAt:     now,
	}
	input := ScoringInput{}
	priority := ComputePriority(task, input, 0, now)

	if priority > HighRiskMaxPrio {
		t.Errorf("expected priority capped at %f, got %f", HighRiskMaxPrio, priority)
	}
}

func TestComputePriority_RecencyBoost(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	oldTime := now.Add(-8 * time.Hour) // 8 hours old, past starvation threshold
	task := OrchestratedTask{
		Urgency:       0.5,
		ExpectedValue: 500,
		RiskLevel:     0.3,
		CreatedAt:     oldTime,
	}
	taskRecent := OrchestratedTask{
		Urgency:       0.5,
		ExpectedValue: 500,
		RiskLevel:     0.3,
		CreatedAt:     now,
	}
	input := ScoringInput{}
	prioOld := ComputePriority(task, input, 0, now)
	prioNew := ComputePriority(taskRecent, input, 0, now)

	if prioOld <= prioNew {
		t.Errorf("expected old task priority %f > new task priority %f", prioOld, prioNew)
	}
}

func TestComputePriority_PortfolioBoost(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	task := OrchestratedTask{
		Urgency:       0.5,
		ExpectedValue: 500,
		RiskLevel:     0.3,
		CreatedAt:     now,
	}
	input := ScoringInput{}
	prioBase := ComputePriority(task, input, 0, now)
	prioBoosted := ComputePriority(task, input, 0.10, now)

	if prioBoosted <= prioBase {
		t.Errorf("expected boosted priority %f > base priority %f", prioBoosted, prioBase)
	}
}

func TestComputeRecencyBoost_NoBoostRecent(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	created := now.Add(-2 * time.Hour)
	boost := ComputeRecencyBoost(created, now)
	if boost != 0 {
		t.Errorf("expected 0 boost for recent task, got %f", boost)
	}
}

func TestComputeRecencyBoost_BoostOld(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	created := now.Add(-12 * time.Hour) // 12h old, boost = (12-6)/6 = 1.0
	boost := ComputeRecencyBoost(created, now)
	if boost != 1.0 {
		t.Errorf("expected 1.0 boost for very old task, got %f", boost)
	}
}

func TestIsExpired(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	task := OrchestratedTask{CreatedAt: now.Add(-25 * time.Hour)}
	if !IsExpired(task, now) {
		t.Error("expected task to be expired")
	}
	task2 := OrchestratedTask{CreatedAt: now.Add(-23 * time.Hour)}
	if IsExpired(task2, now) {
		t.Error("expected task to NOT be expired")
	}
}

func TestIsInCooldown(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	task := OrchestratedTask{UpdatedAt: now.Add(-2 * time.Minute)}
	if !IsInCooldown(task, now) {
		t.Error("expected task to be in cooldown")
	}
	task2 := OrchestratedTask{UpdatedAt: now.Add(-10 * time.Minute)}
	if IsInCooldown(task2, now) {
		t.Error("expected task to NOT be in cooldown")
	}
}

func TestShouldReduceDispatch(t *testing.T) {
	if !ShouldReduceDispatch(0.80) {
		t.Error("expected dispatch reduction at 0.80 load")
	}
	if ShouldReduceDispatch(0.50) {
		t.Error("expected no dispatch reduction at 0.50 load")
	}
}

// ============================
// Queue Tests
// ============================

func TestPriorityQueue_Ordering(t *testing.T) {
	pq := NewPriorityQueue()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)

	pq.Enqueue(TaskQueueEntry{TaskID: "low", PriorityScore: 0.3, InsertedAt: now})
	pq.Enqueue(TaskQueueEntry{TaskID: "high", PriorityScore: 0.9, InsertedAt: now})
	pq.Enqueue(TaskQueueEntry{TaskID: "mid", PriorityScore: 0.5, InsertedAt: now})

	top, ok := pq.Dequeue()
	if !ok || top.TaskID != "high" {
		t.Errorf("expected 'high' first, got %s", top.TaskID)
	}
}

func TestPriorityQueue_TieBreaking(t *testing.T) {
	pq := NewPriorityQueue()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)

	pq.Enqueue(TaskQueueEntry{TaskID: "newer", PriorityScore: 0.5, InsertedAt: now.Add(1 * time.Hour)})
	pq.Enqueue(TaskQueueEntry{TaskID: "older", PriorityScore: 0.5, InsertedAt: now})

	top, ok := pq.Dequeue()
	if !ok || top.TaskID != "older" {
		t.Errorf("expected 'older' to win tie-break, got %s", top.TaskID)
	}
}

func TestPriorityQueue_EmptyDequeue(t *testing.T) {
	pq := NewPriorityQueue()
	_, ok := pq.Dequeue()
	if ok {
		t.Error("expected false on empty dequeue")
	}
}

func TestPriorityQueue_TopN(t *testing.T) {
	pq := NewPriorityQueue()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)

	pq.Enqueue(TaskQueueEntry{TaskID: "a", PriorityScore: 0.9, InsertedAt: now})
	pq.Enqueue(TaskQueueEntry{TaskID: "b", PriorityScore: 0.5, InsertedAt: now})
	pq.Enqueue(TaskQueueEntry{TaskID: "c", PriorityScore: 0.7, InsertedAt: now})

	top := pq.TopN(2)
	if len(top) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(top))
	}
	if top[0].TaskID != "a" || top[1].TaskID != "c" {
		t.Errorf("expected [a, c], got [%s, %s]", top[0].TaskID, top[1].TaskID)
	}
	// Original queue should be unchanged.
	if pq.Len() != 3 {
		t.Errorf("expected queue length 3 after TopN, got %d", pq.Len())
	}
}

func TestPriorityQueue_BuildFromEntries(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	entries := []TaskQueueEntry{
		{TaskID: "low", PriorityScore: 0.2, InsertedAt: now},
		{TaskID: "high", PriorityScore: 0.8, InsertedAt: now},
	}
	pq := BuildFromEntries(entries)
	top, _ := pq.Dequeue()
	if top.TaskID != "high" {
		t.Errorf("expected 'high', got %s", top.TaskID)
	}
}

func TestPriorityQueue_Peek(t *testing.T) {
	pq := NewPriorityQueue()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)

	pq.Enqueue(TaskQueueEntry{TaskID: "a", PriorityScore: 0.9, InsertedAt: now})
	pq.Enqueue(TaskQueueEntry{TaskID: "b", PriorityScore: 0.5, InsertedAt: now})

	top, ok := pq.Peek()
	if !ok || top.TaskID != "a" {
		t.Errorf("expected 'a' from peek, got %s", top.TaskID)
	}
	if pq.Len() != 2 {
		t.Error("peek should not remove elements")
	}
}

// ============================
// Engine Tests
// ============================

func TestEngine_CreateTask(t *testing.T) {
	engine, ts, _ := newTestEngine()
	ctx := context.Background()

	task, err := engine.CreateTask(ctx, "manual", "test goal", 0.5, 500, 0.3, "consulting")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.Status != TaskStatusPending {
		t.Errorf("expected pending, got %s", task.Status)
	}
	if task.Goal != "test goal" {
		t.Errorf("expected 'test goal', got %s", task.Goal)
	}

	stored, ok := ts.GetTaskDirect(task.ID)
	if !ok {
		t.Fatal("task not found in store")
	}
	if stored.Source != "manual" {
		t.Errorf("expected source 'manual', got %s", stored.Source)
	}
}

func TestEngine_GetTask(t *testing.T) {
	engine, _, _ := newTestEngine()
	ctx := context.Background()

	task, _ := engine.CreateTask(ctx, "manual", "goal", 0.5, 100, 0.1, "")
	retrieved, err := engine.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if retrieved.ID != task.ID {
		t.Errorf("expected %s, got %s", task.ID, retrieved.ID)
	}
}

func TestEngine_GetTask_NotFound(t *testing.T) {
	engine, _, _ := newTestEngine()
	ctx := context.Background()

	_, err := engine.GetTask(ctx, "nonexistent")
	if err != ErrTaskNotFound {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestEngine_ListTasks(t *testing.T) {
	engine, _, _ := newTestEngine()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, _ = engine.CreateTask(ctx, "manual", "goal", 0.5, 100, 0.1, "")
	}

	tasks, err := engine.ListTasks(ctx, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 5 {
		t.Errorf("expected 5 tasks, got %d", len(tasks))
	}
}

func TestEngine_RecomputePriorities(t *testing.T) {
	engine, ts, qs := newTestEngine()
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	restore := fixedTime(now)
	defer restore()

	task, _ := engine.CreateTask(ctx, "manual", "goal", 0.5, 500, 0.3, "")
	// Set timestamps in the past to avoid cooldown.
	ts.SetTimestamps(task.ID, now.Add(-10*time.Minute), now.Add(-10*time.Minute))

	err := engine.RecomputePriorities(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Task should be queued now.
	updated, _ := ts.Get(ctx, task.ID)
	if updated.Status != TaskStatusQueued {
		t.Errorf("expected queued, got %s", updated.Status)
	}
	if updated.PriorityScore <= 0 {
		t.Error("expected positive priority score")
	}

	// Queue should have an entry.
	entry, ok := qs.GetEntryDirect(task.ID)
	if !ok {
		t.Fatal("expected queue entry")
	}
	if entry.PriorityScore != updated.PriorityScore {
		t.Errorf("queue score %f != task score %f", entry.PriorityScore, updated.PriorityScore)
	}
}

func TestEngine_RecomputePriorities_ExpiresOldTasks(t *testing.T) {
	engine, ts, _ := newTestEngine()
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	restore := fixedTime(now)
	defer restore()

	task, _ := engine.CreateTask(ctx, "manual", "old goal", 0.5, 500, 0.3, "")
	// Set creation time to 25 hours ago.
	ts.SetTimestamps(task.ID, now.Add(-25*time.Hour), now.Add(-25*time.Hour))

	_ = engine.RecomputePriorities(ctx)

	updated, _ := ts.Get(ctx, task.ID)
	if updated.Status != TaskStatusFailed {
		t.Errorf("expected failed (expired), got %s", updated.Status)
	}
}

func TestEngine_RecomputePriorities_SkipsRunning(t *testing.T) {
	engine, ts, _ := newTestEngine()
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	restore := fixedTime(now)
	defer restore()

	task, _ := engine.CreateTask(ctx, "manual", "running goal", 0.5, 500, 0.3, "")
	// Manually set to running.
	t2, _ := ts.Get(ctx, task.ID)
	t2.Status = TaskStatusRunning
	t2.CreatedAt = now.Add(-10 * time.Minute)
	t2.UpdatedAt = now.Add(-10 * time.Minute)
	_ = ts.Update(ctx, t2)

	_ = engine.RecomputePriorities(ctx)

	// Should remain running.
	updated, _ := ts.Get(ctx, task.ID)
	if updated.Status != TaskStatusRunning {
		t.Errorf("expected running, got %s", updated.Status)
	}
}

func TestEngine_RecomputePriorities_SkipsCooldown(t *testing.T) {
	engine, _, _ := newTestEngine()
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	restore := fixedTime(now)
	defer restore()

	task, _ := engine.CreateTask(ctx, "manual", "cooldown goal", 0.5, 500, 0.3, "")
	// Task was just created, within cooldown window.

	_ = engine.RecomputePriorities(ctx)

	// Should remain pending (skipped due to cooldown).
	updated, _ := engine.GetTask(ctx, task.ID)
	if updated.Status != TaskStatusPending {
		t.Errorf("expected pending (cooldown), got %s", updated.Status)
	}
}

func TestEngine_RecomputePriorities_ObjectivePenalty(t *testing.T) {
	engine, ts, _ := newTestEngine()
	engine.WithObjective(&mockObjectiveProvider{signalType: "penalty", signalStrength: 0.5})
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	restore := fixedTime(now)
	defer restore()

	task, _ := engine.CreateTask(ctx, "manual", "penalty test", 0.5, 500, 0.3, "")
	ts.SetTimestamps(task.ID, now.Add(-10*time.Minute), now.Add(-10*time.Minute))

	_ = engine.RecomputePriorities(ctx)

	updated, _ := ts.Get(ctx, task.ID)
	// With penalty, priority should be lower.
	if updated.PriorityScore > 0.40 {
		t.Errorf("expected reduced priority due to penalty, got %f", updated.PriorityScore)
	}
}

func TestEngine_RecomputePriorities_PortfolioBoost(t *testing.T) {
	engine, ts, _ := newTestEngine()
	engine.WithPortfolio(&mockPortfolioProvider{boosts: map[string]float64{"consulting": 0.10}})
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	restore := fixedTime(now)
	defer restore()

	task1, _ := engine.CreateTask(ctx, "manual", "consulting task", 0.5, 500, 0.3, "consulting")
	task2, _ := engine.CreateTask(ctx, "manual", "other task", 0.5, 500, 0.3, "other")
	ts.SetTimestamps(task1.ID, now.Add(-10*time.Minute), now.Add(-10*time.Minute))
	ts.SetTimestamps(task2.ID, now.Add(-10*time.Minute), now.Add(-10*time.Minute))

	_ = engine.RecomputePriorities(ctx)

	t1, _ := ts.Get(ctx, task1.ID)
	t2, _ := ts.Get(ctx, task2.ID)
	if t1.PriorityScore <= t2.PriorityScore {
		t.Errorf("expected consulting task priority %f > other task priority %f", t1.PriorityScore, t2.PriorityScore)
	}
}

// ============================
// Dispatch Tests
// ============================

func TestEngine_Dispatch_Basic(t *testing.T) {
	engine, ts, qs := newTestEngine()
	execLoop := &mockExecutionLoopProvider{}
	engine.WithExecutionLoop(execLoop)
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	restore := fixedTime(now)
	defer restore()

	task, _ := engine.CreateTask(ctx, "manual", "dispatch me", 0.8, 800, 0.2, "")
	ts.SetTimestamps(task.ID, now.Add(-10*time.Minute), now.Add(-10*time.Minute))

	// Score and queue the task.
	_ = engine.RecomputePriorities(ctx)

	// Dispatch.
	result, err := engine.Dispatch(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Dispatched) != 1 {
		t.Fatalf("expected 1 dispatched, got %d", len(result.Dispatched))
	}
	if result.Dispatched[0] != task.ID {
		t.Errorf("expected %s dispatched, got %s", task.ID, result.Dispatched[0])
	}

	// Task should be running.
	updated, _ := ts.Get(ctx, task.ID)
	if updated.Status != TaskStatusRunning {
		t.Errorf("expected running, got %s", updated.Status)
	}

	// Queue should be empty.
	if qs.CountDirect() != 0 {
		t.Errorf("expected empty queue, got %d entries", qs.CountDirect())
	}

	// Execution loop should have been called.
	if len(execLoop.calls) != 1 {
		t.Errorf("expected 1 exec call, got %d", len(execLoop.calls))
	}
}

func TestEngine_Dispatch_GovernanceFrozen(t *testing.T) {
	engine, _, _ := newTestEngine()
	engine.WithGovernance(&mockGovernanceProvider{mode: "frozen"})
	ctx := context.Background()

	_, err := engine.Dispatch(ctx)
	if err != ErrGovernanceFrozen {
		t.Errorf("expected ErrGovernanceFrozen, got %v", err)
	}
}

func TestEngine_Dispatch_MaxRunning(t *testing.T) {
	engine, ts, qs := newTestEngine()
	execLoop := &mockExecutionLoopProvider{}
	engine.WithExecutionLoop(execLoop)
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	restore := fixedTime(now)
	defer restore()

	// Create 2 running tasks to fill slots.
	for i := 0; i < MaxRunningTasks; i++ {
		task, _ := engine.CreateTask(ctx, "manual", "running", 0.5, 100, 0.1, "")
		t2, _ := ts.Get(ctx, task.ID)
		t2.Status = TaskStatusRunning
		_ = ts.Update(ctx, t2)
	}

	// Create a pending task and queue it.
	task, _ := engine.CreateTask(ctx, "manual", "waiting", 0.5, 100, 0.1, "")
	ts.SetTimestamps(task.ID, now.Add(-10*time.Minute), now.Add(-10*time.Minute))
	_ = qs.Upsert(ctx, TaskQueueEntry{TaskID: task.ID, PriorityScore: 0.5, InsertedAt: now})

	_, err := engine.Dispatch(ctx)
	if err != ErrMaxRunning {
		t.Errorf("expected ErrMaxRunning, got %v", err)
	}
}

func TestEngine_Dispatch_RiskBlocked(t *testing.T) {
	engine, ts, qs := newTestEngine()
	execLoop := &mockExecutionLoopProvider{}
	engine.WithExecutionLoop(execLoop)
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	restore := fixedTime(now)
	defer restore()

	task, _ := engine.CreateTask(ctx, "manual", "risky task", 0.5, 100, 0.95, "")
	ts.SetTimestamps(task.ID, now.Add(-10*time.Minute), now.Add(-10*time.Minute))
	_ = engine.RecomputePriorities(ctx)

	// Manually ensure it's in queue (recompute may have set it).
	_ = qs.Upsert(ctx, TaskQueueEntry{TaskID: task.ID, PriorityScore: 0.3, InsertedAt: now})

	result, err := engine.Dispatch(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Blocked) != 1 {
		t.Errorf("expected 1 blocked, got %d", len(result.Blocked))
	}

	updated, _ := ts.Get(ctx, task.ID)
	if updated.Status != TaskStatusPaused {
		t.Errorf("expected paused, got %s", updated.Status)
	}
}

func TestEngine_Dispatch_SupervisedHighRisk(t *testing.T) {
	engine, ts, qs := newTestEngine()
	engine.WithGovernance(&mockGovernanceProvider{mode: "supervised"})
	execLoop := &mockExecutionLoopProvider{}
	engine.WithExecutionLoop(execLoop)
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	restore := fixedTime(now)
	defer restore()

	task, _ := engine.CreateTask(ctx, "manual", "modestly risky", 0.5, 100, 0.75, "")
	// Set queued status and in queue.
	t2, _ := ts.Get(ctx, task.ID)
	t2.Status = TaskStatusQueued
	t2.CreatedAt = now.Add(-10 * time.Minute)
	t2.UpdatedAt = now.Add(-10 * time.Minute)
	_ = ts.Update(ctx, t2)
	_ = qs.Upsert(ctx, TaskQueueEntry{TaskID: task.ID, PriorityScore: 0.4, InsertedAt: now})

	result, err := engine.Dispatch(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Skipped) != 1 {
		t.Errorf("expected 1 skipped, got %d", len(result.Skipped))
	}
}

func TestEngine_Dispatch_ReduceOnOverload(t *testing.T) {
	engine, ts, qs := newTestEngine()
	engine.WithCapacity(&mockCapacityProvider{load: 0.80})
	execLoop := &mockExecutionLoopProvider{}
	engine.WithExecutionLoop(execLoop)
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	restore := fixedTime(now)
	defer restore()

	// Create 3 queued tasks.
	for i := 0; i < 3; i++ {
		task, _ := engine.CreateTask(ctx, "manual", "task", 0.5, 100, 0.1, "")
		t2, _ := ts.Get(ctx, task.ID)
		t2.Status = TaskStatusQueued
		t2.CreatedAt = now.Add(-10 * time.Minute)
		t2.UpdatedAt = now.Add(-10 * time.Minute)
		_ = ts.Update(ctx, t2)
		_ = qs.Upsert(ctx, TaskQueueEntry{TaskID: task.ID, PriorityScore: 0.5, InsertedAt: now.Add(time.Duration(i) * time.Minute)})
	}

	result, err := engine.Dispatch(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With overload, should only dispatch 1.
	if len(result.Dispatched) != 1 {
		t.Errorf("expected 1 dispatched under overload, got %d", len(result.Dispatched))
	}
}

func TestEngine_Dispatch_EmptyQueue(t *testing.T) {
	engine, _, _ := newTestEngine()
	execLoop := &mockExecutionLoopProvider{}
	engine.WithExecutionLoop(execLoop)
	ctx := context.Background()

	result, err := engine.Dispatch(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Dispatched) != 0 {
		t.Errorf("expected 0 dispatched, got %d", len(result.Dispatched))
	}
}

func TestEngine_Dispatch_MaxTasksPerCycle(t *testing.T) {
	engine, ts, qs := newTestEngine()
	execLoop := &mockExecutionLoopProvider{}
	engine.WithExecutionLoop(execLoop)
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	restore := fixedTime(now)
	defer restore()

	// Create 5 queued tasks.
	for i := 0; i < 5; i++ {
		task, _ := engine.CreateTask(ctx, "manual", "task", 0.5, 100, 0.1, "")
		t2, _ := ts.Get(ctx, task.ID)
		t2.Status = TaskStatusQueued
		t2.CreatedAt = now.Add(-10 * time.Minute)
		t2.UpdatedAt = now.Add(-10 * time.Minute)
		_ = ts.Update(ctx, t2)
		_ = qs.Upsert(ctx, TaskQueueEntry{TaskID: task.ID, PriorityScore: 0.5, InsertedAt: now.Add(time.Duration(i) * time.Minute)})
	}

	result, err := engine.Dispatch(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// MaxRunningTasks=2 should cap at 2 (even though MaxTasksPerCycle=3).
	if len(result.Dispatched) > MaxRunningTasks {
		t.Errorf("dispatched %d exceeds MaxRunningTasks %d", len(result.Dispatched), MaxRunningTasks)
	}
}

func TestEngine_Dispatch_ExecutionLoopFailure(t *testing.T) {
	engine, ts, qs := newTestEngine()
	execLoop := &mockExecutionLoopProvider{failNext: true}
	engine.WithExecutionLoop(execLoop)
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	restore := fixedTime(now)
	defer restore()

	task, _ := engine.CreateTask(ctx, "manual", "fail task", 0.5, 100, 0.1, "")
	t2, _ := ts.Get(ctx, task.ID)
	t2.Status = TaskStatusQueued
	t2.CreatedAt = now.Add(-10 * time.Minute)
	t2.UpdatedAt = now.Add(-10 * time.Minute)
	_ = ts.Update(ctx, t2)
	_ = qs.Upsert(ctx, TaskQueueEntry{TaskID: task.ID, PriorityScore: 0.5, InsertedAt: now})

	result, err := engine.Dispatch(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Skipped) != 1 {
		t.Errorf("expected 1 skipped, got %d", len(result.Skipped))
	}
}

func TestEngine_Dispatch_NoExecutionLoop(t *testing.T) {
	engine, ts, qs := newTestEngine()
	// No execution loop provider set.
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	restore := fixedTime(now)
	defer restore()

	task, _ := engine.CreateTask(ctx, "manual", "no exec loop", 0.5, 100, 0.1, "")
	t2, _ := ts.Get(ctx, task.ID)
	t2.Status = TaskStatusQueued
	t2.CreatedAt = now.Add(-10 * time.Minute)
	t2.UpdatedAt = now.Add(-10 * time.Minute)
	_ = ts.Update(ctx, t2)
	_ = qs.Upsert(ctx, TaskQueueEntry{TaskID: task.ID, PriorityScore: 0.5, InsertedAt: now})

	result, err := engine.Dispatch(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Skipped) != 1 {
		t.Errorf("expected 1 skipped, got %d", len(result.Skipped))
	}
}

// ============================
// State Transition Tests
// ============================

func TestValidateTransition(t *testing.T) {
	tests := []struct {
		from, to TaskStatus
		valid    bool
	}{
		{TaskStatusPending, TaskStatusQueued, true},
		{TaskStatusPending, TaskStatusPaused, true},
		{TaskStatusPending, TaskStatusRunning, false},
		{TaskStatusQueued, TaskStatusRunning, true},
		{TaskStatusQueued, TaskStatusPaused, true},
		{TaskStatusRunning, TaskStatusCompleted, true},
		{TaskStatusRunning, TaskStatusFailed, true},
		{TaskStatusRunning, TaskStatusPaused, true},
		{TaskStatusCompleted, TaskStatusRunning, false},
		{TaskStatusFailed, TaskStatusQueued, false},
		{TaskStatusPaused, TaskStatusQueued, true},
		{TaskStatusPaused, TaskStatusRunning, false},
	}

	for _, tc := range tests {
		result := ValidateTransition(tc.from, tc.to)
		if result != tc.valid {
			t.Errorf("ValidateTransition(%s→%s) = %v, want %v", tc.from, tc.to, result, tc.valid)
		}
	}
}

func TestEngine_CompleteTask(t *testing.T) {
	engine, ts, _ := newTestEngine()
	ctx := context.Background()

	task, _ := engine.CreateTask(ctx, "manual", "to complete", 0.5, 100, 0.1, "")
	// Transition to queued → running → completed.
	t2, _ := ts.Get(ctx, task.ID)
	t2.Status = TaskStatusRunning
	_ = ts.Update(ctx, t2)

	completed, err := engine.CompleteTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if completed.Status != TaskStatusCompleted {
		t.Errorf("expected completed, got %s", completed.Status)
	}
}

func TestEngine_FailTask(t *testing.T) {
	engine, ts, _ := newTestEngine()
	ctx := context.Background()

	task, _ := engine.CreateTask(ctx, "manual", "to fail", 0.5, 100, 0.1, "")
	t2, _ := ts.Get(ctx, task.ID)
	t2.Status = TaskStatusRunning
	_ = ts.Update(ctx, t2)

	failed, err := engine.FailTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if failed.Status != TaskStatusFailed {
		t.Errorf("expected failed, got %s", failed.Status)
	}
}

func TestEngine_PauseTask(t *testing.T) {
	engine, ts, _ := newTestEngine()
	ctx := context.Background()

	task, _ := engine.CreateTask(ctx, "manual", "to pause", 0.5, 100, 0.1, "")
	t2, _ := ts.Get(ctx, task.ID)
	t2.Status = TaskStatusRunning
	_ = ts.Update(ctx, t2)

	paused, err := engine.PauseTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if paused.Status != TaskStatusPaused {
		t.Errorf("expected paused, got %s", paused.Status)
	}
}

func TestEngine_InvalidTransition(t *testing.T) {
	engine, _, _ := newTestEngine()
	ctx := context.Background()

	task, _ := engine.CreateTask(ctx, "manual", "test", 0.5, 100, 0.1, "")
	// pending → completed is invalid.
	_, err := engine.CompleteTask(ctx, task.ID)
	if err != ErrInvalidTransition {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

// ============================
// Adapter Tests
// ============================

func TestGraphAdapter_NilSafe(t *testing.T) {
	var adapter *GraphAdapter
	ctx := context.Background()

	// All methods should return zero values without panicking.
	task, err := adapter.CreateTask(ctx, "manual", "goal", 0.5, 100, 0.1, "")
	if err != nil || task.ID != "" {
		t.Error("expected zero value from nil adapter")
	}

	_, err = adapter.GetTask(ctx, "id")
	if err != nil {
		t.Error("expected nil error from nil adapter")
	}

	tasks, err := adapter.ListTasks(ctx, 10)
	if err != nil || tasks != nil {
		t.Error("expected nil from nil adapter")
	}

	err = adapter.RecomputePriorities(ctx)
	if err != nil {
		t.Error("expected nil error from nil adapter")
	}

	result, err := adapter.Dispatch(ctx)
	if err != nil || len(result.Dispatched) != 0 {
		t.Error("expected empty result from nil adapter")
	}

	q, err := adapter.GetQueue(ctx, 10)
	if err != nil || q != nil {
		t.Error("expected nil from nil adapter")
	}
}

func TestGraphAdapter_NilEngine(t *testing.T) {
	adapter := &GraphAdapter{engine: nil}
	ctx := context.Background()

	task, err := adapter.CreateTask(ctx, "manual", "goal", 0.5, 100, 0.1, "")
	if err != nil || task.ID != "" {
		t.Error("expected zero value from nil engine adapter")
	}
}

// ============================
// In-Memory Store Tests
// ============================

func TestInMemoryQueueStore_Ordering(t *testing.T) {
	qs := NewInMemoryQueueStore()
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)

	_ = qs.Upsert(ctx, TaskQueueEntry{TaskID: "low", PriorityScore: 0.2, InsertedAt: now})
	_ = qs.Upsert(ctx, TaskQueueEntry{TaskID: "high", PriorityScore: 0.8, InsertedAt: now})
	_ = qs.Upsert(ctx, TaskQueueEntry{TaskID: "mid", PriorityScore: 0.5, InsertedAt: now})

	entries, _ := qs.List(ctx, 10)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].TaskID != "high" {
		t.Errorf("expected 'high' first, got %s", entries[0].TaskID)
	}
}

func TestInMemoryQueueStore_Remove(t *testing.T) {
	qs := NewInMemoryQueueStore()
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)

	_ = qs.Upsert(ctx, TaskQueueEntry{TaskID: "a", PriorityScore: 0.5, InsertedAt: now})
	_ = qs.Remove(ctx, "a")

	count, _ := qs.Count(ctx)
	if count != 0 {
		t.Errorf("expected 0 entries after remove, got %d", count)
	}
}

func TestInMemoryQueueStore_Upsert(t *testing.T) {
	qs := NewInMemoryQueueStore()
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)

	_ = qs.Upsert(ctx, TaskQueueEntry{TaskID: "a", PriorityScore: 0.5, InsertedAt: now})
	_ = qs.Upsert(ctx, TaskQueueEntry{TaskID: "a", PriorityScore: 0.9, InsertedAt: now})

	count, _ := qs.Count(ctx)
	if count != 1 {
		t.Errorf("expected 1 entry after upsert, got %d", count)
	}

	entry, ok := qs.GetEntryDirect("a")
	if !ok || entry.PriorityScore != 0.9 {
		t.Error("expected updated priority score")
	}
}

// ============================
// Edge Case Tests
// ============================

func TestComputePriority_ZeroValues(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	task := OrchestratedTask{CreatedAt: now}
	input := ScoringInput{}
	priority := ComputePriority(task, input, 0, now)

	// objective=1.0*0.30 + value=0 + urgency=0 + recency=0 - risk=0 = 0.30
	if priority < 0.29 || priority > 0.31 {
		t.Errorf("expected priority ~0.30, got %f", priority)
	}
}

func TestComputePriority_MaxValues(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	task := OrchestratedTask{
		Urgency:       1.0,
		ExpectedValue: 2000,
		RiskLevel:     0.0,
		CreatedAt:     now.Add(-24 * time.Hour),
	}
	input := ScoringInput{}
	priority := ComputePriority(task, input, 0.12, now)
	if priority > 1.0 {
		t.Errorf("expected priority <= 1.0, got %f", priority)
	}
}

func TestComputePriority_NegativePortfolioBoost(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	task := OrchestratedTask{
		Urgency:       0.5,
		ExpectedValue: 500,
		RiskLevel:     0.3,
		CreatedAt:     now,
	}
	input := ScoringInput{}
	prioPos := ComputePriority(task, input, 0.10, now)
	prioNeg := ComputePriority(task, input, -0.10, now)
	if prioNeg >= prioPos {
		t.Errorf("expected negative boost priority %f < positive boost priority %f", prioNeg, prioPos)
	}
}

func TestEngine_FullCycle(t *testing.T) {
	engine, ts, _ := newTestEngine()
	execLoop := &mockExecutionLoopProvider{}
	engine.WithExecutionLoop(execLoop)
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	restore := fixedTime(now)
	defer restore()

	// Create tasks.
	task1, _ := engine.CreateTask(ctx, "actuation", "high priority", 0.9, 900, 0.1, "consulting")
	task2, _ := engine.CreateTask(ctx, "manual", "low priority", 0.2, 200, 0.1, "")
	ts.SetTimestamps(task1.ID, now.Add(-10*time.Minute), now.Add(-10*time.Minute))
	ts.SetTimestamps(task2.ID, now.Add(-10*time.Minute), now.Add(-10*time.Minute))

	// Recompute.
	_ = engine.RecomputePriorities(ctx)

	// Dispatch.
	result, err := engine.Dispatch(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Dispatched) < 1 {
		t.Fatal("expected at least 1 dispatched")
	}

	// Complete first dispatched.
	_, err = engine.CompleteTask(ctx, result.Dispatched[0])
	if err != nil {
		t.Fatalf("unexpected error completing task: %v", err)
	}

	// Verify execution loop was called.
	if len(execLoop.calls) < 1 {
		t.Error("expected execution loop to be called")
	}
}

func TestEngine_QueueSizeLimit(t *testing.T) {
	engine, ts, qs := newTestEngine()
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	restore := fixedTime(now)
	defer restore()

	// Create MaxTasksInQueue + 5 tasks.
	for i := 0; i < MaxTasksInQueue+5; i++ {
		task, _ := engine.CreateTask(ctx, "manual", "task", 0.5, 100, 0.1, "")
		ts.SetTimestamps(task.ID, now.Add(-10*time.Minute), now.Add(-10*time.Minute))
	}

	_ = engine.RecomputePriorities(ctx)

	// Queue should be bounded.
	count, _ := qs.Count(ctx)
	if count > MaxTasksInQueue+5 {
		t.Errorf("queue should not grow unboundedly, got %d", count)
	}
}

func TestTaskStatus_IsTerminal(t *testing.T) {
	if !TaskStatusCompleted.IsTerminal() {
		t.Error("completed should be terminal")
	}
	if !TaskStatusFailed.IsTerminal() {
		t.Error("failed should be terminal")
	}
	if TaskStatusPending.IsTerminal() {
		t.Error("pending should not be terminal")
	}
	if TaskStatusRunning.IsTerminal() {
		t.Error("running should not be terminal")
	}
	if TaskStatusPaused.IsTerminal() {
		t.Error("paused should not be terminal")
	}
	if TaskStatusQueued.IsTerminal() {
		t.Error("queued should not be terminal")
	}
}

func TestClamp01(t *testing.T) {
	if clamp01(-0.5) != 0 {
		t.Error("clamp01(-0.5) should be 0")
	}
	if clamp01(1.5) != 1 {
		t.Error("clamp01(1.5) should be 1")
	}
	if clamp01(0.5) != 0.5 {
		t.Error("clamp01(0.5) should be 0.5")
	}
}

func TestClamp(t *testing.T) {
	if clamp(-0.5, -0.10, 0.12) != -0.10 {
		t.Error("clamp(-0.5, -0.10, 0.12) should be -0.10")
	}
	if clamp(0.5, -0.10, 0.12) != 0.12 {
		t.Error("clamp(0.5, -0.10, 0.12) should be 0.12")
	}
	if clamp(0.05, -0.10, 0.12) != 0.05 {
		t.Error("clamp(0.05, -0.10, 0.12) should be 0.05")
	}
}

func TestEngine_GetQueue(t *testing.T) {
	engine, _, qs := newTestEngine()
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)

	_ = qs.Upsert(ctx, TaskQueueEntry{TaskID: "a", PriorityScore: 0.8, InsertedAt: now})
	_ = qs.Upsert(ctx, TaskQueueEntry{TaskID: "b", PriorityScore: 0.5, InsertedAt: now})

	queue, err := engine.GetQueue(ctx, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(queue) != 2 {
		t.Errorf("expected 2 queue entries, got %d", len(queue))
	}
}

func TestEngine_Dispatch_TerminalTasksRemovedFromQueue(t *testing.T) {
	engine, ts, qs := newTestEngine()
	execLoop := &mockExecutionLoopProvider{}
	engine.WithExecutionLoop(execLoop)
	ctx := context.Background()
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	restore := fixedTime(now)
	defer restore()

	// Create a completed task that's still in the queue (stale entry).
	task, _ := engine.CreateTask(ctx, "manual", "completed task", 0.5, 100, 0.1, "")
	t2, _ := ts.Get(ctx, task.ID)
	t2.Status = TaskStatusCompleted
	_ = ts.Update(ctx, t2)
	_ = qs.Upsert(ctx, TaskQueueEntry{TaskID: task.ID, PriorityScore: 0.9, InsertedAt: now})

	result, err := engine.Dispatch(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Dispatched) != 0 {
		t.Errorf("expected 0 dispatched (terminal), got %d", len(result.Dispatched))
	}
	// Stale entry should be removed.
	_, ok := qs.GetEntryDirect(task.ID)
	if ok {
		t.Error("expected stale queue entry to be removed")
	}
}
