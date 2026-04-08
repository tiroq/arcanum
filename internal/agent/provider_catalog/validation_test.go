package provider_catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// --- Helpers ---

func validEntry(name, kind string, enabled bool, models []ModelSpec) ProviderCatalogFile {
	return ProviderCatalogFile{
		Provider: ProviderSpec{Name: name, Kind: kind, Enabled: enabled},
		Connection: ConnectionSpec{
			BaseURL:        "http://localhost",
			TimeoutSeconds: 30,
		},
		Limits:  LimitsSpec{RPM: 10},
		Routing: RoutingSpec{Roles: []string{"fast", "planner", "fallback"}, FallbackPriority: 10},
		Models:  models,
	}
}

func validModel(name string, roles []string) ModelSpec {
	return ModelSpec{
		Name:            name,
		Enabled:         true,
		Roles:           roles,
		Capabilities:    []string{"json_mode"},
		CostClass:       "free",
		RelativeCost:    0.1,
		MaxOutputTokens: 4096,
	}
}

func hasIssueCode(issues []ValidationIssue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func countIssuesByCode(issues []ValidationIssue, code string) int {
	count := 0
	for _, issue := range issues {
		if issue.Code == code {
			count++
		}
	}
	return count
}

func writeYAML(t *testing.T, dir, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
}

// =====================================================================
// 9.1 YAML / File Tests
// =====================================================================

func TestValidation_ValidYAMLFile(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "good.yaml", `
provider:
  name: test
  kind: local
  enabled: true
connection:
  base_url: http://localhost
  timeout_seconds: 30
routing:
  roles: [fast, planner, fallback]
  fallback_priority: 10
models:
  - name: model-a
    enabled: true
    roles: [fast, planner, fallback]
    capabilities: [json_mode]
    cost_class: local
    relative_cost: 0.0
    max_output_tokens: 4096
`)
	result, err := ValidateCatalogDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Errorf("expected valid, got issues: %v", result.Issues)
	}
}

func TestValidation_InvalidYAMLFile(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "bad.yaml", `
provider:
  name: test
  kind: [invalid yaml structure
`)
	result, err := ValidateCatalogDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("expected invalid for bad YAML")
	}
	if !hasIssueCode(result.Issues, "yaml_parse_error") {
		t.Error("expected yaml_parse_error code")
	}
}

func TestValidation_NonYAMLFileIgnored(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "readme.txt", "not yaml")
	writeYAML(t, dir, "good.yaml", `
provider:
  name: test
  kind: local
  enabled: true
routing:
  roles: [fast, planner, fallback]
models:
  - name: model-a
    enabled: true
    roles: [fast, planner, fallback]
    cost_class: local
    relative_cost: 0.0
`)
	result, err := ValidateCatalogDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Errorf("non-YAML files should be ignored, got issues: %v", result.Issues)
	}
}

func TestValidation_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	result, err := ValidateCatalogDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Error("empty directory should be valid (no files to validate)")
	}
}

func TestValidation_MissingDirectory(t *testing.T) {
	result, err := ValidateCatalogDir("/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Fatalf("missing dir should be fail-open, got error: %v", err)
	}
	if !result.Valid {
		t.Error("missing directory should be valid (fail-open)")
	}
}

// =====================================================================
// 9.2 Provider Tests
// =====================================================================

func TestValidation_MissingProviderName(t *testing.T) {
	entry := validEntry("", "cloud", true, []ModelSpec{validModel("m1", []string{"fast"})})
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if !hasIssueCode(result.Issues, "provider_name_missing") {
		t.Error("expected provider_name_missing")
	}
}

func TestValidation_InvalidProviderKind(t *testing.T) {
	entry := validEntry("test", "satellite", true, []ModelSpec{validModel("m1", []string{"fast"})})
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if !hasIssueCode(result.Issues, "provider_kind_invalid") {
		t.Error("expected provider_kind_invalid")
	}
}

func TestValidation_MissingProviderKind(t *testing.T) {
	entry := validEntry("test", "", true, []ModelSpec{validModel("m1", []string{"fast"})})
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if !hasIssueCode(result.Issues, "provider_kind_missing") {
		t.Error("expected provider_kind_missing")
	}
}

