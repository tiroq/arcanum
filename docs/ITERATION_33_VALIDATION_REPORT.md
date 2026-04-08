# Iteration — Provider Catalog Validator Layer: Validation Report

## 1. Overview

### What Was Implemented

A strict, deterministic provider catalog validation layer exposed both as a reusable Go library and a standalone CLI tool.

**Library** (`internal/agent/provider_catalog/`):
- `validation_types.go` — Structured output types: `ValidationSeverity`, `ValidationIssue`, `ValidationResult`
- `validation_rules.go` — All validation rule implementations (provider, connection, limits, routing, model, cross-file)
- `validator.go` — Public API: `ValidateCatalogDir()`, `ValidateCatalogFiles()`, `ValidateCatalogEntryStructured()`, `ValidateCatalog()`; plus backward-compatible legacy `ValidateCatalogEntry()`

**CLI** (`cmd/provider-catalog-validate/`):
- `main.go` — Thin wrapper over the library with `--dir`, `--format`, and `--fail-on-warning` flags

### Why Library + CLI

1. **Library** enables startup validation, test assertions, and programmatic access without subprocess overhead
2. **CLI** enables CI pipeline integration, pre-commit hooks, and developer-friendly local validation
3. All validation logic lives in the library; the CLI contains zero business logic

---

## 2. Architecture

### Package Layout

```
internal/agent/provider_catalog/
  validation_types.go    # ValidationSeverity, ValidationIssue, ValidationResult
  validation_rules.go    # All rule implementations (per-entry + cross-file)
  validator.go           # Public API + legacy compatibility
  validation_test.go     # 44 validation-specific tests

cmd/provider-catalog-validate/
  main.go                # CLI wrapper (flags, output, exit codes)
```

### Data Flow

```
YAML files on disk
  → ValidateCatalogDir(dir)
    → os.ReadDir → filter .yaml/.yml → sort → ValidateCatalogFiles(files)
      → for each file:
        → os.ReadFile → yaml.Unmarshal
        → skip non-provider files (e.g. defaults.yaml)
        → validateSingleEntry(entry, filename)
          → validateProviderFields
          → validateConnectionFields
          → validateLimitsFields
          → validateRoutingFields
          → validateModels → validateModelFields (per model)
      → validateCrossFile(allEntries, allFilenames)
        → duplicate providers
        → duplicate targets
        → critical role coverage
        → local fallback check
        → all-external check
    → buildResult(issues) → sortIssues → count errors/warnings → ValidationResult
```

---

## 3. Validation Rules

### Schema Rules (Per-File)

| Code | Severity | Rule |
|------|----------|------|
| `yaml_parse_error` | error | File must parse as valid YAML |
| `provider_name_missing` | error | `provider.name` required |
| `provider_kind_missing` | error | `provider.kind` required |
| `provider_kind_invalid` | error | Must be `local`, `cloud`, or `router` |
| `provider_enabled_without_models` | error/warn | Enabled provider must have models (error if none, warning if all disabled) |
| `model_name_missing` | error | `models[].name` required |
| `model_duplicate` | error | Model names unique within provider |
| `enabled_model_cap_exceeded` | error | Max 10 models per provider |
| `api_key_env_invalid` | error | Must match `^[A-Za-z_][A-Za-z0-9_]*$` |
| `timeout_invalid` | error | `timeout_seconds` must be >= 0 |
| `limit_negative` | error | `rpm`, `tpm`, `rpd`, `tpd` must be >= 0 |
| `fallback_priority_invalid` | error | Must be >= 0 |

### Semantic Rules (Per-Model)

