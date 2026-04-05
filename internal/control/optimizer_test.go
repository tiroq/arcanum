package control

import (
	"context"
	"errors"
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

func baseMetrics(provider, role string, total, success, failure, accepted int64,
	avgTokens, avgLatencyMS float64) RunMetrics {
	m := RunMetrics{
		Provider:       provider,
		ModelRole:      role,
		TotalRuns:      total,
		SuccessRuns:    success,
		FailureRuns:    failure,
		AcceptedRuns:   accepted,
		AvgTokensTotal: avgTokens,
		AvgLatencyMS:   avgLatencyMS,
	}
	if total > 0 {
		m.FailureRate = float64(failure) / float64(total)
		m.AcceptanceRate = float64(accepted) / float64(total)
		m.FallbackRate = m.FailureRate
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
// AnalyzeAndRecommend tests
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

func TestOptimizer_Recommend_IncreaseEscalation_LowAcceptance(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	// Low failure but poor acceptance rate: 2/10 accepted (20 %)
	m := baseMetrics("ollama", "default", 10, 9, 1, 2, 400, 7000)
	rec := opt.recommend(m)
	if rec.Action != ActionIncreaseEscalation {
		t.Errorf("want ActionIncreaseEscalation for low acceptance (20%%), got %q", rec.Action)
	}
}

func TestOptimizer_Recommend_ReduceEscalation_HighLatency(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	// Low failure, high acceptance, but avg latency > 30 s
	m := baseMetrics("openai", "review", 20, 19, 1, 15, 900, 35_000)
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

func TestOptimizer_Recommend_Keep_GoodMetrics(t *testing.T) {
	opt := newTestOptimizer(&stubOptimizerStore{})
	// All metrics within healthy range
	m := baseMetrics("ollama", "fast", 50, 48, 2, 40, 300, 5000)
	rec := opt.recommend(m)
	if rec.Action != ActionKeep {
		t.Errorf("want ActionKeep for healthy metrics, got %q", rec.Action)
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
// Metrics fields tests
// ──────────────────────────────────────────────────────────────────────────────

func TestRunMetrics_RatesArePopulated(t *testing.T) {
	m := baseMetrics("ollama", "fast", 10, 7, 3, 5, 400, 8000)
	if got := m.FailureRate; got != 0.3 {
		t.Errorf("failure_rate: want 0.3, got %v", got)
	}
	if got := m.AcceptanceRate; got != 0.5 {
		t.Errorf("acceptance_rate: want 0.5, got %v", got)
	}
	if got := m.FallbackRate; got != m.FailureRate {
		t.Errorf("fallback_rate should equal failure_rate proxy, got %v vs %v", got, m.FailureRate)
	}
}

func TestRunMetrics_ZeroRatesForEmptyDataset(t *testing.T) {
	m := baseMetrics("ollama", "fast", 0, 0, 0, 0, 0, 0)
	if m.FailureRate != 0 || m.AcceptanceRate != 0 {
		t.Error("rates should be zero when total_runs is zero")
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
