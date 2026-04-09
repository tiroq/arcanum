package profile

import (
	"fmt"

	"github.com/tiroq/arcanum/internal/providers"
)

// RoleProfiles maps each ModelRole to its ordered candidate chain.
type RoleProfiles map[providers.ModelRole][]ModelCandidate

// ResolveFromConfig is disabled. The DSL-based profile resolution has been removed.
// Use provider_catalog.LoadExecutionProfiles() to build role profiles from the
// catalog's execution_profiles section.
func ResolveFromConfig(
	_, _, _, _ string, // legacy model name params
	_, _, _, _ string, // legacy profile DSL params
) (RoleProfiles, error) {
	return nil, fmt.Errorf(
		"ResolveFromConfig is disabled: use provider_catalog.LoadExecutionProfiles() instead — " +
			"define execution settings in providers/<name>.yaml under models[].execution")
}

// CandidatesForRole returns the candidate chain for the given role.
// If no profile is configured for the role, returns the default role's candidates.
func (rp RoleProfiles) CandidatesForRole(role providers.ModelRole) []ModelCandidate {
	if candidates, ok := rp[role]; ok && len(candidates) > 0 {
		return candidates
	}
	if candidates, ok := rp[providers.RoleDefault]; ok {
		return candidates
	}
	return nil
}
