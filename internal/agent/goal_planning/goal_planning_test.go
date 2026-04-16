package goal_planning

import (
"context"
"testing"
"time"

"github.com/tiroq/arcanum/internal/agent/goals"
)

// --- Decomposer Tests ---

func TestDecomposeGoal_IncomeType(t *testing.T) {
rules := DefaultDecompositionRules()
goal := goals.SystemGoal{
ID:       "monthly_income_growth",
Type:     "income",
Priority: 0.95,
Horizon:  "monthly",
}

subgoals := DecomposeGoal(goal, rules)
if len(subgoals) != 3 {
t.Fatalf("expected 3 subgoals for income, got %d", len(subgoals))
}

for i, sg := range subgoals {
expectedID := deterministicID("monthly_income_growth", i)
if sg.ID != expectedID {
t.Errorf("subgoal %d: expected ID %s, got %s", i, expectedID, sg.ID)
}
if sg.GoalID != "monthly_income_growth" {
t.Errorf("subgoal %d: wrong goal_id: %s", i, sg.GoalID)
}
if sg.Status != SubgoalNotStarted {
t.Errorf("subgoal %d: expected not_started, got %s", i, sg.Status)
}
if sg.Horizon != HorizonMonthly {
t.Errorf("subgoal %d: expected monthly horizon, got %s", i, sg.Horizon)
}
}

if subgoals[0].Priority < subgoals[1].Priority || subgoals[1].Priority < subgoals[2].Priority {
t.Error("priorities should be decreasing with offsets")
}
}

func TestDecomposeGoal_UnknownType(t *testing.T) {
rules := DefaultDecompositionRules()
goal := goals.SystemGoal{ID: "unknown", Type: "fantasy", Priority: 0.5}
subgoals := DecomposeGoal(goal, rules)
if len(subgoals) != 0 {
t.Fatalf("expected 0 subgoals for unknown type, got %d", len(subgoals))
}
}

func TestDecomposeGoal_InvalidHorizon(t *testing.T) {
rules := DefaultDecompositionRules()
goal := goals.SystemGoal{ID: "g1", Type: "safety", Priority: 1.0, Horizon: "invalid"}
subgoals := DecomposeGoal(goal, rules)
for _, sg := range subgoals {
if sg.Horizon != HorizonContinuous {
t.Errorf("invalid horizon should default to continuous, got %s", sg.Horizon)
}
}
}

func TestDecomposeGoal_OverridesTargetFromMetrics(t *testing.T) {
rules := DefaultDecompositionRules()
goal := goals.SystemGoal{
ID:       "monthly_income_growth",
Type:     "income",
Priority: 0.90,
Horizon:  "monthly",
SuccessMetrics: []goals.Metric{
{Name: "verified_monthly_income", Target: "20000"},
},
}
subgoals := DecomposeGoal(goal, rules)
if len(subgoals) < 1 {
t.Fatal("expected at least 1 subgoal")
}
if subgoals[0].TargetValue != 20000 {
t.Errorf("expected target value 20000, got %f", subgoals[0].TargetValue)
}
}

func TestDecomposeGoal_InvalidMetricTarget(t *testing.T) {
rules := DefaultDecompositionRules()
goal := goals.SystemGoal{
ID:       "monthly_income_growth",
Type:     "income",
Priority: 0.90,
Horizon:  "monthly",
SuccessMetrics: []goals.Metric{
{Name: "verified_monthly_income", Target: "not_a_number"},
},
}
subgoals := DecomposeGoal(goal, rules)
if len(subgoals) < 1 {
t.Fatal("expected at least 1 subgoal")
}
if subgoals[0].TargetValue != 13000 {
t.Errorf("expected default target 13000, got %f", subgoals[0].TargetValue)
}
}

func TestDecomposeGoal_DeterministicIDs(t *testing.T) {
rules := DefaultDecompositionRules()
goal := goals.SystemGoal{ID: "g1", Type: "safety", Priority: 1.0, Horizon: "weekly"}
run1 := DecomposeGoal(goal, rules)
run2 := DecomposeGoal(goal, rules)
if len(run1) != len(run2) {
t.Fatal("nondeterministic result count")
}
for i := range run1 {
if run1[i].ID != run2[i].ID {
t.Errorf("subgoal %d: IDs differ between runs: %s vs %s", i, run1[i].ID, run2[i].ID)
}
}
}

