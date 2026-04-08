package provider_catalog

import (
	"fmt"
	"sort"
)

// ResolverInput describes what the system needs from a provider+model target.
type ResolverInput struct {
	PreferredRole        string   `json:"preferred_role"`
	RequiredCapabilities []string `json:"required_capabilities"`
	AllowExternal        bool     `json:"allow_external"`
}

// RejectedModel records why a provider+model candidate was filtered out.
type RejectedModel struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Reason   string `json:"reason"`
}

// ResolveCandidates filters the catalog registry to return eligible
// provider+model candidates for a routing request.
// Results are returned in deterministic order (provider ASC, model ASC).
func ResolveCandidates(registry *CatalogRegistry, input ResolverInput) ([]ProviderModel, []RejectedModel) {
	if registry == nil {
		return nil, nil
	}

	all := registry.All()
	var candidates []ProviderModel
	var rejected []RejectedModel

	for _, pm := range all {
		if reason, ok := filterModel(pm, input); !ok {
			rejected = append(rejected, RejectedModel{
				Provider: pm.ProviderName,
				Model:    pm.ModelName,
				Reason:   reason,
			})
			continue
		}
		candidates = append(candidates, pm)
	}

	// Deterministic sort: provider ASC, model ASC.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].ProviderName != candidates[j].ProviderName {
			return candidates[i].ProviderName < candidates[j].ProviderName
		}
		return candidates[i].ModelName < candidates[j].ModelName
	})

	return candidates, rejected
}

// filterModel checks a single provider+model candidate against all filtering rules.
func filterModel(pm ProviderModel, input ResolverInput) (string, bool) {
	// Check: enabled.
	if !pm.Enabled {
		return "model disabled", false
	}

	// Check: external allowed.
	if !input.AllowExternal && pm.IsExternal() {
		return "external providers not allowed", false
	}

	// Check: role compatibility.
	if input.PreferredRole != "" {
		if !pm.HasRole(input.PreferredRole) && !pm.HasRole("fallback") {
			return fmt.Sprintf("does not have role %q", input.PreferredRole), false
		}
	}

	// Check: required capabilities.
	for _, req := range input.RequiredCapabilities {
		if !pm.HasCapability(req) {
			return fmt.Sprintf("missing capability %q", req), false
		}
	}

	return "", true
}

// ComputeModelCapabilityFit returns a [0,1] score based on how well
// a model's capabilities match the required capabilities.
//   - exact match (all required capabilities present) → 1.0
//   - partial match (some present) → fraction matched
//   - no required capabilities → 1.0 (neutral)
//   - no capabilities match → 0.0
func ComputeModelCapabilityFit(pm ProviderModel, requiredCapabilities []string) float64 {
	if len(requiredCapabilities) == 0 {
		return 1.0 // no requirements → neutral
	}

	matched := 0
	for _, req := range requiredCapabilities {
		if pm.HasCapability(req) {
			matched++
		}
	}

	return float64(matched) / float64(len(requiredCapabilities))
}
