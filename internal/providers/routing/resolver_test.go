package routing_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tiroq/arcanum/internal/providers"
	"github.com/tiroq/arcanum/internal/providers/routing"
)

// ── ParseEscalationLevel ──────────────────────────────────────────────────────

func TestParseEscalationLevel_ValidValues(t *testing.T) {
	cases := []struct {
		input    string
		expected routing.EscalationLevel
	}{
		{"local_only", routing.EscalationLocalOnly},
		{"LOCAL_ONLY", routing.EscalationLocalOnly}, // case-insensitive
		{"local", routing.EscalationLocalOnly},      // alias
		{"local_cloud", routing.EscalationLocalCloud},
		{"LOCAL_CLOUD", routing.EscalationLocalCloud},
		{"local_cloud_openrouter", routing.EscalationLocalCloudOpenRouter},
		{"full", routing.EscalationLocalCloudOpenRouter}, // alias
		{"local_openrouter", routing.EscalationLocalOpenRouter},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := routing.ParseEscalationLevel(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestParseEscalationLevel_InvalidValue(t *testing.T) {
	_, err := routing.ParseEscalationLevel("cloud_only")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown escalation level")
}

// ── NewRoutingPolicy ─────────────────────────────────────────────────────────

func TestNewRoutingPolicy_Valid(t *testing.T) {
	p, err := routing.NewRoutingPolicy("local_only", "local_only", "local_cloud", "local_cloud")
	require.NoError(t, err)
	assert.Equal(t, routing.EscalationLocalOnly, p.Fast.Escalation)
	assert.Equal(t, routing.EscalationLocalOnly, p.Default.Escalation)
	assert.Equal(t, routing.EscalationLocalCloud, p.Planner.Escalation)
	assert.Equal(t, routing.EscalationLocalCloud, p.Review.Escalation)
}

func TestNewRoutingPolicy_InvalidFast(t *testing.T) {
	_, err := routing.NewRoutingPolicy("bad_level", "local_only", "local_cloud", "local_cloud")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fast escalation")
}

func TestNewRoutingPolicy_InvalidPlanner(t *testing.T) {
	_, err := routing.NewRoutingPolicy("local_only", "local_only", "INVALID", "local_cloud")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "planner escalation")
}

// ── ResolveProfiles — error cases ─────────────────────────────────────────────

func TestResolveProfiles_EmptyLocalDefaultModel(t *testing.T) {
	policy, _ := routing.NewRoutingPolicy("local_only", "local_only", "local_only", "local_only")
	_, _, err := routing.ResolveProfiles(routing.Input{
		Policy:            policy,
		LocalDefaultModel: "", // intentionally empty
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LocalDefaultModel")
}

func TestResolveProfiles_OpenRouterMissingModel_WhenEnabled(t *testing.T) {
	policy, _ := routing.NewRoutingPolicy("local_only", "local_only", "local_cloud_openrouter", "local_only")
	_, _, err := routing.ResolveProfiles(routing.Input{
		Policy:            policy,
		LocalDefaultModel: "llama3.2",
		OpenRouterEnabled: true,
		OpenRouterModel:   "", // intentionally empty
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "planner")
	assert.Contains(t, err.Error(), "OpenRouter")
}

// ── ResolveProfiles — DSL override ───────────────────────────────────────────

func TestResolveProfiles_DSLOverrideWins(t *testing.T) {
	// Even with planner policy set to local_cloud, a DSL override takes precedence.
	policy, _ := routing.NewRoutingPolicy("local_only", "local_only", "local_cloud", "local_cloud")

	rp, decisions, err := routing.ResolveProfiles(routing.Input{
		Policy:            policy,
		LocalDefaultModel: "llama3.2",
		CloudEnabled:      true,
		CloudModel:        "llama3.2",
		DSLOverrides: map[providers.ModelRole]string{
			providers.RolePlanner: "qwen2.5:14b-instruct?provider=ollama&timeout=300",
		},
	})
	require.NoError(t, err)

	plannerCandidates := rp.CandidatesForRole(providers.RolePlanner)
	require.Len(t, plannerCandidates, 1, "DSL override should produce exactly 1 candidate")
	assert.Equal(t, "qwen2.5:14b-instruct", plannerCandidates[0].ModelName)
	assert.Equal(t, "ollama", plannerCandidates[0].ProviderName)

	var plannerDecision *routing.RouteDecision
	for i := range decisions {
		if decisions[i].Role == "planner" {
			plannerDecision = &decisions[i]
			break
		}
	}
	require.NotNil(t, plannerDecision)
	assert.Equal(t, "dsl_override", plannerDecision.ProfileSource)
}

// ── ResolveProfiles — local-only roles ───────────────────────────────────────

func TestResolveProfiles_FastRole_LocalOnly_StaysLocal(t *testing.T) {
	// Fast role with local_only policy should produce exactly 1 candidate
	// even when cloud and OpenRouter are both enabled.
	policy, _ := routing.NewRoutingPolicy(
		"local_only",  // fast
		"local_only",  // default
		"local_cloud", // planner
		"local_cloud", // review
	)
	rp, decisions, err := routing.ResolveProfiles(routing.Input{
		Policy:            policy,
		LocalDefaultModel: "llama3.2",
		LocalFastModel:    "llama3.2:3b",
		CloudEnabled:      true,
		CloudModel:        "llama3.2",
		OpenRouterEnabled: true,
		OpenRouterModel:   "openai/gpt-4o-mini",
	})
	require.NoError(t, err)

	fastCandidates := rp.CandidatesForRole(providers.RoleFast)
	require.Len(t, fastCandidates, 1)
	assert.Equal(t, "llama3.2:3b", fastCandidates[0].ModelName)
	assert.Equal(t, "ollama", fastCandidates[0].ProviderName)

	var fastDecision *routing.RouteDecision
	for i := range decisions {
		if decisions[i].Role == "fast" {
			fastDecision = &decisions[i]
			break
		}
	}
	require.NotNil(t, fastDecision)
	assert.Equal(t, routing.EscalationLocalOnly, fastDecision.EscalationLevel)
	assert.Equal(t, "policy", fastDecision.ProfileSource)
	assert.Equal(t, []string{"ollama"}, fastDecision.AvailableProviders)
	assert.Empty(t, fastDecision.SkippedProviders, "local_only should not list cloud as skipped — it is not in the escalation path")
}

func TestResolveProfiles_DefaultRole_LocalOnly_StaysLocal(t *testing.T) {
	policy, _ := routing.NewRoutingPolicy("local_only", "local_only", "local_cloud", "local_cloud")
	rp, _, err := routing.ResolveProfiles(routing.Input{
		Policy:            policy,
		LocalDefaultModel: "llama3.2",
		CloudEnabled:      true,
		CloudModel:        "llama3.2",
	})
	require.NoError(t, err)

	defaultCandidates := rp.CandidatesForRole(providers.RoleDefault)
	require.Len(t, defaultCandidates, 1)
	assert.Equal(t, "llama3.2", defaultCandidates[0].ModelName)
	assert.Equal(t, "ollama", defaultCandidates[0].ProviderName)
}

// ── ResolveProfiles — cloud escalation ───────────────────────────────────────

func TestResolveProfiles_PlannerRole_LocalCloud_CloudEnabled(t *testing.T) {
	policy, _ := routing.NewRoutingPolicy("local_only", "local_only", "local_cloud", "local_cloud")
	rp, decisions, err := routing.ResolveProfiles(routing.Input{
		Policy:            policy,
		LocalDefaultModel: "qwen2.5:7b-instruct",
		LocalPlannerModel: "qwen2.5:14b-instruct",
		CloudEnabled:      true,
		CloudModel:        "qwen2.5:14b-instruct",
	})
	require.NoError(t, err)

	candidates := rp.CandidatesForRole(providers.RolePlanner)
	require.Len(t, candidates, 2)
	assert.Equal(t, "qwen2.5:14b-instruct", candidates[0].ModelName)
	assert.Equal(t, "ollama", candidates[0].ProviderName)
	assert.Equal(t, "qwen2.5:14b-instruct", candidates[1].ModelName)
	assert.Equal(t, "ollama-cloud", candidates[1].ProviderName)

	var d *routing.RouteDecision
	for i := range decisions {
		if decisions[i].Role == "planner" {
			d = &decisions[i]
			break
		}
	}
	require.NotNil(t, d)
	assert.Equal(t, routing.EscalationLocalCloud, d.EscalationLevel)
	assert.Equal(t, []string{"ollama", "ollama-cloud"}, d.AvailableProviders)
	assert.Empty(t, d.SkippedProviders)
}

func TestResolveProfiles_PlannerRole_LocalCloud_CloudDisabled_GracefulDegradation(t *testing.T) {
	// When cloud is disabled, policy gracefully degrades to local-only — no error.
	policy, _ := routing.NewRoutingPolicy("local_only", "local_only", "local_cloud", "local_cloud")
	rp, decisions, err := routing.ResolveProfiles(routing.Input{
		Policy:            policy,
		LocalDefaultModel: "qwen2.5:7b-instruct",
		CloudEnabled:      false,
	})
	require.NoError(t, err)

	candidates := rp.CandidatesForRole(providers.RolePlanner)
	require.Len(t, candidates, 1, "cloud disabled: should degrade to local-only without error")
	assert.Equal(t, "ollama", candidates[0].ProviderName)

	var d *routing.RouteDecision
	for i := range decisions {
		if decisions[i].Role == "planner" {
			d = &decisions[i]
			break
		}
	}
	require.NotNil(t, d)
	assert.Contains(t, d.SkippedProviders, "ollama-cloud (disabled)")
}

// ── ResolveProfiles — full escalation ────────────────────────────────────────

func TestResolveProfiles_ReviewRole_FullEscalation_AllEnabled(t *testing.T) {
	policy, _ := routing.NewRoutingPolicy("local_only", "local_only", "local_cloud", "local_cloud_openrouter")
	rp, decisions, err := routing.ResolveProfiles(routing.Input{
		Policy:            policy,
		LocalDefaultModel: "qwen2.5:7b-instruct",
		CloudEnabled:      true,
		CloudModel:        "qwen2.5:7b-instruct",
		OpenRouterEnabled: true,
		OpenRouterModel:   "openai/gpt-4o-mini",
	})
	require.NoError(t, err)

	candidates := rp.CandidatesForRole(providers.RoleReview)
	require.Len(t, candidates, 3)
	assert.Equal(t, "ollama", candidates[0].ProviderName)
	assert.Equal(t, "ollama-cloud", candidates[1].ProviderName)
	assert.Equal(t, "openrouter", candidates[2].ProviderName)
	assert.Equal(t, "openai/gpt-4o-mini", candidates[2].ModelName)

	var d *routing.RouteDecision
	for i := range decisions {
		if decisions[i].Role == "review" {
			d = &decisions[i]
			break
		}
	}
	require.NotNil(t, d)
	assert.Equal(t, routing.EscalationLocalCloudOpenRouter, d.EscalationLevel)
	assert.Equal(t, []string{"ollama", "ollama-cloud", "openrouter"}, d.AvailableProviders)
	assert.Empty(t, d.SkippedProviders)
}

// ── ResolveProfiles — local→openrouter (skip cloud) ──────────────────────────

func TestResolveProfiles_LocalOpenRouter_SkipsCloud(t *testing.T) {
	// local_openrouter: cloud is not in the escalation path at all (not "disabled").
	policy, _ := routing.NewRoutingPolicy("local_only", "local_only", "local_openrouter", "local_openrouter")
	rp, decisions, err := routing.ResolveProfiles(routing.Input{
		Policy:            policy,
		LocalDefaultModel: "llama3.2",
		CloudEnabled:      true, // cloud is enabled, but should be ignored for this level
		CloudModel:        "llama3.2",
		OpenRouterEnabled: true,
		OpenRouterModel:   "openai/gpt-4o-mini",
	})
	require.NoError(t, err)

	candidates := rp.CandidatesForRole(providers.RolePlanner)
	require.Len(t, candidates, 2)
	assert.Equal(t, "ollama", candidates[0].ProviderName)
	assert.Equal(t, "openrouter", candidates[1].ProviderName)

	var d *routing.RouteDecision
	for i := range decisions {
		if decisions[i].Role == "planner" {
			d = &decisions[i]
			break
		}
	}
	require.NotNil(t, d)
	// Cloud must NOT appear in either available or skipped — it's not in the escalation path.
	assert.NotContains(t, d.AvailableProviders, "ollama-cloud")
	assert.Empty(t, d.SkippedProviders)
}

// ── ResolveProfiles — OpenRouter disabled ────────────────────────────────────

func TestResolveProfiles_OpenRouterDisabled_NoError(t *testing.T) {
	// OpenRouter not enabled: escalation level mentions OpenRouter, but it's disabled.
	// Should gracefully degrade without error.
	policy, _ := routing.NewRoutingPolicy("local_only", "local_only", "local_cloud_openrouter", "local_cloud_openrouter")
	rp, decisions, err := routing.ResolveProfiles(routing.Input{
		Policy:            policy,
		LocalDefaultModel: "llama3.2",
		CloudEnabled:      true,
		CloudModel:        "llama3.2",
		OpenRouterEnabled: false,
		OpenRouterModel:   "", // empty is fine when disabled
	})
	require.NoError(t, err)

	plannerCandidates := rp.CandidatesForRole(providers.RolePlanner)
	require.Len(t, plannerCandidates, 2, "local + cloud only, OpenRouter disabled")
	assert.Equal(t, "ollama", plannerCandidates[0].ProviderName)
	assert.Equal(t, "ollama-cloud", plannerCandidates[1].ProviderName)

	var d *routing.RouteDecision
	for i := range decisions {
		if decisions[i].Role == "planner" {
			d = &decisions[i]
			break
		}
	}
	require.NotNil(t, d)
	assert.Contains(t, d.SkippedProviders, "openrouter (disabled)")
}

// ── ResolveProfiles — model fallback ─────────────────────────────────────────

func TestResolveProfiles_LocalModelFallsBackToDefault(t *testing.T) {
	// When LocalFastModel is empty, fast role uses LocalDefaultModel.
	policy, _ := routing.NewRoutingPolicy("local_only", "local_only", "local_cloud", "local_cloud")
	rp, _, err := routing.ResolveProfiles(routing.Input{
		Policy:            policy,
		LocalDefaultModel: "qwen2.5:7b-instruct",
		LocalFastModel:    "", // empty — should fall back
	})
	require.NoError(t, err)

	fastCandidates := rp.CandidatesForRole(providers.RoleFast)
	require.Len(t, fastCandidates, 1)
	assert.Equal(t, "qwen2.5:7b-instruct", fastCandidates[0].ModelName, "fast role must fall back to default model")
}

func TestResolveProfiles_CloudModelFallsBackToLocalModel(t *testing.T) {
	// When CloudModel is empty, cloud candidate uses the role's local model.
	policy, _ := routing.NewRoutingPolicy("local_only", "local_only", "local_cloud", "local_cloud")
	rp, _, err := routing.ResolveProfiles(routing.Input{
		Policy:            policy,
		LocalDefaultModel: "qwen2.5:7b-instruct",
		CloudEnabled:      true,
		CloudModel:        "", // empty — should fall back to local model
	})
	require.NoError(t, err)

	plannerCandidates := rp.CandidatesForRole(providers.RolePlanner)
	require.Len(t, plannerCandidates, 2)
	assert.Equal(t, "qwen2.5:7b-instruct", plannerCandidates[1].ModelName, "cloud candidate must use local model when CloudModel is empty")
	assert.Equal(t, "ollama-cloud", plannerCandidates[1].ProviderName)
}

// ── ResolveProfiles — candidate provider names ────────────────────────────────

func TestResolveProfiles_AllCandidatesHaveExplicitProviderName(t *testing.T) {
	// Policy-resolved candidates must always have explicit ProviderName set
	// so execution traces show unambiguous provider attribution.
	policy, _ := routing.NewRoutingPolicy("local_only", "local_cloud", "local_cloud_openrouter", "local_cloud_openrouter")
	rp, _, err := routing.ResolveProfiles(routing.Input{
		Policy:            policy,
		LocalDefaultModel: "llama3.2",
		CloudEnabled:      true,
		CloudModel:        "llama3.2",
		OpenRouterEnabled: true,
		OpenRouterModel:   "openai/gpt-4o-mini",
	})
	require.NoError(t, err)

	for _, role := range []providers.ModelRole{
		providers.RoleDefault,
		providers.RoleFast,
		providers.RolePlanner,
		providers.RoleReview,
	} {
		for _, c := range rp.CandidatesForRole(role) {
			assert.NotEmpty(t, c.ProviderName,
				"role %q candidate %q must have explicit ProviderName for trace attribution",
				role, c.ModelName,
			)
		}
	}
}

// ── ResolveProfiles — all decisions returned ──────────────────────────────────

func TestResolveProfiles_ReturnsDecisionForEveryRole(t *testing.T) {
	policy, _ := routing.NewRoutingPolicy("local_only", "local_only", "local_cloud", "local_cloud")
	_, decisions, err := routing.ResolveProfiles(routing.Input{
		Policy:            policy,
		LocalDefaultModel: "llama3.2",
	})
	require.NoError(t, err)
	require.Len(t, decisions, 4, "must return one RouteDecision per role")

	roles := map[string]bool{}
	for _, d := range decisions {
		roles[d.Role] = true
		assert.NotEmpty(t, d.Justification, "every decision must have a justification")
		assert.NotEmpty(t, d.ProfileSource, "every decision must have a profile source")
	}
	assert.True(t, roles["default"])
	assert.True(t, roles["fast"])
	assert.True(t, roles["planner"])
	assert.True(t, roles["review"])
}
