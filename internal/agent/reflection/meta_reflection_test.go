package reflection

import (
	"context"
	"testing"
	"time"
)

// === Mock providers for aggregation tests ===

type mockIncomeProvider struct {
	totalOutcomes   int
	successRate     float64
	avgAccuracy     float64
	estimatedIncome float64
	oppCount        int
}

func (m mockIncomeProvider) GetPerformanceStats(ctx context.Context) (int, float64, float64, float64) {
	return m.totalOutcomes, m.successRate, m.avgAccuracy, m.estimatedIncome
}
func (m mockIncomeProvider) GetOpportunityCount(ctx context.Context) int {
	return m.oppCount
}

type mockTruthProvider struct {
	verifiedIncome float64
}

func (m mockTruthProvider) GetVerifiedIncome(ctx context.Context) float64 {
	return m.verifiedIncome
}

type mockSignalProvider struct {
	derived map[string]float64
}

func (m mockSignalProvider) GetDerivedState(ctx context.Context) map[string]float64 {
	return m.derived
}

type mockCapacityProvider struct {
	ownerLoad      float64
	availableHours float64
}

func (m mockCapacityProvider) GetOwnerLoadScore(ctx context.Context) float64 {
	return m.ownerLoad
}
func (m mockCapacityProvider) GetAvailableHoursToday(ctx context.Context) float64 {
	return m.availableHours
}

type mockExtActProvider struct {
	counts map[string]int
}

func (m mockExtActProvider) GetRecentActionCounts(ctx context.Context, since time.Time) map[string]int {
	return m.counts
}

// mockAuditor records audit events for testing.
type mockAuditor struct {
	events []string
}

func (m *mockAuditor) RecordEvent(_ context.Context, _, _ string, _ interface{}, eventType, _, _ string, _ any) error {
	m.events = append(m.events, eventType)
	return nil
}

// === Aggregation Tests ===

func TestAggregation_CorrectCounts(t *testing.T) {
	agg := NewAggregator(nil).
		WithIncome(mockIncomeProvider{
			totalOutcomes:   10,
			successRate:     0.8,
			avgAccuracy:     0.9,
			estimatedIncome: 5000,
			oppCount:        5,
		})

	ctx := context.Background()
	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()

	data := agg.Aggregate(ctx, start, end)

	if data.ActionsCount != 10 {
		t.Errorf("expected 10 actions, got %d", data.ActionsCount)
	}
	if data.OpportunitiesCount != 5 {
		t.Errorf("expected 5 opportunities, got %d", data.OpportunitiesCount)
	}
	if data.SuccessRate != 0.8 {
		t.Errorf("expected 0.8 success rate, got %f", data.SuccessRate)
	}
}

func TestAggregation_VerifiedOverridesEstimated(t *testing.T) {
	agg := NewAggregator(nil).
		WithIncome(mockIncomeProvider{
			totalOutcomes:   5,
			successRate:     0.6,
			avgAccuracy:     0.7,
			estimatedIncome: 3000,
			oppCount:        3,
		}).
		WithFinancialTruth(mockTruthProvider{verifiedIncome: 4500}).
		WithCapacity(mockCapacityProvider{availableHours: 10})

	ctx := context.Background()
	data := agg.Aggregate(ctx, time.Now().Add(-24*time.Hour), time.Now())

	if data.IncomeEstimated != 3000 {
		t.Errorf("expected estimated 3000, got %f", data.IncomeEstimated)
	}
	if data.IncomeVerified != 4500 {
		t.Errorf("expected verified 4500, got %f", data.IncomeVerified)
	}
	// Value per hour should use verified income
	expectedVPH := 4500.0 / 10.0
	if data.ValuePerHour != expectedVPH {
		t.Errorf("expected value_per_hour %f, got %f", expectedVPH, data.ValuePerHour)
	}
}

