# Iteration 41 — Time Allocation / Owner Capacity Validation Report

## 1. Summary

Implemented a bounded capacity-aware prioritization layer that evaluates opportunities,
proposals, and tasks against the owner's limited time and energy. The system now stops
assuming infinite owner time and explicitly computes what is worth doing given constraints.

### New Files
- `internal/agent/capacity/types.go` — Constants, entities (CapacityState, CapacityItem, CapacityDecision, CapacitySummary, FamilyConfig, BlockedTime)
- `internal/agent/capacity/store.go` — PostgreSQL persistence (UPSERT single-row state + decisions)
- `internal/agent/capacity/scorer.go` — Pure scoring functions (ComputeAvailableCapacity, ComputeValuePerHour, ComputeCapacityFitScore, EvaluateItem, ComputeCapacityPenalty, ComputeCapacityBoost)
- `internal/agent/capacity/engine.go` — Orchestrator (RecomputeState, EvaluateItems, GetRecommendations, audit events)
- `internal/agent/capacity/adapter.go` — GraphAdapter implementing CapacityProvider (GetCapacityPenalty, GetCapacityBoost) — nil-safe, fail-open
- `internal/agent/capacity/family_loader.go` — YAML family_context.yaml loader with fail-open defaults
- `internal/agent/capacity/signal_adapter.go` — Bridge from signals.Engine to DerivedStateProvider
- `internal/agent/capacity/capacity_test.go` — 33 tests
- `internal/db/migrations/000044_create_agent_capacity.up.sql` — Migration (agent_capacity_state + agent_capacity_decisions)
- `internal/db/migrations/000044_create_agent_capacity.down.sql` — Rollback migration

### Modified Files
- `internal/agent/decision_graph/planner_adapter.go` — Added CapacityProvider interface, `capacity` field, `WithCapacity()`, capacity penalty hook in scoring pipeline
- `internal/api/handlers.go` — Added `capacityAdapter` field, `WithCapacity()`, 4 handler methods
- `internal/api/router.go` — Added 4 capacity routes
- `cmd/api-gateway/main.go` — Wired capacity engine, family config loader, signal adapter, graph adapter

---

## 2. Capacity Model

### Available Capacity Computation

```
base_capacity = max_daily_work_hours                     (from family_context.yaml, default 8)
blocked_hours = sum of blocked time ranges               (parsed from "HH:MM-HH:MM" format)
overload_penalty = ((load - 0.60) / (1 - 0.60)) × 0.50 × base   (if load > 0.60)

available = base_capacity - blocked_hours - overload_penalty
clamped to [0, max_daily_work_hours]
```

Example with family_context.yaml:
- base = 8h, blocked = 3h (18:00-21:00), load = 0.80
- penalty = ((0.80-0.60)/0.40) × 0.50 × 8 = 2.0h
- available = 8 - 3 - 2 = **3.0h**

---

## 3. Prioritization Model

### Value Per Hour

```
value_per_hour = expected_value / max(estimated_effort, 0.5)
```

### Capacity Fit Score (4 components, weighted)

```
value_component      = clamp(value_per_hour / 50.0, 0, 1) × 0.35
urgency_component    = clamp(urgency, 0, 1) × 0.25
effort_fit_component = clamp(1 - (effort / available_hours), 0, 1) × 0.25
load_component       = clamp(1 - owner_load_score, 0, 1) × 0.15

capacity_fit_score   = sum of all components, clamped [0, 1]
```

### Recommendation

- `capacity_fit_score >= 0.40` → **recommended**
- `capacity_fit_score < 0.40` → **deferred** with reason:
  - `exceeds_available_capacity` (effort > available hours)
  - `owner_overloaded` (load > 0.60)
  - `low_value_per_hour` (VPH < 10)
  - `low_capacity_fit` (default)

---

## 4. Family Protection

| Condition | Effect |
|---|---|
| Blocked time ranges (family_context.yaml) | Reduces available_hours_today |
| `minimum_family_time_hours` | Enforced via blocked ranges |
| High owner_load_score (> 0.60) | Overload penalty reduces capacity |
| Long tasks vs low capacity | Low effort_fit drives deferral |
| Overload state | Penalised via both capacity reduction and load_component |

When family time is blocked or load is high:
- recommended_count decreases (threshold constant)
- long/heavy tasks are penalised via effort_fit_component
- small high-value actions are preferred (both via fit score and explicit boost)

---

## 5. Planner / Income Integration

### Decision Graph Pipeline Position

```
arbitration → resource_penalty → goal_alignment → income_signal →
outcome_attribution → signal_ingestion → financial_pressure →
→ capacity_penalty ← (NEW, Iteration 41)
→ select_best_path
```

### Capacity Penalty (applied to all paths)
```
available_fraction = available_hours / max_daily_hours
base_penalty = (1 - available_fraction) × 0.15

if owner_load > 0.60:
  overload_extra = (load - 0.60) × 0.15 × 0.50

penalty = clamp(base + extra, 0, 0.15)    // CapacityPenaltyMax = 0.15
```

### Capacity Boost (for small high-value actions)
```
Eligible if: effort ≤ 2h AND value_per_hour ≥ 50
boost = fit_score × 0.10                  // CapacityBoostMax = 0.10
```

All fail-open: `penalty = 0` if capacity data is missing.

---

## 6. API

