# Provider Routing Precedence + Legacy Cleanup Report

> Iteration 33 — April 9, 2026

---

## 1. Summary

### What Was Discovered

The codebase contains **two completely separate routing systems** that were not clearly documented or distinguished:

1. **Worker Execution Routing** (`internal/providers/routing`) — governs which local Ollama model and execution options (think mode, timeout, JSON mode) are used within the worker process for actual LLM calls.

2. **Agent Decision Graph Routing** (`internal/agent/provider_routing`) — governs which provider (ollama, openrouter, cerebras, etc.) the agent's decision graph selects when planning the execution of a task.

These two systems share no code, no inputs, and no state. Legacy `OLLAMA_*_PROFILE` env vars feed only into system (1), not system (2).

### Legacy Env Status

`OLLAMA_DEFAULT_PROFILE`, `OLLAMA_FAST_PROFILE`, `OLLAMA_PLANNER_PROFILE`, `OLLAMA_REVIEW_PROFILE` were mapped to `MODEL_*_PROFILE` names and did influence model selection in the worker execution layer. However, they had **no influence whatever on the agent decision graph routing engine**.

Classification: **active_but_execution_only** — not routing-relevant.

### `providers/_global.yaml` Status — Fixed

Before this iteration, `providers/_global.yaml` was **completely decorative**:

- The `LoadCatalog` function read it but tried to parse it as a `ProviderCatalogFile`
- `ValidateCatalogEntry` rejected it (provider.name empty) with a WARN log
- **Zero fields from `_global.yaml` were enforced at runtime**

After this iteration, `_global.yaml` is:

- Loaded via a dedicated `LoadGlobalPolicy` function (separate from catalog loader)
- Wired into the `Router` via `WithGlobalPolicy`
- **Fully enforced**: `allow_external`, `max_fallback_chain`, role preferences, and degrade_policy ordering are all actively applied to routing decisions

---

## 2. Actual Runtime Precedence Chain

### Agent Decision Graph Provider Routing (api-gateway service)

This governs which provider is selected for agent planning tasks (annotated on StrategyOverride).

```
Input source                 Code location                         Active?  Routing?  Model?
─────────────────────────────────────────────────────────────────────────────────────────────
providers/_global.yaml       LoadGlobalPolicy + Router.WithGlobalPolicy  YES   YES       partial
  └─ allow_external          Router.Route gating                    YES   YES       no
  └─ max_fallback_chain      buildFallbackChain                     YES   YES       no
  └─ role preferences        preferenceBoostFor in scoreProviders   YES   YES       no
  └─ degrade_policy          buildFallbackChain tier sort           YES   YES       no

providers/*.yaml catalog     LoadCatalog + CatalogRegistry          YES   YES*      YES*
  └─ provider registry       hardcoded in main.go (not catalog)     YES   YES       no
  └─ catalog registry        CatalogRegistry.All()                  YES   no**      YES
  [** catalog not connected to Router.Route; used only for API endpoints]

provider_routing Router       internal/agent/provider_routing        YES   YES       limited
  └─ scoring (5 components)  scorer.go                              YES   YES       no
  └─ role filter             filterProvider                         YES   YES       no
  └─ quota filter            CheckQuota                             YES   YES       no
  └─ global policy gate      Router.Route (allow_external)          YES   YES       no

OLLAMA_*_PROFILE env vars    → not read by routing engine           no    no        no
MODEL_*_PROFILE env vars     → not read by routing engine           no    no        no
ROUTING_*_ESCALATION vars    → not read by routing engine           no    no        no
```

*NOTE: The `CatalogRegistry` is loaded and available for the API (`GET /api/v1/agent/providers/catalog`, `/targets`) but is NOT wired into the `Router.Route` decision. The Router's provider registry is populated with hardcoded provider definitions in `cmd/api-gateway/main.go`. This is intentional: the catalog defines model-level metadata while the routing engine works at provider level.

### Worker Execution Routing (worker service)

This governs which local Ollama model and execution options are used for actual LLM calls.

