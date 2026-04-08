# Iteration 31 — Provider Policy + Quota-Aware Routing Layer: Validation Report

## 1. Overview

### What Was Added
- **Provider Registry**: formal provider definition model (kind, roles, capabilities, limits, cost, health)
- **Quota Tracking**: per-provider RPM/TPM/RPD/TPD usage state with deterministic minute/day reset
- **Deterministic Scoring**: explicit 4-component scoring formula bounded to [0,1]
- **Routing Engine**: candidate filtering → scoring → tie-breaking → fallback chain builder
- **Bounded Fallback Chain**: max 3 providers, no duplicates, respects filtering rules
- **Degradation Policy**: explicit ladder from strong external → router → local → empty
- **API Endpoints**: 3 new endpoints for providers/status, providers/usage, providers/decisions
- **Audit Events**: 4 events (routing_decided, quota_exceeded, fallback_used, degraded_to_local)
- **DB Migration**: `000035_create_agent_provider_usage` for persisted quota state

### What Changed vs Previous Architecture
- **No breaking changes** — provider routing is additive and fail-open
- Previous system had ad-hoc provider selection via profile DSL and escalation config
- New system adds formal policy layer with quota awareness and deterministic selection
- Existing provider/LLM infrastructure (OllamaProvider, OpenAIProvider, AuditedProvider) is untouched
- Decision graph pipeline order is unchanged; provider routing is a pre-execution concern

## 2. Architecture

### Routing Flow

```
Task Request
    │
    ▼
┌─────────────────────┐
│   RoutingInput       │  goal_type, task_type, preferred_role,
│                      │  estimated_tokens, latency_budget_ms,
│                      │  confidence_required, allow_external
└─────────┬───────────┘
          │
          ▼
┌─────────────────────┐
│   Provider Registry  │  All registered providers
│   (Registry.Enabled) │  Filter: enabled only
└─────────┬───────────┘
          │
          ▼
┌─────────────────────┐
│   Candidate Filter   │  Check: reachable, role, external allowed, quota
│                      │  Reject with reason → RejectedProvider[]
└─────────┬───────────┘
          │
          ▼
┌─────────────────────┐
│   Deterministic      │  score = 0.30×latency_fit + 0.30×quota_headroom
│   Scoring            │         + 0.20×reliability_fit + 0.20×cost_efficiency
└─────────┬───────────┘
          │
          ▼
┌─────────────────────┐
│   Sort + Tie-Break   │  Score DESC → local before external → name ASC
└─────────┬───────────┘
          │
          ▼
┌─────────────────────┐
│   Build Fallback     │  Top-K remaining (max 3, no duplicates)
│   Chain              │
└─────────┬───────────┘
          │
          ▼
┌─────────────────────┐
│   RoutingDecision    │  selected_provider, fallback_chain, reason, trace
└─────────────────────┘
```

## 3. Provider Registry

### Providers Supported

| Provider     | Kind   | Roles                                    | Capabilities                           |
|-------------|--------|------------------------------------------|----------------------------------------|
| ollama      | local  | fast, planner, reviewer, batch, fallback | json_mode, low_latency                 |
| openrouter  | router | planner, reviewer, fallback              | json_mode, long_context, tool_calling  |
| ollama-cloud| cloud  | planner, reviewer, batch                 | json_mode                              |

### Registry Operations
- `Register(Provider)` — add/replace provider
- `Get(name)` — lookup by name
- `All()` — all providers, deterministic name-sorted order
- `Enabled()` — enabled providers only
- `ByRole(role)` — filter by role
- `ByCapability(cap)` — filter by capability

## 4. Quota Model

### RPM / TPM / RPD / TPD Handling

Each provider has explicit limits:
- **RPM** (requests per minute)
- **TPM** (tokens per minute)
- **RPD** (requests per day)
- **TPD** (tokens per day)

A **zero value** means the limit is **unknown** — treated conservatively as "not unlimited". Unknown limits never cause rejection.

### Projected Usage Check

Before routing to a provider:
```
projected = current_usage + request_estimate
if projected > known_limit → reject provider
```

### Reset Logic

- **Minute reset**: when `now.Truncate(minute) != last.Truncate(minute)` → zero minute counters
- **Day reset**: when `now.UTC().Truncate(24h) != last.UTC().Truncate(24h)` → zero daily counters
- No cron or background worker required — resets are applied deterministically on read/write

### Persistence

- `agent_provider_usage` PostgreSQL table with UPSERT on `provider_name`
- In-memory state with optional DB persistence via `QuotaTracker`
- Loaded from DB on startup via `LoadFromDB()`

