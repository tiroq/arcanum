package provider_routing

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// MaxRecentDecisions limits the number of routing decisions kept in memory.
const MaxRecentDecisions = 100

// Router is the provider routing engine. It selects the best provider for a task
// while respecting quotas, enforcing determinism, and building bounded fallback chains.
type Router struct {
	registry *Registry
	quotas   *QuotaTracker
	auditor  audit.AuditRecorder
	logger   *zap.Logger
	policy   *GlobalPolicyConfig // optional; nil = use defaults

	mu              sync.Mutex
	recentDecisions []RoutingRecord
}

// NewRouter creates a new provider routing engine.
func NewRouter(registry *Registry, quotas *QuotaTracker, auditor audit.AuditRecorder, logger *zap.Logger) *Router {
	return &Router{
		registry:        registry,
		quotas:          quotas,
		auditor:         auditor,
		logger:          logger,
		recentDecisions: make([]RoutingRecord, 0, MaxRecentDecisions),
	}
}

// WithGlobalPolicy attaches the global routing policy to the router.
// Returns the router for method chaining. Safe to call with a nil policy (no-op).
// The policy gates external providers, overrides max fallback chain length,
// applies role-based preference boosts, and orders the fallback chain by tier.
func (r *Router) WithGlobalPolicy(cfg *GlobalPolicyConfig) *Router {
	r.policy = cfg
	return r
}

// Route selects the best provider for the given input and builds a fallback chain.
// This is the main entry point for all provider routing decisions.
func (r *Router) Route(ctx context.Context, input RoutingInput) RoutingDecision {
	// Apply global policy gate: if the global policy disallows external providers,
	// override the input's AllowExternal even when the caller requested external.
	// This ensures providers/_global.yaml has final say on external access.
	if r.policy != nil && !r.policy.AllowExternal {
		input.AllowExternal = false
	}

	trace := RoutingTrace{
		ConsideredProviders: make([]string, 0),
		RejectedProviders:   make([]RejectedProvider, 0),
		RankedProviders:     make([]RankedProvider, 0),
	}

	// 1. Get all enabled providers
	candidates := r.registry.Enabled()
	if len(candidates) == 0 {
		decision := RoutingDecision{
			SelectedProvider: "",
			Reason:           "no providers available in registry",
			Trace: RoutingTrace{
				FinalReason: "empty registry or all providers disabled",
			},
		}
		r.recordDecision(ctx, input, decision)
		r.emitAudit(ctx, "provider.routing_decided", input, decision)
		return decision
	}

	// 2. Filter candidates
	var valid []Provider
	for _, p := range candidates {
		trace.ConsideredProviders = append(trace.ConsideredProviders, p.Name)

		if reason, ok := r.filterProvider(p, input); !ok {
			trace.RejectedProviders = append(trace.RejectedProviders, RejectedProvider{
				Provider: p.Name,
				Reason:   reason,
			})
			continue
		}
		valid = append(valid, p)
	}

	if len(valid) == 0 {
		// Try local-only fallback if external was allowed but all failed
		decision := RoutingDecision{
			SelectedProvider: "",
			Reason:           "all providers rejected by filtering",
			Trace:            trace,
		}
		trace.FinalReason = "no valid providers after filtering"
		decision.Trace = trace
		r.recordDecision(ctx, input, decision)
		r.emitAudit(ctx, "provider.routing_decided", input, decision)
		r.emitAudit(ctx, "provider.degraded_to_local", input, decision)
		return decision
	}

	// 3. Score valid candidates
	scored := r.scoreProviders(valid, input)
	trace.RankedProviders = scored

	// 4. Select primary (highest score, deterministic tie-break)
	primary := scored[0]

	// 5. Build fallback chain (remaining providers, no duplicates, bounded)
	primaryKey := primary.Provider
	if primary.Model != "" {
		primaryKey = primary.Provider + "/" + primary.Model
	}
	fallbackChain := r.buildFallbackChain(scored[1:], primaryKey, input)

	reason := fmt.Sprintf("selected %s: %s", primary.Provider, primary.Reason)
	if primary.Model != "" {
		reason = fmt.Sprintf("selected %s/%s: %s", primary.Provider, primary.Model, primary.Reason)
	}
	trace.FinalReason = reason

	decision := RoutingDecision{
		SelectedProvider: primary.Provider,
		SelectedModel:    primary.Model,
		FallbackChain:    fallbackChain,
		Reason:           reason,
		Trace:            trace,
	}

	r.recordDecision(ctx, input, decision)
	r.emitAudit(ctx, "provider.routing_decided", input, decision)

	if primary.Provider != "" {
		p, ok := r.registry.Get(primary.Provider)
		if ok && p.IsLocal() {
			// Check if this was a forced local fallback
			for _, rp := range trace.RejectedProviders {
				if rp.Reason != "" {
					r.emitAudit(ctx, "provider.degraded_to_local", input, decision)
					break
				}
			}
		}
	}

	return decision
}

