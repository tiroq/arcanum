# Iteration 50 Validation Report — Global Objective Function + Risk Model

**Date**: 2025-07-16
**Status**: READY_WITH_GUARDS

---

## 1. Summary

Iteration 50 implements a **Global Objective Function** that unifies all agent subsystems (income, capacity, portfolio, pricing, financial pressure/truth, external actions) into a single deterministic utility score with an explicit risk model. The objective function acts as a bounded planner signal in the decision graph scoring pipeline, positioned after portfolio and before `SelectBestPath`.

**Key deliverables:**
- Unified 5-component utility model (income, family, owner, execution, strategic)
- Bounded 5-component risk model (financial, overload, execution, concentration, pricing)
- Deterministic net utility: `net_utility = clamp01(utility - risk * 0.60)`
- Decision graph planner integration with bounded boost/penalty `[-0.06, +0.08]`
- 4 API endpoints, 3 database tables, 6 audit events
- 36 tests (28 top-level + 8 sub-tests), all passing
- Zero regressions across 50 test packages

---

## 2. Utility Model

### Components and Weights

| Component        | Weight | Source Provider         | Formula                                    |
|------------------|--------|------------------------|--------------------------------------------|
| Income Utility   | 0.30   | FinancialTruthProvider | `clamp01(verified_income / max(target, 1))` |
| Family Utility   | 0.25   | CapacityProvider       | `clamp01(family_time / max_daily_work) * (1 - instability * 0.30)` |
| Owner Utility    | 0.20   | CapacityProvider       | `clamp01(1 - overload_factor * 0.80)`      |
| Execution Utility| 0.15   | ExternalActionsProvider| `clamp01(success_rate * 0.50 + readiness * 0.50)` |
| Strategic Utility| 0.10   | PortfolioProvider      | `clamp01(diversification * 0.50 + dominant_stability * 0.50)` |

**Weights sum to 1.0** (verified by test `TestUtilityWeightsSumTo1`).

### Aggregate Formula

```
utility_score = Σ(component_i * weight_i)   clamped [0, 1]
```

---

## 3. Risk Model

### Components and Weights

| Risk Component      | Weight | Source Provider          | Formula                                      |
|----------------------|--------|--------------------------|----------------------------------------------|
| Financial Risk       | 0.30   | FinancialPressureProvider| `clamp01(pressure * 0.70 + income_gap_ratio * 0.30)` |
| Overload Risk        | 0.25   | CapacityProvider         | `clamp01(load * 0.60 + blocked/max_daily * 0.40)` |
| Execution Risk       | 0.20   | ExternalActionsProvider  | `clamp01((1 - success_rate) * 0.60 + failure_rate * 0.40)` |
| Concentration Risk   | 0.15   | PortfolioProvider        | `clamp01((1 - diversification) * 0.70 + dominance_fraction * 0.30)` |
| Pricing Risk         | 0.10   | PricingProvider          | `clamp01((1 - confidence) * 0.50 + discount_rate * 0.50)` |

**Weights sum to 1.0** (verified by test `TestRiskWeightsSumTo1`).

### Aggregate Formula

```
risk_score = Σ(risk_i * weight_i)   clamped [0, 1]
```

---

## 4. Net Objective Function

```
net_utility = clamp01(utility_score - risk_score * RiskPenaltyWeight)
```

Where `RiskPenaltyWeight = 0.60`.

### Properties
- **Deterministic**: identical inputs always produce identical outputs
- **Bounded**: always in `[0, 1]`
- **Neutral point**: `NeutralNetUtility = 0.50` — above is positive signal, below is penalty
- **Division-safe**: all denominators guarded against zero

### Dominant Factor Analysis
- Identifies the largest positive contributor (utility component with highest weighted value)
- Identifies the largest risk contributor (risk component with highest weighted value)
- Stored in `ObjectiveSummary` for observability

---

## 5. Planner Integration

### Pipeline Position

```
arbitration → resource_penalty → goal_alignment → income_signal → 
outcome_attribution → signal_ingestion → financial_pressure → 
capacity → portfolio → objective_function → SelectBestPath
```

### Signal Computation

```go
if net_utility > NeutralNetUtility (0.50):
    boost = (net_utility - 0.50) * 2 * ObjectiveBoostMax(0.08)   → [0, 0.08]
else if net_utility < NeutralNetUtility:
    penalty = (0.50 - net_utility) * 2 * ObjectivePenaltyMax(0.06)  → [0, 0.06]
```

- **Boost** applied to ALL paths equally (global signal, not action-specific)
- **Penalty** applied to ALL paths equally
- Emits `objective.signal_applied` audit event with signal type, strength, and context tags
- Fail-open: if ObjectiveFunctionProvider is nil, pipeline continues unchanged

### Interface

```go
type ObjectiveFunctionProvider interface {
    GetObjectiveSignal(ctx context.Context) ObjectiveSignalExport
}

type ObjectiveSignalExport struct {
    SignalType  string   // "boost", "penalty", or "neutral"
    Strength    float64  // magnitude [0, 0.08] or [0, 0.06]
    Explanation string
    ContextTags []string // dominant factors
}
```

---

## 6. API Endpoints

| Method | Path                                  | Description                        |
|--------|---------------------------------------|------------------------------------|
| GET    | `/api/v1/agent/objective/state`       | Current utility state              |
| GET    | `/api/v1/agent/objective/risk`        | Current risk state                 |
| GET    | `/api/v1/agent/objective/summary`     | Full objective summary (net utility, dominant factors) |
| POST   | `/api/v1/agent/objective/recompute`   | Trigger recomputation from all providers |

All endpoints follow existing auth middleware chain: `rec(log(auth(handler)))`.

---

## 7. Database Schema