```
Input source                 Code location                         Active?  Provider?  Model?
──────────────────────────────────────────────────────────────────────────────────────────────
MODEL_DEFAULT_PROFILE        OllamaConfig.DefaultProfile            YES     no         YES (local)
MODEL_FAST_PROFILE           OllamaConfig.FastProfile               YES     no         YES (local)
MODEL_PLANNER_PROFILE        OllamaConfig.PlannerProfile            YES     no         YES (local)
MODEL_REVIEW_PROFILE         OllamaConfig.ReviewProfile             YES     no         YES (local)
  └── OLLAMA_*_PROFILE       applyProfileBackcompat (alias)         YES     no         YES (local)
  [All mapped into DSLOverrides → routing.ResolveProfiles]
  [DSLOverrides bypass ROUTING_*_ESCALATION policy when set]

ROUTING_*_ESCALATION vars    routing.RoutingPolicy                  YES     YES        YES (local)
  [active ONLY when MODEL_*_PROFILE is not set for a role]

OLLAMA_*_MODEL vars          localModelForRole                      YES     no         YES (local)
  [active ONLY when MODEL_*_PROFILE is not set for a role]

providers/*.yaml catalog     → NOT used by worker routing           no      no         no
providers/_global.yaml       → NOT used by worker routing           no      no         no
agent provider_routing Router→ NOT used by worker routing           no      no         no
```

---

## 3. Legacy Env Classification

### OLLAMA_DEFAULT_PROFILE

| Field            | Value |
|------------------|-------|
| Classification   | **active_but_execution_only** |
| Code location    | `internal/config/config.go:193` (`applyProfileBackcompat`) |
| Maps to          | `MODEL_DEFAULT_PROFILE` → `OllamaConfig.DefaultProfile` |
| Worker effect    | Selects which local Ollama model runs for the "default" role |
| Routing effect   | None — never passed to `Router.Route` or scoring engine |
| Warning emitted  | Yes: `[config] DEPRECATED: OLLAMA_DEFAULT_PROFILE is deprecated; rename to MODEL_DEFAULT_PROFILE (execution-only: ...)` |

### OLLAMA_FAST_PROFILE

| Field            | Value |
|------------------|-------|
| Classification   | **active_but_execution_only** |
| Code location    | `internal/config/config.go:194` |
| Maps to          | `MODEL_FAST_PROFILE` → `OllamaConfig.FastProfile` |
| Worker effect    | Selects local model chain + think/timeout/json for fast role |
| Routing effect   | None |
| Warning emitted  | Yes |

### OLLAMA_PLANNER_PROFILE

| Field            | Value |
|------------------|-------|
| Classification   | **active_but_execution_only** |
| Code location    | `internal/config/config.go:195` |
| Maps to          | `MODEL_PLANNER_PROFILE` → `OllamaConfig.PlannerProfile` |
| Worker effect    | Selects local model chain + think/timeout/json for planner role |
| Routing effect   | None |
| Warning emitted  | Yes |

### OLLAMA_REVIEW_PROFILE

| Field            | Value |
|------------------|-------|
| Classification   | **active_but_execution_only** |
| Code location    | `internal/config/config.go:196` |
| Maps to          | `MODEL_REVIEW_PROFILE` → `OllamaConfig.ReviewProfile` |
| Worker effect    | Selects local model chain + think/timeout/json for reviewer role |
| Routing effect   | None |
| Warning emitted  | Yes |

---

## 4. Global Policy Validation

### Before This Iteration

| Field                          | Status  |
|-------------------------------|---------|
| `providers/_global.yaml` loaded | No — silently skipped by `LoadCatalog` due to schema mismatch |
| `routing_policy.allow_external` | Ignored |
| `routing_policy.max_fallback_chain` | Ignored |
| `routing_policy.priorities` (role preferences) | Ignored |
| `routing_policy.degrade_policy` | Ignored |
| `routing_policy.constraints` | Ignored |
| `routing_policy.prefer_free` | Ignored |

### After This Iteration

