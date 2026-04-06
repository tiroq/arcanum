package goals

import (
"testing"
)

func emptySnapshot() SystemSnapshot {
return SystemSnapshot{
QueueStats: map[string]int64{
"queued":          0,
"leased":          0,
"retry_scheduled": 0,
"failed":          0,
"dead_letter":     0,
},
}
}

func findGoalByType(gs []Goal, goalType GoalType) *Goal {
for i := range gs {
if gs[i].Type == string(goalType) {
return &gs[i]
}
}
return nil
}

func TestEvaluateSystem_EmptySystem(t *testing.T) {
snap := emptySnapshot()
gs := EvaluateSystem(snap)
if len(gs) != 0 {
t.Fatalf("expected 0 goals for empty system, got %d: %+v", len(gs), gs)
}
}

func TestEvaluateSystem_HighFailureRate(t *testing.T) {
snap := emptySnapshot()
snap.TotalJobsRecent = 100
snap.FailedJobsRecent = 30
snap.SucceededJobsRecent = 70

gs := EvaluateSystem(snap)
g := findGoalByType(gs, GoalIncreaseReliability)
if g == nil {
t.Fatal("expected increase_reliability goal, not found")
}
if g.Priority <= 0 {
t.Errorf("expected priority > 0, got %f", g.Priority)
}
if g.Confidence <= 0 {
t.Errorf("expected confidence > 0, got %f", g.Confidence)
}
if g.Evidence["failure_rate"] == nil {
t.Error("expected failure_rate in evidence")
}
}

func TestEvaluateSystem_FailureRateBelowThreshold(t *testing.T) {
snap := emptySnapshot()
snap.TotalJobsRecent = 100
snap.FailedJobsRecent = 15
snap.SucceededJobsRecent = 85

gs := EvaluateSystem(snap)
g := findGoalByType(gs, GoalIncreaseReliability)
if g != nil {
t.Fatal("expected no increase_reliability goal for 15% failure rate")
}
}

func TestEvaluateSystem_ExactlyAtFailureThreshold(t *testing.T) {
snap := emptySnapshot()
snap.TotalJobsRecent = 100
snap.FailedJobsRecent = 20
snap.SucceededJobsRecent = 80

gs := EvaluateSystem(snap)
g := findGoalByType(gs, GoalIncreaseReliability)
if g != nil {
t.Fatal("expected no increase_reliability goal at exactly 20%")
}
}

func TestEvaluateSystem_RetryBacklog(t *testing.T) {
snap := emptySnapshot()
snap.QueueStats["retry_scheduled"] = 25

gs := EvaluateSystem(snap)
g := findGoalByType(gs, GoalReduceRetryRate)
if g == nil {
t.Fatal("expected reduce_retry_rate goal, not found")
}
if g.Confidence != 0.90 {
t.Errorf("expected confidence 0.90, got %f", g.Confidence)
}
}

func TestEvaluateSystem_RetryBacklogBelowThreshold(t *testing.T) {
snap := emptySnapshot()
snap.QueueStats["retry_scheduled"] = 5

gs := EvaluateSystem(snap)
g := findGoalByType(gs, GoalReduceRetryRate)
if g != nil {
t.Fatal("expected no reduce_retry_rate goal for 5 retries")
}
}

func TestEvaluateSystem_LowAcceptanceRate(t *testing.T) {
snap := emptySnapshot()
snap.AcceptedProposals = 2
snap.RejectedProposals = 8
snap.TotalProposals = 10

gs := EvaluateSystem(snap)
g := findGoalByType(gs, GoalImproveModelQuality)
if g == nil {
t.Fatal("expected increase_model_quality goal, not found")
}
rate := g.Evidence["acceptance_rate"].(float64)
if rate < 0.19 || rate > 0.21 {
t.Errorf("expected acceptance_rate ~0.20, got %f", rate)
}
}

func TestEvaluateSystem_HealthyAcceptanceRate(t *testing.T) {
snap := emptySnapshot()
snap.AcceptedProposals = 8
snap.RejectedProposals = 2
snap.TotalProposals = 10

gs := EvaluateSystem(snap)
g := findGoalByType(gs, GoalImproveModelQuality)
if g != nil {
t.Fatal("expected no increase_model_quality goal for 80% acceptance")
}
}

func TestEvaluateSystem_NoProposals(t *testing.T) {
snap := emptySnapshot()
snap.TotalProposals = 0

gs := EvaluateSystem(snap)
g := findGoalByType(gs, GoalImproveModelQuality)
if g != nil {
t.Fatal("expected no model quality goal when no proposals exist")
}
}

