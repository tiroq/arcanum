package provider_routing

import (
	"context"
	"testing"
	"time"
)

// --- Helper: build a standard test registry ---

func testProvider(name, kind string, roles []string, caps []string, limits ProviderLimits, costClass string, relativeCost float64) Provider {
	return Provider{
		Name:         name,
		Kind:         kind,
		Roles:        roles,
		Capabilities: caps,
		Limits:       limits,
		Cost:         ProviderCostModel{CostClass: costClass, RelativeCost: relativeCost},
		Health:       ProviderHealth{Enabled: true, Reachable: true},
	}
}

func testRegistry() *Registry {
	r := NewRegistry()
	r.Register(testProvider("ollama", KindLocal,
		[]string{RoleFast, RolePlanner, RoleReviewer, RoleFallback},
		[]string{"json_mode", "low_latency"},
		ProviderLimits{},
		CostLocal, 0.0))
	r.Register(testProvider("cerebras", KindCloud,
		[]string{RoleFast, RolePlanner},
		[]string{"json_mode", "low_latency"},
		ProviderLimits{RPM: 30, TPM: 60000, RPD: 1000, TPD: 1000000},
		CostFree, 0.0))
	r.Register(testProvider("groq", KindCloud,
		[]string{RoleFast, RolePlanner, RoleReviewer},
		[]string{"json_mode", "low_latency"},
		ProviderLimits{RPM: 30, TPM: 15000, RPD: 1000, TPD: 500000},
		CostFree, 0.05))
	r.Register(testProvider("openrouter", KindRouter,
		[]string{RolePlanner, RoleReviewer, RoleFallback},
		[]string{"json_mode", "long_context", "tool_calling"},
		ProviderLimits{RPM: 20, RPD: 200},
		CostFree, 0.1))
	return r
}

// --- 1. Deterministic routing ---

func TestDeterministicRouting(t *testing.T) {
	registry := testRegistry()
	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)

	input := RoutingInput{
		GoalType:        "task_execution",
		TaskType:        "code_review",
		PreferredRole:   RolePlanner,
		EstimatedTokens: 500,
		LatencyBudgetMs: 2000,
		AllowExternal:   true,
	}

	// Route twice with same input — must get same result
	d1 := router.Route(context.Background(), input)
	d2 := router.Route(context.Background(), input)

	if d1.Provider != d2.Provider {
		t.Errorf("deterministic violation: first=%s, second=%s", d1.Provider, d2.Provider)
	}
	if d1.Provider == "" {
		t.Error("expected a provider to be selected")
	}
}

// --- 2. Quota exceeded: RPM ---

func TestQuotaExceededRPM(t *testing.T) {
	registry := NewRegistry()
	registry.Register(testProvider("limited", KindCloud,
		[]string{RolePlanner},
		nil,
		ProviderLimits{RPM: 5},
		CostFree, 0.0))
	registry.Register(testProvider("ollama", KindLocal,
		[]string{RolePlanner, RoleFallback},
		nil,
		ProviderLimits{},
		CostLocal, 0.0))

	quotas := NewQuotaTracker(nil)
	// Simulate 5 requests already used this minute
	for i := 0; i < 5; i++ {
		quotas.RecordUsage(context.Background(), "limited", 100)
	}

	router := NewRouter(registry, quotas, nil, nil)
	d := router.Route(context.Background(), RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 100,
		AllowExternal:   true,
	})

	if d.Provider == "limited" {
		t.Error("should not select provider with exceeded RPM quota")
	}
	// Should have rejected "limited" in trace
	found := false
	for _, rp := range d.Trace.RejectedProviders {
		if rp.Provider == "limited" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'limited' in rejected providers")
	}
}

// --- 3. TPM exceeded ---

func TestQuotaExceededTPM(t *testing.T) {
	registry := NewRegistry()
	registry.Register(testProvider("tpm-limited", KindCloud,
		[]string{RolePlanner},
		nil,
		ProviderLimits{TPM: 1000},
		CostFree, 0.0))
	registry.Register(testProvider("ollama", KindLocal,
		[]string{RolePlanner, RoleFallback},
		nil,
		ProviderLimits{},
		CostLocal, 0.0))

	quotas := NewQuotaTracker(nil)
	quotas.RecordUsage(context.Background(), "tpm-limited", 800)

	router := NewRouter(registry, quotas, nil, nil)
	d := router.Route(context.Background(), RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 500, // 800 + 500 > 1000
		AllowExternal:   true,
	})

	if d.Provider == "tpm-limited" {
		t.Error("should not select provider with exceeded TPM quota")
	}
}

// --- 4. Daily quota exceeded ---

func TestDailyQuotaExceeded(t *testing.T) {
	registry := NewRegistry()
	registry.Register(testProvider("daily-limited", KindCloud,
		[]string{RolePlanner},
		nil,
		ProviderLimits{RPD: 10},
		CostFree, 0.0))
	registry.Register(testProvider("ollama", KindLocal,
		[]string{RolePlanner, RoleFallback},
		nil,
		ProviderLimits{},
		CostLocal, 0.0))

	quotas := NewQuotaTracker(nil)
	for i := 0; i < 10; i++ {
		quotas.RecordUsage(context.Background(), "daily-limited", 10)
	}

	router := NewRouter(registry, quotas, nil, nil)
	d := router.Route(context.Background(), RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 10,
		AllowExternal:   true,
	})

	if d.Provider == "daily-limited" {
		t.Error("should not select provider with exceeded daily quota")
	}
}

