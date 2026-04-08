# Iteration 32 — Provider Catalog + Model-Aware Routing — Validation Report

## 1. Architecture Summary

Iteration 32 introduces a **declarative YAML-driven provider catalog** and extends the existing provider routing engine from provider-level to **provider+model-level** routing. The system now selects not just which provider to use, but which specific model on that provider.

### New Package: `internal/agent/provider_catalog/`

| File | Purpose |
|---|---|
| `types.go` | Core domain types: `ProviderCatalogFile`, `ProviderSpec`, `ModelSpec`, `ProviderModel`, `RoutingTarget` |
| `loader.go` | Loads and validates all `*.yaml` / `*.yml` files from a directory; fail-open on missing directory |
| `validator.go` | Schema + semantic validation: name, kind, cost class, relative cost range, model count bound, duplicate detection |
| `registry.go` | In-memory `CatalogRegistry` — thread-safe, deterministic sort (provider ASC, model ASC) |
| `resolver.go` | Resolves provider+model candidates by role, capability, and external/local policy |

### Modified Package: `internal/agent/provider_routing/`

| File | Change |
|---|---|
| `types.go` | 5-component scoring weights; `RoutingInput.RequiredCapabilities`; `RoutingDecision.SelectedModel`; `RoutingTarget` type; `RankedProvider.Model`/`RejectedProvider.Model`; `RoutingRecord.SelectedModel` |
| `scorer.go` | 5-component scoring: latency (0.30) + quota (0.25) + reliability (0.20) + cost (0.15) + model_capability (0.10) |
| `router.go` | Model-aware tie-breaking (relative_cost + model lexical); model-aware fallback chain (same provider, different models allowed); model in audit payloads |
| `adapter.go` | `RouteForTask` returns 4 values (added `selectedModel`) |

### Integration Points

| Component | Change |
|---|---|
| `decision_graph/planner_adapter.go` | Added `ProviderRoutingProvider` interface; `WithProviderRouting()` wiring; calls `RouteForTask` in `EvaluateForPlanner`; emits `provider.target_selected` audit event |
| `planning/planner.go` | `StrategyOverride` gains `SelectedProvider`/`SelectedModel`; propagates `_ctx_provider` and `_ctx_model` in action params |
| `api/handlers.go` | `WithProviderCatalog()` wiring; `ProviderCatalog()` and `ProviderTargets()` handlers |
| `api/router.go` | `GET /api/v1/agent/providers/catalog` and `GET /api/v1/agent/providers/targets` |
| `cmd/api-gateway/main.go` | Loads catalog from `PROVIDERS_CATALOG_DIR` (default: `providers/`); builds `CatalogRegistry`; wires into handlers and decision graph |
| `internal/config/config.go` | `ProvidersConfig.CatalogDir` field (`PROVIDERS_CATALOG_DIR` env var) |

### Database Migration

- **000036**: `agent_provider_model_usage` table with `(provider_name, model_name)` composite primary key

## 2. YAML Catalog Examples

### `providers/ollama.yaml` (local)
```yaml
provider:
  name: ollama
  kind: local
  enabled: true
models:
  - name: qwen3:1.7b      # fast, low-latency
  - name: qwen2.5:7b-instruct  # planner/reviewer
  - name: llama3.2:3b      # fallback
```

### `providers/groq.yaml` (cloud)
```yaml
provider:
  name: groq
  kind: cloud
  enabled: true
models:
  - name: llama-3.3-70b-versatile  # planner, json_mode + long_context
  - name: llama-3.1-8b-instant     # fast
```

## 3. Scoring Model

### 5-Component Weighted Score

| Component | Weight | Source |
|---|---|---|
| `latency_fit` | 0.30 | Provider kind vs latency budget |
| `quota_headroom` | 0.25 | Remaining quota across RPM/TPM/RPD/TPD |
| `reliability_fit` | 0.20 | Provider health state |
| `cost_efficiency` | 0.15 | Cost class mapping |
| `model_capability_fit` | 0.10 | Fraction of required capabilities matched |

**Backward compatible**: When no capabilities are required, `model_capability_fit = 1.0` (no penalty).

### Tie-Breaking Order (deterministic)

1. Score DESC (epsilon < 0.001)
2. Local before external
3. Lower `relative_cost` first
4. Model name ASC (lexicographic)
5. Provider name ASC (lexicographic)

## 4. Routing Flow