func TestDecomposeGoal_AllTypes(t *testing.T) {
rules := DefaultDecompositionRules()
types := []string{"safety", "income", "efficiency", "operational", "learning", "evolution"}
for _, goalType := range types {
goal := goals.SystemGoal{ID: "test_" + goalType, Type: goalType, Priority: 0.80, Horizon: "weekly"}
subgoals := DecomposeGoal(goal, rules)
if len(subgoals) == 0 {
t.Errorf("type %s produced no subgoals", goalType)
}
if len(subgoals) > MaxSubgoalsPerGoal {
t.Errorf("type %s: %d subgoals exceed max %d", goalType, len(subgoals), MaxSubgoalsPerGoal)
}
}
}

func TestDecomposeGoal_PriorityClamp(t *testing.T) {
rules := DefaultDecompositionRules()
goal := goals.SystemGoal{ID: "g1", Type: "income", Priority: 1.0, Horizon: "monthly"}
subgoals := DecomposeGoal(goal, rules)
for _, sg := range subgoals {
if sg.Priority < 0 || sg.Priority > 1 {
t.Errorf("priority out of bounds: %f", sg.Priority)
}
}
}

// --- Progress Tests ---

func TestMeasureProgress_Standard(t *testing.T) {
sg := Subgoal{TargetValue: 100, CurrentValue: 50}
p := MeasureProgress(sg)
if p != 0.5 {
t.Errorf("expected 0.5, got %f", p)
}
}

func TestMeasureProgress_Completed(t *testing.T) {
sg := Subgoal{TargetValue: 100, CurrentValue: 100}
p := MeasureProgress(sg)
if p != 1.0 {
t.Errorf("expected 1.0, got %f", p)
}
}

func TestMeasureProgress_Exceeds(t *testing.T) {
sg := Subgoal{TargetValue: 100, CurrentValue: 200}
p := MeasureProgress(sg)
if p != 1.0 {
t.Errorf("expected 1.0 (clamped), got %f", p)
}
}

func TestMeasureProgress_ZeroTarget(t *testing.T) {
sg := Subgoal{TargetValue: 0, CurrentValue: 0}
p := MeasureProgress(sg)
if p != 1.0 {
t.Errorf("expected 1.0 for zero at zero target, got %f", p)
}
}

func TestMeasureProgress_ZeroTargetWithViolations(t *testing.T) {
sg := Subgoal{TargetValue: 0, CurrentValue: 3}
p := MeasureProgress(sg)
if p < 0 || p > 1 {
t.Errorf("progress out of bounds: %f", p)
}
if p >= 1.0 {
t.Error("violations should reduce progress below 1.0")
}
}

func TestIsStale(t *testing.T) {
now := time.Now().UTC()
sg := Subgoal{UpdatedAt: now.Add(-25 * time.Hour)}
if !IsStale(sg, now) {
t.Error("25h old should be stale")
}
sgFresh := Subgoal{UpdatedAt: now.Add(-1 * time.Hour)}
if IsStale(sgFresh, now) {
t.Error("1h old should not be stale")
}
}

func TestShouldAutoComplete(t *testing.T) {
sg := Subgoal{Status: SubgoalActive, ProgressScore: 0.95}
if !ShouldAutoComplete(sg) {
t.Error("95% active subgoal should auto-complete")
}
sg2 := Subgoal{Status: SubgoalActive, ProgressScore: 0.80}
if ShouldAutoComplete(sg2) {
t.Error("80% should not auto-complete")
}
sg3 := Subgoal{Status: SubgoalNotStarted, ProgressScore: 1.0}
if ShouldAutoComplete(sg3) {
t.Error("not_started should not auto-complete")
}
}

func TestShouldBlock(t *testing.T) {
now := time.Now().UTC()
sg := Subgoal{
Status:        SubgoalActive,
ProgressScore: 0.05,
UpdatedAt:     now.Add(-25 * time.Hour),
}
if !ShouldBlock(sg, now) {
t.Error("stale + low progress active should block")
}
sgOK := Subgoal{
Status:        SubgoalActive,
ProgressScore: 0.50,
UpdatedAt:     now.Add(-25 * time.Hour),
}
if ShouldBlock(sgOK, now) {
t.Error("stale but good progress should not block")
}
}