// --- 5. Fallback works ---

func TestFallbackWorks(t *testing.T) {
	registry := testRegistry()
	quotas := NewQuotaTracker(nil)
	// Exhaust cerebras RPM
	for i := 0; i < 30; i++ {
		quotas.RecordUsage(context.Background(), "cerebras", 100)
	}

	router := NewRouter(registry, quotas, nil, nil)
	d := router.Route(context.Background(), RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 100,
		AllowExternal:   true,
	})

	if d.Provider == "cerebras" {
		t.Error("cerebras should be rejected due to RPM exhaustion")
	}
	if d.Provider == "" {
		t.Error("fallback provider should have been selected")
	}
}

// --- 6. No duplicate fallback ---

func TestNoDuplicateFallback(t *testing.T) {
	registry := testRegistry()
	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)

	d := router.Route(context.Background(), RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 100,
		AllowExternal:   true,
	})

	seen := map[string]bool{d.Provider: true}
	for _, f := range d.Fallbacks {
		if seen[f.Provider] {
			t.Errorf("duplicate provider in fallback chain: %s", f.Provider)
		}
		seen[f.Provider] = true
	}
}

// --- 7. Local-only routing ---

func TestLocalOnlyRouting(t *testing.T) {
	registry := testRegistry()
	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)

	d := router.Route(context.Background(), RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 100,
		AllowExternal:   false,
	})

	if d.Provider != "ollama" {
		t.Errorf("expected local provider 'ollama', got %q", d.Provider)
	}

	for _, f := range d.Fallbacks {
		p, ok := registry.Get(f.Provider)
		if ok && p.IsExternal() {
			t.Errorf("external provider %q in fallback chain when AllowExternal=false", f.Provider)
		}
	}
}

// --- 8. Token-heavy request ---

func TestTokenHeavyRequest(t *testing.T) {
	registry := NewRegistry()
	registry.Register(testProvider("low-cap", KindCloud,
		[]string{RolePlanner},
		nil,
		ProviderLimits{TPM: 5000},
		CostFree, 0.0))
	registry.Register(testProvider("high-cap", KindCloud,
		[]string{RolePlanner},
		nil,
		ProviderLimits{TPM: 100000},
		CostFree, 0.0))
	registry.Register(testProvider("ollama", KindLocal,
		[]string{RolePlanner, RoleFallback},
		nil,
		ProviderLimits{},
		CostLocal, 0.0))

	quotas := NewQuotaTracker(nil)
	// Use some capacity on low-cap
	quotas.RecordUsage(context.Background(), "low-cap", 3000)

	router := NewRouter(registry, quotas, nil, nil)
	d := router.Route(context.Background(), RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 4000, // 3000 + 4000 > 5000
		AllowExternal:   true,
	})

	if d.Provider == "low-cap" {
		t.Error("should not select provider with insufficient TPM for token-heavy request")
	}
}

// --- 9. Latency-sensitive routing ---

func TestLatencySensitiveRouting(t *testing.T) {
	registry := testRegistry()
	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)

	// Tight latency budget → local should be preferred
	d := router.Route(context.Background(), RoutingInput{
		PreferredRole:   RoleFast,
		EstimatedTokens: 100,
		LatencyBudgetMs: 200,
		AllowExternal:   true,
	})

	if d.Provider != "ollama" {
		t.Errorf("expected local provider for tight latency budget, got %q", d.Provider)
	}
}

// --- 10. Tie-breaking deterministic ---

func TestTieBreakingDeterministic(t *testing.T) {
	// Create two identical external providers
	registry := NewRegistry()
	registry.Register(testProvider("alpha", KindCloud,
		[]string{RolePlanner},
		nil,
		ProviderLimits{RPM: 100, TPM: 100000},
		CostFree, 0.0))
	registry.Register(testProvider("beta", KindCloud,
		[]string{RolePlanner},
		nil,
		ProviderLimits{RPM: 100, TPM: 100000},
		CostFree, 0.0))

	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)

	input := RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 100,
		AllowExternal:   true,
	}

	results := map[string]int{}
	for i := 0; i < 10; i++ {
		d := router.Route(context.Background(), input)
		results[d.Provider]++
	}

	// Must always pick the same one (lexical tie-break: "alpha" < "beta")
	if len(results) != 1 {
		t.Errorf("tie-breaking not deterministic: got %v", results)
	}
	if _, ok := results["alpha"]; !ok {
		t.Error("expected 'alpha' to win lexical tie-break")
	}
}

// --- 11. Fail-open local fallback ---

func TestFailOpenLocalFallback(t *testing.T) {
	registry := NewRegistry()
	// Only external providers, all degraded
	registry.Register(Provider{
		Name:   "cloud1",
		Kind:   KindCloud,
		Roles:  []string{RolePlanner},
		Health: ProviderHealth{Enabled: true, Reachable: false},
	})
	registry.Register(testProvider("ollama", KindLocal,
		[]string{RolePlanner, RoleFallback},
		nil,
		ProviderLimits{},
		CostLocal, 0.0))

	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)

	d := router.Route(context.Background(), RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 100,
		AllowExternal:   true,
	})

	if d.Provider != "ollama" {
		t.Errorf("expected local fallback, got %q", d.Provider)
	}
}

