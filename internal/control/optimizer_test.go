package control

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/config"
)

// ──────────────────────────────────────────────────────────────────────────────
// Stub store
// ──────────────────────────────────────────────────────────────────────────────

type stubOptimizerStore struct {
	metrics []RunMetrics
	err     error
}

func (s *stubOptimizerStore) QueryRunMetrics(_ context.Context, _ time.Time) ([]RunMetrics, error) {
	return s.metrics, s.err
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// baseMetrics builds RunMetrics with proxy signals (no real fallback or
// acceptance signal) — backwards-compatible with legacy data.
func baseMetrics(provider, role string, total, success, failure, accepted int64,
	avgTokens, avgLatencyMS float64) RunMetrics {
	m := RunMetrics{
		Provider:         provider,
		ModelRole:        role,
		TotalRuns:        total,
		SuccessRuns:      success,
		FailureRuns:      failure,
		AcceptedRuns:     accepted,
		AvgTokensTotal:   avgTokens,
		AvgLatencyMS:     avgLatencyMS,
		AvgAttemptNumber: 1.0,
		Quality: DataQuality{
			HasRealFallbackSignal:   false,
			HasRealAcceptanceSignal: false,
			SampleSize:              total,
		},
	}
	if total > 0 {
		m.FailureRate = float64(failure) / float64(total)
		m.AcceptanceRate = float64(accepted) / float64(total)
		// Proxy: fallback mirrors failure when no real signal.
		m.FallbackRuns = failure
		m.FallbackRate = m.FailureRate
	}
	return m
}

// realSignalMetrics builds RunMetrics with real fallback + acceptance signals.
func realSignalMetrics(provider, role string,
	total, success, failure, accepted, rejected, fallbackRuns int64,
	avgTokens, avgLatencyMS, avgAttempt float64) RunMetrics {
	m := RunMetrics{
		Provider:         provider,
		ModelRole:        role,
		TotalRuns:        total,
		SuccessRuns:      success,
		FailureRuns:      failure,
		AcceptedRuns:     accepted,
		RejectedRuns:     rejected,
		FallbackRuns:     fallbackRuns,
		AvgTokensTotal:   avgTokens,
		AvgLatencyMS:     avgLatencyMS,
		AvgAttemptNumber: avgAttempt,
		Quality: DataQuality{
			HasRealFallbackSignal:   true,
			HasRealAcceptanceSignal: true,
			SampleSize:              total,
		},
	}
	if total > 0 {
		m.FailureRate = float64(failure) / float64(total)
		m.AcceptanceRate = float64(accepted) / float64(total)
		m.RejectionRate = float64(rejected) / float64(total)
		m.FallbackRate = float64(fallbackRuns) / float64(total)
	}
	return m
}

func newTestOptimizer(store OptimizerStore) *Optimizer {
	return NewOptimizer(store, config.RoutingPolicyConfig{
		FastEscalation:    "local_only",
		DefaultEscalation: "local_only",
		PlannerEscalation: "local_cloud",
		ReviewEscalation:  "local_cloud",
	}, zap.NewNop())
}

// ──────────────────────────────────────────────────────────────────────────────
// AnalyzeAndRecommend integration path
// ──────────────────────────────────────────────────────────────────────────────

func TestOptimizer_AnalyzeAndRecommend_Empty(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{metrics: nil})
	recs, err := opt.AnalyzeAndRecommend(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("expected 0 recommendations for empty dataset, got %d", len(recs))
	}
}

func TestOptimizer_AnalyzeAndRecommend_StoreError(t *testing.T) {
	storeErr := errors.New("db unavailable")
	opt := newTestOptimizer(&stubOptimizerStore{err: storeErr})
	_, err := opt.AnalyzeAndRecommend(context.Background())
	if err == nil {
		t.Fatal("expected error when store returns error")
	}
}

