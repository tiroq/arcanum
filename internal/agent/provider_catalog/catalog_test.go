package provider_catalog

import (
	"os"
	"path/filepath"
	"testing"
)

// --- Helper: build test catalog entries ---

func testCatalogEntry(name, kind string, enabled bool, models []ModelSpec) ProviderCatalogFile {
	return ProviderCatalogFile{
		Provider: ProviderSpec{
			Name:    name,
			Kind:    kind,
			Enabled: enabled,
		},
		Connection: ConnectionSpec{
			BaseURL:        "http://localhost",
			TimeoutSeconds: 30,
		},
		Limits: LimitsSpec{RPM: 30, TPM: 6000},
		Routing: RoutingSpec{
			Roles:            []string{"planner", "reviewer"},
			FallbackPriority: 10,
			AllowExternal:    true,
		},
		Models: models,
	}
}

func testModel(name string, enabled bool, roles, caps []string, costClass string, relativeCost float64) ModelSpec {
	return ModelSpec{
		Name:            name,
		Enabled:         enabled,
		Roles:           roles,
		Capabilities:    caps,
		CostClass:       costClass,
		RelativeCost:    relativeCost,
		MaxOutputTokens: 4096,
	}
}

// --- 1. Validation Tests ---

func TestValidateCatalogEntry_Valid(t *testing.T) {
	entry := testCatalogEntry("groq", "cloud", true, []ModelSpec{
		testModel("llama-3.3-70b", true, []string{"planner"}, []string{"json_mode"}, "free", 0.1),
	})
	errs := ValidateCatalogEntry(entry, "groq.yaml")
	if len(errs) > 0 {
		t.Errorf("expected valid, got errors: %v", errs)
	}
}

func TestValidateCatalogEntry_MissingName(t *testing.T) {
	entry := testCatalogEntry("", "cloud", true, []ModelSpec{
		testModel("llama", true, nil, nil, "free", 0.1),
	})
	errs := ValidateCatalogEntry(entry, "test.yaml")
	if len(errs) == 0 {
		t.Error("expected error for missing name")
	}
}

func TestValidateCatalogEntry_InvalidKind(t *testing.T) {
	entry := testCatalogEntry("test", "satellite", true, []ModelSpec{
		testModel("llama", true, nil, nil, "free", 0.1),
	})
	errs := ValidateCatalogEntry(entry, "test.yaml")
	if len(errs) == 0 {
		t.Error("expected error for invalid kind")
	}
}

func TestValidateCatalogEntry_EnabledNoModels(t *testing.T) {
	entry := testCatalogEntry("test", "cloud", true, nil)
	errs := ValidateCatalogEntry(entry, "test.yaml")
	if len(errs) == 0 {
		t.Error("expected error for enabled provider with no models")
	}
}

func TestValidateCatalogEntry_DisabledNoModels(t *testing.T) {
	entry := testCatalogEntry("test", "cloud", false, nil)
	errs := ValidateCatalogEntry(entry, "test.yaml")
	if len(errs) > 0 {
		t.Errorf("disabled provider should not require models, got: %v", errs)
	}
}

func TestValidateCatalogEntry_DuplicateModelName(t *testing.T) {
	entry := testCatalogEntry("test", "cloud", true, []ModelSpec{
		testModel("llama", true, nil, nil, "free", 0.1),
		testModel("llama", true, nil, nil, "free", 0.2),
	})
	errs := ValidateCatalogEntry(entry, "test.yaml")
	if len(errs) == 0 {
		t.Error("expected error for duplicate model name")
	}
}

func TestValidateCatalogEntry_InvalidCostClass(t *testing.T) {
	entry := testCatalogEntry("test", "cloud", true, []ModelSpec{
		testModel("llama", true, nil, nil, "premium", 0.5),
	})
	errs := ValidateCatalogEntry(entry, "test.yaml")
	if len(errs) == 0 {
		t.Error("expected error for invalid cost class")
	}
}

func TestValidateCatalogEntry_RelativeCostOutOfRange(t *testing.T) {
	entry := testCatalogEntry("test", "cloud", true, []ModelSpec{
		testModel("llama", true, nil, nil, "free", 1.5),
	})
	errs := ValidateCatalogEntry(entry, "test.yaml")
	if len(errs) == 0 {
		t.Error("expected error for relative_cost > 1.0")
	}
}