func TestAggregation_NilProviders_FailOpen(t *testing.T) {
	agg := NewAggregator(nil)

	ctx := context.Background()
	data := agg.Aggregate(ctx, time.Now().Add(-1*time.Hour), time.Now())

	if data.ActionsCount != 0 {
		t.Errorf("expected 0 actions with nil providers, got %d", data.ActionsCount)
	}
	if data.IncomeVerified != 0 {
		t.Errorf("expected 0 verified income with nil providers, got %f", data.IncomeVerified)
	}
}

func TestAggregation_SignalsSummary(t *testing.T) {
	agg := NewAggregator(nil).
		WithSignals(mockSignalProvider{derived: map[string]float64{
			"failure_rate":    0.15,
			"owner_load":      0.65,
			"income_pressure": 0.3,
		}})

	ctx := context.Background()
	data := agg.Aggregate(ctx, time.Now().Add(-24*time.Hour), time.Now())

	if len(data.SignalsSummary) != 3 {
		t.Errorf("expected 3 signal entries, got %d", len(data.SignalsSummary))
	}
	if data.SignalsSummary["failure_rate"] != 0.15 {
		t.Errorf("expected failure_rate 0.15, got %f", data.SignalsSummary["failure_rate"])
	}
}

// === Analyzer Tests ===

func TestAnalyzer_LowEfficiencyDetection(t *testing.T) {
	data := AggregatedData{
		ValuePerHour:       10.0, // below threshold of 15
		ManualActionCounts: map[string]int{},
	}
	insights := MetaAnalyze(data)

	if len(insights.Inefficiencies) == 0 {
		t.Fatal("expected low efficiency inefficiency, got none")
	}
	found := false
	for _, i := range insights.Inefficiencies {
		if i.Type == "low_efficiency" {
			found = true
		}
	}
	if !found {
		t.Error("expected low_efficiency type inefficiency")
	}
}

func TestAnalyzer_NoLowEfficiency_WhenAboveThreshold(t *testing.T) {
	data := AggregatedData{
		ValuePerHour:       20.0, // above threshold of 15
		ManualActionCounts: map[string]int{},
	}
	insights := MetaAnalyze(data)

	for _, i := range insights.Inefficiencies {
		if i.Type == "low_efficiency" {
			t.Error("expected no low_efficiency when above threshold")
		}
	}
}

func TestAnalyzer_OverloadDetection(t *testing.T) {
	data := AggregatedData{
		OwnerLoadScore:     0.85, // above threshold of 0.7
		ManualActionCounts: map[string]int{},
	}
	insights := MetaAnalyze(data)

	if len(insights.RiskFlags) == 0 {
		t.Fatal("expected overload risk flag, got none")
	}
	found := false
	for _, r := range insights.RiskFlags {
		if r.Type == "overload_risk" {
			found = true
			if r.Severity <= 0 || r.Severity > 1 {
				t.Errorf("severity should be in (0,1], got %f", r.Severity)
			}
		}
	}
	if !found {
		t.Error("expected overload_risk type risk flag")
	}
}

func TestAnalyzer_NoOverload_WhenBelowThreshold(t *testing.T) {
	data := AggregatedData{
		OwnerLoadScore:     0.5, // below threshold
		ManualActionCounts: map[string]int{},
	}
	insights := MetaAnalyze(data)

	for _, r := range insights.RiskFlags {
		if r.Type == "overload_risk" {
			t.Error("expected no overload when below threshold")
		}
	}
}

func TestAnalyzer_PricingMisalignment(t *testing.T) {
	data := AggregatedData{
		AvgAccuracy:        0.5, // below threshold of 0.7
		ManualActionCounts: map[string]int{},
	}
	insights := MetaAnalyze(data)

	found := false
	for _, i := range insights.Inefficiencies {
		if i.Type == "pricing_misalignment" {
			found = true
		}
	}
	if !found {
		t.Error("expected pricing_misalignment inefficiency")
	}
}