| Code | Severity | Rule |
|------|----------|------|
| `model_cost_class_invalid` | error | Must be `free`, `local`, `cheap`, `promo`, or `unknown` |
| `model_relative_cost_out_of_range` | error | Must be [0.0, 1.0] |
| `model_max_output_tokens_invalid` | error | Must be >= 0 |
| `role_invalid` | warning | Roles should be `fast`, `planner`, `reviewer`, `batch`, or `fallback` |
| `model_capability_invalid` | warning | Capabilities should be `json_mode`, `long_context`, `low_latency`, `tool_calling`, or `structured_output` |
| `model_enabled_without_roles` | warning | Enabled model with no roles and no provider roles to inherit |

### Cross-File Rules

| Code | Severity | Rule |
|------|----------|------|
| `provider_duplicate` | error | Provider names unique across directory |
| `provider_model_duplicate` | error | (provider, model) targets unique across directory |
| `critical_role_missing` | error | At least one enabled model must cover `fast`, `planner`, `fallback` |
| `no_local_fallback_warning` | warning | No enabled local provider exists |
| `all_external_warning` | warning | All enabled providers are external |

---

## 4. CLI Interface

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--dir` | `providers` | Path to provider catalog directory |
| `--format` | `text` | Output format: `text` or `json` |
| `--fail-on-warning` | `false` | Treat warnings as validation failures |

### Output Modes

**Text mode** — human-readable:
```
Provider catalog validation: VALID
  Errors:   0
  Warnings: 0
```

**JSON mode** — machine-readable:
```json
{
  "valid": true,
  "error_count": 0,
  "warning_count": 0,
  "issues": null
}
```

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Valid catalog |
| 1 | Validation errors (or warnings with `--fail-on-warning`) |
| 2 | Runtime/internal error |

---

## 5. Example Validation Output

### Valid Catalog

```
$ provider-catalog-validate --dir providers --format text
Provider catalog validation: VALID
  Errors:   0
  Warnings: 0
```

### Invalid Catalog

```
$ provider-catalog-validate --dir /tmp/bad --format text
Provider catalog validation: INVALID
  Errors:   6
  Warnings: 2

Issues:
  [WARN ] (catalog): all enabled providers are external (all_external_warning)
  [WARN ] (catalog): no enabled local provider exists (no_local_fallback_warning)
  [ERROR] (catalog) > roles: no enabled model covers critical role "fast" (critical_role_missing)
  [ERROR] (catalog) > roles: no enabled model covers critical role "planner" (critical_role_missing)
  [ERROR] (catalog) > roles: no enabled model covers critical role "fallback" (critical_role_missing)
  [ERROR] bad.yaml > models: enabled provider must have at least one model (provider_enabled_without_models)
  [ERROR] bad.yaml > provider.kind: provider.kind "satellite" is not valid (provider_kind_invalid)
  [ERROR] bad.yaml > provider.name: provider.name is required (provider_name_missing)