func TestIsDependencyMet(t *testing.T) {
all := []Subgoal{
{ID: "a", Status: SubgoalCompleted},
{ID: "b", DependsOn: "a", Status: SubgoalNotStarted},
{ID: "c", DependsOn: "missing", Status: SubgoalNotStarted},
{ID: "d", Status: SubgoalNotStarted},
}
if !IsDependencyMet(all[1], all) {
t.Error("b's dependency on completed a should be met")
}
if IsDependencyMet(all[2], all) {
t.Error("c's dependency on missing should not be met")
}
if !IsDependencyMet(all[3], all) {
t.Error("d with no dependency should be met")
}
}

func TestComputeOverallProgress(t *testing.T) {
subgoals := []Subgoal{
{Priority: 1.0, ProgressScore: 0.80},
{Priority: 0.5, ProgressScore: 0.40},
}
p := ComputeOverallProgress(subgoals)
if p < 0.66 || p > 0.67 {
t.Errorf("expected ~0.667, got %f", p)
}
}

func TestComputeOverallProgress_Empty(t *testing.T) {
p := ComputeOverallProgress(nil)
if p != 0 {
t.Errorf("expected 0 for empty, got %f", p)
}
}

func TestComputeTaskUrgency(t *testing.T) {
now := time.Now().UTC()
sg := Subgoal{
Horizon:       HorizonWeekly,
CreatedAt:     now.Add(-3 * 24 * time.Hour),
ProgressScore: 0.20,
}
urgency := ComputeTaskUrgency(sg, now)
if urgency < 0 || urgency > 1 {
t.Errorf("urgency out of bounds: %f", urgency)
}
if urgency < 0.3 {
t.Errorf("expected meaningful urgency for mid-horizon low-progress, got %f", urgency)
}
}

func TestComputeTaskPriority(t *testing.T) {
priority := ComputeTaskPriority(0.70, 0.95, 0.20)
if priority < 0 || priority > 1 {
t.Errorf("priority out of bounds: %f", priority)
}
if priority < 0.75 || priority > 0.85 {
t.Errorf("expected ~0.82, got %f", priority)
}
}

// --- Planner Tests ---

func TestPlanTasks_ActiveSubgoals(t *testing.T) {
now := time.Now().UTC()
subgoals := []Subgoal{
{
ID: "s1", GoalID: "g1", Status: SubgoalActive,
ProgressScore: 0.30, Priority: 0.90,
PreferredAction: "analyze_opportunity",
Horizon: HorizonWeekly, CreatedAt: now.Add(-2 * 24 * time.Hour),
},
{
ID: "s2", GoalID: "g1", Status: SubgoalActive,
ProgressScore: 0.10, Priority: 0.80,
PreferredAction: "propose_income_action",
Horizon: HorizonMonthly, CreatedAt: now.Add(-5 * 24 * time.Hour),
},
}
emissions := PlanTasks(subgoals, now)
if len(emissions) != 2 {
t.Fatalf("expected 2 emissions, got %d", len(emissions))
}
if emissions[0].Priority < emissions[1].Priority {
t.Error("emissions should be sorted by priority descending")
}
}

func TestPlanTasks_SkipsCompleted(t *testing.T) {
now := time.Now().UTC()
subgoals := []Subgoal{
{ID: "s1", Status: SubgoalActive, ProgressScore: 0.95, Priority: 0.90, Horizon: HorizonWeekly, CreatedAt: now},
}
emissions := PlanTasks(subgoals, now)
if len(emissions) != 0 {
t.Fatalf("expected 0 emissions for completed subgoal, got %d", len(emissions))
}
}

func TestPlanTasks_SkipsNotActive(t *testing.T) {
now := time.Now().UTC()
subgoals := []Subgoal{
{ID: "s1", Status: SubgoalNotStarted, Priority: 0.90, Horizon: HorizonWeekly, CreatedAt: now},
{ID: "s2", Status: SubgoalBlocked, Priority: 0.90, Horizon: HorizonWeekly, CreatedAt: now},
{ID: "s3", Status: SubgoalCompleted, Priority: 0.90, Horizon: HorizonWeekly, CreatedAt: now},
{ID: "s4", Status: SubgoalFailed, Priority: 0.90, Horizon: HorizonWeekly, CreatedAt: now},
}
emissions := PlanTasks(subgoals, now)
if len(emissions) != 0 {
t.Fatalf("expected 0 emissions for non-active subgoals, got %d", len(emissions))
}
}

