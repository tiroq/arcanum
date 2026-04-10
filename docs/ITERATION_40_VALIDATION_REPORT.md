# Iteration 40 — Opportunity Discovery Engine: Validation Report

**Date:** 2025-07-24
**Status:** READY_WITH_GUARDS

---

## 1. Summary

Iteration 40 adds a deterministic **Opportunity Discovery Engine** that scans operational signals, action outcomes, and proposal history for income-relevant patterns and converts them into `DiscoveryCandidate` records. Candidates can be promoted to the existing income engine as `IncomeOpportunity` entries. The implementation follows "deterministic rules first" — no LLM calls, no probabilistic logic.

---

## 2. Discovery Pipeline

```
gather inputs (signals, outcomes, proposals)
  → evaluate 5 deterministic rules
    → deduplicate via dedupe_key + time window + active opportunity check
      → persist new candidates (or increment evidence on existing)
        → emit audit events
```

All stages are **fail-open**: provider errors are logged and skipped, nil stores produce no candidates, nil auditors silently skip audit events.

---

## 3. Rule Set

| # | Rule | Source | Candidate Type | Threshold |
|---|------|--------|----------------|-----------|
| 1 | `repeated_manual_work` | Repeated manual action outcomes | `automation_candidate` | ≥3 |
| 2 | `repeated_solved_issue` | Repeated fix successes | `resale_or_repackage_candidate` | ≥3 |
| 3 | `inbound_need` | External or manual signals (`new_opportunity`, `pending_tasks`) | `consulting_lead` | ≥1 |
| 4 | `cost_waste` | Cost spike signals | `cost_saving_candidate` | ≥2 |
| 5 | `reusable_success` | Repeated successful proposals or outcomes | `product_feature_candidate` | ≥3 |

All rules are **pure functions** — no side effects, no I/O during evaluation.

---

## 4. Deduplication Model

- **Dedupe key**: `{source_type}:{action_or_signal_type}` (per candidate)
- **Time window**: 72 hours (`DedupeWindowHours`)
- **Two-layer check**:
  1. Active income opportunity exists for this type+key → skip entirely (`action=skipped`)
  2. Existing candidate with same dedupe_key+type within window → increment evidence (`action=incremented`)
- `CandidateChecker` interface decouples dedup logic from the database store

---

## 5. Income Engine Integration

- **Promotion**: `Promoter.Promote()` maps `CandidateToOpportunityType` and calls `income.Engine.CreateOpportunity()`
- **Mapping**: automation→automation, resale_repackage→service, consulting_lead→consulting, cost_saving→other, product_feature→service
- **Status tracking**: promoted candidates get `status=promoted`
- No money-related actions executed automatically — promotion is a manual API call

---

## 6. Database

- **Migration 000044**: `agent_discovery_candidates` table with 13 columns
- **Indexes**: `idx_discovery_dedupe` (dedupe_key, candidate_type, status, created_at), `idx_discovery_status` (status), `idx_discovery_created_at` (created_at)
- **Down migration**: `DROP TABLE IF EXISTS agent_discovery_candidates`

---

## 7. API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/agent/income/discovery/candidates` | List discovery candidates |
| POST | `/api/v1/agent/income/discovery/run` | Execute a discovery pass |
| GET | `/api/v1/agent/income/discovery/stats` | Get discovery statistics |
| POST | `/api/v1/agent/income/discovery/promote/{id}` | Promote candidate to income opportunity |

---

## 8. Audit Events

| Event | Trigger |
|-------|---------|
| `income.discovery_run` | Discovery run completed |
| `income.discovery_candidate_created` | New candidate persisted |
| `income.discovery_candidate_deduped` | Candidate deduplicated (incremented or skipped) |
| `income.discovery_candidate_promoted` | Candidate promoted to income opportunity |

All events include rule name, candidate type, dedupe key, evidence count, and confidence.

---

## 9. Files Created/Modified

### New files (8)
- `internal/agent/discovery/types.go` — Core types, constants, provider interfaces
- `internal/agent/discovery/store.go` — PostgreSQL CandidateStore
- `internal/agent/discovery/rules.go` — 5 deterministic rules + Rule interface
- `internal/agent/discovery/deduplicator.go` — Dedup via CandidateChecker interface
- `internal/agent/discovery/promoter.go` — Candidate → IncomeOpportunity promotion
- `internal/agent/discovery/engine.go` — Pipeline orchestrator
- `internal/agent/discovery/adapters.go` — SQL adapters for providers (signals, outcomes, proposals, opportunities)
- `internal/agent/discovery/discovery_test.go` — 24 unit tests