```
YAML Catalog → CatalogRegistry (startup)
                    ↓
Task arrives → ResolveCandidates (filter by role, capability, external policy)
                    ↓
Provider Registry → Route() → Score → Sort → Select primary + fallback chain
                    ↓
RoutingDecision { SelectedProvider, SelectedModel, FallbackChain, Trace }
                    ↓
GraphPlannerAdapter → StrategyOverride { SelectedProvider, SelectedModel }
                    ↓
Action Params: _ctx_provider, _ctx_model
```

## 5. Fail-Open Guarantees

| Scenario | Behavior |
|---|---|
| Catalog dir missing | Empty catalog, no error |
| Invalid YAML file | Skipped with warning log |
| Validation failure | Skipped with warning log |
| Nil catalog registry | API returns empty arrays |
| No model match | Falls back to provider-only routing |
| No capabilities required | `model_capability_fit = 1.0` |
| Provider routing nil | Decision graph skips routing step |

## 6. Test Results

### New Tests: `provider_catalog` — 41 PASS

| Category | Count | Tests |
|---|---|---|
| Validation | 9 | Valid entry, missing name, invalid kind, enabled no models, disabled no models, duplicate model, invalid cost class, relative cost range, too many models |
| Registry | 9 | Build, disabled provider, disabled model, get by key, by role, by provider, targets, role inheritance, empty catalog |
| Resolver | 5 | Role filtering, capability filtering, external blocked, nil registry, fallback role |
| Model capability | 4 | Exact match, partial match, no match, no requirements |
| YAML loader | 6 | Valid directory, invalid YAML skipped, missing directory, empty directory, validation failure skipped, deterministic order |
| Domain types | 6 | HasRole, HasCapability, IsLocal, IsExternal, RoutingTarget String, RoutingTarget IsEmpty |
| Scale | 2 | 50-model catalog, single provider/model |

### Existing Tests: `provider_routing` — 35 PASS (all pre-existing, no modifications needed)

### Full Suite: 38 packages OK, 0 failures

## 7. Regression Summary

| Area | Status |
|---|---|
| Provider routing (35 tests) | ✅ All pass |
| Decision graph (22+ tests) | ✅ All pass |
| Path learning | ✅ All pass |
| Path comparison | ✅ All pass |
| Counterfactual | ✅ All pass |
| Calibration (all layers) | ✅ All pass |
| Resource optimization | ✅ All pass |
| Governance | ✅ All pass |
| API handlers | ✅ All pass |
| Full `go build ./...` | ✅ Clean |

## 8. New API Endpoints

| Method | Path | Response |
|---|---|---|
| `GET` | `/api/v1/agent/providers/catalog` | Full catalog with all providers and models |
| `GET` | `/api/v1/agent/providers/targets` | Flat list of `{provider, model, kind, roles, capabilities}` |

Both endpoints require admin authentication (existing middleware).

## 9. Audit Events

| Event | Trigger |
|---|---|
| `provider.target_selected` | Decision graph selects provider+model target |
| `provider.routing_decided` | Router completes routing decision (now includes `selected_model`) |
| `provider.quota_exceeded` | Quota check fails (unchanged) |
| `provider.fallback_used` | Fallback chain activated (unchanged) |

## 10. Risk Analysis

| Risk | Mitigation |
|---|---|
| YAML schema drift | Strict validation at load time; invalid files skipped |
| Model count explosion | `MaxModelsPerProvider = 10` enforced at validation |
| Scoring weight changes break existing behavior | When no capabilities required, 5th component = 1.0 (neutral) |
| External providers leaking in local-only mode | `AllowExternal` filter at resolver level + routing level |
| Thread safety | `CatalogRegistry` uses `sync.RWMutex`; read-heavy workload optimized |

## 11. Definition of Done Checklist

- [x] YAML catalog loaded at startup and validated
- [x] `CatalogRegistry` provides deterministic, thread-safe in-memory lookup
- [x] Routing engine extended with `model_capability_fit` (5th scoring component)
- [x] Tie-breaking includes `relative_cost` and model-level lexical sort
- [x] Fallback chain supports same provider with different models
- [x] `SelectedModel` propagated through `StrategyOverride` → `_ctx_model` param
- [x] API endpoints: `/providers/catalog` and `/providers/targets`
- [x] 41 new tests + 35 existing routing tests + full suite green
- [x] All adapters fail-open and nil-safe
- [x] DB migration 000036 created
- [x] No breaking changes to existing pipeline
- [x] Backward compatible: provider-only routing still works when no catalog loaded