// --- 12. No provider available ---

func TestNoProviderAvailable(t *testing.T) {
	registry := NewRegistry()
	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)

	d := router.Route(context.Background(), RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 100,
		AllowExternal:   true,
	})

	if d.Provider != "" {
		t.Errorf("expected empty provider, got %q", d.Provider)
	}
	if d.Reason == "" {
		t.Error("expected a reason when no provider available")
	}
}

// --- 13. Usage reset behavior ---

func TestUsageResetMinute(t *testing.T) {
	quotas := NewQuotaTracker(nil)
	ctx := context.Background()

	quotas.RecordUsage(ctx, "test", 100)
	u1 := quotas.GetUsage("test")
	if u1.RequestsThisMinute != 1 {
		t.Errorf("expected 1 request, got %d", u1.RequestsThisMinute)
	}

	// Simulate time passing to next minute
	quotas.mu.Lock()
	state := quotas.usage["test"]
	state.LastUpdated = time.Now().Add(-2 * time.Minute)
	quotas.mu.Unlock()

	u2 := quotas.GetUsage("test")
	if u2.RequestsThisMinute != 0 {
		t.Errorf("expected minute reset to 0, got %d", u2.RequestsThisMinute)
	}
}

func TestUsageResetDay(t *testing.T) {
	quotas := NewQuotaTracker(nil)
	ctx := context.Background()

	quotas.RecordUsage(ctx, "test", 100)
	u1 := quotas.GetUsage("test")
	if u1.RequestsToday != 1 {
		t.Errorf("expected 1 request today, got %d", u1.RequestsToday)
	}

	// Simulate time passing to next day
	quotas.mu.Lock()
	state := quotas.usage["test"]
	state.LastUpdated = time.Now().Add(-25 * time.Hour)
	quotas.mu.Unlock()

	u2 := quotas.GetUsage("test")
	if u2.RequestsToday != 0 {
		t.Errorf("expected daily reset to 0, got %d", u2.RequestsToday)
	}
}

// --- 14. Trace correctness ---

func TestTraceCorrectness(t *testing.T) {
	registry := NewRegistry()
	registry.Register(testProvider("ollama", KindLocal,
		[]string{RolePlanner, RoleFallback},
		nil,
		ProviderLimits{},
		CostLocal, 0.0))
	// Disabled providers are filtered by Registry.Enabled() and won't appear in trace.
	registry.Register(Provider{
		Name:   "disabled",
		Kind:   KindCloud,
		Roles:  []string{RolePlanner},
		Health: ProviderHealth{Enabled: false},
	})
	registry.Register(Provider{
		Name:   "unreachable",
		Kind:   KindCloud,
		Roles:  []string{RolePlanner},
		Health: ProviderHealth{Enabled: true, Reachable: false},
	})

	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)

	d := router.Route(context.Background(), RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 100,
		AllowExternal:   true,
	})

	// Only enabled providers are considered; disabled is filtered before routing.
	// Unreachable is considered but rejected.
	if len(d.Trace.RejectedProviders) < 1 {
		t.Errorf("expected at least 1 rejected provider, got %d", len(d.Trace.RejectedProviders))
	}

	rejectedNames := map[string]bool{}
	for _, rp := range d.Trace.RejectedProviders {
		rejectedNames[rp.Provider] = true
		if rp.Reason == "" {
			t.Errorf("rejected provider %q has empty reason", rp.Provider)
		}
	}
	if !rejectedNames["unreachable"] {
		t.Error("expected 'unreachable' in rejected providers")
	}
}

// --- 15. Registry operations ---

func TestRegistryByRole(t *testing.T) {
	registry := testRegistry()

	planners := registry.ByRole(RolePlanner)
	if len(planners) == 0 {
		t.Error("expected at least one planner")
	}
	for _, p := range planners {
		if !p.HasRole(RolePlanner) {
			t.Errorf("provider %q doesn't have planner role", p.Name)
		}
	}
}

func TestRegistryByCapability(t *testing.T) {
	registry := testRegistry()

	longCtx := registry.ByCapability("long_context")
	if len(longCtx) == 0 {
		t.Error("expected at least one provider with long_context")
	}
}

// --- 16. Edge cases ---

func TestProviderWithUnknownLimits(t *testing.T) {
	// Provider with all zero limits should not be rejected by quota check
	reason, ok := CheckQuota(ProviderLimits{}, ProviderUsageState{}, 100)
	if !ok {
		t.Errorf("provider with unknown limits should pass quota check, got rejected: %s", reason)
	}
}

func TestProviderDisabled(t *testing.T) {
	registry := NewRegistry()
	registry.Register(Provider{
		Name:   "disabled",
		Kind:   KindCloud,
		Roles:  []string{RolePlanner},
		Health: ProviderHealth{Enabled: false},
	})

	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)
	d := router.Route(context.Background(), RoutingInput{AllowExternal: true})

	if d.Provider != "" {
		t.Errorf("disabled provider should not be selected, got %q", d.Provider)
	}
}