func TestOptimizer_AnalyzeAndRecommend_ReturnsOnePerPair(t *testing.T) {
	metrics := []RunMetrics{
		baseMetrics("ollama", "fast", 20, 18, 2, 16, 300, 5000),
		baseMetrics("openai", "planner", 10, 9, 1, 8, 800, 15000),
	}
	opt := newTestOptimizer(&stubOptimizerStore{metrics: metrics})
	recs, err := opt.AnalyzeAndRecommend(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 2 {
		t.Errorf("expected 2 recommendations, got %d", len(recs))
	}
}

func TestOptimizer_AnalyzeAndRecommend_DataQualityPropagated(t *testing.T) {
	m := realSignalMetrics("ollama", "fast", 20, 18, 2, 15, 3, 2, 300, 5000, 1.1)
	opt := newTestOptimizer(&stubOptimizerStore{metrics: []RunMetrics{m}})
	recs, err := opt.AnalyzeAndRecommend(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(recs))
	}
	dq := recs[0].DataQuality
	if !dq.HasRealFallbackSignal {
		t.Error("expected HasRealFallbackSignal=true to be propagated to recommendation")
	}
	if !dq.HasRealAcceptanceSignal {
		t.Error("expected HasRealAcceptanceSignal=true to be propagated to recommendation")
	}
	if dq.SampleSize != 20 {
		t.Errorf("expected SampleSize=20, got %d", dq.SampleSize)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// recommend() — determinism and correctness
// ──────────────────────────────────────────────────────────────────────────────

func TestOptimizer_Recommend_InsufficientSample(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	// 4 runs < thresholdMinSampleSize (5)
	m := baseMetrics("ollama", "fast", 4, 4, 0, 4, 200, 3000)
	rec := opt.recommend(m)
	if rec.Action != ActionKeep {
		t.Errorf("want ActionKeep for small sample, got %q", rec.Action)
	}
	if rec.CurrentPolicy != "local_only" {
		t.Errorf("want current_policy=local_only, got %q", rec.CurrentPolicy)
	}
	if !strings.Contains(rec.Explanation, "Insufficient") {
		t.Errorf("explanation should mention 'Insufficient', got: %q", rec.Explanation)
	}
}

func TestOptimizer_Recommend_InsufficientSample_ReportsSignalQuality(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	m := baseMetrics("ollama", "fast", 3, 3, 0, 3, 200, 3000)
	rec := opt.recommend(m)
	// Explanation should contain signal quality labels.
	if !strings.Contains(rec.Explanation, "acceptance_signal=") {
		t.Errorf("expected signal quality labels in explanation, got: %q", rec.Explanation)
	}
}

func TestOptimizer_Recommend_RemoveProvider(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	// Exactly 50 % failure rate → remove
	m := baseMetrics("ollama", "default", 10, 5, 5, 3, 400, 8000)
	rec := opt.recommend(m)
	if rec.Action != ActionRemoveProvider {
		t.Errorf("want ActionRemoveProvider for ≥50%% failure, got %q", rec.Action)
	}
	if rec.Metrics.FailureRate < thresholdRemoveFailureRate {
		t.Errorf("metrics.failure_rate should be >= %.2f, got %.2f",
			thresholdRemoveFailureRate, rec.Metrics.FailureRate)
	}
}

func TestOptimizer_Recommend_RemoveProvider_AboveThreshold(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	// 70 % failure rate — well above threshold
	m := baseMetrics("ollama", "planner", 10, 3, 7, 1, 600, 12000)
	rec := opt.recommend(m)
	if rec.Action != ActionRemoveProvider {
		t.Errorf("want ActionRemoveProvider for 70%% failure, got %q", rec.Action)
	}
	if rec.Provider != "ollama" || rec.Role != "planner" {
		t.Errorf("unexpected provider/role in recommendation: %q/%q", rec.Provider, rec.Role)
	}
}

func TestOptimizer_Recommend_IncreaseEscalation_HighFailure(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	// 30 % failure rate — between 20 % and 50 % thresholds
	m := baseMetrics("ollama", "fast", 10, 7, 3, 6, 350, 6000)
	rec := opt.recommend(m)
	if rec.Action != ActionIncreaseEscalation {
		t.Errorf("want ActionIncreaseEscalation for 30%% failure, got %q", rec.Action)
	}
}

func TestOptimizer_Recommend_IncreaseEscalation_HighFallbackRate_RealSignal(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	// Low hard-failure, but 4/10 runs used fallback model (real signal)
	m := realSignalMetrics("ollama", "fast", 10, 9, 1, 7, 1, 4, 350, 6000, 1.0)
	// fallback_rate = 4/10 = 40 % > thresholdEscalateFallbackRate (30 %)
	rec := opt.recommend(m)
	if rec.Action != ActionIncreaseEscalation {
		t.Errorf("want ActionIncreaseEscalation for high real fallback rate, got %q", rec.Action)
	}
	if !strings.Contains(rec.Explanation, "measured") {
		t.Errorf("explanation should say 'measured' for real fallback signal, got: %q", rec.Explanation)
	}
}

func TestOptimizer_Recommend_IncreaseEscalation_HighFallbackRate_ProxySignal(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	// No real fallback signal — failure_rate=0.3 acts as proxy fallback
	m := baseMetrics("ollama", "fast", 10, 7, 3, 6, 350, 6000)
	// proxy: fallback_rate = failure_rate = 0.3, which is exactly >= 0.30
	// but failure_rate 0.3 ≥ 0.20 → escalate_failure fires first
	rec := opt.recommend(m)
	if rec.Action != ActionIncreaseEscalation {
		t.Errorf("want ActionIncreaseEscalation, got %q", rec.Action)
	}
}

func TestOptimizer_Recommend_IncreaseEscalation_LowAcceptance_RealSignal(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	// Low failure, real accepted/rejected signal, low acceptance rate (20 %)
	m := realSignalMetrics("ollama", "default", 10, 9, 1, 2, 5, 1, 400, 7000, 1.0)
	rec := opt.recommend(m)
	if rec.Action != ActionIncreaseEscalation {
		t.Errorf("want ActionIncreaseEscalation for low real acceptance (20%%), got %q", rec.Action)
	}
	if !strings.Contains(rec.Explanation, "real acceptance rate") {
		t.Errorf("explanation should mention 'real acceptance rate', got: %q", rec.Explanation)
	}
}

func TestOptimizer_Recommend_LowAcceptance_NoEscalateWithoutRealSignal(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	// Low acceptance but NO real signal — proposals may still be pending.
	// Low fake acceptance alone should NOT trigger escalation via acceptance arm.
	m := baseMetrics("ollama", "default", 10, 9, 1, 2, 400, 7000) // proxy signal
	// failure_rate=0.1, fallback_rate=0.1 — both below escalation thresholds.
	// acceptance_rate=0.2 but no real signal → should NOT escalate via acceptance arm.
	rec := opt.recommend(m)
	// The failure_rate=0.1 and fallback_rate=0.1 are both below their thresholds.
	// Without real acceptance signal, the acceptance arm is guarded → keep or other.
	if rec.Action == ActionIncreaseEscalation {
		if strings.Contains(rec.Explanation, "real acceptance rate") {
			t.Error("should not use acceptance arm without real acceptance signal")
		}
	}
}

func TestOptimizer_Recommend_ReduceEscalation_HighLatency(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	// Low failure, high acceptance, but avg latency > 30 s, low fallback
	m := realSignalMetrics("openai", "review", 20, 19, 1, 15, 2, 1, 900, 35_000, 1.0)
	rec := opt.recommend(m)
	if rec.Action != ActionReduceEscalation {
		t.Errorf("want ActionReduceEscalation for high latency + healthy metrics, got %q", rec.Action)
	}
}

func TestOptimizer_Recommend_ReduceEscalation_NotTriggeredWithHighFailure(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	// High latency but also high failure → escalation wins over reduction
	m := baseMetrics("openai", "review", 10, 7, 3, 5, 900, 40_000)
	rec := opt.recommend(m)
	// failure_rate = 0.3 → increase_escalation takes priority over reduce
	if rec.Action == ActionReduceEscalation {
		t.Errorf("should not ActionReduceEscalation when failure rate is also high")
	}
}

func TestOptimizer_Recommend_ReduceEscalation_NotTriggeredWithHighRealFallback(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	// High latency but high real fallback rate → escalation needed, not reduction
	m := realSignalMetrics("openai", "review", 10, 9, 1, 8, 1, 4, 900, 40_000, 1.0)
	// fallback_rate = 4/10 = 40 % > thresholdEscalateFallbackRate → escalate
	rec := opt.recommend(m)
	if rec.Action == ActionReduceEscalation {
		t.Errorf("should not ActionReduceEscalation when real fallback rate is high")
	}
}

func TestOptimizer_Recommend_Keep_GoodMetrics(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	// All metrics within healthy range
	m := baseMetrics("ollama", "fast", 50, 48, 2, 40, 300, 5000)
	rec := opt.recommend(m)
	if rec.Action != ActionKeep {
		t.Errorf("want ActionKeep for healthy metrics, got %q", rec.Action)
	}
}

func TestOptimizer_Recommend_Keep_ExplainationMentionsFallbackLabel(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	m := baseMetrics("ollama", "fast", 50, 48, 2, 40, 300, 5000)
	rec := opt.recommend(m)
	// Proxy signal → explanation should say "inferred"
	if !strings.Contains(rec.Explanation, "inferred") {
		t.Errorf("keep explanation should say 'inferred' for proxy signal, got: %q", rec.Explanation)
	}
}

func TestOptimizer_Recommend_Keep_RealSignalExplanationSaysMeasured(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	m := realSignalMetrics("ollama", "fast", 50, 48, 2, 40, 5, 2, 300, 5000, 1.0)
	rec := opt.recommend(m)
	if rec.Action != ActionKeep {
		t.Errorf("want ActionKeep, got %q", rec.Action)
	}
	if !strings.Contains(rec.Explanation, "measured") {
		t.Errorf("keep explanation with real signal should say 'measured', got: %q", rec.Explanation)
	}
}

func TestOptimizer_Recommend_Deterministic(t *testing.T) {
	// Calling recommend twice with identical input must produce identical output.
	opt := newTestOptimizer(&stubOptimizerStore{})
	m := baseMetrics("ollama", "planner", 20, 16, 4, 12, 600, 18_000)
	r1 := opt.recommend(m)
	r2 := opt.recommend(m)
	if r1.Action != r2.Action {
		t.Errorf("recommend is non-deterministic: %q vs %q", r1.Action, r2.Action)
	}
	if r1.Explanation != r2.Explanation {
		t.Error("recommend explanation differs between identical calls")
	}
}

func TestOptimizer_Recommend_Deterministic_RealSignals(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	m := realSignalMetrics("openai", "review", 30, 27, 3, 22, 4, 3, 800, 20_000, 1.2)
	r1 := opt.recommend(m)
	r2 := opt.recommend(m)
	if r1.Action != r2.Action {
		t.Errorf("recommend (real signals) is non-deterministic: %q vs %q", r1.Action, r2.Action)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// policyFor tests
// ──────────────────────────────────────────────────────────────────────────────

func TestOptimizer_PolicyFor(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	cases := []struct {
		role   string
		expect string
	}{
		{"fast", "local_only"},
		{"default", "local_only"},
		{"planner", "local_cloud"},
		{"review", "local_cloud"},
		{"unknown_role", "unknown"},
	}
	for _, tc := range cases {
		got := opt.policyFor(tc.role)
		if got != tc.expect {
			t.Errorf("policyFor(%q): want %q, got %q", tc.role, tc.expect, got)
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// RunMetrics field correctness
// ──────────────────────────────────────────────────────────────────────────────

func TestRunMetrics_ProxySignalRates(t *testing.T) {
	m := baseMetrics("ollama", "fast", 10, 7, 3, 5, 400, 8000)
	if got := m.FailureRate; got != 0.3 {
		t.Errorf("failure_rate: want 0.3, got %v", got)
	}
	if got := m.AcceptanceRate; got != 0.5 {
		t.Errorf("acceptance_rate: want 0.5, got %v", got)
	}
	// Proxy: fallback mirrors failure.
	if got := m.FallbackRate; got != m.FailureRate {
		t.Errorf("proxy fallback_rate should equal failure_rate, got %v vs %v", got, m.FailureRate)
	}
	if m.Quality.HasRealFallbackSignal {
		t.Error("proxy metrics should have HasRealFallbackSignal=false")
	}
}

func TestRunMetrics_RealSignalRates(t *testing.T) {
	m := realSignalMetrics("ollama", "fast", 10, 8, 2, 6, 2, 3, 400, 8000, 1.5)
	if got := m.FallbackRate; got != 0.3 {
		t.Errorf("real fallback_rate: want 0.3, got %v", got)
	}
	if got := m.RejectionRate; got != 0.2 {
		t.Errorf("rejection_rate: want 0.2, got %v", got)
	}
	if !m.Quality.HasRealFallbackSignal {
		t.Error("real metrics should have HasRealFallbackSignal=true")
	}
	if !m.Quality.HasRealAcceptanceSignal {
		t.Error("real metrics should have HasRealAcceptanceSignal=true")
	}
}

func TestRunMetrics_ZeroRatesForEmptyDataset(t *testing.T) {
	m := baseMetrics("ollama", "fast", 0, 0, 0, 0, 0, 0)
	if m.FailureRate != 0 || m.AcceptanceRate != 0 || m.FallbackRate != 0 {
		t.Error("all rates should be zero when total_runs is zero")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Boundary condition tests
// ──────────────────────────────────────────────────────────────────────────────

func TestOptimizer_Recommend_ExactlyAtRemoveThreshold(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	// Exactly 50 % failure: 5 of 10
	m := baseMetrics("ollama", "default", 10, 5, 5, 2, 400, 7000)
	rec := opt.recommend(m)
	if rec.Action != ActionRemoveProvider {
		t.Errorf("exactly at remove threshold (50%%): want ActionRemoveProvider, got %q", rec.Action)
	}
}

func TestOptimizer_Recommend_JustBelowRemoveThreshold(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	// 4 of 10 fail = 40 % — above escalate but below remove
	m := baseMetrics("ollama", "default", 10, 6, 4, 5, 400, 7000)
	rec := opt.recommend(m)
	if rec.Action != ActionIncreaseEscalation {
		t.Errorf("just below remove threshold: want ActionIncreaseEscalation, got %q", rec.Action)
	}
}

func TestOptimizer_Recommend_ExactlyAtMinSample(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	// Exactly thresholdMinSampleSize (5) with high failure — should recommend
	m := baseMetrics("ollama", "fast", 5, 0, 5, 0, 0, 1000)
	rec := opt.recommend(m)
	// 100 % failure → ActionRemoveProvider (not the "insufficient data" keep)
	if rec.Action != ActionRemoveProvider {
		t.Errorf("exactly at min sample with 100%% failure: want ActionRemoveProvider, got %q", rec.Action)
	}
}

func TestOptimizer_Recommend_ExactlyAtFallbackThreshold_RealSignal(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	// Exactly 3/10 real fallback = 30 % = threshold; low failure (1/10)
	m := realSignalMetrics("ollama", "fast", 10, 9, 1, 8, 0, 3, 300, 5000, 1.0)
	rec := opt.recommend(m)
	if rec.Action != ActionIncreaseEscalation {
		t.Errorf("exactly at fallback threshold (30%%): want ActionIncreaseEscalation, got %q", rec.Action)
	}
}

func TestOptimizer_Recommend_SignalLabel_Real(t *testing.T) {
	if got := signalLabel(true); got != "real" {
		t.Errorf("signalLabel(true): want 'real', got %q", got)
	}
}

func TestOptimizer_Recommend_SignalLabel_Proxy(t *testing.T) {
	if got := signalLabel(false); got != "proxy" {
		t.Errorf("signalLabel(false): want 'proxy', got %q", got)
	}
}


// ──────────────────────────────────────────────────────────────────────────────
// Stub store
