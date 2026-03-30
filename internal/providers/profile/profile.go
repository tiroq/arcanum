package profile

import (
	"github.com/tiroq/arcanum/internal/providers"
)

// RoleProfiles maps each ModelRole to its ordered candidate chain.
type RoleProfiles map[providers.ModelRole][]ModelCandidate

// ResolveFromConfig builds RoleProfiles from profile DSL strings and legacy model names.
//
// For each role, the resolution order is:
//  1. If a profile DSL string is provided (non-empty), parse it.
//  2. Else if a legacy single model name is provided, wrap it as a single candidate.
//  3. Else fall back to the default model as a single candidate.
func ResolveFromConfig(
	defaultModel string,
	fastModel string,
	plannerModel string,
	reviewModel string,
	defaultProfile string,
	fastProfile string,
	plannerProfile string,
	reviewProfile string,
) (RoleProfiles, error) {
	rp := make(RoleProfiles, 4)

	resolvers := []struct {
		role        providers.ModelRole
		profileDSL  string
		legacyModel string
	}{
		{providers.RoleDefault, defaultProfile, defaultModel},
		{providers.RoleFast, fastProfile, fastModel},
		{providers.RolePlanner, plannerProfile, plannerModel},
		{providers.RoleReview, reviewProfile, reviewModel},
	}

	for _, r := range resolvers {
		candidates, err := resolveRole(r.profileDSL, r.legacyModel, defaultModel)
		if err != nil {
			return nil, err
		}
		rp[r.role] = candidates
	}

	return rp, nil
}

func resolveRole(profileDSL, legacyModel, defaultModel string) ([]ModelCandidate, error) {
	if profileDSL != "" {
		return ParseProfile(profileDSL)
	}
	model := legacyModel
	if model == "" {
		model = defaultModel
	}
	return []ModelCandidate{{ModelName: model}}, nil
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