func TestPlanTasks_RespectsCooldown(t *testing.T) {
now := time.Now().UTC()
subgoals := []Subgoal{
{
ID: "s1", Status: SubgoalActive, ProgressScore: 0.30, Priority: 0.90,
Horizon: HorizonWeekly, CreatedAt: now.Add(-24 * time.Hour),
LastTaskEmitted: now.Add(-10 * time.Minute),
},
}
emissions := PlanTasks(subgoals, now)
if len(emissions) != 0 {
t.Fatalf("expected 0 emissions during cooldown, got %d", len(emissions))
}
}

func TestPlanTasks_SkipsDependencyNotMet(t *testing.T) {
now := time.Now().UTC()
subgoals := []Subgoal{
{ID: "dep", GoalID: "g1", Status: SubgoalActive, Priority: 0.90, Horizon: HorizonWeekly, CreatedAt: now},
{ID: "child", GoalID: "g1", DependsOn: "dep", Status: SubgoalActive, Priority: 0.80, Horizon: HorizonWeekly, CreatedAt: now},
}
emissions := PlanTasks(subgoals, now)
for _, e := range emissions {
if e.SubgoalID == "child" {
t.Error("child with unmet dependency should not emit")
}
}
}

// --- State Machine Tests ---

func TestValidateSubgoalTransition(t *testing.T) {
tests := []struct {
from, to SubgoalStatus
valid    bool
}{
{SubgoalNotStarted, SubgoalActive, true},
{SubgoalActive, SubgoalCompleted, true},
{SubgoalActive, SubgoalFailed, true},
{SubgoalActive, SubgoalBlocked, true},
{SubgoalBlocked, SubgoalActive, true},
{SubgoalBlocked, SubgoalFailed, true},
{SubgoalNotStarted, SubgoalCompleted, false},
{SubgoalNotStarted, SubgoalFailed, false},
{SubgoalCompleted, SubgoalActive, false},
{SubgoalFailed, SubgoalActive, false},
}
for _, tt := range tests {
got := ValidateSubgoalTransition(tt.from, tt.to)
if got != tt.valid {
t.Errorf("%s->%s: expected %v, got %v", tt.from, tt.to, tt.valid, got)
}
}
}

// --- Engine Tests ---

func TestEngine_DecomposeGoals(t *testing.T) {
ctx := context.Background()
store := NewInMemorySubgoalStore()
progress := NewInMemoryProgressStore()
engine := NewEngine(store, progress, nil, nil)

sysGoals := []goals.SystemGoal{
{ID: "g1", Type: "income", Priority: 0.95, Horizon: "monthly"},
{ID: "g2", Type: "safety", Priority: 1.0, Horizon: "daily"},
}

created, err := engine.DecomposeGoals(ctx, sysGoals)
if err != nil {
t.Fatal(err)
}
if created != 5 {
t.Errorf("expected 5 created, got %d", created)
}

created2, err := engine.DecomposeGoals(ctx, sysGoals)
if err != nil {
t.Fatal(err)
}
if created2 != 0 {
t.Errorf("expected 0 on second decompose, got %d", created2)
}
}

func TestEngine_ActivateSubgoals(t *testing.T) {
ctx := context.Background()
store := NewInMemorySubgoalStore()
progress := NewInMemoryProgressStore()
engine := NewEngine(store, progress, nil, nil)

for i := 0; i < 3; i++ {
_ = store.Insert(ctx, Subgoal{
ID: deterministicID("g1", i), GoalID: "g1",
Status: SubgoalNotStarted, Priority: 0.90 - float64(i)*0.05,
Horizon: HorizonWeekly,
})
}

activated, err := engine.ActivateSubgoals(ctx)
if err != nil {
t.Fatal(err)
}
if activated != 3 {
t.Errorf("expected 3 activated, got %d", activated)
}

all, _ := store.ListAll(ctx)
for _, sg := range all {
if sg.Status != SubgoalActive {
t.Errorf("subgoal %s should be active, got %s", sg.ID, sg.Status)
}
}
}

