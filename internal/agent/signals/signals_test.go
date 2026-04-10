package signals

import (
	"context"
	"testing"
	"time"
)

// --- Normalisation correctness (8 cases) ---

func TestNormalize_FailedJobs(t *testing.T) {
	e := RawEvent{
		ID:         "e1",
		Source:     "worker",
		EventType:  "job_failed",
		Payload:    map[string]any{"count": float64(5)},
		ObservedAt: time.Now().UTC(),
	}
	sig, ok := Normalize(e)
	if !ok {
		t.Fatal("expected normalisation")
	}
	if sig.SignalType != SignalFailedJobs {
		t.Errorf("expected signal type %s, got %s", SignalFailedJobs, sig.SignalType)
	}
	if sig.Value != 5 {
		t.Errorf("expected value 5, got %f", sig.Value)
	}
	if sig.Severity != SeverityMedium {
		t.Errorf("expected severity medium, got %s", sig.Severity)
	}
	if sig.Confidence != 0.9 {
		t.Errorf("expected confidence 0.9, got %f", sig.Confidence)
	}
	if sig.RawEventID != "e1" {
		t.Errorf("expected raw_event_id e1, got %s", sig.RawEventID)
	}
}

func TestNormalize_DeadLetterSpike(t *testing.T) {
	e := RawEvent{
		ID:         "e2",
		Source:     "bus",
		EventType:  "dead_letter",
		Payload:    map[string]any{"count": float64(25), "queue": "jobs.failed"},
		ObservedAt: time.Now().UTC(),
	}
	sig, ok := Normalize(e)
	if !ok {
		t.Fatal("expected normalisation")
	}
	if sig.SignalType != SignalDeadLetterSpike {
		t.Errorf("expected %s, got %s", SignalDeadLetterSpike, sig.SignalType)
	}
	if sig.Severity != SeverityHigh {
		t.Errorf("expected high severity for count=25, got %s", sig.Severity)
	}
	if len(sig.ContextTags) != 1 || sig.ContextTags[0] != "jobs.failed" {
		t.Errorf("expected context tag [jobs.failed], got %v", sig.ContextTags)
	}
}

func TestNormalize_PendingTasks(t *testing.T) {
	e := RawEvent{
		ID:        "e3",
		Source:    "scheduler",
		EventType: "pending_tasks",
		Payload:   map[string]any{"count": float64(15)},
		ObservedAt: time.Now().UTC(),
	}
	sig, ok := Normalize(e)
	if !ok {
		t.Fatal("expected normalisation")
	}
	if sig.SignalType != SignalPendingTasks {
		t.Errorf("expected %s, got %s", SignalPendingTasks, sig.SignalType)
	}
	if sig.Severity != SeverityMedium {
		t.Errorf("expected medium severity for count=15, got %s", sig.Severity)
	}
}

func TestNormalize_OverdueTasks(t *testing.T) {
	e := RawEvent{
		ID:        "e4",
		Source:    "scheduler",
		EventType: "overdue_tasks",
		Payload:   map[string]any{"count": float64(12)},
		ObservedAt: time.Now().UTC(),
	}
	sig, ok := Normalize(e)
	if !ok {
		t.Fatal("expected normalisation")
	}
	if sig.SignalType != SignalOverdueTasks {
		t.Errorf("expected %s, got %s", SignalOverdueTasks, sig.SignalType)
	}
	if sig.Severity != SeverityHigh {
		t.Errorf("expected high severity for count=12, got %s", sig.Severity)
	}
}

func TestNormalize_CostSpike(t *testing.T) {
	e := RawEvent{
		ID:        "e5",
		Source:    "billing",
		EventType: "cost_spike",
		Payload:   map[string]any{"amount": float64(75)},
		ObservedAt: time.Now().UTC(),
	}
	sig, ok := Normalize(e)
	if !ok {
		t.Fatal("expected normalisation")
	}
	if sig.SignalType != SignalCostSpike {
		t.Errorf("expected %s, got %s", SignalCostSpike, sig.SignalType)
	}
	if sig.Severity != SeverityMedium {
		t.Errorf("expected medium severity for amount=75, got %s", sig.Severity)
	}
}