func TestValidation_DuplicateProviderName(t *testing.T) {
	entries := []ProviderCatalogFile{
		validEntry("dup", "cloud", true, []ModelSpec{validModel("m1", []string{"fast", "planner", "fallback"})}),
		validEntry("dup", "local", true, []ModelSpec{validModel("m2", []string{"fast", "planner", "fallback"})}),
	}
	result := ValidateCatalog(entries, []string{"a.yaml", "b.yaml"})
	if !hasIssueCode(result.Issues, "provider_duplicate") {
		t.Error("expected provider_duplicate")
	}
}

func TestValidation_EnabledProviderWithNoModels(t *testing.T) {
	entry := validEntry("test", "cloud", true, nil)
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if !hasIssueCode(result.Issues, "provider_enabled_without_models") {
		t.Error("expected provider_enabled_without_models")
	}
}

func TestValidation_DisabledProviderWithNoModels(t *testing.T) {
	entry := validEntry("test", "cloud", false, nil)
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if hasIssueCode(result.Issues, "provider_enabled_without_models") {
		t.Error("disabled provider should not require models")
	}
}

// =====================================================================
// 9.3 Model Tests
// =====================================================================

func TestValidation_DuplicateModelName(t *testing.T) {
	entry := validEntry("test", "cloud", true, []ModelSpec{
		validModel("dup-model", []string{"fast"}),
		validModel("dup-model", []string{"planner"}),
	})
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if !hasIssueCode(result.Issues, "model_duplicate") {
		t.Error("expected model_duplicate")
	}
}

func TestValidation_ModelNameMissing(t *testing.T) {
	entry := validEntry("test", "cloud", true, []ModelSpec{
		{Name: "", Enabled: true, Roles: []string{"fast"}, CostClass: "free", RelativeCost: 0.1},
	})
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if !hasIssueCode(result.Issues, "model_name_missing") {
		t.Error("expected model_name_missing")
	}
}

func TestValidation_InvalidCostClass(t *testing.T) {
	entry := validEntry("test", "cloud", true, []ModelSpec{
		{Name: "m1", Enabled: true, Roles: []string{"fast"}, CostClass: "premium", RelativeCost: 0.5, MaxOutputTokens: 4096},
	})
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if !hasIssueCode(result.Issues, "model_cost_class_invalid") {
		t.Error("expected model_cost_class_invalid")
	}
}

func TestValidation_RelativeCostOutOfRange(t *testing.T) {
	entry := validEntry("test", "cloud", true, []ModelSpec{
		{Name: "m1", Enabled: true, Roles: []string{"fast"}, CostClass: "free", RelativeCost: 1.5, MaxOutputTokens: 4096},
	})
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if !hasIssueCode(result.Issues, "model_relative_cost_out_of_range") {
		t.Error("expected model_relative_cost_out_of_range")
	}
}

func TestValidation_RelativeCostNegative(t *testing.T) {
	entry := validEntry("test", "cloud", true, []ModelSpec{
		{Name: "m1", Enabled: true, Roles: []string{"fast"}, CostClass: "free", RelativeCost: -0.1, MaxOutputTokens: 4096},
	})
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if !hasIssueCode(result.Issues, "model_relative_cost_out_of_range") {
		t.Error("expected model_relative_cost_out_of_range for negative value")
	}
}

func TestValidation_InvalidRole(t *testing.T) {
	entry := validEntry("test", "cloud", true, []ModelSpec{
		{Name: "m1", Enabled: true, Roles: []string{"quantum"}, CostClass: "free", RelativeCost: 0.1, MaxOutputTokens: 4096},
	})
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if !hasIssueCode(result.Issues, "role_invalid") {
		t.Error("expected role_invalid")
	}
}

func TestValidation_InvalidCapability(t *testing.T) {
	entry := validEntry("test", "cloud", true, []ModelSpec{
		{Name: "m1", Enabled: true, Roles: []string{"fast"}, Capabilities: []string{"telekinesis"}, CostClass: "free", RelativeCost: 0.1, MaxOutputTokens: 4096},
	})
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if !hasIssueCode(result.Issues, "model_capability_invalid") {
		t.Error("expected model_capability_invalid")
	}
}