| Field                          | Status  | Enforcement |
|-------------------------------|---------|-------------|
| `providers/_global.yaml` loaded | **Yes** via `LoadGlobalPolicy` | `cmd/api-gateway/main.go` |
| `routing_policy.allow_external` | **Enforced** | `Router.Route` gates `input.AllowExternal` |
| `routing_policy.max_fallback_chain` | **Enforced** | `buildFallbackChain` uses `policy.MaxFallbackChain` |
| `routing_policy.priorities` | **Enforced** | `preferenceBoostFor` adds scoring boost (0.10 → 0.0 by position) |
| `routing_policy.degrade_policy` | **Enforced** | `buildFallbackChain` re-sorts by tier (external_strong → router → local) |
| `routing_policy.constraints` | Parsed, not yet enforced (see §9) |
| `routing_policy.prefer_free` | Parsed, not yet enforced (see §9) |

### What Was Fixed

1. **`LoadGlobalPolicy` function added** — `internal/agent/provider_catalog/global_policy.go`
   - Reads `providers/_global.yaml` directly (not through catalog loader)
   - Fails-open when file is absent

2. **Catalog loader now skips `_`-prefixed files** — `internal/agent/provider_catalog/loader.go`
   - Files starting with `_` are meta/config files and are skipped
   - Eliminates the warning log that previously appeared on every startup

3. **`GlobalPolicyConfig` type added** — `internal/agent/provider_routing/types.go`
   - Bridge type to avoid import cycles between `provider_catalog` and `provider_routing`

4. **`Router.WithGlobalPolicy` method added** — `internal/agent/provider_routing/router.go`
   - Attaches policy to router; safe to call with nil (no-op, preserves existing behavior)

5. **`Router.Route` applies `AllowExternal` gate** — global policy can block external providers globally

6. **`scoreProviders` applies preference boost** — position-based boost (0.10 → 0.00 decreasing by 0.03 per position)

7. **`buildFallbackChain` applies tier ordering** — degrade_policy sort + respects `MaxFallbackChain` from policy

8. **`cmd/api-gateway/main.go` wired** — loads global policy at startup, converts to `GlobalPolicyConfig`, calls `WithGlobalPolicy`

---

## 5. Cleanup Changes

### `internal/agent/provider_catalog/global_policy.go` — NEW

- `GlobalPolicy`, `GlobalRoutingPolicy`, `RolePriority`, `GlobalPolicyConstraints` types
- `LoadGlobalPolicy(dir, logger)` function

### `internal/agent/provider_catalog/loader.go` — MODIFIED

- Added `"strings"` import
- Added `strings.HasPrefix(e.Name(), "_")` check to skip meta files
- Before: `_global.yaml` was read, parse attempted, validation failed, WARN logged every startup
- After: `_global.yaml` is silently skipped by `LoadCatalog`; loaded via `LoadGlobalPolicy`

### `internal/agent/provider_routing/types.go` — MODIFIED

- Added `GlobalPolicyConfig` struct (bridge type, no import cycle)
- Before: no way to pass global policy to Router
- After: Router accepts GlobalPolicyConfig via `WithGlobalPolicy`

### `internal/agent/provider_routing/scorer.go` — MODIFIED

- Added `PreferenceBoost float64` field to `ScoreComponents`
- Updated `FormatScoreReason` to include preference boost in log output when non-zero
- Before: preference boost was invisible in trace output
- After: operators can see per-provider preference boost in routing trace

### `internal/agent/provider_routing/router.go` — MODIFIED

- Added `policy *GlobalPolicyConfig` field to `Router`
- Added `WithGlobalPolicy(cfg *GlobalPolicyConfig) *Router` method
- Modified `Route` to apply global `AllowExternal` gate
- Modified `scoreProviders` to apply preference boost via `preferenceBoostFor`
- Modified `buildFallbackChain` to respect `policy.MaxFallbackChain` and apply `degradeTierIndex` sort
- Added `preferenceBoostFor`, `degradeTierIndex` helper methods
- Before: routing was purely score-based with no policy influence
- After: global policy preferences, allow_external gate, max chain, and degrade ordering are all applied

### `internal/config/config.go` — MODIFIED

- Updated `applyProfileBackcompat` godoc and warning message
- Warning now explicitly states: **"execution-only: affects local Ollama model selection and options, NOT provider routing"**

### `.env.example` — MODIFIED

