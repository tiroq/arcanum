# Legacy Profile Removal Report — Iteration 34

**Status:** `CLEAN_AND_READY`  
**Build:** `go build ./...` — clean  
**Tests:** `go test ./... -count=1` — 38 packages, all PASS

---

## Summary

This iteration fully removes the legacy execution profile env-var system (`MODEL_*_PROFILE` and `OLLAMA_*_PROFILE`) from the runtime. The provider/model catalog YAML (`providers/ollama.yaml`) is now the sole source of truth for all execution settings: model fallback chains, thinking mode, timeout, and JSON output mode.

Any service startup that finds one of the 8 removed vars still set in the environment will fail immediately with an explicit error and migration instructions.

---

## Runtime Behavior — Before

```
env: MODEL_FAST_PROFILE=qwen3:1.7b?think=off&timeout=60|qwen3:8b?think=on&timeout=180
           ↓
config.Load() → OllamaConfig.FastProfile (string)
           ↓
cmd/worker/main.go builds routing.Input.DSLOverrides[RoleFast] = "qwen3:1.7b?..."
           ↓
resolver.resolveRole() → profile.ParseProfile(dslString) → []profile.ModelCandidate
           ↓
execution engine receives ordered candidates, attempts each in sequence
```

Aliases `OLLAMA_DEFAULT_PROFILE`, `OLLAMA_FAST_PROFILE`, etc. were silently promoted to `MODEL_*_PROFILE` via `applyProfileBackcompat`.

---

## Runtime Behavior — After

```
providers/ollama.yaml:
  execution_profiles:
    fast:
      - model: qwen3:1.7b
        think: off
        timeout_seconds: 60
      - model: qwen3:8b       # fallback
        think: on
        timeout_seconds: 180
           ↓
provider_catalog.LoadExecutionProfiles("providers/", "ollama", logger)
           ↓
cmd/worker/main.go → routing.Input.CatalogLocalCandidates[RoleFast] = []profile.ModelCandidate{...}
           ↓
resolver.resolveRole() → catalog candidates used directly (no DSL parsing)
           ↓
execution engine receives ordered candidates, attempts each in sequence
```

`OLLAMA_*_MODEL` environment vars (e.g. `OLLAMA_DEFAULT_MODEL`, `OLLAMA_FAST_MODEL`) remain supported as a simple single-candidate fallback when no `execution_profiles` section is configured for a role.

---

## Files Changed

| File | Change |
|------|--------|
| `internal/agent/provider_catalog/types.go` | Added `ExecutionCandidateSpec`, `ExecutionProfilesSpec`, `ExecutionProfiles` field to `ProviderCatalogFile` |
| `internal/agent/provider_catalog/execution_loader.go` | **NEW** — `LoadExecutionProfiles(dir, providerName, logger)` |
| `internal/agent/provider_catalog/execution_loader_test.go` | **NEW** — 12 tests |
| `providers/ollama.yaml` | Added `execution_profiles` section (default/fast/planner/review) |
| `internal/config/config.go` | Removed `DefaultProfile`/`FastProfile`/`PlannerProfile`/`ReviewProfile` from `OllamaConfig`; removed `applyProfileBackcompat`; added `checkLegacyProfileVars` fail-fast |
| `internal/config/config_test.go` | Removed 5 profile tests; added 3 fail-fast tests |
| `internal/providers/routing/resolver.go` | Replaced `DSLOverrides` with `CatalogLocalCandidates` in `Input`; updated `resolveRole` |
| `internal/providers/routing/resolver_test.go` | Replaced `TestResolveProfiles_DSLOverrideWins` with 2 catalog-based tests |
| `cmd/worker/main.go` | Added `provider_catalog` import; replaced DSLOverrides block with `LoadExecutionProfiles` call |
| `.env.example` | Removed 8 profile vars; added catalog-based docs and REMOVED list |
| `README.md` | Replaced profile DSL section with catalog YAML example |

---

## Tests Added / Updated