| Method | Endpoint | Description |
|---|---|---|
| GET | `/api/v1/agent/capacity/state` | Current capacity state |
| POST | `/api/v1/agent/capacity/recompute` | Recompute from signals + family config |
| GET | `/api/v1/agent/capacity/recommendations` | Recent capacity decisions |
| GET | `/api/v1/agent/capacity/summary` | Aggregate recommendation summary |

---

## 7. Audit Events

| Event | When |
|---|---|
| `capacity.state_computed` | After recomputing capacity state |
| `capacity.item_evaluated` | Per item evaluation |
| `capacity.recommendation_generated` | After batch evaluation |
| `capacity.penalty_applied` | When penalty applied in decision graph |

---

## 8. Tests — 33 passing

### Capacity State Tests (1-3)
1. ✅ blocked family time reduces capacity → 8h - 3h = 5h
2. ✅ high owner load reduces capacity → 8h → 6h with load=0.80
3. ✅ no constraints → base capacity preserved (8h)

### Scoring Tests (4-7)
4. ✅ higher value-per-hour scores higher
5. ✅ oversized task penalized (effort > available)
6. ✅ small high-value task preferred
7. ✅ deterministic repeated runs (3 identical outputs)

### Family Protection Tests (8-10)
8. ✅ blocked family time suppresses long tasks (6h task deferred with 2h available)
9. ✅ minimum family time honored (3h blocked = 5h remaining)
10. ✅ overload state penalizes heavy items

### Integration Tests (11-13)
11. ✅ income proposal ranking changes with low capacity
12. ✅ planner penalty/boost applied correctly and bounded
13. ✅ no capacity data → fail-open behavior (nil adapter returns 0)

### Additional Tests (14+)
- ✅ Value-per-hour edge cases (zero, negative, floor)
- ✅ Family config loader (real file, missing file, empty, invalid YAML)
- ✅ Blocked hours computation (single, multiple, invalid)
- ✅ Capacity fit score bounded [0,1]
- ✅ Defer reason generation
- ✅ Adapter nil safety
- ✅ Signal derived adapter
- ✅ Capacity penalty clamping

---

## 9. Regression Summary

| Package | Status |
|---|---|
| Full `go build ./...` | ✅ Clean |
| `internal/agent/capacity/...` | ✅ 33/33 pass |
| `internal/agent/income/...` | ✅ Pass |
| `internal/agent/signals/...` | ✅ Pass |
| `internal/agent/financial_pressure/...` | ✅ Pass |
| `internal/agent/decision_graph/...` | ✅ Pass |
| All 28 agent packages (excl. discovery) | ✅ Pass |
| `internal/agent/discovery/...` | ⚠️ Pre-existing build failure (Iteration 40 — missing zap import in test) |

---

## 10. Validation Examples

### Example 1: High-Value Short Task
- Item: 1h consulting lead, $500 expected value, urgency=0.7
- State: 6h available, load=0.4
- VPH = $500/1h = $500 → value_component = 0.35 (capped at 1.0×0.35)
- urgency = 0.7×0.25 = 0.175
- effort_fit = (1-1/6)×0.25 = 0.208
- load = 0.6×0.15 = 0.09
- **fit_score = 0.823 → RECOMMENDED** ✅

### Example 2: Low-Value Long Task
- Item: 8h grind, $80 expected value, urgency=0.2
- State: 4h available, load=0.5
- VPH = $80/8h = $10 → value_component = 0.07
- urgency = 0.05
- effort_fit = (1-8/4)=clamped=0 → 0
- load = 0.5×0.15 = 0.075
- **fit_score = 0.195 → DEFERRED** (exceeds_available_capacity) ✅

### Example 3: Overload Suppression
- Item: 5h task, $100 value, urgency=0.4
- State: 4h available (reduced by overload), load=0.9
- VPH = $100/5h = $20 → value_component = 0.14
- urgency = 0.10
- effort_fit = (1-5/4)=clamped=0 → 0
- load = 0.1×0.15 = 0.015
- **fit_score = 0.255 → DEFERRED** (owner_overloaded) ✅

---

## 11. Remaining Risks

| Risk | Mitigation |
|---|---|
| Effort estimates inaccurate | MinimumEffortFloor=0.5h prevents division-by-zero; real outcomes feed back via outcome attribution |
| No real calendar integration | Static blocked_time from family_context.yaml; future iteration can add calendar sync |
| Owner load heuristic quality | Based on signals.deriver composite; future iteration can refine |
| Single-day horizon | Weekly estimate = daily×5; future iteration can add multi-day planning |
| No dynamic recomputation trigger | Manual POST /recompute; future iteration can wire auto-recompute on signal ingest |

---

## 12. Rollout Recommendation

### **READY_WITH_GUARDS**

Rationale:
- All scoring is deterministic and bounded
- All components fail-open (nil-safe adapters)
- CapacityPenaltyMax = 0.15 caps decision graph influence at 15%
- CapacityBoostMax = 0.10 caps boost at 10%
- No auto-scheduling of irreversible tasks
- No side effects — read-only penalty/boost in pipeline
- Family time constraints are respected
- Pre-existing discovery test failure is unrelated

Guards:
- Monitor capacity.state_computed audit events for reasonable values
- Verify penalty/boost don't override stronger signals (financial pressure, goal alignment)
- Validate family_context.yaml blocked ranges produce expected available hours
