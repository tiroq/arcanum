# Provider Routing Cleanup Runbook

> Iteration 33 — April 9, 2026

---

## 1. Purpose

This runbook describes how to:

1. Verify that `providers/_global.yaml` is **actively enforced** at runtime (not silently skipped)
2. Verify that legacy `OLLAMA_*_PROFILE` env vars do **not affect** agent decision-graph provider routing
3. Inspect deprecation warnings and understand their scope
4. Test routing behavior for each role (fast, planner, reviewer, fallback)
5. Diagnose and recover from common misconfiguration

---

## 2. Preconditions

- `providers/_global.yaml` exists in the catalog dir (default: `providers/`)
- `PROVIDERS_CATALOG_DIR` is set in the environment (or defaults to `providers/`)
- The `api-gateway` service is running (or can be started)
- `go test ./...` passes all 38 packages

---

## 3. How to Verify `_global.yaml` Is Active

### Step 1: Check startup log

On service start, the api-gateway will log:

```
INFO  global routing policy wired into provider router
    {"allow_external": true, "max_fallback_chain": 3, "role_preferences_count": 4}
```

If this log is absent, the global policy was not loaded. Check:
- `PROVIDERS_CATALOG_DIR` points to the right directory
- `providers/_global.yaml` exists and is valid YAML
- No `ERROR ... failed to load global routing policy` log

### Step 2: Verify routing decisions reflect preferences

Send a routing request for the `fast` role:

```bash
curl -s http://localhost:8080/api/v1/agent/providers/decisions | jq '.[-1]'
```

Look for `selected_provider` matching the first entry in `routing_policy.priorities` for the `fast` role in `_global.yaml`.

### Step 3: Run the global policy tests

```bash
go test ./internal/agent/provider_catalog/... -v -run "TestLoadGlobalPolicy|TestLoadCatalog_Skips"
```

Expected output: all 7 tests PASS.

```bash
go test ./internal/agent/provider_routing/... -v -run "TestGlobalPolicy"
```

Expected output: all 11 tests PASS.

### Step 4: Verify `_global.yaml` is NOT loaded as a catalog file

The startup log must NOT contain:

```
WARN  failed to validate catalog entry  {"file": "_global.yaml", ...}
```

If it does, the `strings.HasPrefix(e.Name(), "_")` check in `loader.go` may be broken.

---

## 4. How to Verify Legacy Env Does Not Affect Provider Routing

### Step 1: Check startup log for deprecation warnings

If `OLLAMA_FAST_PROFILE` (or any `OLLAMA_*_PROFILE`) is set, the startup log will contain:

```
WARN  [config] DEPRECATED: OLLAMA_FAST_PROFILE is deprecated;
      rename to MODEL_FAST_PROFILE
      (execution-only: affects local Ollama model selection and options, NOT provider routing)
```

This confirms the variable is:
- Recognized and migrated (backcompat preserved)
- Execution-only (no effect on agent routing)

### Step 2: Run the legacy isolation test

```bash
go test ./internal/agent/provider_routing/... -v -run "TestLegacyEnv_OllamaProfileDoesNotAffectProviderRouting"
```

Expected: PASS.

### Step 3: Run the config type-safety test

```bash
go test ./internal/config/... -v -run "TestLegacyOllamaProfile"
```

Expected: both `TestLegacyOllamaProfileDeprecationWarning` and `TestLegacyOllamaProfileIsExecutionOnly` PASS.

### Step 4: Structural confirmation

The `RoutingPolicyConfig` struct in `internal/config/config.go` has no profile fields:

```bash
grep -n "Profile" internal/config/config.go | grep -i "routing"
```

Expected output: none. (Profile fields exist only in `OllamaConfig`, not in `RoutingPolicyConfig`.)

---

## 5. How to Test Routing Per Role

### 5a. Test `fast` role routing

```bash
cat > /tmp/test_fast.go << 'EOF'
// Test: does the fast role route to the preferred provider?
// Check: GET /api/v1/agent/providers/decisions after triggering a fast-role task
EOF
```

Or run the routing test:

```bash
go test ./internal/agent/provider_routing/... -v -run "TestGlobalPolicy_FastRolePreferenceOrdering"
```

### 5b. Test `planner` role routing

```bash
go test ./internal/agent/provider_routing/... -v -run "TestGlobalPolicy_PlannerRolePreferenceOrdering"
```

### 5c. Test `fallback` / `reviewer` role routing

```bash
go test ./internal/agent/provider_routing/... -v -run "TestGlobalPolicy_FallbackRolePreferenceOrdering"
```

### 5d. Test `allow_external: false` enforcement

```bash
go test ./internal/agent/provider_routing/... -v -run "TestGlobalPolicy_AllowExternalFalseBlocksExternalProviders"
```

### 5e. Test full regression (all existing + new tests)

```bash
go test ./internal/agent/provider_routing/... -count=1
```

Expected: all tests PASS, including the 24 pre-existing tests and 11 new tests.

---

## 6. How to Read the Routing Trace

The `GET /api/v1/agent/providers/decisions` endpoint returns recent routing decisions. Each entry includes:

```json
{
  "selected_provider": "ollama",
  "selected_model": "llama3.2:3b",
  "score": 0.88,
  "components": {
    "latency_fit": 0.90,
    "quota_headroom": 1.00,
    "reliability_fit": 1.00,
    "cost_efficiency": 1.00,
    "model_capability_fit": 1.00,
    "preference_boost": 0.10,
    "final_score": 0.88
  },
  "reason": "lat=0.90 quota=1.00 rel=1.00 cost=1.00 cap=1.00 pref=0.10",
  "fallback_chain": ["groq", "ollama"],
  "trace": { ... }
}
```

**Fields to check for global policy enforcement:**

| Field | Confirms |
|-------|---------|
| `preference_boost > 0` | Role preferences from `_global.yaml` are applied |
| `fallback_chain.length ≤ max_fallback_chain` | `max_fallback_chain` from `_global.yaml` respected |
| No external provider in decision when `allow_external: false` | Global gate enforced |
| Fallback chain order matches `degrade_policy` sequence | Tier ordering enforced |

---

## 7. How to Verify `_global.yaml` Schema

The valid schema for `providers/_global.yaml`:

```yaml
routing_policy:
  prefer_free: true                       # bool (parsed, not yet enforced)
  allow_external: false                   # bool (enforced: gates all external providers)
  max_fallback_chain: 3                   # int (enforced: caps fallback chain length)
  priorities:                             # enforced: preference boost by position
    - role: fast
      prefer: [groq, gemini-flash, ollama]
    - role: planner
      prefer: [cerebras, openrouter, ollama]
    - role: reviewer
      prefer: [openrouter, ollama]
    - role: fallback
      prefer: [openrouter, ollama]
  constraints:                            # parsed, not yet enforced
    latency_sensitive_threshold_ms: 500
    heavy_task_tokens_threshold: 4000
  degrade_policy:                         # enforced: fallback chain tier ordering
    - external_strong                     # KindCloud
    - external_fast                       # KindCloud
    - router                              # KindRouter
    - local                              # KindLocal
```

Run the validator:

```bash
go test ./internal/agent/provider_catalog/... -v -run "TestLoadGlobalPolicy_ParsesFullPolicy"
```

---

## 8. Diagnosing Startup Issues

### Problem: "global routing policy wired" log not appearing

1. Check `PROVIDERS_CATALOG_DIR` env var points to the directory containing `_global.yaml`
2. Check `providers/_global.yaml` exists:
   ```bash
   ls -la "${PROVIDERS_CATALOG_DIR:-providers}/_global.yaml"
   ```
3. Check for `ERROR ... failed to load global routing policy` in startup log
4. Verify the file is valid YAML:
   ```bash
   python3 -c "import yaml; yaml.safe_load(open('providers/_global.yaml'))"
   ```

### Problem: "_global.yaml validation failure" WARN still appears

This means the `strings.HasPrefix` guard in `loader.go` is not running. Check that the binary was rebuilt after code changes:

```bash
go build ./cmd/api-gateway/... && ./bin/api-gateway --help
```

### Problem: External providers still selected when `allow_external: false`