## 5. Routing Logic

### Filtering

A provider is rejected if any of these conditions is true:
1. Not enabled
2. Not reachable
3. External and `AllowExternal=false`
4. Does not have required role (and no fallback role)
5. Projected quota exceeds any known limit

### Scoring

```
score = latency_fit × 0.30 + quota_headroom × 0.30 + reliability_fit × 0.20 + cost_efficiency × 0.20
```

| Component       | Logic                                                                         |
|----------------|-------------------------------------------------------------------------------|
| latency_fit    | Local=1.0 (tight budget), Cloud=0.3–0.8, Router=0.2–0.7 based on budget     |
| quota_headroom | min(remaining/limit) across all known limits; unknown limits → 1.0            |
| reliability_fit| Enabled+Reachable+!Degraded=1.0; Degraded=0.4; Unreachable=0.1; Disabled=0.0 |
| cost_efficiency| local=1.0; free=0.95; cheap=0.7; unknown=0.5                                 |

All components bounded to [0,1].

### Tie-Breaking

When scores differ by < 0.001 (TieBreakEpsilon):
1. Local providers preferred over external
2. Lexical name sort (ascending)

### Fallback

- Max chain length: 3 (configurable via `MaxFallbackChainLength`)
- No duplicate providers in chain
- Same filtering rules apply to fallback candidates
- If `AllowExternal=false`, fallback chain is local-only

## 6. Example Decisions

### Low-Latency Task (LatencyBudgetMs=200)
```
Input:  role=fast, tokens=100, latency=200ms, external=true
Result: selected=ollama (local, latency_fit=1.0)
        fallback=[cerebras, groq]
Reason: local provider excels at tight latency budgets
```

### Token-Heavy Task (EstimatedTokens=50000)
```
Input:  role=planner, tokens=50000, latency=5000ms, external=true
Result: selected=cerebras (TPM=60000 headroom: high)
        fallback=[groq, openrouter, ollama]
Reason: cerebras has highest TPM capacity
```

### External-Disabled Task
```
Input:  role=planner, tokens=500, external=false
Result: selected=ollama (only local provider)
        fallback=[]
Reason: all external providers rejected (AllowExternal=false)
```

### Quota-Exhausted Scenario
```
Input:  role=planner, tokens=100, external=true
State:  cerebras RPM=30/30, groq RPM=30/30, openrouter RPM=20/20
Result: selected=ollama (local fallback)
        fallback=[]
Reason: all external providers rejected due to RPM exhaustion
```

## 7. Fallback / Degradation

### Degradation Ladder

```
Strong free external provider (cerebras, groq)
    → Weaker free external provider
        → Router/aggregator fallback (openrouter)
            → Local medium (ollama with planner model)
                → Local small (ollama with fast model)
                    → No provider / safe degradation (empty selection)
```

### Behavior
- Each step is explicit in the routing trace
- `provider.degraded_to_local` audit event emitted when local is selected due to external failures
- Empty selection returns `SelectedProvider=""` with explicit reason

## 8. API

### `GET /api/v1/agent/providers/status`
Returns all registered providers with metadata:
```json
{
  "providers": [
    {
      "name": "ollama",
      "kind": "local",
      "roles": ["fast", "planner", "reviewer", "batch", "fallback"],
      "capabilities": ["json_mode", "low_latency"],
      "limits": {"rpm": 0, "tpm": 0, "rpd": 0, "tpd": 0},
      "cost": {"cost_class": "local", "relative_cost": 0},
      "health": {"enabled": true, "reachable": true, "degraded": false}
    }
  ],
  "count": 3
}
```

### `GET /api/v1/agent/providers/usage`
Returns current quota usage with remaining budget:
```json
{
  "usage": [
    {
      "provider_name": "cerebras",
      "requests_this_minute": 5,
      "tokens_this_minute": 12000,
      "requests_today": 150,
      "tokens_today": 450000,
      "remaining_rpm": 25,
      "remaining_tpm": 48000,
      "remaining_rpd": 850,
      "remaining_tpd": 550000
    }
  ]
}
```

### `GET /api/v1/agent/providers/decisions`
Returns recent routing decisions:
```json
{
  "decisions": [
    {
      "id": "uuid",
      "goal_type": "task_execution",
      "task_type": "code_review",
      "selected_provider": "cerebras",
      "fallback_chain": ["groq", "openrouter", "ollama"],
      "reason": "selected cerebras: latency=0.60 quota=0.98 reliability=1.00 cost=0.95 → score=0.870",
      "created_at": "2026-04-08T12:00:00Z"
    }
  ]
}
```