```

---

## 6. Test Results

All 44 validation tests pass. Full package (including existing 20+ tests) passes with zero regressions.

| # | Test | Outcome |
|---|------|---------|
| 1 | `TestValidation_ValidYAMLFile` | PASS |
| 2 | `TestValidation_InvalidYAMLFile` | PASS |
| 3 | `TestValidation_NonYAMLFileIgnored` | PASS |
| 4 | `TestValidation_EmptyDirectory` | PASS |
| 5 | `TestValidation_MissingDirectory` | PASS |
| 6 | `TestValidation_MissingProviderName` | PASS |
| 7 | `TestValidation_InvalidProviderKind` | PASS |
| 8 | `TestValidation_MissingProviderKind` | PASS |
| 9 | `TestValidation_DuplicateProviderName` | PASS |
| 10 | `TestValidation_EnabledProviderWithNoModels` | PASS |
| 11 | `TestValidation_DisabledProviderWithNoModels` | PASS |
| 12 | `TestValidation_DuplicateModelName` | PASS |
| 13 | `TestValidation_ModelNameMissing` | PASS |
| 14 | `TestValidation_InvalidCostClass` | PASS |
| 15 | `TestValidation_RelativeCostOutOfRange` | PASS |
| 16 | `TestValidation_RelativeCostNegative` | PASS |
| 17 | `TestValidation_InvalidRole` | PASS |
| 18 | `TestValidation_InvalidCapability` | PASS |
| 19 | `TestValidation_InvalidMaxOutputTokens` | PASS |
| 20 | `TestValidation_EnabledModelWithoutRoles` | PASS |
| 21 | `TestValidation_EnabledModelInheritsProviderRoles` | PASS |
| 22 | `TestValidation_InvalidAPIKeyEnv` | PASS |
| 23 | `TestValidation_ValidAPIKeyEnv` | PASS |
| 24 | `TestValidation_NegativeTimeout` | PASS |
| 25 | `TestValidation_NegativeLimit` | PASS |
| 26 | `TestValidation_ZeroLimitsAreValid` | PASS |
| 27 | `TestValidation_NegativeFallbackPriority` | PASS |
| 28 | `TestValidation_InvalidRoutingRole` | PASS |
| 29 | `TestValidation_MissingCriticalRole` | PASS |
| 30 | `TestValidation_NoLocalFallbackWarning` | PASS |
| 31 | `TestValidation_AllExternalWarning` | PASS |
| 32 | `TestValidation_TooManyEnabledModels` | PASS |
| 33 | `TestValidation_DuplicateProviderModelTarget` | PASS |
| 34 | `TestValidation_TextOutput` | PASS |
| 35 | `TestValidation_JSONOutput` | PASS |
| 36 | `TestValidation_JSONOutputWithErrors` | PASS |
| 37 | `TestValidation_DeterministicIssueOrdering` | PASS |
| 38 | `TestValidation_IssueOrderingStable` | PASS |
| 39 | `TestValidation_ExistingProviderCatalogPasses` | PASS |
| 40 | `TestValidation_LegacyValidateCatalogEntryCompatibility` | PASS |
| 41 | `TestValidation_DisabledProviderAllModelsDisabled` | PASS |
| 42 | `TestValidation_PromoCostClassAccepted` | PASS |
| 43 | `TestValidation_DefaultsYAMLSkipped` | PASS |
| 44 | `TestValidation_ValidateFromCatalogFiles` | PASS |
| 45 | `TestValidation_MultipleNegativeLimits` | PASS |
| 46 | `TestValidation_ValidCatalogWithWarningsStillValid` | PASS |
| 47 | `TestValidation_YMLExtensionAccepted` | PASS |

---

## 7. Regression Summary

| Check | Status |
|-------|--------|
| `go build ./...` | PASS |
| `go test ./internal/agent/provider_catalog/` | PASS (all existing + new tests) |
| Legacy `ValidateCatalogEntry()` | Preserved, backward compatible |
| Existing `LoadCatalog()` | Unchanged, still uses legacy validator |
| CLI build | PASS |
| CLI against project `providers/` | PASS (exit 0, valid) |

---

## 8. Risks

### What Validator Cannot Catch

1. **Runtime connectivity** — whether provider URLs are reachable
2. **Secret validity** — whether API keys in env vars are valid
3. **Model availability** — whether declared models actually exist at the provider
4. **Provider-specific schema** — provider-specific required fields beyond the common schema
5. **Semantic drift** — whether a model's capabilities description matches its actual behavior

### What Remains Runtime-Only

1. Actual provider health and latency
2. Quota exhaustion
3. Model deprecation by providers
4. Token limit enforcement during inference

---

## 9. Next Step Recommendation

1. **Startup integration** — call `ValidateCatalogDir()` at api-gateway startup before `LoadCatalog()`, abort in strict mode on errors
2. **CI integration** — add `go run ./cmd/provider-catalog-validate/ --dir providers --fail-on-warning` to CI pipeline
3. **Makefile target** — add `make validate-providers` for developer convenience
4. **Schema evolution** — as provider YAML schema grows (e.g. model-level limits, preferred_for), extend validation rules
5. **Provider-specific validators** — optional per-kind validators (e.g. router must have fallback semantics)