### Migrations (2)
- `internal/db/migrations/000044_create_agent_discovery_candidates.up.sql`
- `internal/db/migrations/000044_create_agent_discovery_candidates.down.sql`

### Modified files (3)
- `internal/api/handlers.go` — Added `discoveryEngine` field, `WithDiscoveryEngine()`, 4 handler methods
- `internal/api/router.go` — Added 4 discovery routes
- `cmd/api-gateway/main.go` — Added wiring block (store, adapters, deduplicator, promoter, engine)

---

## 10. Tests

**24 tests, all passing.**

| Category | Count | Tests |
|----------|-------|-------|
| Rule logic | 9 | RepeatedManualWork, BelowThreshold, RepeatedSolvedIssue, InboundNeed, InboundNeed_ManualSource, CostWaste, CostWaste_BelowThreshold, ReusableSuccess, ReusableSuccess_FromOutcomes |
| Deduplication | 3 | SameDedupKeyNotDuplicated, ExpiredWindowAllowsRecreation, ActiveOpportunitySuppresses |
| Promotion | 2 | CandidateToOpportunityTypeMapping, PreservesFields |
| Integration | 4 | Engine_RunProducesCandidates, NoInputsNoCandidates, NilProviderFailOpen, NilStoreRunsOK |
| Helpers | 4 | ConfidenceFromCount, IsManualAction, CandidateTypes_Valid, EstimateValues_Capped |
| Audit | 1 | Engine_EmitsAuditEvents |
| Meta | 1 | DefaultRules_AllPresent |

---

## 11. Regression Summary

All 28 testable agent packages pass — 0 failures, 0 regressions:

```
ok  internal/agent/actionmemory
ok  internal/agent/actions
ok  internal/agent/arbitration
ok  internal/agent/calibration
ok  internal/agent/capacity
ok  internal/agent/causal
ok  internal/agent/counterfactual
ok  internal/agent/decision_graph
ok  internal/agent/discovery          (24 tests)
ok  internal/agent/exploration
ok  internal/agent/financial_pressure
ok  internal/agent/goals
ok  internal/agent/governance
ok  internal/agent/income
ok  internal/agent/meta_reasoning
ok  internal/agent/outcome
ok  internal/agent/path_comparison
ok  internal/agent/path_learning
ok  internal/agent/planning
ok  internal/agent/policy
ok  internal/agent/provider_catalog
ok  internal/agent/provider_routing
ok  internal/agent/reflection
ok  internal/agent/resource_optimization
ok  internal/agent/scheduler
ok  internal/agent/signals
ok  internal/agent/stability
ok  internal/agent/strategy
ok  internal/agent/strategy_learning
```

Full build: `go build ./...` → clean.

---

## 12. Remaining Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| Discovery adapters query tables from other migrations (agent_signals, agent_action_outcomes, agent_income_proposals, agent_income_opportunities) — if those tables don't exist, adapters will fail at runtime | Low | Adapters are fail-open; errors are logged and empty results returned |
| Rule thresholds are compile-time constants — tuning requires code change | Low | Expose via config in a future iteration if needed |
| No scheduled/automatic discovery runs — requires manual POST to `/run` | Medium | Add to scheduler in a future iteration |
| Promotion is manual — no auto-promote for high-confidence candidates | By design | "Do NOT execute money-related actions automatically" |

---

## 13. Architecture Compliance

| Principle | Status |
|-----------|--------|
| Bus-first (events via audit) | ✅ 4 audit event types |
| Explicit state machine | ✅ new → promoted / skipped |
| No silent failure | ✅ All errors logged, fail-open |
| Observability | ✅ Audit events, API stats endpoint |
| Deterministic recovery | ✅ Idempotent dedupe, evidence counting |
| Minimal magic | ✅ Pure rule functions, no LLM, no randomness |

---

## 14. Rollout Recommendation

**READY_WITH_GUARDS**

Guards:
1. Run migration 000044 before deploying
2. Verify existing migrations (000038–000041) are applied (adapters depend on those tables)
3. Monitor `income.discovery_run` audit events for unexpected candidate volumes
4. Consider adding discovery to the scheduler for periodic automated runs