func TestAnalyzer_NoPricingMisalignment_WhenAboveThreshold(t *testing.T) {
	data := AggregatedData{
		AvgAccuracy:        0.85,
		ManualActionCounts: map[string]int{},
	}
	insights := MetaAnalyze(data)

	for _, i := range insights.Inefficiencies {
		if i.Type == "pricing_misalignment" {
			t.Error("expected no pricing_misalignment when above threshold")
		}
	}
}

func TestAnalyzer_IncomeInstability(t *testing.T) {
	data := AggregatedData{
		ActionsCount:       10,
		SuccessRate:        0.1, // only 1 success out of 10 (below min 3)
		ManualActionCounts: map[string]int{},
	}
	insights := MetaAnalyze(data)

	found := false
	for _, r := range insights.RiskFlags {
		if r.Type == "income_instability" {
			found = true
		}
	}
	if !found {
		t.Error("expected income_instability risk flag")
	}
}

func TestAnalyzer_AutomationDetection(t *testing.T) {
	data := AggregatedData{
		ManualActionCounts: map[string]int{
			"send_invoice": 5, // above threshold of 3
			"check_email":  2, // below threshold
		},
	}
	insights := MetaAnalyze(data)

	foundAutomation := false
	for _, imp := range insights.Improvements {
		if imp.Type == "automation_opportunity" && imp.ActionType == "send_invoice" {
			foundAutomation = true
		}
	}
	if !foundAutomation {
		t.Error("expected automation_opportunity for send_invoice")
	}

	// check_email should NOT trigger
	for _, imp := range insights.Improvements {
		if imp.ActionType == "check_email" {
			t.Error("check_email should not trigger automation opportunity")
		}
	}
}

func TestAnalyzer_NoAutomation_BelowThreshold(t *testing.T) {
	data := AggregatedData{
		ManualActionCounts: map[string]int{
			"task_a": 2,
		},
	}
	insights := MetaAnalyze(data)

	for _, imp := range insights.Improvements {
		if imp.Type == "automation_opportunity" {
			t.Error("expected no automation with count below threshold")
		}
	}
}

func TestAnalyzer_SignalsBounded(t *testing.T) {
	// Extreme values — all signals should have strength in [0,1]
	data := AggregatedData{
		ValuePerHour:       0.01,
		OwnerLoadScore:     0.99,
		AvgAccuracy:        0.01,
		ActionsCount:       100,
		SuccessRate:        0.0,
		ManualActionCounts: map[string]int{"task_x": 100},
	}
	insights := MetaAnalyze(data)

	for _, sig := range insights.Signals {
		if sig.Strength < 0 || sig.Strength > 1 {
			t.Errorf("signal %s has unbounded strength %f", sig.SignalType, sig.Strength)
		}
	}
}

func TestAnalyzer_EmptyData_NoInsights(t *testing.T) {
	data := AggregatedData{
		ManualActionCounts: map[string]int{},
	}
	insights := MetaAnalyze(data)

	if len(insights.Inefficiencies) != 0 {
		t.Errorf("expected no inefficiencies for empty data, got %d", len(insights.Inefficiencies))
	}
	if len(insights.RiskFlags) != 0 {
		t.Errorf("expected no risk flags for empty data, got %d", len(insights.RiskFlags))
	}
}

// === Trigger Tests ===

func TestTrigger_TimeBased_Fires(t *testing.T) {
	trigger := NewTrigger(TriggerConfig{IntervalHours: 1})
	now := time.Now()

	// First call: never run → fires
	if !trigger.ShouldTrigger(now) {
		t.Error("expected trigger to fire on first run")
	}

	trigger.Reset(now)

	// After reset, within interval → should NOT fire
	soon := now.Add(30 * time.Minute)
	if trigger.ShouldTrigger(soon) {
		t.Error("expected trigger NOT to fire within interval")
	}

	// After interval → should fire
	later := now.Add(2 * time.Hour)
	if !trigger.ShouldTrigger(later) {
		t.Error("expected trigger to fire after interval elapsed")
	}
}