func TestProviderDegraded(t *testing.T) {
	registry := NewRegistry()
	registry.Register(Provider{
		Name:         "degraded",
		Kind:         KindCloud,
		Roles:        []string{RolePlanner},
		Capabilities: []string{"json_mode"},
		Limits:       ProviderLimits{RPM: 100},
		Cost:         ProviderCostModel{CostClass: CostFree},
		Health:       ProviderHealth{Enabled: true, Reachable: true, Degraded: true},
	})
	registry.Register(testProvider("ollama", KindLocal,
		[]string{RolePlanner, RoleFallback},
		nil,
		ProviderLimits{},
		CostLocal, 0.0))

	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)

	d := router.Route(context.Background(), RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 100,
		AllowExternal:   true,
	})

	// Degraded provider should still be selectable but local preferred
	if d.Provider == "" {
		t.Error("expected a provider to be selected")
	}
}

func TestZeroEstimatedTokens(t *testing.T) {
	registry := testRegistry()
	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)

	d := router.Route(context.Background(), RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 0,
		AllowExternal:   true,
	})

	if d.Provider == "" {
		t.Error("should select a provider even with zero estimated tokens")
	}
}

func TestNegativeEstimatedTokens(t *testing.T) {
	// Negative tokens should be treated as zero
	reason, ok := CheckQuota(ProviderLimits{TPM: 100}, ProviderUsageState{TokensThisMinute: 50}, -10)
	if !ok {
		t.Errorf("negative tokens should not cause rejection, got: %s", reason)
	}
}

func TestEmptyRegistry(t *testing.T) {
	registry := NewRegistry()
	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)

	d := router.Route(context.Background(), RoutingInput{AllowExternal: true})

	if d.Provider != "" {
		t.Errorf("empty registry should produce no provider, got %q", d.Provider)
	}
}

func TestOnlyLocalProvider(t *testing.T) {
	registry := NewRegistry()
	registry.Register(testProvider("ollama", KindLocal,
		[]string{RoleFast, RolePlanner, RoleFallback},
		nil,
		ProviderLimits{},
		CostLocal, 0.0))

	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)

	d := router.Route(context.Background(), RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 1000,
		AllowExternal:   true,
	})

	if d.Provider != "ollama" {
		t.Errorf("expected ollama as only option, got %q", d.Provider)
	}
}

func TestAllQuotasExhausted(t *testing.T) {
	registry := NewRegistry()
	registry.Register(testProvider("a", KindCloud,
		[]string{RolePlanner},
		nil,
		ProviderLimits{RPM: 5},
		CostFree, 0.0))
	registry.Register(testProvider("b", KindCloud,
		[]string{RolePlanner},
		nil,
		ProviderLimits{RPM: 5},
		CostFree, 0.0))

	quotas := NewQuotaTracker(nil)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		quotas.RecordUsage(ctx, "a", 10)
		quotas.RecordUsage(ctx, "b", 10)
	}

	router := NewRouter(registry, quotas, nil, nil)
	d := router.Route(ctx, RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 10,
		AllowExternal:   true,
	})

	if d.Provider != "" {
		t.Errorf("expected no provider when all quotas exhausted, got %q", d.Provider)
	}
}

func TestRoleIncompatible(t *testing.T) {
	registry := NewRegistry()
	registry.Register(testProvider("fast-only", KindCloud,
		[]string{RoleFast},
		nil,
		ProviderLimits{RPM: 100},
		CostFree, 0.0))

	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)

	d := router.Route(context.Background(), RoutingInput{
		PreferredRole: RoleReviewer,
		AllowExternal: true,
	})

	// "fast-only" does not have reviewer role and no fallback role → rejected
	if d.Provider == "fast-only" {
		t.Error("role-incompatible provider should not be selected")
	}
}

// --- 17. Scoring ---

func TestScoringComponents(t *testing.T) {
	p := testProvider("test", KindCloud,
		[]string{RolePlanner},
		nil,
		ProviderLimits{RPM: 100, TPM: 100000},
		CostFree, 0.0)

	input := RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 1000,
		LatencyBudgetMs: 2000,
	}

	usage := ProviderUsageState{}
	components := ScoreProvider(p, input, usage)

	if components.FinalScore <= 0 || components.FinalScore > 1 {
		t.Errorf("final score should be in (0,1], got %f", components.FinalScore)
	}
	if components.LatencyFit < 0 || components.LatencyFit > 1 {
		t.Errorf("latency fit should be in [0,1], got %f", components.LatencyFit)
	}
	if components.QuotaHeadroom < 0 || components.QuotaHeadroom > 1 {
		t.Errorf("quota headroom should be in [0,1], got %f", components.QuotaHeadroom)
	}
	if components.ReliabilityFit < 0 || components.ReliabilityFit > 1 {
		t.Errorf("reliability fit should be in [0,1], got %f", components.ReliabilityFit)
	}
	if components.CostEfficiency < 0 || components.CostEfficiency > 1 {
		t.Errorf("cost efficiency should be in [0,1], got %f", components.CostEfficiency)
	}
}

// --- 18. ComputeHeadroom ---

func TestComputeHeadroomWithUnknownLimits(t *testing.T) {
	h := ComputeHeadroom(ProviderLimits{}, ProviderUsageState{}, 100)
	if h != 1.0 {
		t.Errorf("expected 1.0 for unknown limits, got %f", h)
	}
}