### New: `internal/agent/provider_catalog/execution_loader_test.go` (12 tests)
- `TestLoadExecutionProfiles_LoadsAllRoles` — all 4 roles loaded with think/timeout/json_mode preserved
- `TestLoadExecutionProfiles_WorkerUsageMatchesCatalog` — execution settings match YAML exactly
- `TestLoadExecutionProfiles_DefaultThinkIsUnset` — absent `think:` → `ThinkDefault` constant
- `TestLoadExecutionProfiles_LocalFallbackStillWorks` — minimal single-model catalog succeeds
- `TestLoadExecutionProfiles_FailsIfFileMissing` — explicit error when provider YAML absent
- `TestLoadExecutionProfiles_FailsIfNoExecutionProfiles` — explicit error when section absent
- `TestLoadExecutionProfiles_FailsIfRoleMissing` — explicit error when required role absent
- `TestLoadExecutionProfiles_FailsIfModelNameEmpty` — explicit error on empty model name
- `TestLoadExecutionProfiles_FailsIfInvalidThinkMode` — explicit error on invalid think value
- `TestLoadExecutionProfiles_InvalidYAMLFails` — malformed YAML returns parse error
- `TestExecutionProfiles_ModelProfileEnvVarNotConsumed` — env vars do not influence loader

### Updated: `internal/config/config_test.go`
- **Removed:** `TestOllamaProfileEnvVars`, `TestOllamaProfileBackcompat`, `TestOllamaProfileNewTakesPrecedenceOverOld`, `TestLegacyOllamaProfileDeprecationWarning`, `TestLegacyOllamaProfileIsExecutionOnly`
- **Added:** `TestLegacyProfileVars_ModelProfileFailsFast` (4 sub-cases), `TestLegacyProfileVars_OllamaProfileFailsFast` (4 sub-cases), `TestLegacyProfileVars_NoneSetSucceeds`

### Updated: `internal/providers/routing/resolver_test.go`
- **Removed:** `TestResolveProfiles_DSLOverrideWins`
- **Added:** `TestResolveProfiles_CatalogCandidatesWin`, `TestResolveProfiles_CatalogCandidates_CloudDisabled`

---

## Regression Summary

```
go build ./...           → no output (clean)
go test ./... -count=1   → 38 packages ok, 0 FAIL
```

Key packages verified: `internal/config`, `internal/agent/provider_catalog`, `internal/providers/routing`, `internal/agent/provider_routing`, `internal/providers/execution`.

---

## Remaining Notes

- `OLLAMA_DEFAULT_MODEL`, `OLLAMA_FAST_MODEL`, `OLLAMA_PLANNER_MODEL`, `OLLAMA_REVIEW_MODEL` are **not** removed — they remain as simple single-candidate fallbacks when no catalog execution_profiles are configured for a role. This is intentional backward compatibility for minimal deployments.
- The `profile.ParseProfile()` function and DSL parser remain in the codebase (`internal/providers/profile/`) as they are used by the `profile.RoleProfiles` loading path; no behavior is changed in execution itself.
- `PROVIDERS_CATALOG_DIR` env var (default: `providers/`) controls where the YAML catalog is loaded from. This was already wired in Iteration 32.

---

## Rollout Recommendation

**CLEAN_AND_READY.**

Operators must migrate before deploying this version:
1. Remove `MODEL_DEFAULT_PROFILE`, `MODEL_FAST_PROFILE`, `MODEL_PLANNER_PROFILE`, `MODEL_REVIEW_PROFILE` from all environment configurations.
2. Remove `OLLAMA_DEFAULT_PROFILE`, `OLLAMA_FAST_PROFILE`, `OLLAMA_PLANNER_PROFILE`, `OLLAMA_REVIEW_PROFILE` (legacy aliases).
3. Add execution settings to `providers/ollama.yaml` under `execution_profiles`.

The service will refuse to start if any of the 8 removed vars are still set, with an explicit error pointing to this document.
