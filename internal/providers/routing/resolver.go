package routing

import (
	"fmt"

	"github.com/tiroq/arcanum/internal/providers"
	"github.com/tiroq/arcanum/internal/providers/profile"
)

// Input holds all data the policy resolver needs to build execution profiles.
type Input struct {
	// Policy is the routing policy to apply for all roles.
	Policy RoutingPolicy

	// Local model names per role. Used as a single-model local candidate when
	// CatalogLocalCandidates is not set for the role.
	// Empty strings fall back to LocalDefaultModel.
	LocalDefaultModel string
	LocalFastModel    string
	LocalPlannerModel string
	LocalReviewModel  string

	// CloudEnabled reports whether Ollama Cloud is configured and enabled.
	CloudEnabled bool
	// CloudModel is the model to request from Ollama Cloud when escalating.
	// If empty, the resolver uses the role's local model (same model family, cloud tier).
	CloudModel string

	// OpenRouterEnabled reports whether OpenRouter is configured and enabled.
	OpenRouterEnabled bool
	// OpenRouterModel is the model name to request from OpenRouter when escalating.
	// Must be non-empty when OpenRouter escalation is required and OpenRouterEnabled is true.
	OpenRouterModel string

	// CatalogLocalCandidates provides pre-built local execution candidates per role,
	// loaded from the provider catalog (providers/<name>.yaml execution_profiles section).
	// When set for a role, these candidates replace the single-model LocalFastModel /
	// LocalPlannerModel / etc. approach for the local tier.
	// Cloud and OpenRouter escalation candidates are still appended per escalation policy.
	CatalogLocalCandidates map[providers.ModelRole][]profile.ModelCandidate
}

// RouteDecision records the routing decision made for a single model role.
// It is emitted at worker startup so operators have full visibility into which
// provider and model will be used for each role.
type RouteDecision struct {
	// Role is the model execution role this decision applies to.
	Role string

	// EscalationLevel is the policy escalation applied.
	// Empty when ProfileSource is "catalog".
	EscalationLevel EscalationLevel

	// Candidates is the resolved ordered candidate chain — primary first, fallbacks after.
	Candidates []profile.ModelCandidate

	// AvailableProviders lists the provider names included in the candidate chain.
	AvailableProviders []string

	// SkippedProviders lists provider names that were excluded because they are disabled.
	// Providers simply not in the escalation path are not listed here.
	SkippedProviders []string

	// ProfileSource is "catalog" when local candidates were loaded from the provider catalog,
	// or "policy" when the routing policy built the candidate chain.
	ProfileSource string

	// Justification is a human-readable description of why this route was chosen.
	// Suitable for structured log fields and operator runbooks.
	Justification string
}

// ResolveProfiles builds a RoleProfiles from catalog-provided local candidates (higher priority)
// and routing policy (applied when no catalog candidates are provided for a role).
//
// Catalog candidates come from the provider catalog's execution_profiles section and define
// per-role model candidate chains with execution settings (think mode, timeout, JSON output).
// Cloud and OpenRouter escalation candidates are appended per the routing policy.
//
// Returns:
//   - RoleProfiles keyed by role, ready to pass into the execution engine.
//   - A RouteDecision per role for structured startup logging / audit.
//   - An error if configuration is invalid (e.g. OpenRouter model missing when required).
func ResolveProfiles(input Input) (profile.RoleProfiles, []RouteDecision, error) {
	if input.LocalDefaultModel == "" {
		return nil, nil, fmt.Errorf("routing: LocalDefaultModel must not be empty")
	}

	rp := make(profile.RoleProfiles, 4)
	decisions := make([]RouteDecision, 0, 4)

	for _, role := range []providers.ModelRole{
		providers.RoleDefault,
		providers.RoleFast,
		providers.RolePlanner,
		providers.RoleReview,
	} {
		decision, err := resolveRole(input, role)
		if err != nil {
			return nil, nil, fmt.Errorf("routing policy for role %q: %w", role, err)
		}
		rp[role] = decision.Candidates
		decisions = append(decisions, decision)
	}

	return rp, decisions, nil
}

