package provider_catalog

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tiroq/arcanum/internal/providers"
	"github.com/tiroq/arcanum/internal/providers/profile"
)

// writeYAML is a helper to write YAML to a temp directory.
func writeExecYAML(t *testing.T, dir, name, content string) {
	t.Helper()
	err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600)
	require.NoError(t, err)
}

// ── LoadExecutionProfiles: success cases ────────────────────────────────────

// TestLoadExecutionProfiles_LoadsAllRoles verifies that a YAML file with a complete
// execution_profiles section produces correct per-role ModelCandidate slices.
// Test requirement 1: worker execution uses provider catalog execution settings.
func TestLoadExecutionProfiles_LoadsAllRoles(t *testing.T) {
	dir := t.TempDir()
	writeExecYAML(t, dir, "ollama.yaml", `
provider:
  name: ollama
  kind: local
  enabled: true
models:
  - name: "qwen3:1.7b"
    enabled: true
    execution:
      think: off
      timeout_seconds: 60
  - name: "qwen3:8b"
    enabled: true
    execution:
      think: on
      timeout_seconds: 240
  - name: "qwen3:review"
    enabled: true
    execution:
      think: off
      json_mode: true
      timeout_seconds: 120
  - name: "qwen3:review-think"
    enabled: true
    execution:
      think: on
      json_mode: true
      timeout_seconds: 180
  - name: "qwen3:default"
    enabled: true
    execution:
      think: off
      timeout_seconds: 120
execution_profiles:
  default:
    - ref: "qwen3:default"
  fast:
    - ref: "qwen3:1.7b"
  planner:
    - ref: "qwen3:8b"
    - ref: "qwen3:1.7b"
  review:
    - ref: "qwen3:review"
    - ref: "qwen3:review-think"
`)

	result, err := LoadExecutionProfiles(dir, "ollama", nil)
	require.NoError(t, err)

	// Default role
	def := result[providers.RoleDefault]
	require.Len(t, def, 1)
	assert.Equal(t, "qwen3:default", def[0].ModelName)
	assert.Equal(t, "ollama", def[0].ProviderName)
	assert.Equal(t, profile.ThinkDisabled, def[0].ThinkMode)
	assert.Equal(t, 120*time.Second, def[0].Timeout)
	assert.False(t, def[0].JSONMode)

	// Fast role
	fast := result[providers.RoleFast]
	require.Len(t, fast, 1)
	assert.Equal(t, "qwen3:1.7b", fast[0].ModelName)
	assert.Equal(t, profile.ThinkDisabled, fast[0].ThinkMode)
	assert.Equal(t, 60*time.Second, fast[0].Timeout)

	// Planner role — 2 candidates with think=on
	planner := result[providers.RolePlanner]
	require.Len(t, planner, 2)
	assert.Equal(t, "qwen3:8b", planner[0].ModelName)
	assert.Equal(t, profile.ThinkEnabled, planner[0].ThinkMode)
	assert.Equal(t, 240*time.Second, planner[0].Timeout)
	assert.Equal(t, "qwen3:1.7b", planner[1].ModelName)
	assert.Equal(t, 60*time.Second, planner[1].Timeout)

	// Review role — 2 candidates with json_mode=true
	review := result[providers.RoleReview]
	require.Len(t, review, 2)
	assert.True(t, review[0].JSONMode)
	assert.True(t, review[1].JSONMode)
	assert.Equal(t, profile.ThinkDisabled, review[0].ThinkMode)
	assert.Equal(t, profile.ThinkEnabled, review[1].ThinkMode)
}

// TestLoadExecutionProfiles_WorkerUsageMatchesCatalog verifies that execution
// settings (think, timeout, json_mode) from the catalog are preserved exactly
// in the returned ModelCandidate structs — no data loss.
// Test requirement 1: execution settings come from catalog, not env vars.
func TestLoadExecutionProfiles_WorkerUsageMatchesCatalog(t *testing.T) {
	dir := t.TempDir()
	writeExecYAML(t, dir, "ollama.yaml", `
provider:
  name: ollama
  kind: local
  enabled: true
models:
  - name: base-model
    enabled: true
    execution:
      timeout_seconds: 90
  - name: fast-model
    enabled: true
    execution:
      think: off
      timeout_seconds: 30
  - name: smart-model
    enabled: true
    execution:
      think: on
      timeout_seconds: 300
  - name: review-model
    enabled: true
    execution:
      json_mode: true
      timeout_seconds: 60
execution_profiles:
  default:
    - ref: base-model
  fast:
    - ref: fast-model
  planner:
    - ref: smart-model
  review:
    - ref: review-model
`)

	result, err := LoadExecutionProfiles(dir, "ollama", nil)
	require.NoError(t, err)
	require.Equal(t, 4, len(result))

	assert.Equal(t, "fast-model", result[providers.RoleFast][0].ModelName)
	assert.Equal(t, 30*time.Second, result[providers.RoleFast][0].Timeout)
	assert.Equal(t, profile.ThinkDisabled, result[providers.RoleFast][0].ThinkMode)

	assert.Equal(t, "smart-model", result[providers.RolePlanner][0].ModelName)
	assert.Equal(t, profile.ThinkEnabled, result[providers.RolePlanner][0].ThinkMode)
	assert.Equal(t, 300*time.Second, result[providers.RolePlanner][0].Timeout)

	assert.True(t, result[providers.RoleReview][0].JSONMode)
}