func TestNormalize_IncomeGap(t *testing.T) {
	e := RawEvent{
		ID:        "e6",
		Source:    "income_tracker",
		EventType: "income_gap",
		Payload:   map[string]any{"gap": float64(3000)},
		ObservedAt: time.Now().UTC(),
	}
	sig, ok := Normalize(e)
	if !ok {
		t.Fatal("expected normalisation")
	}
	if sig.SignalType != SignalIncomeGap {
		t.Errorf("expected %s, got %s", SignalIncomeGap, sig.SignalType)
	}
	if sig.Severity != SeverityHigh {
		t.Errorf("expected high severity for gap=3000, got %s", sig.Severity)
	}
}

func TestNormalize_NewOpportunity(t *testing.T) {
	e := RawEvent{
		ID:        "e7",
		Source:    "scout",
		EventType: "new_opportunity",
		Payload:   map[string]any{"estimated_value": float64(2000), "opportunity_type": "consulting"},
		ObservedAt: time.Now().UTC(),
	}
	sig, ok := Normalize(e)
	if !ok {
		t.Fatal("expected normalisation")
	}
	if sig.SignalType != SignalNewOpportunity {
		t.Errorf("expected %s, got %s", SignalNewOpportunity, sig.SignalType)
	}
	if len(sig.ContextTags) != 1 || sig.ContextTags[0] != "consulting" {
		t.Errorf("expected context tag [consulting], got %v", sig.ContextTags)
	}
}

func TestNormalize_HighCognitiveLoad(t *testing.T) {
	e := RawEvent{
		ID:        "e8",
		Source:    "self_monitor",
		EventType: "high_cognitive_load",
		Payload:   map[string]any{"score": float64(0.9)},
		ObservedAt: time.Now().UTC(),
	}
	sig, ok := Normalize(e)
	if !ok {
		t.Fatal("expected normalisation")
	}
	if sig.SignalType != SignalHighCognitiveLoad {
		t.Errorf("expected %s, got %s", SignalHighCognitiveLoad, sig.SignalType)
	}
	if sig.Severity != SeverityHigh {
		t.Errorf("expected high severity for score=0.9, got %s", sig.Severity)
	}
}

func TestNormalize_UnknownEventType(t *testing.T) {
	e := RawEvent{
		ID:        "e9",
		Source:    "unknown",
		EventType: "something_random",
		Payload:   map[string]any{},
		ObservedAt: time.Now().UTC(),
	}
	_, ok := Normalize(e)
	if ok {
		t.Fatal("expected no normalisation for unknown event type")
	}
}

func TestNormalize_MissingPayloadField(t *testing.T) {
	e := RawEvent{
		ID:        "e10",
		Source:    "worker",
		EventType: "job_failed",
		Payload:   map[string]any{},
		ObservedAt: time.Now().UTC(),
	}
	sig, ok := Normalize(e)
	if !ok {
		t.Fatal("expected normalisation even with missing count")
	}
	if sig.Value != 1 {
		t.Errorf("expected default value 1, got %f", sig.Value)
	}
}

// --- Derived state correctness (5 cases) ---

func TestComputeDerivedState_Empty(t *testing.T) {
	derived := ComputeDerivedState(nil)
	for _, k := range []string{DerivedFailureRate, DerivedDeadLetterRate, DerivedOwnerLoadScore, DerivedIncomePressure, DerivedInfraCostPressure} {
		if derived[k] != 0 {
			t.Errorf("expected %s = 0 for empty signals, got %f", k, derived[k])
		}
	}
}

func TestComputeDerivedState_FailureRate(t *testing.T) {
	sigs := []Signal{
		{SignalType: SignalFailedJobs, Value: 10},
		{SignalType: SignalPendingTasks, Value: 5},
	}
	derived := ComputeDerivedState(sigs)
	// failure_rate = 10 / (2 * 10) = 0.5
	if derived[DerivedFailureRate] != 0.5 {
		t.Errorf("expected failure_rate 0.5, got %f", derived[DerivedFailureRate])
	}
}