func TestTrigger_EventBased_FailureSpike(t *testing.T) {
	trigger := NewTrigger(TriggerConfig{
		IntervalHours:     0, // disable time-based
		FailureSpikeCount: 3,
	})
	// Set last run to prevent time-based trigger
	trigger.Reset(time.Now())

	now := time.Now().Add(time.Minute)

	trigger.RecordFailure()
	trigger.RecordFailure()
	if trigger.ShouldTrigger(now) {
		t.Error("expected no trigger with only 2 failures")
	}

	trigger.RecordFailure()
	if !trigger.ShouldTrigger(now) {
		t.Error("expected trigger after 3 failures")
	}
}

func TestTrigger_EventBased_Pressure(t *testing.T) {
	trigger := NewTrigger(TriggerConfig{
		IntervalHours:     0,
		PressureThreshold: 0.8,
	})
	trigger.Reset(time.Now())
	now := time.Now().Add(time.Minute)

	trigger.UpdatePressure(0.5)
	if trigger.ShouldTrigger(now) {
		t.Error("expected no trigger at 0.5 pressure")
	}

	trigger.UpdatePressure(0.9)
	if !trigger.ShouldTrigger(now) {
		t.Error("expected trigger at 0.9 pressure (> 0.8 threshold)")
	}
}

func TestTrigger_Accumulative_Fires(t *testing.T) {
	trigger := NewTrigger(TriggerConfig{
		IntervalHours:    0,
		AccumActionCount: 5,
	})
	trigger.Reset(time.Now())
	now := time.Now().Add(time.Minute)

	for i := 0; i < 4; i++ {
		trigger.RecordAction()
	}
	if trigger.ShouldTrigger(now) {
		t.Error("expected no trigger with 4 actions (threshold 5)")
	}

	trigger.RecordAction()
	if !trigger.ShouldTrigger(now) {
		t.Error("expected trigger with 5 actions")
	}
}

func TestTrigger_BelowThreshold_DoesNotFire(t *testing.T) {
	trigger := NewTrigger(TriggerConfig{
		IntervalHours:     0,
		FailureSpikeCount: 10,
		AccumActionCount:  50,
		PressureThreshold: 0.9,
	})
	trigger.Reset(time.Now())
	now := time.Now().Add(time.Minute)

	trigger.RecordAction()
	trigger.RecordFailure()
	trigger.UpdatePressure(0.1)

	if trigger.ShouldTrigger(now) {
		t.Error("expected no trigger with values well below thresholds")
	}
}

func TestTrigger_Reset_ClearsState(t *testing.T) {
	trigger := NewTrigger(TriggerConfig{AccumActionCount: 3})
	trigger.Reset(time.Now())

	trigger.RecordAction()
	trigger.RecordAction()
	trigger.RecordAction()

	trigger.Reset(time.Now())
	state := trigger.GetState()
	if state.ActionsSinceRun != 0 {
		t.Errorf("expected 0 actions after reset, got %d", state.ActionsSinceRun)
	}
}

// === Engine Tests ===