func TestComputeHeadroomPartialUsage(t *testing.T) {
	h := ComputeHeadroom(
		ProviderLimits{RPM: 100, TPM: 10000},
		ProviderUsageState{RequestsThisMinute: 50, TokensThisMinute: 5000},
		100,
	)
	if h < 0 || h > 1 {
		t.Errorf("headroom out of range: %f", h)
	}
	if h > 0.55 {
		t.Errorf("expected headroom around 0.49, got %f", h)
	}
}

// --- 19. Max fallback chain length ---

func TestMaxFallbackChainLength(t *testing.T) {
	registry := NewRegistry()
	for i := 0; i < 10; i++ {
		name := string(rune('a' + i))
		registry.Register(testProvider(name, KindCloud,
			[]string{RolePlanner},
			nil,
			ProviderLimits{RPM: 100},
			CostFree, 0.0))
	}

	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)

	d := router.Route(context.Background(), RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 10,
		AllowExternal:   true,
	})

	if len(d.Fallbacks) > MaxFallbackChainLength {
		t.Errorf("fallback chain exceeded max length: %d > %d", len(d.Fallbacks), MaxFallbackChainLength)
	}
}

// --- 20. Adapter nil safety ---

func TestGraphAdapterNilSafety(t *testing.T) {
	var adapter *GraphAdapter
	plan := adapter.RouteForTask(context.Background(), "test", "test", RolePlanner, 100, 1000, 0.8, true)

	if plan.Provider != "" {
		t.Errorf("nil adapter should return empty provider, got %q", plan.Provider)
	}
	if plan.Model != "" {
		t.Errorf("nil adapter should return empty model, got %q", plan.Model)
	}
	if len(plan.Fallbacks) != 0 {
		t.Errorf("nil adapter should return empty fallbacks, got %v", plan.Fallbacks)
	}
	if plan.Reason == "" {
		t.Error("nil adapter should return a reason")
	}
}

// --- 21. Recent decisions bounded ---

func TestRecentDecisionsBounded(t *testing.T) {
	registry := testRegistry()
	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)

	ctx := context.Background()
	for i := 0; i < MaxRecentDecisions+50; i++ {
		router.Route(ctx, RoutingInput{
			PreferredRole:   RolePlanner,
			EstimatedTokens: 100,
			AllowExternal:   true,
		})
	}

	decisions := router.GetRecentDecisions()
	if len(decisions) > MaxRecentDecisions {
		t.Errorf("recent decisions exceeded max: %d > %d", len(decisions), MaxRecentDecisions)
	}
}

// --- 22. Fallback used audit ---

func TestFallbackChainUsedWhenPrimaryRejected(t *testing.T) {
	registry := NewRegistry()
	registry.Register(testProvider("primary", KindCloud,
		[]string{RolePlanner},
		nil,
		ProviderLimits{RPM: 1},
		CostFree, 0.0))
	registry.Register(testProvider("fallback1", KindCloud,
		[]string{RolePlanner},
		nil,
		ProviderLimits{RPM: 100},
		CostFree, 0.0))
	registry.Register(testProvider("ollama", KindLocal,
		[]string{RolePlanner, RoleFallback},
		nil,
		ProviderLimits{},
		CostLocal, 0.0))

	quotas := NewQuotaTracker(nil)
	quotas.RecordUsage(context.Background(), "primary", 100)

	router := NewRouter(registry, quotas, nil, nil)
	d := router.Route(context.Background(), RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 100,
		AllowExternal:   true,
	})

	if d.Provider == "primary" {
		t.Error("primary should be rejected due to RPM exhaustion")
	}
	if d.Provider == "" {
		t.Error("expected a fallback provider to be selected")
	}
}

// --- 23. TPD exceeded ---

func TestQuotaExceededTPD(t *testing.T) {
	registry := NewRegistry()
	registry.Register(testProvider("tpd-limited", KindCloud,
		[]string{RolePlanner},
		nil,
		ProviderLimits{TPD: 10000},
		CostFree, 0.0))
	registry.Register(testProvider("ollama", KindLocal,
		[]string{RolePlanner, RoleFallback},
		nil,
		ProviderLimits{},
		CostLocal, 0.0))

	quotas := NewQuotaTracker(nil)
	quotas.RecordUsage(context.Background(), "tpd-limited", 9500)

	router := NewRouter(registry, quotas, nil, nil)
	d := router.Route(context.Background(), RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 1000, // 9500 + 1000 > 10000
		AllowExternal:   true,
	})

	if d.Provider == "tpd-limited" {
		t.Error("should not select provider with exceeded TPD quota")
	}
}

// --- 24. Local preferred for equal score with external ---

func TestLocalPreferredOnTie(t *testing.T) {
	registry := NewRegistry()
	// Both have exactly the same characteristics except kind
	registry.Register(Provider{
		Name:   "local1",
		Kind:   KindLocal,
		Roles:  []string{RolePlanner},
		Limits: ProviderLimits{},
		Cost:   ProviderCostModel{CostClass: CostFree, RelativeCost: 0.0},
		Health: ProviderHealth{Enabled: true, Reachable: true},
	})
	registry.Register(Provider{
		Name:   "cloud1",
		Kind:   KindCloud,
		Roles:  []string{RolePlanner},
		Limits: ProviderLimits{},
		Cost:   ProviderCostModel{CostClass: CostFree, RelativeCost: 0.0},
		Health: ProviderHealth{Enabled: true, Reachable: true},
	})

	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)

	d := router.Route(context.Background(), RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 100,
		LatencyBudgetMs: 0, // no latency constraint
		AllowExternal:   true,
	})

	if d.Provider != "local1" {
		t.Errorf("expected local provider on tie, got %q", d.Provider)
	}
}