func TestValidateCatalogEntry_TooManyModels(t *testing.T) {
	models := make([]ModelSpec, MaxModelsPerProvider+1)
	for i := range models {
		models[i] = testModel("model-"+string(rune('a'+i)), true, nil, nil, "free", 0.1)
	}
	entry := testCatalogEntry("test", "cloud", true, models)
	errs := ValidateCatalogEntry(entry, "test.yaml")
	found := false
	for _, e := range errs {
		if len(e) > 0 {
			found = true
		}
	}
	if !found {
		t.Error("expected error for too many models")
	}
}

// --- 2. Registry Tests ---

func TestCatalogRegistry_BuildFromCatalog(t *testing.T) {
	cr := NewCatalogRegistry()
	catalogs := []ProviderCatalogFile{
		testCatalogEntry("groq", "cloud", true, []ModelSpec{
			testModel("llama-3.3-70b", true, []string{"planner"}, []string{"json_mode"}, "free", 0.1),
			testModel("llama-3.1-8b", true, []string{"fast"}, []string{"json_mode"}, "free", 0.05),
		}),
		testCatalogEntry("ollama", "local", true, []ModelSpec{
			testModel("qwen3:1.7b", true, []string{"fast"}, []string{"low_latency"}, "local", 0.0),
		}),
	}
	cr.BuildFromCatalog(catalogs)

	if cr.Count() != 3 {
		t.Errorf("expected 3 models, got %d", cr.Count())
	}

	all := cr.All()
	// Deterministic order: groq/llama-3.1-8b, groq/llama-3.3-70b, ollama/qwen3:1.7b
	if all[0].ProviderName != "groq" || all[0].ModelName != "llama-3.1-8b" {
		t.Errorf("expected groq/llama-3.1-8b first, got %s/%s", all[0].ProviderName, all[0].ModelName)
	}
	if all[2].ProviderName != "ollama" || all[2].ModelName != "qwen3:1.7b" {
		t.Errorf("expected ollama/qwen3:1.7b last, got %s/%s", all[2].ProviderName, all[2].ModelName)
	}
}

func TestCatalogRegistry_DisabledProviderIgnored(t *testing.T) {
	cr := NewCatalogRegistry()
	catalogs := []ProviderCatalogFile{
		testCatalogEntry("disabled", "cloud", false, []ModelSpec{
			testModel("model-a", true, nil, nil, "free", 0.1),
		}),
		testCatalogEntry("enabled", "cloud", true, []ModelSpec{
			testModel("model-b", true, nil, nil, "free", 0.1),
		}),
	}
	cr.BuildFromCatalog(catalogs)

	if cr.Count() != 1 {
		t.Errorf("expected 1 model (disabled skipped), got %d", cr.Count())
	}
}

func TestCatalogRegistry_DisabledModelIgnored(t *testing.T) {
	cr := NewCatalogRegistry()
	catalogs := []ProviderCatalogFile{
		testCatalogEntry("groq", "cloud", true, []ModelSpec{
			testModel("model-active", true, nil, nil, "free", 0.1),
			testModel("model-off", false, nil, nil, "free", 0.1),
		}),
	}
	cr.BuildFromCatalog(catalogs)

	if cr.Count() != 1 {
		t.Errorf("expected 1 model (disabled skipped), got %d", cr.Count())
	}
}

func TestCatalogRegistry_GetByKey(t *testing.T) {
	cr := NewCatalogRegistry()
	catalogs := []ProviderCatalogFile{
		testCatalogEntry("groq", "cloud", true, []ModelSpec{
			testModel("llama-3.3-70b", true, []string{"planner"}, nil, "free", 0.1),
		}),
	}
	cr.BuildFromCatalog(catalogs)

	pm, ok := cr.Get("groq", "llama-3.3-70b")
	if !ok {
		t.Fatal("expected to find groq/llama-3.3-70b")
	}
	if pm.CostClass != "free" {
		t.Errorf("expected cost_class=free, got %s", pm.CostClass)
	}

	_, ok = cr.Get("groq", "nonexistent")
	if ok {
		t.Error("expected not found for nonexistent model")
	}
}