**Migration**: `000054_create_agent_objective.up.sql`

### Tables

| Table                      | Pattern         | Key Columns                                                 |
|----------------------------|-----------------|-------------------------------------------------------------|
| `agent_objective_state`    | Single-row UPSERT | utility_score, income/family/owner/execution/strategic components |
| `agent_risk_state`         | Single-row UPSERT | risk_score, financial/overload/execution/concentration/pricing risks |
| `agent_objective_summary`  | Single-row UPSERT | net_utility, dominant_positive/risk_factor, risk_penalty_weight |

All tables use `CHECK (id = 'current')` constraint for single-row enforcement.

---

## 8. Audit Events

| Event                        | Emitted When                        | Payload                              |
|------------------------------|-------------------------------------|--------------------------------------|
| `objective.state_updated`    | Utility state recomputed            | All 5 utility components + score     |
| `objective.risk_updated`     | Risk state recomputed               | All 5 risk components + score        |
| `objective.summary_updated`  | Net utility recomputed              | net_utility, dominant factors, penalty weight |
| `objective.recomputed`       | Full recompute cycle complete       | net_utility, signal type, explanation |
| `objective.signal_computed`  | Signal derived for planner          | signal_type, strength, context_tags  |
| `objective.signal_applied`   | Signal applied in scoring pipeline  | signal_type, strength, paths affected |

---

## 9. Tests

### New Tests: 36 (28 top-level + 8 sub-tests)

| Category             | Tests | Description                                         |
|----------------------|-------|-----------------------------------------------------|
| Utility Model        | 5     | Each component increases with its primary input     |
| Risk Model           | 4     | Each risk increases with its primary input          |
| Bound Checks         | 6     | Utility and risk always in [0,1] (incl. sub-tests)  |
| Net Objective        | 4     | Higher risk lowers net; low risk improves; deterministic; fail-open |
| Signal               | 4     | Bounded boost, bounded penalty, zero at neutral, no-op when nil |
| Dominant Factors     | 2     | Correct positive and risk factor identification     |
| Full Scenarios       | 4     | High-income/high-stability, high-pressure/high-risk, overloaded owner, concentration |
| Weight Consistency   | 2     | Utility weights sum to 1.0, risk weights sum to 1.0 |
| Edge Cases           | 3     | Clamp01, division-by-zero safety, net utility bounds |
| Engine               | 2     | Fail-open GatherInputs, fail-open no providers      |

### Regression Results

```
50 test packages — ALL PASS — ZERO FAILURES
```

Key subsystems verified:
- `decision_graph` — PASS (planner_adapter.go modified)
- `portfolio` — PASS (provider interface consumed)
- `pricing` — PASS (provider interface consumed)
- `scheduling` — PASS (no changes)
- `financial_pressure` — PASS (provider interface consumed)
- `capacity` — PASS (provider interface consumed)
- `income` — PASS (provider interface consumed)
- `api` — PASS (handlers.go and router.go modified)

---

## 10. File Inventory

| File                                                          | Action   | Lines |
|---------------------------------------------------------------|----------|-------|
| `internal/agent/objective/types.go`                           | NEW      | 140   |
| `internal/agent/objective/store.go`                           | NEW      | 181   |
| `internal/agent/objective/scorer.go`                          | NEW      | 297   |
| `internal/agent/objective/engine.go`                          | NEW      | 270   |
| `internal/agent/objective/adapter.go`                         | NEW      | 68    |
| `internal/agent/objective/objective_test.go`                  | NEW      | 440   |
| `internal/db/migrations/000054_create_agent_objective.up.sql` | NEW      | 33    |
| `internal/db/migrations/000054_create_agent_objective.down.sql`| NEW     | 5     |
| `internal/agent/decision_graph/planner_adapter.go`            | MODIFIED | +40   |
| `internal/api/handlers.go`                                    | MODIFIED | +55   |
| `internal/api/router.go`                                      | MODIFIED | +4    |
| `cmd/api-gateway/main.go`                                     | MODIFIED | +120  |
| **Total new code**                                            |          | ~1434 |

---

## 11. Architecture Compliance

| Principle                | Status | Notes                                               |
|--------------------------|--------|-----------------------------------------------------|
| Bus-first architecture   | ✅     | No direct service calls; scoring via pipeline only  |
| Explicit state machines  | ✅     | No state transitions (read-only aggregation model)  |
| No silent failure        | ✅     | All errors logged; fail-open with zero values       |
| Observability first      | ✅     | 6 audit events; dominant factor tracking            |
| Deterministic recovery   | ✅     | Recompute endpoint rebuilds from live provider data |
| Minimal magic            | ✅     | Pure functions; explicit weights; no hidden logic   |
| Fail-open everywhere     | ✅     | Nil-safe adapter; zero-value defaults on errors     |

---

## 12. Remaining Risks & Guards

1. **No concrete CalendarConnector**: inherited from Iteration 48 — scheduling integration is interface-only
2. **Cold-start zeros**: on first boot, all providers return zero → net_utility=0.0 → mild penalty signal; this is correct fail-open behavior but operators should trigger initial data population
3. **No periodic recompute**: objective is recomputed on-demand via POST endpoint; a periodic scheduler tick could be added in a future iteration
4. **Schema migration sequence**: migration 000054 requires prior migrations (000050–000053) to be applied first

---

## 13. Rollout Recommendation

### READY_WITH_GUARDS

**Guards required:**
- Run migration 000054 after all prior migrations
- Populate initial financial state and capacity state before relying on objective signal
- Monitor `objective.signal_applied` audit events to verify pipeline integration
- Consider adding periodic recompute to scheduler in a subsequent iteration

**No blockers identified.** All tests pass, all contracts satisfied, all patterns followed.