func TestComputeDerivedState_DeadLetterRate(t *testing.T) {
	sigs := []Signal{
		{SignalType: SignalDeadLetterSpike, Value: 30},
		{SignalType: SignalFailedJobs, Value: 2},
		{SignalType: SignalPendingTasks, Value: 1},
	}
	derived := ComputeDerivedState(sigs)
	// dead_letter_rate = 30 / (3 * 10) = 1.0 (clamped)
	if derived[DerivedDeadLetterRate] != 1.0 {
		t.Errorf("expected dead_letter_rate 1.0, got %f", derived[DerivedDeadLetterRate])
	}
}

func TestComputeDerivedState_OwnerLoadScore(t *testing.T) {
	sigs := []Signal{
		{SignalType: SignalHighCognitiveLoad, Value: 0.8},
		{SignalType: SignalPendingTasks, Value: 50},
		{SignalType: SignalOverdueTasks, Value: 10},
	}
	derived := ComputeDerivedState(sigs)
	// cogLoad = clamp(0.8) = 0.8
	// pending = clamp(50/100) = 0.5
	// overdue = clamp(10/20) = 0.5
	// owner_load = (0.8 + 0.5 + 0.5) / 3 = 0.6
	if derived[DerivedOwnerLoadScore] != 0.6 {
		t.Errorf("expected owner_load_score 0.6, got %f", derived[DerivedOwnerLoadScore])
	}
}

func TestComputeDerivedState_IncomePressure(t *testing.T) {
	sigs := []Signal{
		{SignalType: SignalIncomeGap, Value: 2500},
		{SignalType: SignalIncomeGap, Value: 2500},
	}
	derived := ComputeDerivedState(sigs)
	// income_pressure = (2500+2500)/2 / 5000 = 0.5
	if derived[DerivedIncomePressure] != 0.5 {
		t.Errorf("expected income_pressure 0.5, got %f", derived[DerivedIncomePressure])
	}
}

func TestComputeDerivedState_InfraCostPressure(t *testing.T) {
	sigs := []Signal{
		{SignalType: SignalCostSpike, Value: 250},
	}
	derived := ComputeDerivedState(sigs)
	// infra_cost_pressure = 250 / 500 = 0.5
	if derived[DerivedInfraCostPressure] != 0.5 {
		t.Errorf("expected infra_cost_pressure 0.5, got %f", derived[DerivedInfraCostPressure])
	}
}

// --- Deterministic recompute ---

func TestComputeDerivedState_Deterministic(t *testing.T) {
	sigs := []Signal{
		{SignalType: SignalFailedJobs, Value: 5},
		{SignalType: SignalDeadLetterSpike, Value: 10},
		{SignalType: SignalHighCognitiveLoad, Value: 0.7},
		{SignalType: SignalIncomeGap, Value: 1000},
		{SignalType: SignalCostSpike, Value: 100},
	}

	d1 := ComputeDerivedState(sigs)
	d2 := ComputeDerivedState(sigs)

	for k, v1 := range d1 {
		v2, ok := d2[k]
		if !ok {
			t.Errorf("key %s missing in second run", k)
			continue
		}
		if v1 != v2 {
			t.Errorf("non-deterministic: %s = %f vs %f", k, v1, v2)
		}
	}
}

// --- Goal mapper ---

func TestMapSignalToGoals(t *testing.T) {
	goals := MapSignalToGoals(SignalFailedJobs)
	if len(goals) != 1 || goals[0] != "system_reliability" {
		t.Errorf("expected [system_reliability], got %v", goals)
	}

	goals = MapSignalToGoals("nonexistent")
	if goals != nil {
		t.Errorf("expected nil for unknown signal, got %v", goals)
	}
}

func TestSignalMatchesGoal(t *testing.T) {
	if !SignalMatchesGoal(SignalFailedJobs, "system_reliability") {
		t.Error("expected failed_jobs to match system_reliability")
	}
	if SignalMatchesGoal(SignalFailedJobs, "monthly_income_growth") {
		t.Error("expected failed_jobs to NOT match monthly_income_growth")
	}
}