func TestCatalogRegistry_ByRole(t *testing.T) {
	cr := NewCatalogRegistry()
	catalogs := []ProviderCatalogFile{
		testCatalogEntry("groq", "cloud", true, []ModelSpec{
			testModel("planner-model", true, []string{"planner"}, nil, "free", 0.1),
			testModel("fast-model", true, []string{"fast"}, nil, "free", 0.05),
		}),
	}
	cr.BuildFromCatalog(catalogs)

	planners := cr.ByRole("planner")
	if len(planners) != 1 {
		t.Errorf("expected 1 planner, got %d", len(planners))
	}
	if planners[0].ModelName != "planner-model" {
		t.Errorf("expected planner-model, got %s", planners[0].ModelName)
	}
}

func TestCatalogRegistry_ByProvider(t *testing.T) {
	cr := NewCatalogRegistry()
	catalogs := []ProviderCatalogFile{
		testCatalogEntry("groq", "cloud", true, []ModelSpec{
			testModel("model-a", true, nil, nil, "free", 0.1),
			testModel("model-b", true, nil, nil, "free", 0.2),
		}),
		testCatalogEntry("ollama", "local", true, []ModelSpec{
			testModel("model-c", true, nil, nil, "local", 0.0),
		}),
	}
	cr.BuildFromCatalog(catalogs)

	groqModels := cr.ByProvider("groq")
	if len(groqModels) != 2 {
		t.Errorf("expected 2 groq models, got %d", len(groqModels))
	}
}

func TestCatalogRegistry_Targets(t *testing.T) {
	cr := NewCatalogRegistry()
	catalogs := []ProviderCatalogFile{
		testCatalogEntry("groq", "cloud", true, []ModelSpec{
			testModel("model-a", true, nil, nil, "free", 0.1),
		}),
	}
	cr.BuildFromCatalog(catalogs)

	targets := cr.Targets()
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Provider != "groq" || targets[0].Model != "model-a" {
		t.Errorf("unexpected target: %+v", targets[0])
	}
}

func TestCatalogRegistry_RoleInheritance(t *testing.T) {
	cr := NewCatalogRegistry()
	// Provider has roles ["planner", "reviewer"], model has no roles → inherit.
	catalogs := []ProviderCatalogFile{
		testCatalogEntry("groq", "cloud", true, []ModelSpec{
			{
				Name:         "inherit-model",
				Enabled:      true,
				Roles:        nil, // no model-level roles → inherit provider roles
				CostClass:    "free",
				RelativeCost: 0.1,
			},
		}),
	}
	cr.BuildFromCatalog(catalogs)

	pm, ok := cr.Get("groq", "inherit-model")
	if !ok {
		t.Fatal("expected to find inherit-model")
	}
	if len(pm.Roles) != 2 || pm.Roles[0] != "planner" {
		t.Errorf("expected inherited roles [planner reviewer], got %v", pm.Roles)
	}
}

// --- 3. Resolver Tests ---

func TestResolveCandidates_RoleFiltering(t *testing.T) {
	cr := NewCatalogRegistry()
	catalogs := []ProviderCatalogFile{
		testCatalogEntry("groq", "cloud", true, []ModelSpec{
			testModel("planner-model", true, []string{"planner"}, nil, "free", 0.1),
			testModel("fast-model", true, []string{"fast"}, nil, "free", 0.05),
		}),
	}
	cr.BuildFromCatalog(catalogs)

	candidates, rejected := ResolveCandidates(cr, ResolverInput{
		PreferredRole: "planner",
		AllowExternal: true,
	})

	if len(candidates) != 1 {
		t.Errorf("expected 1 candidate, got %d", len(candidates))
	}
	if len(rejected) != 1 {
		t.Errorf("expected 1 rejected, got %d", len(rejected))
	}
	if candidates[0].ModelName != "planner-model" {
		t.Errorf("expected planner-model, got %s", candidates[0].ModelName)
	}
}