func TestEngine_ActivateSubgoals_RespectsMaxActive(t *testing.T) {
ctx := context.Background()
store := NewInMemorySubgoalStore()
progress := NewInMemoryProgressStore()
engine := NewEngine(store, progress, nil, nil)

for i := 0; i < MaxActiveSubgoals+5; i++ {
_ = store.Insert(ctx, Subgoal{
ID: deterministicID("g1", i), GoalID: "g1",
Status: SubgoalNotStarted, Priority: 0.90, Horizon: HorizonWeekly,
})
}

activated, err := engine.ActivateSubgoals(ctx)
if err != nil {
t.Fatal(err)
}
if activated > MaxActiveSubgoals {
t.Errorf("activated %d exceeds max %d", activated, MaxActiveSubgoals)
}
}

func TestEngine_ActivateSubgoals_DependencyBlocking(t *testing.T) {
ctx := context.Background()
store := NewInMemorySubgoalStore()
progress := NewInMemoryProgressStore()
engine := NewEngine(store, progress, nil, nil)

_ = store.Insert(ctx, Subgoal{ID: "parent", GoalID: "g1", Status: SubgoalNotStarted})
_ = store.Insert(ctx, Subgoal{ID: "child", GoalID: "g1", DependsOn: "parent", Status: SubgoalNotStarted})

activated, err := engine.ActivateSubgoals(ctx)
if err != nil {
t.Fatal(err)
}
_ = activated

parentSG, _ := store.Get(ctx, "parent")
if parentSG.Status != SubgoalActive {
t.Error("parent should be active")
}
childSG, _ := store.Get(ctx, "child")
if childSG.Status == SubgoalActive {
t.Error("child should NOT be active (parent not completed)")
}
}

func TestEngine_UpdateProgress_AutoComplete(t *testing.T) {
ctx := context.Background()
store := NewInMemorySubgoalStore()
progress := NewInMemoryProgressStore()
engine := NewEngine(store, progress, nil, nil)

_ = store.Insert(ctx, Subgoal{
ID: "s1", GoalID: "g1", Status: SubgoalActive,
TargetValue: 100, CurrentValue: 95, UpdatedAt: time.Now().UTC(),
})

err := engine.UpdateProgress(ctx)
if err != nil {
t.Fatal(err)
}

sg, _ := store.Get(ctx, "s1")
if sg.Status != SubgoalCompleted {
t.Errorf("expected completed, got %s", sg.Status)
}
}

func TestEngine_UpdateProgress_Block(t *testing.T) {
ctx := context.Background()
store := NewInMemorySubgoalStore()
progress := NewInMemoryProgressStore()
engine := NewEngine(store, progress, nil, nil)

_ = store.Insert(ctx, Subgoal{
ID: "s1", GoalID: "g1", Status: SubgoalActive,
TargetValue: 100, CurrentValue: 5,
UpdatedAt: time.Now().UTC().Add(-30 * time.Hour),
})

err := engine.UpdateProgress(ctx)
if err != nil {
t.Fatal(err)
}

sg, _ := store.Get(ctx, "s1")
if sg.Status != SubgoalBlocked {
t.Errorf("expected blocked, got %s", sg.Status)
}
}

func TestEngine_PlanAndEmitTasks_DryRun(t *testing.T) {
ctx := context.Background()
store := NewInMemorySubgoalStore()
progress := NewInMemoryProgressStore()
engine := NewEngine(store, progress, nil, nil)

_ = store.Insert(ctx, Subgoal{
ID: "s1", GoalID: "g1", Status: SubgoalActive,
ProgressScore: 0.30, Priority: 0.90,
PreferredAction: "analyze_opportunity",
Horizon: HorizonWeekly, CreatedAt: time.Now().UTC().Add(-48 * time.Hour),
})

emitted, err := engine.PlanAndEmitTasks(ctx)
if err != nil {
t.Fatal(err)
}
if emitted != 1 {
t.Errorf("expected 1 emission (dry run), got %d", emitted)
}
}

type mockEmitter struct {
emitted []TaskEmission
}