func TestCountMatchingSignals(t *testing.T) {
	sigs := []Signal{
		{SignalType: SignalFailedJobs},
		{SignalType: SignalDeadLetterSpike},
		{SignalType: SignalIncomeGap},
	}
	count := CountMatchingSignals(sigs, "system_reliability")
	if count != 2 {
		t.Errorf("expected 2 matching signals for system_reliability, got %d", count)
	}
}

func TestMapSignalsToGoals(t *testing.T) {
	sigs := []Signal{
		{SignalType: SignalFailedJobs},
		{SignalType: SignalIncomeGap},
		{SignalType: SignalNewOpportunity},
	}
	goalMap := MapSignalsToGoals(sigs)
	if len(goalMap["system_reliability"]) != 1 {
		t.Errorf("expected 1 signal for system_reliability, got %d", len(goalMap["system_reliability"]))
	}
	if len(goalMap["monthly_income_growth"]) != 2 {
		t.Errorf("expected 2 signals for monthly_income_growth, got %d", len(goalMap["monthly_income_growth"]))
	}
}

// --- Planner integration (signals present/absent) ---

func TestGraphAdapter_Nil(t *testing.T) {
	// Nil adapter must not panic (fail-open).
	var a *GraphAdapter
	sigs, derived := a.GetActiveSignals(context.Background())
	if sigs != nil || derived != nil {
		t.Error("expected nil from nil adapter")
	}
	count := a.CountSignalsForGoal(context.Background(), "system_reliability")
	if count != 0 {
		t.Errorf("expected 0 from nil adapter, got %d", count)
	}
}

func TestGraphAdapter_NilEngine(t *testing.T) {
	a := NewGraphAdapter(nil, nil)
	sigs, derived := a.GetActiveSignals(context.Background())
	if sigs != nil || derived != nil {
		t.Error("expected nil from nil engine")
	}
	count := a.CountSignalsForGoal(context.Background(), "system_reliability")
	if count != 0 {
		t.Errorf("expected 0 from nil engine, got %d", count)
	}
}

// --- Severity mapping ---

func TestSeverityFromValue(t *testing.T) {
	tests := []struct {
		value   float64
		med     float64
		high    float64
		want    string
	}{
		{1, 3, 10, SeverityLow},
		{5, 3, 10, SeverityMedium},
		{15, 3, 10, SeverityHigh},
		{0, 0.5, 0.8, SeverityLow},
		{0.6, 0.5, 0.8, SeverityMedium},
		{0.9, 0.5, 0.8, SeverityHigh},
	}
	for _, tt := range tests {
		got := severityFromValue(tt.value, tt.med, tt.high)
		if got != tt.want {
			t.Errorf("severityFromValue(%f, %f, %f) = %s, want %s", tt.value, tt.med, tt.high, got, tt.want)
		}
	}
}

// --- Float extraction ---

func TestFloatFromPayload(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		key     string
		def     float64
		want    float64
	}{
		{"float64", map[string]any{"k": float64(42)}, "k", 0, 42},
		{"int", map[string]any{"k": 7}, "k", 0, 7},
		{"missing", map[string]any{}, "k", 99, 99},
		{"string_value", map[string]any{"k": "abc"}, "k", 5, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := floatFromPayload(tt.payload, tt.key, tt.def)
			if got != tt.want {
				t.Errorf("got %f, want %f", got, tt.want)
			}
		})
	}
}

// --- Clamp ---

func TestClamp01(t *testing.T) {
	tests := []struct {
		in   float64
		want float64
	}{
		{-0.5, 0},
		{0, 0},
		{0.5, 0.5},
		{1.0, 1.0},
		{1.5, 1},
	}
	for _, tt := range tests {
		got := clamp01(tt.in)
		if got != tt.want {
			t.Errorf("clamp01(%f) = %f, want %f", tt.in, got, tt.want)
		}
	}
}