## 9. Audit Events

| Event                      | Emitted When                                | Payload                                                          |
|---------------------------|---------------------------------------------|------------------------------------------------------------------|
| `provider.routing_decided` | Every routing decision                      | goal_type, task_type, selected_provider, fallback_chain, reason, rejected_providers |
| `provider.quota_exceeded`  | Provider rejected due to quota              | provider, goal_type, estimated_tokens, reason                    |
| `provider.fallback_used`   | Fallback provider selected after rejections | Same as routing_decided                                          |
| `provider.degraded_to_local` | Local selected because external unavailable | Same as routing_decided                                        |

## 10. Tests

| # | Test Name                              | Status |
|---|----------------------------------------|--------|
| 1 | TestDeterministicRouting               | PASS   |
| 2 | TestQuotaExceededRPM                   | PASS   |
| 3 | TestQuotaExceededTPM                   | PASS   |
| 4 | TestDailyQuotaExceeded                 | PASS   |
| 5 | TestFallbackWorks                      | PASS   |
| 6 | TestNoDuplicateFallback                | PASS   |
| 7 | TestLocalOnlyRouting                   | PASS   |
| 8 | TestTokenHeavyRequest                  | PASS   |
| 9 | TestLatencySensitiveRouting            | PASS   |
| 10| TestTieBreakingDeterministic           | PASS   |
| 11| TestFailOpenLocalFallback              | PASS   |
| 12| TestNoProviderAvailable                | PASS   |
| 13| TestUsageResetMinute                   | PASS   |
| 14| TestUsageResetDay                      | PASS   |
| 15| TestTraceCorrectness                   | PASS   |
| 16| TestRegistryByRole                     | PASS   |
| 17| TestRegistryByCapability               | PASS   |
| 18| TestProviderWithUnknownLimits          | PASS   |
| 19| TestProviderDisabled                   | PASS   |
| 20| TestProviderDegraded                   | PASS   |
| 21| TestZeroEstimatedTokens                | PASS   |
| 22| TestNegativeEstimatedTokens            | PASS   |
| 23| TestEmptyRegistry                      | PASS   |
| 24| TestOnlyLocalProvider                  | PASS   |
| 25| TestAllQuotasExhausted                 | PASS   |
| 26| TestRoleIncompatible                   | PASS   |
| 27| TestScoringComponents                  | PASS   |
| 28| TestComputeHeadroomWithUnknownLimits   | PASS   |
| 29| TestComputeHeadroomPartialUsage        | PASS   |
| 30| TestMaxFallbackChainLength             | PASS   |
| 31| TestGraphAdapterNilSafety              | PASS   |
| 32| TestRecentDecisionsBounded             | PASS   |
| 33| TestFallbackChainUsedWhenPrimaryRejected| PASS  |
| 34| TestQuotaExceededTPD                   | PASS   |
| 35| TestLocalPreferredOnTie                | PASS   |

**35 tests, all passing.**

## 11. Regression Summary

- **Build status**: `go build ./...` — PASS (zero errors)
- **Full test suite**: `go test ./...` — ALL PASS
- **No regressions** in any existing package:
  - decision_graph, calibration, arbitration, resource_optimization, governance — all cached/pass
  - api, contracts — recompiled and pass with new handler integrations

## 12. Risks

| Risk | Description | Mitigation |
|------|-------------|------------|
| Static health | Provider health is set at registration, no live probing | Acceptable for this iteration; live health probing can be added later |
| In-memory quotas | Quota state can be lost on restart without DB | DB persistence is implemented; loaded on startup via `LoadFromDB()` |
| Provider registration | Providers are registered in main.go code, not config-driven | Can be refactored to config-driven in future iteration |
| No provider learning | System doesn't learn which providers perform best over time | Out of scope; can be added as bounded provider learning in a future iteration |
| Quota accuracy | Minute/day windows are approximations, not precise sliding windows | Deterministic and predictable; good enough for free-tier protection |

## 13. Next Step Recommendation

**Recommended next iteration: Provider Performance Tracking + Adaptive Routing**

The natural evolution after formal policy is bounded provider learning:
1. Track per-provider latency, error rate, and success rate over time
2. Feed provider performance history into scoring (replace static reliability_fit)
3. Detect provider degradation automatically from observed metrics
4. Adaptive health: mark providers unreachable when error rate exceeds threshold
5. Provider-aware decision graph integration: annotate graph nodes with provider choice

This would close the loop from "policy-based routing" to "experience-based routing" while keeping the system deterministic and bounded.
