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

// Route selects the best provider for the given input and builds a fallback chain.
// This is the main entry point for all provider routing decisions.
func (r *Router) Route(ctx context.Context, input RoutingInput) RoutingDecision {
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
// Iteration 32: operates on provider+model pairs. Same provider with different
// models is allowed; duplicate provider+model pairs are rejected.
func (r *Router) buildFallbackChain(remaining []RankedProvider, primary string, input RoutingInput) []string {
	chain := make([]string, 0, MaxFallbackChainLength)
	primaryKey := primary
	seen := map[string]bool{primaryKey: true}

	for _, rp := range remaining {
		if len(chain) >= MaxFallbackChainLength {
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