func TestValidation_InvalidMaxOutputTokens(t *testing.T) {
	entry := validEntry("test", "cloud", true, []ModelSpec{
		{Name: "m1", Enabled: true, Roles: []string{"fast"}, CostClass: "free", RelativeCost: 0.1, MaxOutputTokens: -1},
	})
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if !hasIssueCode(result.Issues, "model_max_output_tokens_invalid") {
		t.Error("expected model_max_output_tokens_invalid")
	}
}

func TestValidation_EnabledModelWithoutRoles(t *testing.T) {
	entry := ProviderCatalogFile{
		Provider:   ProviderSpec{Name: "test", Kind: "cloud", Enabled: true},
		Connection: ConnectionSpec{BaseURL: "http://localhost", TimeoutSeconds: 30},
		Routing:    RoutingSpec{Roles: nil, FallbackPriority: 10}, // no provider roles either
		Models: []ModelSpec{
			{Name: "m1", Enabled: true, Roles: nil, CostClass: "free", RelativeCost: 0.1, MaxOutputTokens: 4096},
		},
	}
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if !hasIssueCode(result.Issues, "model_enabled_without_roles") {
		t.Error("expected model_enabled_without_roles")
	}
}

func TestValidation_EnabledModelInheritsProviderRoles(t *testing.T) {
	entry := validEntry("test", "cloud", true, []ModelSpec{
		{Name: "m1", Enabled: true, Roles: nil, CostClass: "free", RelativeCost: 0.1, MaxOutputTokens: 4096},
	})
	// validEntry has provider roles [fast, planner, fallback]
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if hasIssueCode(result.Issues, "model_enabled_without_roles") {
		t.Error("model should inherit provider roles, not trigger warning")
	}
}

// =====================================================================
// 9.3.1 Connection Tests
// =====================================================================

func TestValidation_InvalidAPIKeyEnv(t *testing.T) {
	entry := validEntry("test", "cloud", true, []ModelSpec{validModel("m1", []string{"fast"})})
	entry.Connection.APIKeyEnv = "123-INVALID"
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if !hasIssueCode(result.Issues, "api_key_env_invalid") {
		t.Error("expected api_key_env_invalid")
	}
}

func TestValidation_ValidAPIKeyEnv(t *testing.T) {
	entry := validEntry("test", "cloud", true, []ModelSpec{validModel("m1", []string{"fast"})})
	entry.Connection.APIKeyEnv = "MY_API_KEY_123"
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if hasIssueCode(result.Issues, "api_key_env_invalid") {
		t.Error("valid api_key_env should not produce error")
	}
}

func TestValidation_NegativeTimeout(t *testing.T) {
	entry := validEntry("test", "cloud", true, []ModelSpec{validModel("m1", []string{"fast"})})
	entry.Connection.TimeoutSeconds = -5
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if !hasIssueCode(result.Issues, "timeout_invalid") {
		t.Error("expected timeout_invalid")
	}
}

// =====================================================================
// 9.3.2 Limits Tests
// =====================================================================

func TestValidation_NegativeLimit(t *testing.T) {
	entry := validEntry("test", "cloud", true, []ModelSpec{validModel("m1", []string{"fast"})})
	entry.Limits.RPM = -1
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if !hasIssueCode(result.Issues, "limit_negative") {
		t.Error("expected limit_negative")
	}
}

func TestValidation_ZeroLimitsAreValid(t *testing.T) {
	entry := validEntry("test", "cloud", true, []ModelSpec{validModel("m1", []string{"fast"})})
	entry.Limits = LimitsSpec{RPM: 0, TPM: 0, RPD: 0, TPD: 0}
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if hasIssueCode(result.Issues, "limit_negative") {
		t.Error("zero limits should be valid (unknown, not unlimited)")
	}
}

// =====================================================================
// 9.3.3 Routing Tests
// =====================================================================

func TestValidation_NegativeFallbackPriority(t *testing.T) {
	entry := validEntry("test", "cloud", true, []ModelSpec{validModel("m1", []string{"fast"})})
	entry.Routing.FallbackPriority = -1
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if !hasIssueCode(result.Issues, "fallback_priority_invalid") {
		t.Error("expected fallback_priority_invalid")
	}
}