// GetRecentDecisions returns recent routing decisions for observability.
func (r *Router) GetRecentDecisions() []RoutingRecord {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]RoutingRecord, len(r.recentDecisions))
	copy(result, r.recentDecisions)
	return result
}

// filterProvider checks a single provider against all filtering rules.
// Returns ("", true) if valid, or (reason, false) if rejected.
func (r *Router) filterProvider(p Provider, input RoutingInput) (string, bool) {
	// Check: enabled
	if !p.Health.Enabled {
		return "provider disabled", false
	}

	// Check: reachable
	if !p.Health.Reachable {
		return "provider unreachable", false
	}

	// Check: degraded (allow but note)
	// Degraded providers pass filtering but score lower in reliability

	// Check: external allowed
	if !input.AllowExternal && p.IsExternal() {
		return "external providers not allowed", false
	}

	// Check: role compatibility (if a preferred role is specified)
	if input.PreferredRole != "" {
		if !p.HasRole(input.PreferredRole) && !p.HasRole(RoleFallback) {
			return fmt.Sprintf("does not have role %q", input.PreferredRole), false
		}
	}

	// Check: quota availability
	usage := r.quotas.GetUsage(p.Name)
	if reason, ok := CheckQuota(p.Limits, usage, input.EstimatedTokens); !ok {
		r.emitQuotaExceeded(context.Background(), p.Name, input, reason)
		return reason, false
	}

	return "", true
}

// scoreProviders scores and sorts providers deterministically.
func (r *Router) scoreProviders(providers []Provider, input RoutingInput) []RankedProvider {
	ranked := make([]RankedProvider, 0, len(providers))

	for _, p := range providers {
		usage := r.quotas.GetUsage(p.Name)
		components := ScoreProvider(p, input, usage)

		// Apply global policy preference boost. Providers appearing earlier
		// in the role's preference list receive a higher score boost, making
		// providers/_global.yaml priorities influence routing decisions.
		boost := r.preferenceBoostFor(p.Name, input.PreferredRole)
		if boost > 0 {
			components.PreferenceBoost = boost
			components.FinalScore = clamp01(components.FinalScore + boost)
		}

		ranked = append(ranked, RankedProvider{
			Provider: p.Name,
			Score:    components.FinalScore,
			Reason:   FormatScoreReason(components),
		})
	}

	// Deterministic sort: by score DESC, then local-before-external,
	// then lower relative_cost, then lexical (provider+model).
	sort.SliceStable(ranked, func(i, j int) bool {
		diff := ranked[i].Score - ranked[j].Score
		if diff > TieBreakEpsilon {
			return true // higher score first
		}
		if diff < -TieBreakEpsilon {
			return false
		}

		// Tie: prefer local over external
		pi, _ := r.registry.Get(ranked[i].Provider)
		pj, _ := r.registry.Get(ranked[j].Provider)
		if pi.IsLocal() != pj.IsLocal() {
			return pi.IsLocal()
		}

		// Tie: lower relative cost
		if pi.Cost.RelativeCost != pj.Cost.RelativeCost {
			return pi.Cost.RelativeCost < pj.Cost.RelativeCost
		}

		// Tie: lexical provider name
		if ranked[i].Provider != ranked[j].Provider {
			return ranked[i].Provider < ranked[j].Provider
		}

		// Tie: lexical model name (Iteration 32)
		return ranked[i].Model < ranked[j].Model
	})

	return ranked
}