func (m *mockEmitter) EmitTask(subgoalID, goalID, actionType string, urgency, expectedValue, riskLevel float64, strategyType string) error {
m.emitted = append(m.emitted, TaskEmission{
SubgoalID: subgoalID, GoalID: goalID, ActionType: actionType,
Urgency: urgency, ExpectedValue: expectedValue,
RiskLevel: riskLevel, StrategyType: strategyType,
})
return nil
}

func TestEngine_PlanAndEmitTasks_WithEmitter(t *testing.T) {
ctx := context.Background()
store := NewInMemorySubgoalStore()
progress := NewInMemoryProgressStore()
emitter := &mockEmitter{}
engine := NewEngine(store, progress, nil, nil).WithEmitter(emitter)

now := time.Now().UTC()
_ = store.Insert(ctx, Subgoal{
ID: "s1", GoalID: "g1", Status: SubgoalActive,
ProgressScore: 0.30, Priority: 0.90,
PreferredAction: "analyze_opportunity",
Horizon: HorizonWeekly, CreatedAt: now.Add(-48 * time.Hour),
})

emitted, err := engine.PlanAndEmitTasks(ctx)
if err != nil {
t.Fatal(err)
}
if emitted != 1 {
t.Fatalf("expected 1 emitted, got %d", emitted)
}
if len(emitter.emitted) != 1 {
t.Fatalf("expected 1 in mock emitter, got %d", len(emitter.emitted))
}
if emitter.emitted[0].SubgoalID != "s1" {
t.Errorf("wrong subgoal emitted: %s", emitter.emitted[0].SubgoalID)
}

sg, _ := store.Get(ctx, "s1")
if sg.LastTaskEmitted.IsZero() {
t.Error("last_task_emitted should be set after emission")
}
}

func TestEngine_RunCycle(t *testing.T) {
ctx := context.Background()
store := NewInMemorySubgoalStore()
progress := NewInMemoryProgressStore()
emitter := &mockEmitter{}
engine := NewEngine(store, progress, nil, nil).WithEmitter(emitter)

sysGoals := []goals.SystemGoal{
{ID: "g1", Type: "income", Priority: 0.95, Horizon: "monthly"},
}

err := engine.RunCycle(ctx, sysGoals)
if err != nil {
t.Fatal(err)
}

all, _ := store.ListAll(ctx)
if len(all) != 3 {
t.Fatalf("expected 3 subgoals, got %d", len(all))
}

activeCount := 0
for _, sg := range all {
if sg.Status == SubgoalActive {
activeCount++
}
}
if activeCount != 3 {
t.Errorf("expected 3 active, got %d", activeCount)
}

if len(emitter.emitted) == 0 {
t.Error("expected at least one task emission")
}
}

func TestEngine_TransitionSubgoal(t *testing.T) {
ctx := context.Background()
store := NewInMemorySubgoalStore()
progress := NewInMemoryProgressStore()
engine := NewEngine(store, progress, nil, nil)

_ = store.Insert(ctx, Subgoal{ID: "s1", GoalID: "g1", Status: SubgoalActive})

err := engine.TransitionSubgoal(ctx, "s1", SubgoalCompleted, "done")
if err != nil {
t.Fatal(err)
}
sg, _ := store.Get(ctx, "s1")
if sg.Status != SubgoalCompleted {
t.Errorf("expected completed, got %s", sg.Status)
}
}

func TestEngine_TransitionSubgoal_Invalid(t *testing.T) {
ctx := context.Background()
store := NewInMemorySubgoalStore()
progress := NewInMemoryProgressStore()
engine := NewEngine(store, progress, nil, nil)

_ = store.Insert(ctx, Subgoal{ID: "s1", GoalID: "g1", Status: SubgoalCompleted})

err := engine.TransitionSubgoal(ctx, "s1", SubgoalActive, "reopen")
if err == nil {
t.Error("expected error for invalid transition")
}
}