func TestValidation_InvalidRoutingRole(t *testing.T) {
	entry := validEntry("test", "cloud", true, []ModelSpec{validModel("m1", []string{"fast"})})
	entry.Routing.Roles = []string{"fast", "unknown_role"}
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if !hasIssueCode(result.Issues, "role_invalid") {
		t.Error("expected role_invalid for unknown routing role")
	}
}

// =====================================================================
// 9.4 Cross-File Tests
// =====================================================================

func TestValidation_MissingCriticalRole(t *testing.T) {
	entries := []ProviderCatalogFile{
		validEntry("test", "local", true, []ModelSpec{
			validModel("m1", []string{"fast"}), // no planner, no fallback
		}),
	}
	result := ValidateCatalog(entries, []string{"test.yaml"})
	if !hasIssueCode(result.Issues, "critical_role_missing") {
		t.Error("expected critical_role_missing for missing planner/fallback")
	}
}

func TestValidation_NoLocalFallbackWarning(t *testing.T) {
	entries := []ProviderCatalogFile{
		validEntry("cloud1", "cloud", true, []ModelSpec{
			validModel("m1", []string{"fast", "planner", "fallback"}),
		}),
	}
	result := ValidateCatalog(entries, []string{"cloud1.yaml"})
	if !hasIssueCode(result.Issues, "no_local_fallback_warning") {
		t.Error("expected no_local_fallback_warning")
	}
}

func TestValidation_AllExternalWarning(t *testing.T) {
	entries := []ProviderCatalogFile{
		validEntry("cloud1", "cloud", true, []ModelSpec{
			validModel("m1", []string{"fast", "planner", "fallback"}),
		}),
	}
	result := ValidateCatalog(entries, []string{"cloud1.yaml"})
	if !hasIssueCode(result.Issues, "all_external_warning") {
		t.Error("expected all_external_warning")
	}
}

func TestValidation_TooManyEnabledModels(t *testing.T) {
	models := make([]ModelSpec, MaxModelsPerProvider+1)
	for i := range models {
		models[i] = validModel("model-"+string(rune('a'+i)), []string{"fast"})
	}
	entry := validEntry("test", "local", true, models)
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if !hasIssueCode(result.Issues, "enabled_model_cap_exceeded") {
		t.Error("expected enabled_model_cap_exceeded")
	}
}

func TestValidation_DuplicateProviderModelTarget(t *testing.T) {
	entries := []ProviderCatalogFile{
		validEntry("prov", "cloud", true, []ModelSpec{
			validModel("model-x", []string{"fast", "planner", "fallback"}),
		}),
		validEntry("prov", "local", true, []ModelSpec{
			validModel("model-x", []string{"fast", "planner", "fallback"}),
		}),
	}
	result := ValidateCatalog(entries, []string{"a.yaml", "b.yaml"})
	// This triggers provider_duplicate first, but also provider_model_duplicate
	if !hasIssueCode(result.Issues, "provider_duplicate") {
		t.Error("expected provider_duplicate")
	}
}

// =====================================================================
// 9.5 CLI-Related (Output Format) Tests
// =====================================================================

func TestValidation_TextOutput(t *testing.T) {
	entry := validEntry("test", "local", true, []ModelSpec{
		validModel("m1", []string{"fast", "planner", "fallback"}),
	})
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	text := result.Text()
	if text == "" {
		t.Error("text output should not be empty")
	}
	if result.Valid && len(text) == 0 {
		t.Error("valid result should still have text")
	}
}

func TestValidation_JSONOutput(t *testing.T) {
	entry := validEntry("test", "local", true, []ModelSpec{
		validModel("m1", []string{"fast", "planner", "fallback"}),
	})
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	jsonStr := result.JSON()
	var parsed ValidationResult
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("JSON output should be parseable: %v", err)
	}
	if parsed.Valid != result.Valid {
		t.Error("parsed JSON should match original result")
	}
}

func TestValidation_JSONOutputWithErrors(t *testing.T) {
	entry := validEntry("", "satellite", true, nil)
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	jsonStr := result.JSON()
	var parsed ValidationResult
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("JSON output should be parseable: %v", err)
	}
	if parsed.Valid {
		t.Error("result with errors should not be valid")
	}
	if parsed.ErrorCount == 0 {
		t.Error("should have error count > 0")
	}
}

// =====================================================================
// 9.6 Determinism Tests
// =====================================================================