1. Check the global policy was loaded (see Step 1 above)
2. Check `policyCfg.AllowExternal` is `false` in the wiring code (`cmd/api-gateway/main.go`)
3. Run:
   ```bash
   go test ./internal/agent/provider_routing/... -v -run "TestGlobalPolicy_AllowExternalFalseBlocksExternalProviders"
   ```
4. If the test passes but runtime behavior doesn't match, the policy may not be wired to the right router instance. Check `main.go` for `providerRouter.WithGlobalPolicy(policyCfg)`.

### Problem: OLLAMA_*_PROFILE still appears to affect routing decisions

This would be a structural error. Verify:

1. The routing decision comes from `internal/agent/provider_routing.Router.Route`
2. The Router is in `api-gateway`, not `worker`
3. The worker is using `internal/providers/routing` (which IS affected by profiles) but this is execution-only

```bash
go test ./internal/agent/provider_routing/... -v -run "TestLegacyEnv_OllamaProfileDoesNotAffectProviderRouting"
```

---

## 9. Pass/Fail Criteria

### P0 — Must Pass

| Criterion | How to verify |
|-----------|---------------|
| All 38 packages build and test PASS | `go test ./... -count=1` |
| `TestLoadGlobalPolicy_*` tests PASS (7 tests) | `go test ./internal/agent/provider_catalog/...` |
| `TestGlobalPolicy_*` routing tests PASS (11 tests) | `go test ./internal/agent/provider_routing/...` |
| `TestLegacyOllamaProfile*` tests PASS (2 tests) | `go test ./internal/config/...` |
| Startup log contains `"global routing policy wired"` | Inspect api-gateway log |
| NO `"failed to validate catalog entry"` for `_global.yaml` | Inspect api-gateway log |

### P1 — Should Pass

| Criterion | How to verify |
|-----------|---------------|
| External providers blocked when `allow_external: false` | `TestGlobalPolicy_AllowExternalFalseBlocksExternalProviders` |
| Preference boost visible in routing decision trace | Check `/api/v1/agent/providers/decisions` |
| Fallback chain length ≤ `max_fallback_chain` | Check `fallback_chain` in decisions endpoint |
| OLLAMA_*_PROFILE emits deprecation warning | Set env var, inspect log on startup |

### P2 — Informational (Not Blocking)

| Criterion | Notes |
|-----------|-------|
| `routing_policy.prefer_free` enforced | Parsed but not yet enforced (§9.2 in report) |
| `routing_policy.constraints` enforced | Parsed but not yet enforced (§9.1 in report) |
| CatalogRegistry wired into routing engine | Requires larger architectural step (§9.3 in report) |

---

## 10. Rollback Instructions

If the global policy causes unexpected routing behavior, apply the workaround:

**Option A: Remove `_global.yaml` file**

The `LoadGlobalPolicy` function fails-open when the file is missing. Routing will fall back to pure score-based routing with no global policy applied.

```bash
mv providers/_global.yaml providers/_global.yaml.bak
# restart api-gateway
```

**Option B: Set `allow_external: true` and clear priorities**

Edit `providers/_global.yaml` to remove `priorities` and `degrade_policy` sections. This preserves the loading machinery but disables preference enforcement.

**Option C: Pass nil policy**

In `cmd/api-gateway/main.go`, comment out the `providerRouter.WithGlobalPolicy(policyCfg)` call. This preserves all other functionality and restores pure score-based routing. This requires a code change and rebuild.

---

## 11. Future Work

See `docs/PROVIDER_ROUTING_PRECEDENCE_REPORT.md` §9 (Remaining Risks) for the full list. Priority items:

1. Enforce `prefer_free: true` in scorer (add `PreferFreeBoost` for `CostClass=free|local`)
2. Enforce `constraints.latency_sensitive_threshold_ms` as a default `LatencyBudgetMs`
3. Connect `CatalogRegistry` to `Router.Route` so model-level metadata influences provider selection
4. Deprecate and remove `MODEL_*_PROFILE` / `OLLAMA_*_PROFILE` in a future major version; replace with catalog-driven model specification