func TestResolveCandidates_CapabilityFiltering(t *testing.T) {
	cr := NewCatalogRegistry()
	catalogs := []ProviderCatalogFile{
		testCatalogEntry("groq", "cloud", true, []ModelSpec{
			testModel("capable", true, []string{"planner"}, []string{"json_mode", "long_context"}, "free", 0.1),
			testModel("basic", true, []string{"planner"}, []string{"json_mode"}, "free", 0.05),
		}),
	}
	cr.BuildFromCatalog(catalogs)

	candidates, rejected := ResolveCandidates(cr, ResolverInput{
		PreferredRole:        "planner",
		RequiredCapabilities: []string{"long_context"},
		AllowExternal:        true,
	})

	if len(candidates) != 1 {
		t.Errorf("expected 1 candidate (long_context), got %d", len(candidates))
	}
	if len(rejected) != 1 {
		t.Errorf("expected 1 rejected, got %d", len(rejected))
	}
	if candidates[0].ModelName != "capable" {
		t.Errorf("expected capable, got %s", candidates[0].ModelName)
	}
}

func TestResolveCandidates_ExternalBlocked(t *testing.T) {
	cr := NewCatalogRegistry()
	catalogs := []ProviderCatalogFile{
		testCatalogEntry("groq", "cloud", true, []ModelSpec{
			testModel("cloud-model", true, nil, nil, "free", 0.1),
		}),
		testCatalogEntry("ollama", "local", true, []ModelSpec{
			testModel("local-model", true, nil, nil, "local", 0.0),
		}),
	}
	cr.BuildFromCatalog(catalogs)

	candidates, rejected := ResolveCandidates(cr, ResolverInput{
		AllowExternal: false,
	})

	if len(candidates) != 1 {
		t.Errorf("expected 1 local candidate, got %d", len(candidates))
	}
	if candidates[0].ProviderName != "ollama" {
		t.Errorf("expected ollama, got %s", candidates[0].ProviderName)
	}
	if len(rejected) != 1 {
		t.Errorf("expected 1 rejected, got %d", len(rejected))
	}
}

func TestResolveCandidates_NilRegistry(t *testing.T) {
	candidates, rejected := ResolveCandidates(nil, ResolverInput{AllowExternal: true})
	if candidates != nil || rejected != nil {
		t.Error("nil registry should return nil results")
	}
}

func TestResolveCandidates_FallbackRole(t *testing.T) {
	cr := NewCatalogRegistry()
	catalogs := []ProviderCatalogFile{
		testCatalogEntry("ollama", "local", true, []ModelSpec{
			testModel("fallback-model", true, []string{"fallback"}, nil, "local", 0.0),
		}),
	}
	cr.BuildFromCatalog(catalogs)

	candidates, _ := ResolveCandidates(cr, ResolverInput{
		PreferredRole: "planner",
		AllowExternal: true,
	})

	// Fallback role should match any preferred role.
	if len(candidates) != 1 {
		t.Errorf("fallback role should match any preferred role, got %d candidates", len(candidates))
	}
}

// --- 4. Model Capability Fit Tests ---

func TestComputeModelCapabilityFit_ExactMatch(t *testing.T) {
	pm := ProviderModel{Capabilities: []string{"json_mode", "long_context"}}
	score := ComputeModelCapabilityFit(pm, []string{"json_mode", "long_context"})
	if score != 1.0 {
		t.Errorf("expected 1.0 for exact match, got %.2f", score)
	}
}

func TestComputeModelCapabilityFit_PartialMatch(t *testing.T) {
	pm := ProviderModel{Capabilities: []string{"json_mode"}}
	score := ComputeModelCapabilityFit(pm, []string{"json_mode", "long_context"})
	if score != 0.5 {
		t.Errorf("expected 0.5 for partial match, got %.2f", score)
	}
}

func TestComputeModelCapabilityFit_NoMatch(t *testing.T) {
	pm := ProviderModel{Capabilities: []string{"json_mode"}}
	score := ComputeModelCapabilityFit(pm, []string{"tool_calling"})
	if score != 0.0 {
		t.Errorf("expected 0.0 for no match, got %.2f", score)
	}
}

func TestComputeModelCapabilityFit_NoRequirements(t *testing.T) {
	pm := ProviderModel{Capabilities: []string{"json_mode"}}
	score := ComputeModelCapabilityFit(pm, nil)
	if score != 1.0 {
		t.Errorf("expected 1.0 for no requirements, got %.2f", score)
	}
}

// --- 5. YAML Loader Tests ---