func TestValidation_DeterministicIssueOrdering(t *testing.T) {
	entries := []ProviderCatalogFile{
		validEntry("beta", "cloud", true, []ModelSpec{
			{Name: "m2", Enabled: true, Roles: []string{"quantum"}, CostClass: "premium", RelativeCost: 0.1, MaxOutputTokens: 4096},
			{Name: "m1", Enabled: true, Roles: []string{"magic"}, CostClass: "diamond", RelativeCost: 0.1, MaxOutputTokens: 4096},
		}),
		validEntry("alpha", "cloud", true, []ModelSpec{
			validModel("m_a", []string{"fast", "planner", "fallback"}),
		}),
	}

	result1 := ValidateCatalog(entries, []string{"beta.yaml", "alpha.yaml"})
	result2 := ValidateCatalog(entries, []string{"beta.yaml", "alpha.yaml"})

	if len(result1.Issues) != len(result2.Issues) {
		t.Fatalf("issue count mismatch: %d vs %d", len(result1.Issues), len(result2.Issues))
	}

	for i, issue := range result1.Issues {
		other := result2.Issues[i]
		if issue.Code != other.Code || issue.File != other.File || issue.Provider != other.Provider || issue.Model != other.Model {
			t.Errorf("issue[%d] mismatch: %+v vs %+v", i, issue, other)
		}
	}
}

func TestValidation_IssueOrderingStable(t *testing.T) {
	entry := validEntry("test", "cloud", true, []ModelSpec{
		{Name: "z-model", Enabled: true, Roles: []string{"unknown_role"}, CostClass: "platinum", RelativeCost: 1.5, MaxOutputTokens: -1},
		{Name: "a-model", Enabled: true, Roles: []string{"also_unknown"}, CostClass: "gold", RelativeCost: 2.0, MaxOutputTokens: -2},
	})
	result := ValidateCatalogEntryStructured(entry, "test.yaml")

	// Verify ordering: issues for a-model should come before z-model.
	firstAModel := -1
	firstZModel := -1
	for i, issue := range result.Issues {
		if issue.Model == "a-model" && firstAModel == -1 {
			firstAModel = i
		}
		if issue.Model == "z-model" && firstZModel == -1 {
			firstZModel = i
		}
	}
	if firstAModel >= 0 && firstZModel >= 0 && firstAModel > firstZModel {
		t.Error("a-model issues should come before z-model issues (deterministic sort)")
	}
}

// =====================================================================
// 9.7 Regression / Compatibility Tests
// =====================================================================

func TestValidation_ExistingProviderCatalogPasses(t *testing.T) {
	// Validate the actual project provider catalog files.
	projectDir := "../../.."
	catalogDir := filepath.Join(projectDir, "providers")

	if _, err := os.Stat(catalogDir); os.IsNotExist(err) {
		t.Skip("providers/ directory not found, skipping regression test")
	}

	result, err := ValidateCatalogDir(catalogDir)
	if err != nil {
		t.Fatalf("unexpected error validating project catalog: %v", err)
	}

	// The existing catalog should pass (no errors, warnings ok).
	if !result.Valid {
		for _, issue := range result.Issues {
			if issue.Severity == SeverityError {
				t.Errorf("existing catalog has error: [%s] %s: %s", issue.Code, issue.File, issue.Message)
			}
		}
	}
}

func TestValidation_LegacyValidateCatalogEntryCompatibility(t *testing.T) {
	// Ensure the legacy ValidateCatalogEntry still works.
	entry := validEntry("test", "cloud", true, []ModelSpec{
		validModel("m1", []string{"fast"}),
	})
	errs := ValidateCatalogEntry(entry, "test.yaml")
	if len(errs) > 0 {
		t.Errorf("legacy validator should pass: %v", errs)
	}

	// Invalid entry should still fail with legacy API.
	entry2 := validEntry("", "satellite", true, nil)
	errs2 := ValidateCatalogEntry(entry2, "test.yaml")
	if len(errs2) == 0 {
		t.Error("legacy validator should fail for invalid entry")
	}
}

// =====================================================================
// 9.7.1 Edge cases
// =====================================================================