// buildFallbackChain creates a bounded, duplicate-free fallback chain.
// When a global policy is attached, the chain is sorted by degrade_policy tier ordering
// and bounded by policy.MaxFallbackChain (overriding MaxFallbackChainLength if > 0).
//
// Iteration 32: operates on provider+model pairs. Same provider with different
// models is allowed; duplicate provider+model pairs are rejected.
func (r *Router) buildFallbackChain(remaining []RankedProvider, primary string, input RoutingInput) []string {
	// Determine effective max fallback chain length.
	maxChain := MaxFallbackChainLength
	if r.policy != nil && r.policy.MaxFallbackChain > 0 {
		maxChain = r.policy.MaxFallbackChain
	}

	// Apply degrade_policy tier ordering to the fallback chain when set.
	// This re-sorts remaining candidates so that, e.g., external_strong providers
	// appear before router providers before local — regardless of raw score order.
	if r.policy != nil && len(r.policy.DegradePolicy) > 0 {
		sort.SliceStable(remaining, func(i, j int) bool {
			pi, oki := r.registry.Get(remaining[i].Provider)
			pj, okj := r.registry.Get(remaining[j].Provider)
			if !oki || !okj {
				return oki // known provider before unknown
			}
			ti := r.degradeTierIndex(pi)
			tj := r.degradeTierIndex(pj)
			return ti < tj
		})
	}

	chain := make([]string, 0, maxChain)
	primaryKey := primary
	seen := map[string]bool{primaryKey: true}

	for _, rp := range remaining {
		if len(chain) >= maxChain {
			break
		}
		key := rp.Provider
		if rp.Model != "" {
			key = rp.Provider + "/" + rp.Model
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		chain = append(chain, rp.Provider)
	}

	return chain
}

// preferenceBoostFor returns a score boost for a provider based on its position
// in the global policy's preference list for the given role.
// The boost is designed to override typical cost-efficiency differences between provider kinds:
//   - Position 0 → +0.10 (first preferred: highest boost)
//   - Each subsequent position → -0.03 (e.g. position 1 → +0.07, position 2 → +0.04)
//   - Providers not in the list receive 0.
//
// This magnitude ensures that preference ordering wins over minor scoring differences
// (e.g. CostLocal=1.0 vs CostFree=0.95) while not overriding significant score gaps.
func (r *Router) preferenceBoostFor(providerName, role string) float64 {
	if r.policy == nil || len(r.policy.RolePreferences) == 0 {
		return 0
	}
	prefs, ok := r.policy.RolePreferences[role]
	if !ok || len(prefs) == 0 {
		return 0
	}
	for i, name := range prefs {
		if name == providerName {
			boost := 0.10 - float64(i)*0.03
			if boost < 0 {
				boost = 0
			}
			return boost
		}
	}
	return 0
}

// degradeTierIndex returns the degrade_policy tier index for a provider.
// Lower index = higher priority in fallback chain.
// Provider kinds are mapped: (external_strong|external_fast) → cloud, router → router, local → local.
// Returns len(DegradePolicy) for providers not matching any tier (sort them last).
func (r *Router) degradeTierIndex(p Provider) int {
	if r.policy == nil || len(r.policy.DegradePolicy) == 0 {
		return 0
	}
	for i, tier := range r.policy.DegradePolicy {
		switch tier {
		case "external_strong", "external_fast":
			if p.Kind == KindCloud {
				return i
			}
		case "router":
			if p.Kind == KindRouter {
				return i
			}
		case "local":
			if p.Kind == KindLocal {
				return i
			}
		}
	}
	return len(r.policy.DegradePolicy) // not found → last
}

func (r *Router) recordDecision(ctx context.Context, input RoutingInput, decision RoutingDecision) {
	r.mu.Lock()
	defer r.mu.Unlock()

	record := RoutingRecord{
		ID:               uuid.New().String(),
		GoalType:         input.GoalType,
		TaskType:         input.TaskType,
		SelectedProvider: decision.SelectedProvider,
		SelectedModel:    decision.SelectedModel,
		FallbackChain:    decision.FallbackChain,
		Reason:           decision.Reason,
		CreatedAt:        time.Now(),
	}

	r.recentDecisions = append(r.recentDecisions, record)
	if len(r.recentDecisions) > MaxRecentDecisions {
		r.recentDecisions = r.recentDecisions[len(r.recentDecisions)-MaxRecentDecisions:]
	}
}

func (r *Router) emitAudit(ctx context.Context, eventType string, input RoutingInput, decision RoutingDecision) {
	if r.auditor == nil {
		return
	}

	payload := map[string]any{
		"goal_type":         input.GoalType,
		"task_type":         input.TaskType,
		"preferred_role":    input.PreferredRole,
		"estimated_tokens":  input.EstimatedTokens,
		"allow_external":    input.AllowExternal,
		"selected_provider": decision.SelectedProvider,
		"selected_model":    decision.SelectedModel,
		"fallback_chain":    decision.FallbackChain,
		"reason":            decision.Reason,
	}

	if len(decision.Trace.RejectedProviders) > 0 {
		rejected := make([]map[string]string, 0, len(decision.Trace.RejectedProviders))
		for _, rp := range decision.Trace.RejectedProviders {
			rejected = append(rejected, map[string]string{
				"provider": rp.Provider,
				"reason":   rp.Reason,
			})
		}
		payload["rejected_providers"] = rejected
	}

	_ = r.auditor.RecordEvent(ctx, "provider_routing", uuid.Nil, eventType, "system", "provider_router", payload)
}

func (r *Router) emitQuotaExceeded(ctx context.Context, provider string, input RoutingInput, reason string) {
	if r.auditor == nil {
		return
	}

	_ = r.auditor.RecordEvent(ctx, "provider_routing", uuid.Nil, "provider.quota_exceeded", "system", "provider_router", map[string]any{
		"provider":         provider,
		"goal_type":        input.GoalType,
		"task_type":        input.TaskType,
		"estimated_tokens": input.EstimatedTokens,
		"reason":           reason,
	})
}