- Added explicit section header: "Execution Profiles (advanced, **execution-only**)"
- Added warning block: profiles control LOCAL EXECUTION ONLY, not provider routing
- Listed the authoritative routing sources (global.yaml, catalog, routing engine)
- Added deprecation aliases section for OLLAMA_*_PROFILE

---

## 6. Tests

### New Tests Added

#### `internal/agent/provider_catalog/global_policy_test.go` — 7 new tests

| Test | Covers |
|------|--------|
| `TestLoadGlobalPolicy_ReturnsNilWhenMissing` | Fail-open on missing file |
| `TestLoadGlobalPolicy_ParsesFullPolicy` | All fields from _global.yaml parsed correctly |
| `TestLoadGlobalPolicy_AllowExternalFalse` | allow_external=false parsed |
| `TestLoadGlobalPolicy_InvalidYAMLReturnsError` | Parse error returned |
| `TestLoadGlobalPolicy_EmptyFileReturnsEmptyPolicy` | Empty file → zero-value policy |
| `TestLoadCatalog_SkipsUnderscorePrefixedFiles` | _global.yaml skipped by LoadCatalog |
| `TestLoadCatalog_SkipsAllUnderscorePrefixedFiles` | All _ files skipped |

#### `internal/agent/provider_routing/provider_routing_test.go` — 11 new tests

| Test | Covers DoD item |
|------|----------------|
| `TestGlobalPolicy_FastRolePreferenceOrdering` | Test 4.3.8: fast prefers groq/gemini/ollama |
| `TestGlobalPolicy_PlannerRolePreferenceOrdering` | Test 4.3.9: planner prefers cerebras/... |
| `TestGlobalPolicy_FallbackRolePreferenceOrdering` | Test 4.3.10: fallback prefers openrouter/ollama |
| `TestGlobalPolicy_AllowExternalFalseBlocksExternalProviders` | Test 4.3.11: allow_external=false blocks external |
| `TestGlobalPolicy_MaxFallbackChainFromPolicy` | Test 4.1.4: max_fallback_chain respected |
| `TestGlobalPolicy_DegradePolicyOrderingInFallbackChain` | Test 4.3.12: degrade_policy deterministic |
| `TestLegacyEnv_OllamaProfileDoesNotAffectProviderRouting` | Tests 4.2.5+4.2.6: legacy env isolated |
| `TestGlobalPolicy_LocalOnlyModeRegression` | Test 4.4.13: local-only mode works |
| `TestGlobalPolicy_RoutingDecisionsPersistWithPolicy` | Test 4.4.15: decisions persist |
| `TestGlobalPolicy_PreferenceBoostBounded` | Score never exceeds 1.0 |
| `TestGlobalPolicy_NilPolicyPreservesExistingBehavior` | Nil policy = no behavior change |

#### `internal/config/config_test.go` — 2 new tests

| Test | Covers DoD item |
|------|----------------|
| `TestLegacyOllamaProfileDeprecationWarning` | Test 4.2.7: deprecation warning emitted |
| `TestLegacyOllamaProfileIsExecutionOnly` | Test 4.2.6: profiles are execution-only (type-enforced) |

### Test Results

All 38 packages: **PASS** (0 failures)

```
ok  github.com/tiroq/arcanum/internal/agent/provider_catalog
ok  github.com/tiroq/arcanum/internal/agent/provider_routing
ok  github.com/tiroq/arcanum/internal/config
ok  [35 other packages unchanged]
```

---

## 7. Regression Summary

| Area | Status |
|------|--------|
| Build (`go build ./...`) | **PASS** |
| All 38 packages | **PASS** |
| Existing provider routing tests (24 tests) | **PASS** |
| Existing catalog tests | **PASS** |
| Existing config tests | **PASS** |
| Decision graph tests | **PASS** |
| Worker execution tests | **PASS** |

No regressions introduced. The `WithGlobalPolicy(nil)` no-op path preserves all existing behavior.

---

## 8. Recommended Env Surface