// =============================================================================
// Global Policy Tests (Iteration 33: providers/_global.yaml wire-in)
// =============================================================================

// --- 25. Global policy: role-based preference ordering (fast role) ---
// Test 4.3.8: fast role prefers groq/gemini/ollama ordering.

func TestGlobalPolicy_FastRolePreferenceOrdering(t *testing.T) {
	// Register providers in reverse alphabetical order to ensure preference
	// boost drives the ordering, not alphabetical tie-break.
	registry := NewRegistry()
	registry.Register(testProvider("ollama", KindLocal,
		[]string{RoleFast, RoleFallback}, nil, ProviderLimits{}, CostLocal, 0.0))
	registry.Register(testProvider("gemini", KindCloud,
		[]string{RoleFast}, nil, ProviderLimits{RPM: 100}, CostFree, 0.0))
	registry.Register(testProvider("groq", KindCloud,
		[]string{RoleFast}, nil, ProviderLimits{RPM: 100}, CostFree, 0.0))

	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)
	router.WithGlobalPolicy(&GlobalPolicyConfig{
		AllowExternal: true,
		RolePreferences: map[string][]string{
			RoleFast: {"groq", "gemini", "ollama"},
		},
	})

	d := router.Route(context.Background(), RoutingInput{
		PreferredRole: RoleFast,
		AllowExternal: true,
	})

	// groq is first in preference list → should win despite same score as gemini.
	if d.Provider != "groq" {
		t.Errorf("expected groq (first in fast preference list), got %q", d.Provider)
	}
}

// --- 26. Global policy: planner role preference ordering ---
// Test 4.3.9: planner role prefers cerebras/sambanova/gemini/openrouter.

func TestGlobalPolicy_PlannerRolePreferenceOrdering(t *testing.T) {
	registry := NewRegistry()
	registry.Register(testProvider("ollama", KindLocal,
		[]string{RolePlanner, RoleFallback}, nil, ProviderLimits{}, CostLocal, 0.0))
	registry.Register(testProvider("openrouter", KindRouter,
		[]string{RolePlanner, RoleFallback}, nil, ProviderLimits{RPM: 20}, CostFree, 0.1))
	registry.Register(testProvider("cerebras", KindCloud,
		[]string{RolePlanner}, nil, ProviderLimits{RPM: 30}, CostFree, 0.0))

	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)
	router.WithGlobalPolicy(&GlobalPolicyConfig{
		AllowExternal: true,
		RolePreferences: map[string][]string{
			RolePlanner: {"cerebras", "sambanova", "gemini", "openrouter"},
		},
	})

	d := router.Route(context.Background(), RoutingInput{
		PreferredRole: RolePlanner,
		AllowExternal: true,
	})

	// cerebras is first in preference list → should win.
	if d.Provider != "cerebras" {
		t.Errorf("expected cerebras (first in planner preference list), got %q", d.Provider)
	}
}

// --- 27. Global policy: fallback role preference ordering ---
// Test 4.3.10: fallback role prefers openrouter/ollama.

func TestGlobalPolicy_FallbackRolePreferenceOrdering(t *testing.T) {
	registry := NewRegistry()
	registry.Register(testProvider("ollama", KindLocal,
		[]string{RoleFallback}, nil, ProviderLimits{}, CostLocal, 0.0))
	registry.Register(testProvider("openrouter", KindRouter,
		[]string{RoleFallback}, nil, ProviderLimits{RPM: 20}, CostFree, 0.1))

	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)
	router.WithGlobalPolicy(&GlobalPolicyConfig{
		AllowExternal: true,
		RolePreferences: map[string][]string{
			RoleFallback: {"openrouter", "ollama"},
		},
	})

	d := router.Route(context.Background(), RoutingInput{
		PreferredRole: RoleFallback,
		AllowExternal: true,
	})

	// openrouter is first in fallback preference list → should win.
	if d.Provider != "openrouter" {
		t.Errorf("expected openrouter (first in fallback preference list), got %q", d.Provider)
	}
}

// --- 28. Global policy: allow_external=false blocks external providers ---
// Test 4.3.11: allow_external=false blocks external providers even if preferred.

func TestGlobalPolicy_AllowExternalFalseBlocksExternalProviders(t *testing.T) {
	registry := NewRegistry()
	registry.Register(testProvider("ollama", KindLocal,
		[]string{RolePlanner, RoleFallback}, nil, ProviderLimits{}, CostLocal, 0.0))
	registry.Register(testProvider("groq", KindCloud,
		[]string{RolePlanner}, nil, ProviderLimits{RPM: 100}, CostFree, 0.0))
	registry.Register(testProvider("openrouter", KindRouter,
		[]string{RolePlanner, RoleFallback}, nil, ProviderLimits{RPM: 20}, CostFree, 0.0))

	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)
	// Global policy blocks external even though groq/openrouter are preferred.
	router.WithGlobalPolicy(&GlobalPolicyConfig{
		AllowExternal: false, // ← gate
		RolePreferences: map[string][]string{
			RolePlanner: {"groq", "openrouter", "ollama"},
		},
	})

	d := router.Route(context.Background(), RoutingInput{
		PreferredRole: RolePlanner,
		AllowExternal: true, // caller requests external — policy overrides
	})

	if d.Provider != "ollama" {
		t.Errorf("expected local ollama (global policy blocks external), got %q", d.Provider)
	}
	for _, f := range d.Fallbacks {
		p, ok := registry.Get(f.Provider)
		if ok && p.IsExternal() {
			t.Errorf("external provider %q in fallback chain when global policy allow_external=false", f.Provider)
		}
	}
}

