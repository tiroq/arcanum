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

	if d1.SelectedProvider != d2.SelectedProvider {
		t.Errorf("deterministic violation: first=%s, second=%s", d1.SelectedProvider, d2.SelectedProvider)
	}
	if d1.SelectedProvider == "" {
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

	if d.SelectedProvider == "limited" {
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

	if d.SelectedProvider == "tpm-limited" {
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

	if d.SelectedProvider == "daily-limited" {
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

	if d.SelectedProvider == "cerebras" {
		t.Error("cerebras should be rejected due to RPM exhaustion")
	}
	if d.SelectedProvider == "" {
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

	seen := map[string]bool{d.SelectedProvider: true}
	for _, f := range d.FallbackChain {
		if seen[f] {
			t.Errorf("duplicate provider in fallback chain: %s", f)
		}
		seen[f] = true
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

	if d.SelectedProvider != "ollama" {
		t.Errorf("expected local provider 'ollama', got %q", d.SelectedProvider)
	}

	for _, f := range d.FallbackChain {
		p, ok := registry.Get(f)
		if ok && p.IsExternal() {
			t.Errorf("external provider %q in fallback chain when AllowExternal=false", f)
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

	if d.SelectedProvider == "low-cap" {
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

	if d.SelectedProvider != "ollama" {
		t.Errorf("expected local provider for tight latency budget, got %q", d.SelectedProvider)
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
		results[d.SelectedProvider]++
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

	if d.SelectedProvider != "ollama" {
		t.Errorf("expected local fallback, got %q", d.SelectedProvider)
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

	if d.SelectedProvider != "" {
		t.Errorf("expected empty provider, got %q", d.SelectedProvider)
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

	if d.SelectedProvider != "" {
		t.Errorf("disabled provider should not be selected, got %q", d.SelectedProvider)
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
	if d.SelectedProvider == "" {
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

	if d.SelectedProvider == "" {
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

	if d.SelectedProvider != "" {
		t.Errorf("empty registry should produce no provider, got %q", d.SelectedProvider)
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

	if d.SelectedProvider != "ollama" {
		t.Errorf("expected ollama as only option, got %q", d.SelectedProvider)
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

	if d.SelectedProvider != "" {
		t.Errorf("expected no provider when all quotas exhausted, got %q", d.SelectedProvider)
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
	if d.SelectedProvider == "fast-only" {
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

	if len(d.FallbackChain) > MaxFallbackChainLength {
		t.Errorf("fallback chain exceeded max length: %d > %d", len(d.FallbackChain), MaxFallbackChainLength)
	}
}

// --- 20. Adapter nil safety ---

func TestGraphAdapterNilSafety(t *testing.T) {
	var adapter *GraphAdapter
	selected, model, chain, reason := adapter.RouteForTask(context.Background(), "test", "test", RolePlanner, 100, 1000, 0.8, true)

	if selected != "" {
		t.Errorf("nil adapter should return empty selected, got %q", selected)
	}
	if model != "" {
		t.Errorf("nil adapter should return empty model, got %q", model)
	}
	if chain != nil {
		t.Errorf("nil adapter should return nil chain, got %v", chain)
	}
	if reason == "" {
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

	if d.SelectedProvider == "primary" {
		t.Error("primary should be rejected due to RPM exhaustion")
	}
	if d.SelectedProvider == "" {
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

	if d.SelectedProvider == "tpd-limited" {
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

	if d.SelectedProvider != "local1" {
		t.Errorf("expected local provider on tie, got %q", d.SelectedProvider)
	}
}