| Variable | Status | Replacement | Rationale |
|----------|--------|-------------|-----------|
| `OLLAMA_DEFAULT_PROFILE` | **deprecate** | `MODEL_DEFAULT_PROFILE` | Alias for backcompat; emits warning; execution-only |
| `OLLAMA_FAST_PROFILE` | **deprecate** | `MODEL_FAST_PROFILE` | Same |
| `OLLAMA_PLANNER_PROFILE` | **deprecate** | `MODEL_PLANNER_PROFILE` | Same |
| `OLLAMA_REVIEW_PROFILE` | **deprecate** | `MODEL_REVIEW_PROFILE` | Same |
| `MODEL_DEFAULT_PROFILE` | **keep** | — | Execution-only; controls worker local Ollama execution options |
| `MODEL_FAST_PROFILE` | **keep** | — | Same |
| `MODEL_PLANNER_PROFILE` | **keep** | — | Same |
| `MODEL_REVIEW_PROFILE` | **keep** | — | Same |
| `ROUTING_FAST_ESCALATION` | **keep** | — | Worker routing policy when no MODEL_FAST_PROFILE set |
| `ROUTING_DEFAULT_ESCALATION` | **keep** | — | Same |
| `ROUTING_PLANNER_ESCALATION` | **keep** | — | Same |
| `ROUTING_REVIEW_ESCALATION` | **keep** | — | Same |
| `PROVIDERS_CATALOG_DIR` | **keep** | — | Dir for providers/*.yaml + _global.yaml |
| `OPENROUTER_ENABLED` | **keep** | — | Gates OpenRouter provider in decision-graph routing |
| `OLLAMA_CLOUD_ENABLED` | **keep** | — | Gates Ollama Cloud provider in decision-graph routing |

### Env vars to remove from `.env.example` (recommended next step)

The deprecated `OLLAMA_*_PROFILE` vars are already absent from `.env.example` (only their modern `MODEL_*_PROFILE` counterparts appear). The `.env.example` now includes an explicit deprecation notice. No further removal is needed at this time.

---

## 9. Remaining Risks

### 9.1 `routing_policy.constraints` Not Enforced

`constraints.latency_sensitive_threshold_ms` and `constraints.heavy_task_tokens_threshold` are parsed but not applied to routing decisions. The current scoring already uses `LatencyBudgetMs` from `RoutingInput`, but there's no automatic derivation of `LatencyBudgetMs` from the global policy threshold. A future iteration should:
- Propagate `latency_sensitive_threshold_ms` as a default `LatencyBudgetMs` when callers don't set one
- Use `heavy_task_tokens_threshold` to influence `EstimatedTokens` defaults

### 9.2 `routing_policy.prefer_free` Not Enforced

`prefer_free: true` is parsed and stored in `GlobalPolicyConfig` but the scoring engine doesn't currently use it. The `CostEfficiency` component already favors `CostLocal` and `CostFree` providers, so this is partially covered. A future iteration could add a `PreferFreeBoost` to providers with `CostClass=free|local` when `prefer_free=true`.

### 9.3 CatalogRegistry Not Wired Into Decision-Graph Routing

The `CatalogRegistry` (loaded from `providers/*.yaml`) is available in `api-gateway` and exposed via API but is NOT connected to `Router.Route`. The Router's provider registry is populated with hardcoded provider definitions. This means model-level metadata from the catalog (capabilities, cost, roles) doesn't influence the routing engine directly. Full catalog-to-routing integration would be a larger architectural step.

### 9.4 Worker Execution Still Uses DSL Override as Highest Priority

In `cmd/worker/main.go`, `MODEL_*_PROFILE` env vars bypass the `ROUTING_*_ESCALATION` policy entirely (see `resolver.go:107`: "DSL override wins over policy"). This was intentional design but creates a situation where the worker's model selection is not governed by any catalog. A future iteration could deprecate `MODEL_*_PROFILE` entirely and replace with catalog-driven model selection.

---

## 10. Rollout Recommendation

**READY_WITH_LEGACY_WARNINGS**

- `_global.yaml` is now active and enforced
- Legacy `OLLAMA_*_PROFILE` vars are execution-only and clearly isolated
- Deprecation warnings are emitted when legacy vars are used
- All tests pass with no regressions
- Two minor features (`prefer_free`, `constraints`) are parsed but not yet enforced (see §9)