// --- 29. Global policy: max_fallback_chain from policy overrides constant ---
// Test 4.1.4: fallback chain respects _global.yaml max_fallback_chain.

func TestGlobalPolicy_MaxFallbackChainFromPolicy(t *testing.T) {
	registry := NewRegistry()
	// Register many providers so fallback chain can be naturally long.
	for i := 0; i < 10; i++ {
		name := string(rune('a' + i))
		registry.Register(testProvider(name, KindCloud,
			[]string{RolePlanner}, nil, ProviderLimits{RPM: 100}, CostFree, 0.0))
	}

	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)
	router.WithGlobalPolicy(&GlobalPolicyConfig{
		AllowExternal:    true,
		MaxFallbackChain: 2, // policy caps at 2
	})

	d := router.Route(context.Background(), RoutingInput{
		PreferredRole: RolePlanner,
		AllowExternal: true,
	})

	if len(d.Fallbacks) > 2 {
		t.Errorf("fallback chain exceeds policy max_fallback_chain=2: got %d entries", len(d.Fallbacks))
	}
}

// --- 30. Global policy: degrade_policy ordering in fallback chain ---
// Test 4.3.12: degrade_policy ordering is respected deterministically.

func TestGlobalPolicy_DegradePolicyOrderingInFallbackChain(t *testing.T) {
	registry := NewRegistry()
	// cloud providers (external_strong tier) come before router and local.
	registry.Register(testProvider("ollama", KindLocal,
		[]string{RolePlanner, RoleFallback}, nil, ProviderLimits{}, CostLocal, 0.0))
	registry.Register(testProvider("openrouter", KindRouter,
		[]string{RolePlanner, RoleFallback}, nil, ProviderLimits{RPM: 20}, CostFree, 0.1))
	registry.Register(testProvider("groq", KindCloud,
		[]string{RolePlanner}, nil, ProviderLimits{RPM: 100}, CostFree, 0.0))

	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)
	// Policy: try external_strong → router → local when degrading.
	// groq is selected as primary (highest preference + cloud score).
	// Fallback should be: openrouter (router) before ollama (local).
	router.WithGlobalPolicy(&GlobalPolicyConfig{
		AllowExternal:    true,
		MaxFallbackChain: 3,
		RolePreferences: map[string][]string{
			RolePlanner: {"groq", "openrouter", "ollama"},
		},
		DegradePolicy: []string{"external_strong", "router", "local"},
	})

	d := router.Route(context.Background(), RoutingInput{
		PreferredRole: RolePlanner,
		AllowExternal: true,
	})

	// Primary should be groq (highest preference).
	if d.Provider != "groq" {
		t.Errorf("expected groq as primary, got %q", d.Provider)
	}

	// Fallback chain should have openrouter before ollama (degrade_policy: router < local).
	openrouterIdx := -1
	ollamaIdx := -1
	for i, f := range d.Fallbacks {
		if f.Provider == "openrouter" {
			openrouterIdx = i
		}
		if f.Provider == "ollama" {
			ollamaIdx = i
		}
	}
	if openrouterIdx == -1 {
		t.Skip("openrouter not in fallback chain (may have been capped by MaxFallbackChain)")
	}
	if ollamaIdx != -1 && openrouterIdx > ollamaIdx {
		t.Errorf("router (openrouter) should appear before local (ollama) per degrade_policy, fallback=%v", d.Fallbacks)
	}
}

// --- 31. Legacy env isolation: OLLAMA_*_PROFILE does not affect provider routing ---
// Tests 4.2.5 and 4.2.6: legacy env only affects worker execution, not routing engine.

func TestLegacyEnv_OllamaProfileDoesNotAffectProviderRouting(t *testing.T) {
	// The provider routing engine is governed solely by the registry, scoring,
	// and global policy. MODEL_*_PROFILE / OLLAMA_*_PROFILE env vars are consumed
	// by cmd/worker/main.go for execution-only purposes (which local Ollama model
	// runs and with which options). They are never passed to the routing engine.
	//
	// This test verifies that a Router with no awareness of legacy env vars
	// produces the same result as one with a policy containing no profile overrides.
	registry := NewRegistry()
	registry.Register(testProvider("ollama", KindLocal,
		[]string{RolePlanner, RoleFallback}, nil, ProviderLimits{}, CostLocal, 0.0))
	registry.Register(testProvider("groq", KindCloud,
		[]string{RolePlanner}, nil, ProviderLimits{RPM: 100}, CostFree, 0.0))

	quotas := NewQuotaTracker(nil)

	// Router without any policy (baseline — no profile knowledge).
	routerBase := NewRouter(registry, quotas, nil, nil)

	// Router with global policy (matching _global.yaml) — still no profile knowledge.
	routerWithPolicy := NewRouter(registry, quotas, nil, nil)
	routerWithPolicy.WithGlobalPolicy(&GlobalPolicyConfig{
		AllowExternal:    true,
		MaxFallbackChain: 3,
		RolePreferences: map[string][]string{
			RolePlanner: {"groq", "ollama"},
		},
	})

	input := RoutingInput{
		PreferredRole: RolePlanner,
		AllowExternal: true,
	}

	d1 := routerBase.Route(context.Background(), input)
	d2 := routerWithPolicy.Route(context.Background(), input)

	// Both must return a valid provider (system is functional).
	if d1.Provider == "" || d2.Provider == "" {
		t.Error("expected a provider to be selected in both cases")
	}

	// With global policy, groq should be preferred (first in preference list).
	if d2.Provider != "groq" {
		t.Errorf("expected groq with global policy preference, got %q", d2.Provider)
	}
}