// TestLoadExecutionProfiles_DefaultThinkIsUnset verifies that an absent "think"
// field in YAML produces ThinkDefault (not ThinkEnabled or ThinkDisabled).
func TestLoadExecutionProfiles_DefaultThinkIsUnset(t *testing.T) {
	dir := t.TempDir()
	writeExecYAML(t, dir, "ollama.yaml", `
provider: {name: ollama, kind: local, enabled: true}
models:
  - name: m1
    enabled: true
    execution:
      timeout_seconds: 60
  - name: m2
    enabled: true
  - name: m3
    enabled: true
  - name: m4
    enabled: true
execution_profiles:
  default: [{ref: m1}]
  fast: [{ref: m2}]
  planner: [{ref: m3}]
  review: [{ref: m4}]
`)

	result, err := LoadExecutionProfiles(dir, "ollama", nil)
	require.NoError(t, err)
	assert.Equal(t, profile.ThinkDefault, result[providers.RoleDefault][0].ThinkMode)
}

// TestLoadExecutionProfiles_LocalFallbackStillWorks verifies that a minimal catalog
// with a single model per role still produces valid candidates.
// Test requirement 5: local fallback still works.
func TestLoadExecutionProfiles_LocalFallbackStillWorks(t *testing.T) {
	dir := t.TempDir()
	writeExecYAML(t, dir, "ollama.yaml", `
provider: {name: ollama, kind: local, enabled: true}
models:
  - name: fallback-model
    enabled: true
    execution:
      timeout_seconds: 60
execution_profiles:
  default: [{ref: fallback-model}]
  fast:    [{ref: fallback-model}]
  planner: [{ref: fallback-model}]
  review:  [{ref: fallback-model}]
`)

	result, err := LoadExecutionProfiles(dir, "ollama", nil)
	require.NoError(t, err)

	for _, role := range []providers.ModelRole{providers.RoleDefault, providers.RoleFast, providers.RolePlanner, providers.RoleReview} {
		candidates := result[role]
		require.NotEmpty(t, candidates, "role %q must have at least one candidate", role)
		assert.Equal(t, "fallback-model", candidates[0].ModelName)
	}
}

// ── LoadExecutionProfiles: fail-fast error cases ──────────────────────────────