func TestEvaluateSystem_QueueBacklog(t *testing.T) {
snap := emptySnapshot()
snap.QueueStats["queued"] = 100

gs := EvaluateSystem(snap)
g := findGoalByType(gs, GoalResolveBacklog)
if g == nil {
t.Fatal("expected resolve_queue_backlog goal, not found")
}
if g.Confidence != 0.95 {
t.Errorf("expected confidence 0.95, got %f", g.Confidence)
}
}

func TestEvaluateSystem_QueueBacklogBelowThreshold(t *testing.T) {
snap := emptySnapshot()
snap.QueueStats["queued"] = 30

gs := EvaluateSystem(snap)
g := findGoalByType(gs, GoalResolveBacklog)
if g != nil {
t.Fatal("expected no backlog goal for 30 queued jobs")
}
}

func TestEvaluateSystem_HighLatency(t *testing.T) {
snap := emptySnapshot()
snap.TotalJobsRecent = 50
snap.SucceededJobsRecent = 50
snap.AvgLatencyMS = 45000

gs := EvaluateSystem(snap)
g := findGoalByType(gs, GoalReduceLatency)
if g == nil {
t.Fatal("expected reduce_latency goal, not found")
}
}

func TestEvaluateSystem_LatencyBelowThreshold(t *testing.T) {
snap := emptySnapshot()
snap.AvgLatencyMS = 15000

gs := EvaluateSystem(snap)
g := findGoalByType(gs, GoalReduceLatency)
if g != nil {
t.Fatal("expected no latency goal for 15s avg")
}
}

func TestEvaluateSystem_DeadLetterRate(t *testing.T) {
snap := emptySnapshot()
snap.TotalJobsRecent = 100
snap.DeadLetterRecent = 15
snap.SucceededJobsRecent = 85

gs := EvaluateSystem(snap)
g := findGoalByType(gs, GoalInvestigateFailures)
if g == nil {
t.Fatal("expected investigate_failed_jobs goal, not found")
}
}

func TestEvaluateSystem_MultipleGoals(t *testing.T) {
snap := emptySnapshot()
snap.TotalJobsRecent = 100
snap.FailedJobsRecent = 40
snap.DeadLetterRecent = 20
snap.SucceededJobsRecent = 40
snap.QueueStats["queued"] = 100
snap.QueueStats["retry_scheduled"] = 50
snap.AcceptedProposals = 1
snap.RejectedProposals = 9
snap.TotalProposals = 10
snap.AvgLatencyMS = 60000

gs := EvaluateSystem(snap)
if len(gs) < 4 {
t.Fatalf("expected at least 4 goals, got %d", len(gs))
}

types := map[string]bool{}
for _, g := range gs {
types[g.Type] = true
}
expected := []GoalType{
GoalIncreaseReliability,
GoalReduceRetryRate,
GoalImproveModelQuality,
GoalResolveBacklog,
GoalReduceLatency,
GoalInvestigateFailures,
}
for _, e := range expected {
if !types[string(e)] {
t.Errorf("expected goal type %q, not found", e)
}
}
}

func TestEvaluateSystem_Deterministic(t *testing.T) {
snap := emptySnapshot()
snap.TotalJobsRecent = 100
snap.FailedJobsRecent = 30
snap.SucceededJobsRecent = 70
snap.QueueStats["retry_scheduled"] = 20

g1 := EvaluateSystem(snap)
g2 := EvaluateSystem(snap)

if len(g1) != len(g2) {
t.Fatalf("non-deterministic: got %d vs %d goals", len(g1), len(g2))
}
for i := range g1 {
if g1[i].Type != g2[i].Type {
t.Errorf("non-deterministic type at %d: %q vs %q", i, g1[i].Type, g2[i].Type)
}
if g1[i].Priority != g2[i].Priority {
t.Errorf("non-deterministic priority at %d: %f vs %f", i, g1[i].Priority, g2[i].Priority)
}
}
}

func TestClamp(t *testing.T) {
tests := []struct {
in, want float64
}{
{-1.0, 0.0},
{0.0, 0.0},
{0.5, 0.5},
{1.0, 1.0},
{1.5, 1.0},
}
for _, tt := range tests {
got := clamp(tt.in)
if got != tt.want {
t.Errorf("clamp(%f) = %f, want %f", tt.in, got, tt.want)
}
}
}

func TestConfidence(t *testing.T) {
tests := []struct {
sample int64
want   float64
}{
{0, 0},
{1, 0.30},
{5, 0.50},
{20, 0.70},
{50, 0.85},
{100, 0.95},
{500, 0.95},
}
for _, tt := range tests {
got := confidence(tt.sample)
if got != tt.want {
t.Errorf("confidence(%d) = %f, want %f", tt.sample, got, tt.want)
}
}
}