func TestEngine_FullRun_ProducesReport(t *testing.T) {
	agg := NewAggregator(nil).
		WithIncome(mockIncomeProvider{
			totalOutcomes:   20,
			successRate:     0.5,
			avgAccuracy:     0.6,
			estimatedIncome: 2000,
			oppCount:        8,
		}).
		WithCapacity(mockCapacityProvider{ownerLoad: 0.85, availableHours: 6})

	trigger := NewTrigger(TriggerConfig{IntervalHours: 0})
	engine := NewMetaEngine(agg, trigger, nil, nil, nil)

	report, err := engine.RunReflection(context.Background(), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil {
		t.Fatal("expected report, got nil")
	}
	if report.ID == "" {
		t.Error("report ID should not be empty")
	}
	if report.ActionsCount != 20 {
		t.Errorf("expected 20 actions, got %d", report.ActionsCount)
	}
	if len(report.RiskFlags) == 0 {
		t.Error("expected risk flags (overload at 0.85)")
	}
}

func TestEngine_NilEngine_FailOpen(t *testing.T) {
	var engine *MetaEngine
	report, err := engine.RunReflection(context.Background(), true)
	if err != nil {
		t.Fatalf("expected no error for nil engine, got %v", err)
	}
	if report != nil {
		t.Error("expected nil report for nil engine")
	}
}

func TestEngine_NilAggregator_FailOpen(t *testing.T) {
	engine := NewMetaEngine(nil, nil, nil, nil, nil)
	report, err := engine.RunReflection(context.Background(), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil {
		t.Fatal("expected empty report, got nil")
	}
}

func TestEngine_TriggerNotFired_ReturnsNil(t *testing.T) {
	trigger := NewTrigger(TriggerConfig{IntervalHours: 100})
	trigger.Reset(time.Now()) // Just ran

	engine := NewMetaEngine(nil, trigger, nil, nil, nil)
	report, err := engine.RunReflection(context.Background(), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report != nil {
		t.Error("expected nil report when trigger doesn't fire")
	}
}

func TestEngine_ForceOverridesTrigger(t *testing.T) {
	trigger := NewTrigger(TriggerConfig{IntervalHours: 100})
	trigger.Reset(time.Now())

	engine := NewMetaEngine(nil, trigger, nil, nil, nil)
	report, err := engine.RunReflection(context.Background(), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil {
		t.Error("expected report when force=true")
	}
}

func TestEngine_EmptySlicesNotNil(t *testing.T) {
	engine := NewMetaEngine(nil, nil, nil, nil, nil)
	report, _ := engine.RunReflection(context.Background(), true)

	if report.Inefficiencies == nil {
		t.Error("Inefficiencies should be empty slice, not nil")
	}
	if report.Improvements == nil {
		t.Error("Improvements should be empty slice, not nil")
	}
	if report.RiskFlags == nil {
		t.Error("RiskFlags should be empty slice, not nil")
	}
}

// === Adapter Tests ===

func TestAdapter_NilSafe(t *testing.T) {
	var adapter *MetaGraphAdapter
	signals, err := adapter.GetReflectionSignals(context.Background())
	if err != nil {
		t.Fatalf("expected no error for nil adapter, got %v", err)
	}
	if signals != nil {
		t.Error("expected nil signals for nil adapter")
	}

	boost := adapter.GetReflectionBoost(context.Background(), nil)
	if boost != 0 {
		t.Errorf("expected 0 boost for nil adapter, got %f", boost)
	}
}

func TestAdapter_NilEngine_Safe(t *testing.T) {
	adapter := NewMetaGraphAdapter(nil, nil)
	signals, err := adapter.GetReflectionSignals(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if signals != nil {
		t.Error("expected nil signals for nil engine")
	}
}

func TestAdapter_EmptySignals_Safe(t *testing.T) {
	engine := NewMetaEngine(nil, nil, nil, nil, nil)
	adapter := NewMetaGraphAdapter(engine, nil)

	boost := adapter.GetReflectionBoost(context.Background(), []string{"efficiency"})
	if boost != 0 {
		t.Errorf("expected 0 boost with no signals, got %f", boost)
	}
}

func TestAdapter_BoostBounded(t *testing.T) {
	agg := NewAggregator(nil).
		WithIncome(mockIncomeProvider{
			totalOutcomes: 10, successRate: 0.05, avgAccuracy: 0.1,
			estimatedIncome: 100, oppCount: 5,
		}).
		WithCapacity(mockCapacityProvider{ownerLoad: 0.99, availableHours: 1})

	engine := NewMetaEngine(agg, nil, nil, nil, nil)
	engine.RunReflection(context.Background(), true)

	adapter := NewMetaGraphAdapter(engine, nil)
	boost := adapter.GetReflectionBoost(context.Background(), nil)

	if boost > ReflectionSignalBoostMax {
		t.Errorf("boost %f exceeds max %f", boost, ReflectionSignalBoostMax)
	}
	if boost < 0 {
		t.Errorf("boost should not be negative: %f", boost)
	}
}

func TestAdapter_BoostWithTagFilter(t *testing.T) {
	agg := NewAggregator(nil).
		WithIncome(mockIncomeProvider{
			totalOutcomes: 10, successRate: 0.8, avgAccuracy: 0.9,
			estimatedIncome: 500, oppCount: 3,
		}).
		WithCapacity(mockCapacityProvider{ownerLoad: 0.85, availableHours: 5})

	engine := NewMetaEngine(agg, nil, nil, nil, nil)
	engine.RunReflection(context.Background(), true)

	adapter := NewMetaGraphAdapter(engine, nil)

	// Only capacity-related signals should contribute
	boostCapacity := adapter.GetReflectionBoost(context.Background(), []string{"capacity"})
	boostPricing := adapter.GetReflectionBoost(context.Background(), []string{"pricing"})

	// Overload at 0.85 should produce a capacity signal
	if boostCapacity == 0 {
		t.Error("expected non-zero boost for capacity tag on overloaded system")
	}
	// No pricing signal expected (accuracy is 0.9, above threshold)
	if boostPricing != 0 {
		t.Errorf("expected 0 boost for pricing tag, got %f", boostPricing)
	}
}

// === Determinism Tests ===

func TestDeterminism_SameInput_SameOutput(t *testing.T) {
	data := AggregatedData{
		ValuePerHour:       8.0,
		OwnerLoadScore:     0.8,
		AvgAccuracy:        0.5,
		ActionsCount:       10,
		SuccessRate:        0.1,
		ManualActionCounts: map[string]int{"task_a": 5},
	}

	insights1 := MetaAnalyze(data)
	insights2 := MetaAnalyze(data)

	if len(insights1.Inefficiencies) != len(insights2.Inefficiencies) {
		t.Error("determinism violated: different inefficiency counts")
	}
	if len(insights1.RiskFlags) != len(insights2.RiskFlags) {
		t.Error("determinism violated: different risk flag counts")
	}
	if len(insights1.Signals) != len(insights2.Signals) {
		t.Error("determinism violated: different signal counts")
	}
	if len(insights1.Improvements) != len(insights2.Improvements) {
		t.Error("determinism violated: different improvement counts")
	}

	for i, s1 := range insights1.Signals {
		s2 := insights2.Signals[i]
		if s1.SignalType != s2.SignalType || s1.Strength != s2.Strength {
			t.Errorf("determinism violated at signal %d: %v vs %v", i, s1, s2)
		}
	}
}

func TestDeterminism_Analyzer_NoRandomness(t *testing.T) {
	// Run 10 times with same input — all results must be identical
	data := AggregatedData{
		ValuePerHour:       12.0,
		OwnerLoadScore:     0.75,
		AvgAccuracy:        0.65,
		ActionsCount:       15,
		SuccessRate:        0.13,
		ManualActionCounts: map[string]int{"deploy": 4},
	}

	baseline := MetaAnalyze(data)
	for i := 0; i < 10; i++ {
		result := MetaAnalyze(data)
		if len(result.Signals) != len(baseline.Signals) {
			t.Fatalf("run %d: signal count mismatch", i)
		}
		for j, sig := range result.Signals {
			if sig.Strength != baseline.Signals[j].Strength {
				t.Fatalf("run %d signal %d: strength %f != %f", i, j, sig.Strength, baseline.Signals[j].Strength)
			}
		}
	}
}

// === Integration Tests ===

func TestIntegration_FullPipeline(t *testing.T) {
	agg := NewAggregator(nil).
		WithIncome(mockIncomeProvider{
			totalOutcomes: 25, successRate: 0.6, avgAccuracy: 0.5,
			estimatedIncome: 3000, oppCount: 10,
		}).
		WithFinancialTruth(mockTruthProvider{verifiedIncome: 3500}).
		WithCapacity(mockCapacityProvider{ownerLoad: 0.75, availableHours: 8}).
		WithExternalActions(mockExtActProvider{counts: map[string]int{"send_email": 4}})

	trigger := NewTrigger(DefaultTriggerConfig())
	engine := NewMetaEngine(agg, trigger, nil, nil, nil)
	adapter := NewMetaGraphAdapter(engine, nil)

	// Run reflection
	report, err := adapter.RunReflection(context.Background(), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil {
		t.Fatal("expected report, got nil")
	}

	// Should have insights
	if report.IncomeVerified != 3500 {
		t.Errorf("expected verified income 3500, got %f", report.IncomeVerified)
	}
	// Pricing misalignment (accuracy 0.5 < 0.7)
	hasPricing := false
	for _, i := range report.Inefficiencies {
		if i.Type == "pricing_misalignment" {
			hasPricing = true
		}
	}
	if !hasPricing {
		t.Error("expected pricing_misalignment in report")
	}
	// Overload (0.75 > 0.7)
	hasOverload := false
	for _, r := range report.RiskFlags {
		if r.Type == "overload_risk" {
			hasOverload = true
		}
	}
	if !hasOverload {
		t.Error("expected overload_risk in report")
	}
	// Automation (send_email = 4 >= 3)
	hasAutomation := false
	for _, imp := range report.Improvements {
		if imp.Type == "automation_opportunity" {
			hasAutomation = true
		}
	}
	if !hasAutomation {
		t.Error("expected automation_opportunity in report")
	}

	// Decision graph signals should be available
	signals, _ := adapter.GetReflectionSignals(context.Background())
	if len(signals) == 0 {
		t.Error("expected signals after reflection run")
	}

	// Boost should be non-zero for relevant tags
	boost := adapter.GetReflectionBoost(context.Background(), nil)
	if boost <= 0 {
		t.Error("expected positive boost after reflection with issues")
	}
	if boost > ReflectionSignalBoostMax {
		t.Errorf("boost %f exceeds max %f", boost, ReflectionSignalBoostMax)
	}
}

func TestIntegration_MultipleTriggerTypes(t *testing.T) {
	// Test that multiple trigger types work correctly
	cfg := TriggerConfig{
		IntervalHours:     24,
		FailureSpikeCount: 3,
		AccumActionCount:  10,
		PressureThreshold: 0.8,
	}
	trigger := NewTrigger(cfg)
	trigger.Reset(time.Now()) // mark as just ran

	now := time.Now().Add(time.Minute)

	// None should fire yet
	if trigger.ShouldTrigger(now) {
		t.Error("no trigger should fire immediately after reset")
	}

	// Failure spike triggers
	for i := 0; i < 3; i++ {
		trigger.RecordFailure()
	}
	if !trigger.ShouldTrigger(now) {
		t.Error("failure spike should trigger")
	}
}

func TestSignalTypes_Complete(t *testing.T) {
	// Verify all 5 signal types can be generated
	data := AggregatedData{
		ValuePerHour:   5.0, // low_efficiency
		OwnerLoadScore: 0.9, // overload_risk
		AvgAccuracy:    0.3, // pricing_misalignment
		ActionsCount:   10,  // income_instability (0 successes)
		SuccessRate:    0.0,
		ManualActionCounts: map[string]int{
			"manual_task": 5, // automation_opportunity
		},
	}
	insights := MetaAnalyze(data)

	signalTypes := make(map[ReflectionSignalType]bool)
	for _, s := range insights.Signals {
		signalTypes[s.SignalType] = true
	}

	expected := []ReflectionSignalType{
		SignalLowEfficiency,
		SignalOverloadRisk,
		SignalPricingMisalignment,
		SignalIncomeInstability,
		SignalAutomationOpportunity,
	}
	for _, st := range expected {
		if !signalTypes[st] {
			t.Errorf("expected signal type %s not found", st)
		}
	}
}