func TestLoadCatalog_ValidDirectory(t *testing.T) {
	dir := t.TempDir()

	// Write a valid YAML file.
	yamlContent := `
provider:
  name: test-provider
  kind: cloud
  enabled: true
connection:
  base_url: http://localhost
  timeout_seconds: 30
limits:
  rpm: 10
routing:
  roles: [planner]
models:
  - name: test-model
    enabled: true
    roles: [planner]
    cost_class: free
    relative_cost: 0.1
`
	if err := os.WriteFile(filepath.Join(dir, "test.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	catalogs, err := LoadCatalog(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(catalogs) != 1 {
		t.Fatalf("expected 1 catalog, got %d", len(catalogs))
	}
	if catalogs[0].Provider.Name != "test-provider" {
		t.Errorf("expected test-provider, got %s", catalogs[0].Provider.Name)
	}
}

func TestLoadCatalog_InvalidYAMLSkipped(t *testing.T) {
	dir := t.TempDir()

	// Write invalid YAML.
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("}{invalid"), 0644); err != nil {
		t.Fatal(err)
	}

	catalogs, err := LoadCatalog(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(catalogs) != 0 {
		t.Errorf("expected 0 catalogs (bad file skipped), got %d", len(catalogs))
	}
}

func TestLoadCatalog_MissingDirectory(t *testing.T) {
	catalogs, err := LoadCatalog("/nonexistent/path", nil)
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if catalogs != nil {
		t.Errorf("expected nil catalogs for missing dir, got %v", catalogs)
	}
}

func TestLoadCatalog_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	catalogs, err := LoadCatalog(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(catalogs) != 0 {
		t.Errorf("expected 0 catalogs, got %d", len(catalogs))
	}
}

func TestLoadCatalog_ValidationFailureSkipped(t *testing.T) {
	dir := t.TempDir()

	// Write YAML that parses but fails validation (missing provider name).
	yamlContent := `
provider:
  name: ""
  kind: cloud
  enabled: true
connection:
  base_url: http://localhost
models:
  - name: test-model
    enabled: true
`
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	catalogs, err := LoadCatalog(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(catalogs) != 0 {
		t.Errorf("expected 0 catalogs (validation failure skipped), got %d", len(catalogs))
	}
}

func TestLoadCatalog_DeterministicOrder(t *testing.T) {
	dir := t.TempDir()

	// Write files in z, a order.
	for _, name := range []string{"z-provider.yaml", "a-provider.yaml"} {
		yamlContent := `
provider:
  name: ` + name[:1] + `-prov
  kind: cloud
  enabled: true
connection:
  base_url: http://localhost
models:
  - name: model-1
    enabled: true
    cost_class: free
    relative_cost: 0.1
`
		if err := os.WriteFile(filepath.Join(dir, name), []byte(yamlContent), 0644); err != nil {
			t.Fatal(err)
		}
	}

	catalogs, err := LoadCatalog(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(catalogs) != 2 {
		t.Fatalf("expected 2 catalogs, got %d", len(catalogs))
	}
	// a-provider.yaml should come first.
	if catalogs[0].Provider.Name != "a-prov" {
		t.Errorf("expected a-prov first, got %s", catalogs[0].Provider.Name)
	}
}

// --- 6. ProviderModel Tests ---

func TestProviderModel_HasRole(t *testing.T) {
	pm := ProviderModel{Roles: []string{"fast", "planner"}}
	if !pm.HasRole("fast") {
		t.Error("expected HasRole(fast) = true")
	}
	if pm.HasRole("reviewer") {
		t.Error("expected HasRole(reviewer) = false")
	}
}

func TestProviderModel_HasCapability(t *testing.T) {
	pm := ProviderModel{Capabilities: []string{"json_mode"}}
	if !pm.HasCapability("json_mode") {
		t.Error("expected HasCapability(json_mode) = true")
	}
	if pm.HasCapability("tool_calling") {
		t.Error("expected HasCapability(tool_calling) = false")
	}
}

func TestProviderModel_IsLocal(t *testing.T) {
	local := ProviderModel{ProviderKind: "local"}
	cloud := ProviderModel{ProviderKind: "cloud"}
	if !local.IsLocal() {
		t.Error("expected local.IsLocal() = true")
	}
	if cloud.IsLocal() {
		t.Error("expected cloud.IsLocal() = false")
	}
}

func TestProviderModel_IsExternal(t *testing.T) {
	cloud := ProviderModel{ProviderKind: "cloud"}
	router := ProviderModel{ProviderKind: "router"}
	local := ProviderModel{ProviderKind: "local"}
	if !cloud.IsExternal() {
		t.Error("expected cloud.IsExternal() = true")
	}
	if !router.IsExternal() {
		t.Error("expected router.IsExternal() = true")
	}
	if local.IsExternal() {
		t.Error("expected local.IsExternal() = false")
	}
}

// --- 7. RoutingTarget Tests ---

func TestRoutingTarget_String(t *testing.T) {
	target := RoutingTarget{Provider: "groq", Model: "llama-3.3-70b"}
	if target.String() != "groq/llama-3.3-70b" {
		t.Errorf("expected groq/llama-3.3-70b, got %s", target.String())
	}

	providerOnly := RoutingTarget{Provider: "ollama"}
	if providerOnly.String() != "ollama" {
		t.Errorf("expected ollama, got %s", providerOnly.String())
	}
}

func TestRoutingTarget_IsEmpty(t *testing.T) {
	empty := RoutingTarget{}
	if !empty.IsEmpty() {
		t.Error("expected IsEmpty() = true for empty target")
	}

	nonEmpty := RoutingTarget{Provider: "groq"}
	if nonEmpty.IsEmpty() {
		t.Error("expected IsEmpty() = false for non-empty target")
	}
}

// --- 8. Large Catalog Test ---

func TestCatalogRegistry_LargeCatalog50Models(t *testing.T) {
	var models []ModelSpec
	for i := 0; i < 10; i++ {
		models = append(models, testModel(
			"model-"+string(rune('a'+i)),
			true,
			[]string{"planner"},
			[]string{"json_mode"},
			"free",
			float64(i)*0.1,
		))
	}

	var catalogs []ProviderCatalogFile
	for i := 0; i < 5; i++ {
		catalogs = append(catalogs, testCatalogEntry(
			"provider-"+string(rune('a'+i)),
			"cloud",
			true,
			models,
		))
	}

	cr := NewCatalogRegistry()
	cr.BuildFromCatalog(catalogs)

	if cr.Count() != 50 {
		t.Errorf("expected 50 models, got %d", cr.Count())
	}

	// Verify deterministic order.
	all := cr.All()
	for i := 1; i < len(all); i++ {
		if all[i-1].ProviderName > all[i].ProviderName {
			t.Errorf("non-deterministic order at index %d: %s > %s",
				i, all[i-1].ProviderName, all[i].ProviderName)
		}
		if all[i-1].ProviderName == all[i].ProviderName && all[i-1].ModelName > all[i].ModelName {
			t.Errorf("non-deterministic model order at index %d: %s > %s",
				i, all[i-1].ModelName, all[i].ModelName)
		}
	}
}

// --- 9. Empty Catalog Test ---

func TestCatalogRegistry_EmptyCatalog(t *testing.T) {
	cr := NewCatalogRegistry()
	cr.BuildFromCatalog(nil)

	if cr.Count() != 0 {
		t.Errorf("expected 0 for empty catalog, got %d", cr.Count())
	}
	if len(cr.All()) != 0 {
		t.Error("expected empty All() for empty catalog")
	}
	if len(cr.Targets()) != 0 {
		t.Error("expected empty Targets() for empty catalog")
	}
}

// --- 10. Only One Provider/Model Test ---

func TestCatalogRegistry_SingleProviderModel(t *testing.T) {
	cr := NewCatalogRegistry()
	catalogs := []ProviderCatalogFile{
		testCatalogEntry("only", "local", true, []ModelSpec{
			testModel("only-model", true, []string{"fast"}, nil, "local", 0.0),
		}),
	}
	cr.BuildFromCatalog(catalogs)

	if cr.Count() != 1 {
		t.Errorf("expected 1, got %d", cr.Count())
	}
	pm, ok := cr.Get("only", "only-model")
	if !ok {
		t.Fatal("expected to find only/only-model")
	}
	if pm.ProviderKind != "local" {
		t.Errorf("expected local, got %s", pm.ProviderKind)
	}
}