// TestLoadExecutionProfiles_FailsIfFileMissing verifies explicit error when
// the provider YAML file does not exist.
// Test requirement 4: startup fails clearly when required config is missing.
func TestLoadExecutionProfiles_FailsIfFileMissing(t *testing.T) {
	dir := t.TempDir()
	// Do not create ollama.yaml

	_, err := LoadExecutionProfiles(dir, "ollama", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ollama.yaml")
	assert.Contains(t, err.Error(), "not found")
}

// TestLoadExecutionProfiles_FailsIfNoExecutionProfiles verifies explicit error when
// the YAML file exists but has no execution_profiles section.
// Test requirement F: fail explicitly if required execution settings are missing from catalog.
func TestLoadExecutionProfiles_FailsIfNoExecutionProfiles(t *testing.T) {
	dir := t.TempDir()
	writeExecYAML(t, dir, "ollama.yaml", `
provider:
  name: ollama
  kind: local
  enabled: true
models:
  - name: qwen3:1.7b
    roles: [fast]
`)

	_, err := LoadExecutionProfiles(dir, "ollama", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "execution_profiles")
	assert.Contains(t, err.Error(), "ollama.yaml")
}

// TestLoadExecutionProfiles_FailsIfRoleMissing verifies explicit error when
// a required role is absent from execution_profiles.
// Test requirement F: fail explicitly if required execution settings are missing from catalog.
func TestLoadExecutionProfiles_FailsIfRoleMissing(t *testing.T) {
	dir := t.TempDir()
	writeExecYAML(t, dir, "ollama.yaml", `
provider: {name: ollama, kind: local, enabled: true}
models:
  - name: m1
    enabled: true
  - name: m2
    enabled: true
  - name: m3
    enabled: true
execution_profiles:
  default: [{ref: m1}]
  fast:    [{ref: m2}]
  planner: [{ref: m3}]
  # review intentionally missing
`)

	_, err := LoadExecutionProfiles(dir, "ollama", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "review")
	assert.Contains(t, err.Error(), "ollama.yaml")
}

// TestLoadExecutionProfiles_FailsIfModelNameEmpty verifies explicit error when
// a candidate has an empty model name.
func TestLoadExecutionProfiles_FailsIfModelNameEmpty(t *testing.T) {
	dir := t.TempDir()
	writeExecYAML(t, dir, "ollama.yaml", `
provider: {name: ollama, kind: local, enabled: true}
models:
  - name: m2
    enabled: true
  - name: m3
    enabled: true
  - name: m4
    enabled: true
execution_profiles:
  default: [{ref: ""}]
  fast:    [{ref: m2}]
  planner: [{ref: m3}]
  review:  [{ref: m4}]
`)

	_, err := LoadExecutionProfiles(dir, "ollama", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ref is required")
}

// TestLoadExecutionProfiles_FailsIfInvalidThinkMode verifies explicit error on
// an unrecognized think mode value.
func TestLoadExecutionProfiles_FailsIfInvalidThinkMode(t *testing.T) {
	dir := t.TempDir()
	writeExecYAML(t, dir, "ollama.yaml", `
provider: {name: ollama, kind: local, enabled: true}
models:
  - name: m1
    enabled: true
    execution:
      think: maybe
  - name: m2
    enabled: true
  - name: m3
    enabled: true
  - name: m4
    enabled: true
execution_profiles:
  default: [{ref: m1}]
  fast:    [{ref: m2}]
  planner: [{ref: m3}]
  review:  [{ref: m4}]
`)

	_, err := LoadExecutionProfiles(dir, "ollama", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "think")
}

// TestLoadExecutionProfiles_InvalidYAMLFails verifies that malformed YAML returns an error.
func TestLoadExecutionProfiles_InvalidYAMLFails(t *testing.T) {
	dir := t.TempDir()
	writeExecYAML(t, dir, "ollama.yaml", "\t{unclosed: yaml")

	_, err := LoadExecutionProfiles(dir, "ollama", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

// ── Execution settings are catalog-only (no env vars) ────────────────────────

// TestExecutionProfiles_ModelProfileEnvVarNotConsumed verifies that the execution
// loader has NO dependency on MODEL_*_PROFILE or OLLAMA_*_PROFILE env vars.
// This is a structural isolation test.
// Test requirement 2: MODEL_*_PROFILE no longer changes runtime behavior.
// Test requirement 3: OLLAMA_*_PROFILE no longer changes runtime behavior.
func TestExecutionProfiles_ModelProfileEnvVarNotConsumed(t *testing.T) {
	dir := t.TempDir()
	writeExecYAML(t, dir, "ollama.yaml", `
provider: {name: ollama, kind: local, enabled: true}
models:
  - name: catalog-default
    enabled: true
    execution:
      timeout_seconds: 60
  - name: catalog-fast
    enabled: true
    execution:
      timeout_seconds: 30
  - name: catalog-planner
    enabled: true
    execution:
      think: on
      timeout_seconds: 300
  - name: catalog-review
    enabled: true
    execution:
      json_mode: true
      timeout_seconds: 60
execution_profiles:
  default: [{ref: catalog-default}]
  fast:    [{ref: catalog-fast}]
  planner: [{ref: catalog-planner}]
  review:  [{ref: catalog-review}]
`)

	// Set env vars that previously controlled execution (now forbidden in config.Load).
	// LoadExecutionProfiles should be entirely unaffected by them.
	t.Setenv("MODEL_FAST_PROFILE", "env-fast-model?think=off&timeout=30")
	t.Setenv("MODEL_PLANNER_PROFILE", "env-planner-model?think=on&timeout=300")
	t.Setenv("OLLAMA_DEFAULT_PROFILE", "env-default-model?timeout=60")
	t.Setenv("OLLAMA_REVIEW_PROFILE", "env-review-model?json=true")

	result, err := LoadExecutionProfiles(dir, "ollama", nil)
	require.NoError(t, err)

	// Catalog values win — env vars have zero influence on LoadExecutionProfiles.
	assert.Equal(t, "catalog-fast", result[providers.RoleFast][0].ModelName,
		"env MODEL_FAST_PROFILE must NOT affect catalog loader")
	assert.Equal(t, "catalog-planner", result[providers.RolePlanner][0].ModelName,
		"env MODEL_PLANNER_PROFILE must NOT affect catalog loader")
	assert.Equal(t, "catalog-default", result[providers.RoleDefault][0].ModelName,
		"env OLLAMA_DEFAULT_PROFILE must NOT affect catalog loader")
	assert.Equal(t, "catalog-review", result[providers.RoleReview][0].ModelName,
		"env OLLAMA_REVIEW_PROFILE must NOT affect catalog loader")
}