// resolveRole returns the RouteDecision for a single role.
func resolveRole(input Input, role providers.ModelRole) (RouteDecision, error) {
	// Catalog-provided local candidates take precedence over single-model config.
	if catCandidates, ok := input.CatalogLocalCandidates[role]; ok && len(catCandidates) > 0 {
		candidates := make([]profile.ModelCandidate, len(catCandidates))
		copy(candidates, catCandidates)

		available := []string{"ollama"}
		var skipped []string

		// Still apply escalation policy to append cloud/openrouter candidates.
		rp := policyForRole(input.Policy, role)

		if rp.Escalation.allowsCloud() {
			if input.CloudEnabled {
				cloudModel := input.CloudModel
				if cloudModel == "" && len(catCandidates) > 0 {
					cloudModel = catCandidates[0].ModelName
				}
				candidates = append(candidates, profile.ModelCandidate{
					ModelName:    cloudModel,
					ProviderName: "ollama-cloud",
				})
				available = append(available, "ollama-cloud")
			} else {
				skipped = append(skipped, "ollama-cloud (disabled)")
			}
		}

		if rp.Escalation.allowsOpenRouter() {
			if input.OpenRouterEnabled {
				if input.OpenRouterModel == "" {
					return RouteDecision{}, fmt.Errorf(
						"role %q allows OpenRouter escalation but no model is configured"+
							" (set ROUTING_OPENROUTER_MODEL or OPENROUTER_DEFAULT_MODEL)",
						role,
					)
				}
				candidates = append(candidates, profile.ModelCandidate{
					ModelName:    input.OpenRouterModel,
					ProviderName: "openrouter",
				})
				available = append(available, "openrouter")
			} else {
				skipped = append(skipped, "openrouter (disabled)")
			}
		}

		return RouteDecision{
			Role:               string(role),
			EscalationLevel:    rp.Escalation,
			Candidates:         candidates,
			AvailableProviders: available,
			SkippedProviders:   skipped,
			ProfileSource:      "catalog",
			Justification: fmt.Sprintf(
				"catalog profile applied: %d local candidates, escalation=%s",
				len(catCandidates), rp.Escalation,
			),
		}, nil
	}

	// Build candidate chain from routing policy (single-model fallback).
	rp := policyForRole(input.Policy, role)
	localModel := localModelForRole(input, role)

	var candidates []profile.ModelCandidate
	var available, skipped []string

	// Local tier — always the primary candidate, never skipped.
	candidates = append(candidates, profile.ModelCandidate{
		ModelName:    localModel,
		ProviderName: "ollama",
	})
	available = append(available, "ollama")

	// Cloud tier (Ollama Cloud) — only when the escalation level allows it.
	if rp.Escalation.allowsCloud() {
		if input.CloudEnabled {
			cloudModel := input.CloudModel
			if cloudModel == "" {
				cloudModel = localModel // same model family, different tier
			}
			candidates = append(candidates, profile.ModelCandidate{
				ModelName:    cloudModel,
				ProviderName: "ollama-cloud",
			})
			available = append(available, "ollama-cloud")
		} else {
			skipped = append(skipped, "ollama-cloud (disabled)")
		}
	}

	// OpenRouter tier — only when the escalation level allows it.
	if rp.Escalation.allowsOpenRouter() {
		if input.OpenRouterEnabled {
			if input.OpenRouterModel == "" {
				return RouteDecision{}, fmt.Errorf(
					"role %q allows OpenRouter escalation but no model is configured"+
						" (set ROUTING_OPENROUTER_MODEL or OPENROUTER_DEFAULT_MODEL)",
					role,
				)
			}
			candidates = append(candidates, profile.ModelCandidate{
				ModelName:    input.OpenRouterModel,
				ProviderName: "openrouter",
			})
			available = append(available, "openrouter")
		} else {
			skipped = append(skipped, "openrouter (disabled)")
		}
	}

	return RouteDecision{
		Role:               string(role),
		EscalationLevel:    rp.Escalation,
		Candidates:         candidates,
		AvailableProviders: available,
		SkippedProviders:   skipped,
		ProfileSource:      "policy",
		Justification: fmt.Sprintf(
			"policy applied: escalation=%s, local_model=%s, candidates=%d",
			rp.Escalation, localModel, len(candidates),
		),
	}, nil
}

func policyForRole(policy RoutingPolicy, role providers.ModelRole) RolePolicy {
	switch role {
	case providers.RoleFast:
		return policy.Fast
	case providers.RolePlanner:
		return policy.Planner
	case providers.RoleReview:
		return policy.Review
	default:
		return policy.Default
	}
}

func localModelForRole(input Input, role providers.ModelRole) string {
	var model string
	switch role {
	case providers.RoleFast:
		model = input.LocalFastModel
	case providers.RolePlanner:
		model = input.LocalPlannerModel
	case providers.RoleReview:
		model = input.LocalReviewModel
	}
	if model == "" {
		return input.LocalDefaultModel
	}
	return model
}