func TestEngine_GetPlanSummary(t *testing.T) {
ctx := context.Background()
store := NewInMemorySubgoalStore()
progress := NewInMemoryProgressStore()
engine := NewEngine(store, progress, nil, nil)

_ = store.Insert(ctx, Subgoal{ID: "s1", GoalID: "g1", Status: SubgoalActive, Priority: 0.9, ProgressScore: 0.50})
_ = store.Insert(ctx, Subgoal{ID: "s2", GoalID: "g1", Status: SubgoalCompleted, Priority: 0.8, ProgressScore: 1.0})
_ = store.Insert(ctx, Subgoal{ID: "s3", GoalID: "g1", Status: SubgoalBlocked, Priority: 0.7, ProgressScore: 0.05})

summary, err := engine.GetPlanSummary(ctx, "g1", "income", 0.95, "monthly")
if err != nil {
t.Fatal(err)
}
if summary.TotalSubgoals != 3 {
t.Errorf("expected 3 total, got %d", summary.TotalSubgoals)
}
if summary.ActiveSubgoals != 1 {
t.Errorf("expected 1 active, got %d", summary.ActiveSubgoals)
}
if summary.CompletedSubgoals != 1 {
t.Errorf("expected 1 completed, got %d", summary.CompletedSubgoals)
}
if summary.BlockedSubgoals != 1 {
t.Errorf("expected 1 blocked, got %d", summary.BlockedSubgoals)
}
if summary.OverallProgress <= 0 {
t.Error("overall progress should be > 0")
}
}

// --- Adapter Tests ---

func TestGraphAdapter_NilSafe(t *testing.T) {
var adapter *GraphAdapter
ctx := context.Background()

sg := adapter.GetSubgoal(ctx, "any")
if sg.ID != "" {
t.Error("nil adapter should return zero subgoal")
}
sgs := adapter.ListSubgoals(ctx, "any")
if sgs != nil {
t.Error("nil adapter should return nil list")
}
all := adapter.ListAllSubgoals(ctx)
if all != nil {
t.Error("nil adapter should return nil all list")
}
p := adapter.GetOverallProgress(ctx, "any")
if p != 0 {
t.Error("nil adapter should return 0 progress")
}
}

func TestGraphAdapter_WithEngine(t *testing.T) {
ctx := context.Background()
store := NewInMemorySubgoalStore()
progress := NewInMemoryProgressStore()
engine := NewEngine(store, progress, nil, nil)
adapter := NewGraphAdapter(engine, nil)

_ = store.Insert(ctx, Subgoal{
ID: "s1", GoalID: "g1", Status: SubgoalActive,
Priority: 0.90, ProgressScore: 0.50,
})

sg := adapter.GetSubgoal(ctx, "s1")
if sg.ID != "s1" {
t.Error("adapter should return subgoal")
}
sgs := adapter.ListSubgoals(ctx, "g1")
if len(sgs) != 1 {
t.Errorf("expected 1, got %d", len(sgs))
}
all := adapter.ListAllSubgoals(ctx)
if len(all) != 1 {
t.Errorf("expected 1, got %d", len(all))
}
p := adapter.GetOverallProgress(ctx, "g1")
if p != 0.50 {
t.Errorf("expected 0.50, got %f", p)
}
}

// --- DefaultDecompositionRules Sanity ---

func TestDefaultDecompositionRules_Coverage(t *testing.T) {
rules := DefaultDecompositionRules()
expectedTypes := []string{"safety", "income", "efficiency", "operational", "learning", "evolution"}
for _, typ := range expectedTypes {
templates, ok := rules[typ]
if !ok {
t.Errorf("missing rules for type %s", typ)
continue
}
if len(templates) == 0 {
t.Errorf("empty templates for type %s", typ)
}
for i, tmpl := range templates {
if tmpl.TitlePattern == "" {
t.Errorf("type %s template %d: empty title", typ, i)
}
if tmpl.TargetMetric == "" {
t.Errorf("type %s template %d: empty target metric", typ, i)
}
if tmpl.PreferredAction == "" {
t.Errorf("type %s template %d: empty preferred action", typ, i)
}
}
}
}

// --- Clamp tests ---

func TestClamp01(t *testing.T) {
tests := []struct{ in, out float64 }{
{-0.5, 0}, {0, 0}, {0.5, 0.5}, {1.0, 1.0}, {1.5, 1.0},
}
for _, tt := range tests {
got := clamp01(tt.in)
if got != tt.out {
t.Errorf("clamp01(%f) = %f, want %f", tt.in, got, tt.out)
}
}
}