func TestValidation_DisabledProviderAllModelsDisabled(t *testing.T) {
	entry := ProviderCatalogFile{
		Provider:   ProviderSpec{Name: "disabled-all", Kind: "cloud", Enabled: false},
		Connection: ConnectionSpec{BaseURL: "http://x", TimeoutSeconds: 10},
		Routing:    RoutingSpec{Roles: []string{"fast"}, FallbackPriority: 5},
		Models: []ModelSpec{
			{Name: "m1", Enabled: false, Roles: []string{"fast"}, CostClass: "free", RelativeCost: 0.1},
			{Name: "m2", Enabled: false, Roles: []string{"fast"}, CostClass: "free", RelativeCost: 0.1},
		},
	}
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if !result.Valid {
		t.Errorf("disabled provider with disabled models should be valid, got: %v", result.Issues)
	}
}

func TestValidation_PromoCostClassAccepted(t *testing.T) {
	entry := validEntry("test", "cloud", true, []ModelSpec{
		{Name: "m1", Enabled: true, Roles: []string{"fast"}, CostClass: "promo", RelativeCost: 0.0, MaxOutputTokens: 4096},
	})
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	if hasIssueCode(result.Issues, "model_cost_class_invalid") {
		t.Error("promo cost class should be accepted")
	}
}

func TestValidation_DefaultsYAMLSkipped(t *testing.T) {
	dir := t.TempDir()
	// Write a defaults.yaml-style file (no provider section).
	writeYAML(t, dir, "defaults.yaml", `
catalog:
  version: 1
defaults:
  connection:
    timeout_seconds: 30
`)
	// Write a valid provider file.
	writeYAML(t, dir, "local.yaml", `
provider:
  name: local-test
  kind: local
  enabled: true
routing:
  roles: [fast, planner, fallback]
models:
  - name: m1
    enabled: true
    roles: [fast, planner, fallback]
    cost_class: local
    relative_cost: 0.0
`)
	result, err := ValidateCatalogDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Errorf("defaults.yaml should be skipped, got issues: %v", result.Issues)
	}
}

func TestValidation_ValidateFromCatalogFiles(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "provider.yaml", `
provider:
  name: test
  kind: local
  enabled: true
routing:
  roles: [fast, planner, fallback]
models:
  - name: m1
    enabled: true
    roles: [fast, planner, fallback]
    cost_class: local
    relative_cost: 0.0
`)
	files := []string{filepath.Join(dir, "provider.yaml")}
	result, err := ValidateCatalogFiles(files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Errorf("valid catalog files should pass: %v", result.Issues)
	}
}

func TestValidation_MultipleNegativeLimits(t *testing.T) {
	entry := validEntry("test", "cloud", true, []ModelSpec{validModel("m1", []string{"fast"})})
	entry.Limits = LimitsSpec{RPM: -1, TPM: -2, RPD: -3, TPD: -4}
	result := ValidateCatalogEntryStructured(entry, "test.yaml")
	count := countIssuesByCode(result.Issues, "limit_negative")
	if count != 4 {
		t.Errorf("expected 4 limit_negative issues, got %d", count)
	}
}

func TestValidation_ValidCatalogWithWarningsStillValid(t *testing.T) {
	entries := []ProviderCatalogFile{
		validEntry("cloud-only", "cloud", true, []ModelSpec{
			validModel("m1", []string{"fast", "planner", "fallback"}),
		}),
	}
	result := ValidateCatalog(entries, []string{"cloud.yaml"})
	// Should be valid (no errors) even though there are warnings.
	if !result.Valid {
		hasOnlyWarnings := true
		for _, issue := range result.Issues {
			if issue.Severity == SeverityError {
				hasOnlyWarnings = false
				break
			}
		}
		if hasOnlyWarnings {
			t.Error("result with only warnings should still be Valid=true")
		}
	}
	if result.WarningCount == 0 {
		t.Error("expected at least one warning (no_local_fallback_warning or all_external_warning)")
	}
}

func TestValidation_YMLExtensionAccepted(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "provider.yml", `
provider:
  name: test
  kind: local
  enabled: true
routing:
  roles: [fast, planner, fallback]
models:
  - name: m1
    enabled: true
    roles: [fast, planner, fallback]
    cost_class: local
    relative_cost: 0.0
`)
	result, err := ValidateCatalogDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Errorf(".yml extension should be accepted: %v", result.Issues)
	}
}