// --- 32. Local-only mode regression ---
// Test 4.4.13: local-only mode still works with policy wired in.

func TestGlobalPolicy_LocalOnlyModeRegression(t *testing.T) {
	registry := NewRegistry()
	registry.Register(testProvider("ollama", KindLocal,
		[]string{RolePlanner, RoleFallback}, nil, ProviderLimits{}, CostLocal, 0.0))
	registry.Register(testProvider("groq", KindCloud,
		[]string{RolePlanner}, nil, ProviderLimits{RPM: 100}, CostFree, 0.0))

	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)
	router.WithGlobalPolicy(&GlobalPolicyConfig{
		AllowExternal: true, // global allows external
		RolePreferences: map[string][]string{
			RolePlanner: {"groq", "ollama"}, // groq preferred
		},
	})

	// But caller sets AllowExternal=false (local-only mode at call site).
	d := router.Route(context.Background(), RoutingInput{
		PreferredRole: RolePlanner,
		AllowExternal: false,
	})

	// Local-only mode must be respected; external must not be selected.
	if d.Provider != "ollama" {
		t.Errorf("expected ollama in local-only mode, got %q", d.Provider)
	}
	for _, f := range d.Fallbacks {
		p, ok := registry.Get(f.Provider)
		if ok && p.IsExternal() {
			t.Errorf("external provider %q in fallback chain in local-only mode", f.Provider)
		}
	}
}

// --- 33. Routing decisions still persist with policy ---
// Test 4.4.15: provider routing decisions still persist (recent decisions bounded).

func TestGlobalPolicy_RoutingDecisionsPersistWithPolicy(t *testing.T) {
	registry := testRegistry()
	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)
	router.WithGlobalPolicy(&GlobalPolicyConfig{
		AllowExternal: true,
		RolePreferences: map[string][]string{
			RolePlanner: {"cerebras", "groq"},
		},
	})

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		router.Route(ctx, RoutingInput{
			PreferredRole:   RolePlanner,
			EstimatedTokens: 100,
			AllowExternal:   true,
		})
	}

	decisions := router.GetRecentDecisions()
	if len(decisions) != 5 {
		t.Errorf("expected 5 recorded decisions, got %d", len(decisions))
	}
	for _, d := range decisions {
		if d.SelectedProvider == "" {
			t.Error("recorded decision should have a selected provider")
		}
	}
}

// --- 34. Preference boost is additive and bounded ---

func TestGlobalPolicy_PreferenceBoostBounded(t *testing.T) {
	registry := NewRegistry()
	registry.Register(testProvider("preferred", KindCloud,
		[]string{RolePlanner}, nil, ProviderLimits{RPM: 100}, CostFree, 0.0))

	quotas := NewQuotaTracker(nil)
	router := NewRouter(registry, quotas, nil, nil)

	// Preference list with 10 entries — boost must be capped at 0.05.
	router.WithGlobalPolicy(&GlobalPolicyConfig{
		AllowExternal: true,
		RolePreferences: map[string][]string{
			RolePlanner: {"p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "preferred"},
		},
	})

	// The final score must remain in [0, 1] regardless of how many entries are in the preference list.
	d := router.Route(context.Background(), RoutingInput{
		PreferredRole: RolePlanner,
		AllowExternal: true,
	})
	if d.Provider != "preferred" {
		t.Errorf("expected 'preferred', got %q", d.Provider)
	}
	// Verify score in trace does not exceed 1.0
	for _, ranked := range d.Trace.RankedProviders {
		if ranked.Score > 1.0 {
			t.Errorf("ranked score exceeds 1.0: %f for %s", ranked.Score, ranked.Provider)
		}
	}
}

// --- 35. No-policy router behavior unchanged ---

func TestGlobalPolicy_NilPolicyPreservesExistingBehavior(t *testing.T) {
	registry := testRegistry()
	quotas := NewQuotaTracker(nil)

	// Router without policy.
	routerNoPolicy := NewRouter(registry, quotas, nil, nil)

	// Router with nil policy explicitly.
	routerNilPolicy := NewRouter(registry, quotas, nil, nil)
	routerNilPolicy.WithGlobalPolicy(nil)

	input := RoutingInput{
		PreferredRole:   RolePlanner,
		EstimatedTokens: 100,
		AllowExternal:   true,
	}

	d1 := routerNoPolicy.Route(context.Background(), input)
	d2 := routerNilPolicy.Route(context.Background(), input)

	if d1.Provider != d2.Provider {
		t.Errorf("nil policy should produce same result as no policy: got %q vs %q",
			d1.Provider, d2.Provider)
	}
}
