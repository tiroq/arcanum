package profile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tiroq/arcanum/internal/providers"
)

func TestResolveFromConfig_AllDefaults(t *testing.T) {
	rp, err := ResolveFromConfig(
		"llama3.2", "", "", "",
		"", "", "", "",
	)
	require.NoError(t, err)

	for _, role := range providers.ValidModelRoles {
		candidates := rp.CandidatesForRole(role)
		require.Len(t, candidates, 1, "role %s", role)
		assert.Equal(t, "llama3.2", candidates[0].ModelName)
	}
}

func TestResolveFromConfig_LegacyModels(t *testing.T) {
	rp, err := ResolveFromConfig(
		"qwen2.5:7b-instruct", "llama3.2:3b", "qwen2.5:14b-instruct", "qwen2.5:7b-instruct",
		"", "", "", "",
	)
	require.NoError(t, err)

	assert.Equal(t, "qwen2.5:7b-instruct", rp.CandidatesForRole(providers.RoleDefault)[0].ModelName)
	assert.Equal(t, "llama3.2:3b", rp.CandidatesForRole(providers.RoleFast)[0].ModelName)
	assert.Equal(t, "qwen2.5:14b-instruct", rp.CandidatesForRole(providers.RolePlanner)[0].ModelName)
	assert.Equal(t, "qwen2.5:7b-instruct", rp.CandidatesForRole(providers.RoleReview)[0].ModelName)
}

func TestResolveFromConfig_ProfileOverridesLegacy(t *testing.T) {
	rp, err := ResolveFromConfig(
		"qwen2.5:7b-instruct", "llama3.2:3b", "", "",
		"", "model-a?think=thinking|model-b?think=nothinking", "", "",
	)
	require.NoError(t, err)

	fast := rp.CandidatesForRole(providers.RoleFast)
	require.Len(t, fast, 2)
	assert.Equal(t, "model-a", fast[0].ModelName)
	assert.Equal(t, ThinkEnabled, fast[0].ThinkMode)
	assert.Equal(t, "model-b", fast[1].ModelName)
	assert.Equal(t, ThinkDisabled, fast[1].ThinkMode)
}

func TestResolveFromConfig_InvalidProfile(t *testing.T) {
	_, err := ResolveFromConfig(
		"qwen2.5:7b-instruct", "", "", "",
		"", "model?think=INVALID", "", "",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid think mode")
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

func TestResolveFromConfig_MixedLegacyAndProfile(t *testing.T) {
	rp, err := ResolveFromConfig(
		"base-model", "fast-legacy", "planner-legacy", "",
		"base-model?think=thinking&timeout=300", "", "planner-a?timeout=240|planner-b", "",
	)
	require.NoError(t, err)

	defaultC := rp.CandidatesForRole(providers.RoleDefault)
	require.Len(t, defaultC, 1)
	assert.Equal(t, "base-model", defaultC[0].ModelName)
	assert.Equal(t, ThinkEnabled, defaultC[0].ThinkMode)

	fastC := rp.CandidatesForRole(providers.RoleFast)
	require.Len(t, fastC, 1)
	assert.Equal(t, "fast-legacy", fastC[0].ModelName)

	plannerC := rp.CandidatesForRole(providers.RolePlanner)
	require.Len(t, plannerC, 2)
	assert.Equal(t, "planner-a", plannerC[0].ModelName)
	assert.Equal(t, "planner-b", plannerC[1].ModelName)

	reviewC := rp.CandidatesForRole(providers.RoleReview)
	require.Len(t, reviewC, 1)
	assert.Equal(t, "base-model", reviewC[0].ModelName)
}
