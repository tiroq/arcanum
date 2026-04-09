package provider_catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Global policy loading ---

func TestLoadGlobalPolicy_ReturnsNilWhenMissing(t *testing.T) {
	dir := t.TempDir()
	policy, err := LoadGlobalPolicy(dir, nil)
	require.NoError(t, err)
	assert.Nil(t, policy, "should fail-open when _global.yaml is absent")
}

func TestLoadGlobalPolicy_ParsesFullPolicy(t *testing.T) {
	dir := t.TempDir()
	content := `
routing_policy:
  prefer_free: true
  allow_external: true
  max_fallback_chain: 3
  priorities:
    fast:
      prefer: [groq, gemini, ollama]
    planner:
      prefer: [cerebras, sambanova, gemini, openrouter]
    reviewer:
      prefer: [groq, gemini, cerebras]
    fallback:
      prefer: [openrouter, ollama]
  constraints:
    latency_sensitive_threshold_ms: 300
    heavy_task_tokens_threshold: 20000
  degrade_policy:
    - external_strong
    - external_fast
    - router
    - local
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "_global.yaml"), []byte(content), 0600))

	policy, err := LoadGlobalPolicy(dir, nil)
	require.NoError(t, err)
	require.NotNil(t, policy)

	rp := policy.RoutingPolicy
	assert.True(t, rp.PreferFree)
	assert.True(t, rp.AllowExternal)
	assert.Equal(t, 3, rp.MaxFallbackChain)
	assert.Equal(t, []string{"groq", "gemini", "ollama"}, rp.Priorities["fast"].Prefer)
	assert.Equal(t, []string{"cerebras", "sambanova", "gemini", "openrouter"}, rp.Priorities["planner"].Prefer)
	assert.Equal(t, []string{"groq", "gemini", "cerebras"}, rp.Priorities["reviewer"].Prefer)
	assert.Equal(t, []string{"openrouter", "ollama"}, rp.Priorities["fallback"].Prefer)
	assert.Equal(t, 300, rp.Constraints.LatencySensitiveThresholdMs)
	assert.Equal(t, 20000, rp.Constraints.HeavyTaskTokensThreshold)
	assert.Equal(t, []string{"external_strong", "external_fast", "router", "local"}, rp.DegradePolicy)
}

func TestLoadGlobalPolicy_AllowExternalFalse(t *testing.T) {
	dir := t.TempDir()
	content := `
routing_policy:
  allow_external: false
  max_fallback_chain: 1
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "_global.yaml"), []byte(content), 0600))

	policy, err := LoadGlobalPolicy(dir, nil)
	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.False(t, policy.RoutingPolicy.AllowExternal)
	assert.Equal(t, 1, policy.RoutingPolicy.MaxFallbackChain)
}

func TestLoadGlobalPolicy_InvalidYAMLReturnsError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "_global.yaml"), []byte("\t{unclosed: yaml"), 0600))

	_, err := LoadGlobalPolicy(dir, nil)
	require.Error(t, err, "invalid YAML should return an error")
}

func TestLoadGlobalPolicy_EmptyFileReturnsEmptyPolicy(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "_global.yaml"), []byte(""), 0600))

	policy, err := LoadGlobalPolicy(dir, nil)
	require.NoError(t, err)
	require.NotNil(t, policy, "empty file should parse to zero-value policy")
	assert.False(t, policy.RoutingPolicy.AllowExternal)
	assert.Equal(t, 0, policy.RoutingPolicy.MaxFallbackChain)
}

// --- Catalog loader skips underscore-prefixed files ---

func TestLoadCatalog_SkipsUnderscorePrefixedFiles(t *testing.T) {
	dir := t.TempDir()

	// Write a valid provider catalog file.
	validProvider := `
provider:
  name: test-provider
  kind: local
  enabled: true
models:
  - name: test-model
    enabled: true
    cost_class: local
    relative_cost: 0.0
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.yaml"), []byte(validProvider), 0600))

	// Write the global policy file (underscore-prefixed — should be skipped).
	globalContent := `
routing_policy:
  prefer_free: true
  allow_external: true
  max_fallback_chain: 3
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "_global.yaml"), []byte(globalContent), 0600))

	catalogs, err := LoadCatalog(dir, nil)
	require.NoError(t, err)

	// Only the valid provider should be loaded; _global.yaml must be skipped.
	assert.Len(t, catalogs, 1, "_global.yaml must be skipped by LoadCatalog")
	assert.Equal(t, "test-provider", catalogs[0].Provider.Name)
}

func TestLoadCatalog_SkipsAllUnderscorePrefixedFiles(t *testing.T) {
	dir := t.TempDir()

	// Write multiple underscore-prefixed files.
	for _, name := range []string{"_global.yaml", "_defaults.yaml", "_meta.yaml"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("anything: true"), 0600))
	}

	catalogs, err := LoadCatalog(dir, nil)
	require.NoError(t, err)
	assert.Empty(t, catalogs, "all underscore-prefixed files must be skipped")
}
