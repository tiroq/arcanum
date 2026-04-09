package profile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tiroq/arcanum/internal/providers"
)

// ResolveFromConfig is disabled. These tests verify it returns an explicit error.

func TestResolveFromConfig_Disabled(t *testing.T) {
	_, err := ResolveFromConfig(
		"llama3.2", "", "", "",
		"", "", "", "",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ResolveFromConfig is disabled")
}

func TestRoleProfiles_CandidatesForRole_FallbackToDefault(t *testing.T) {
	rp := RoleProfiles{
		providers.RoleDefault: {{ModelName: "default-model"}},
	}

	candidates := rp.CandidatesForRole(providers.RoleFast)
	require.Len(t, candidates, 1)
	assert.Equal(t, "default-model", candidates[0].ModelName)
}

func TestRoleProfiles_CandidatesForRole_EmptyMap(t *testing.T) {
	rp := RoleProfiles{}
	assert.Nil(t, rp.CandidatesForRole(providers.RoleDefault))
}
