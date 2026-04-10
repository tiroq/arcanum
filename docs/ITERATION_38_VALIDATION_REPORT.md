# Iteration 38 — Financial Pressure Layer Validation Report

**Date:** 2026-04-10
**Status:** Complete
**Tests:** 29 new tests — all PASS
**Regressions:** None (income, decision_graph, full build clean)

---

## 1. Formula

```
income_gap    = target_income_month - current_income_month
norm_gap      = clamp(income_gap / target_income_month, 0, 1)    [0 if target ≤ 0]
buffer_ratio  = cash_buffer / monthly_expenses                   [∞ if expenses = 0]
norm_buffer   = clamp(1 - buffer_ratio, 0, 1)                   [0 if expenses = 0]
pressure      = clamp(norm_gap × 0.50 + norm_buffer × 0.50, 0, 1)
```

Urgency mapping:
| Pressure Score | Urgency Level |
|----------------|---------------|
| < 0.30         | low           |
| < 0.60         | medium        |
| < 0.80         | high          |
| ≥ 0.80         | critical      |

---

## 2. Worked Examples

### Example A — High pressure (zero income, depleted buffer)
| Field | Value |
|-------|-------|
| current_income_month | $0 |
| target_income_month | $10,000 |
| monthly_expenses | $5,000 |
| cash_buffer | $0 |

```
norm_gap = 10000/10000 = 1.0
buffer_ratio = 0/5000 = 0.0
norm_buffer = 1 - 0 = 1.0
pressure = 1.0×0.50 + 1.0×0.50 = 1.0 → critical
```

### Example B — Low pressure (target met, fully buffered)
| Field | Value |
|-------|-------|
| current_income_month | $6,000 |
| target_income_month | $5,000 |
| monthly_expenses | $3,000 |
| cash_buffer | $12,000 |

```
income_gap = -1000 → norm_gap = 0
buffer_ratio = 12000/3000 = 4.0
norm_buffer = clamp(1 - 4.0, 0, 1) = 0
pressure = 0 → low
```

### Example C — Moderate pressure
| Field | Value |
|-------|-------|
| current_income_month | $3,000 |
| target_income_month | $6,000 |
| monthly_expenses | $4,000 |
| cash_buffer | $2,000 |

```
norm_gap = 3000/6000 = 0.50
buffer_ratio = 2000/4000 = 0.50
norm_buffer = 1 - 0.50 = 0.50
pressure = 0.50×0.50 + 0.50×0.50 = 0.50 → medium
```

---

## 3. Before/After Income Scoring

The `ApplyPressureToIncomeScore` function adjusts:

```
final_score = base_score × (1 + pressure × 0.50)
```

| Base Score | Pressure | Final Score | Change |
|------------|----------|-------------|--------|
| 0.50       | 0.00     | 0.50        | +0%    |
| 0.50       | 0.50     | 0.625       | +25%   |
| 0.50       | 1.00     | 0.75        | +50%   |
| 0.80       | 1.00     | 1.00 (clamped) | +25% |
| 0.00       | 1.00     | 0.00        | +0%    |

**Key property:** zero-scored opportunities remain zero regardless of pressure.

---

## 4. Planner Impact — Decision Graph Path Boost

Financial pressure applies a bounded additive boost to income-related paths:

```
path_boost = pressure × PressurePathBoostMax (0.20)
```

Pipeline position (after income signal, before SelectBestPath):
```
arbitration → resource_penalty → goal_alignment → income_signal → signal_ingestion → financial_pressure → select
```

| Pressure | Path Boost | Max Total Score |
|----------|------------|-----------------|
| 0.00     | 0.00       | unchanged       |
| 0.50     | 0.10       | clamped to 1.0  |
| 1.00     | 0.20       | clamped to 1.0  |

Safety constraints:
- Only income-related actions receive the boost (propose_income_action, analyze_opportunity, schedule_work)
- Boost is additive and clamped to [0,1] — cannot exceed total score 1.0
- Goal alignment with `forbid_actions` overrides pressure (rejected paths stay at 0.0)
- Fail-open: nil provider or zero pressure → no change to paths

---

## 5. Architecture

### New Files (4 source + 1 test + 2 migration)
| File | Purpose |
|------|---------|
| `internal/agent/financial_pressure/types.go` | FinancialState, FinancialPressure, constants |
| `internal/agent/financial_pressure/store.go` | Single-row PostgreSQL UPSERT store |
| `internal/agent/financial_pressure/scorer.go` | Deterministic pressure computation + income scoring |
| `internal/agent/financial_pressure/adapter.go` | GraphAdapter (implements FinancialPressureProvider) |
| `internal/agent/financial_pressure/financial_pressure_test.go` | 29 tests |
| `internal/db/migrations/000041_create_agent_financial_state.up.sql` | Table creation |
| `internal/db/migrations/000041_create_agent_financial_state.down.sql` | Rollback |

### Modified Files
| File | Change |
|------|--------|
| `internal/agent/decision_graph/planner_adapter.go` | +FinancialPressureProvider interface, field, setter, pipeline hook |
| `internal/api/handlers.go` | +FinancialState/FinancialPressure handlers, field, import, setter |
| `internal/api/router.go` | +2 routes (/financial/state, /financial/pressure) |
| `cmd/api-gateway/main.go` | +import, store+adapter creation, wiring |

### API Endpoints
| Method | Path | Description |
|--------|------|-------------|
| GET    | `/api/v1/agent/financial/state` | Read state + computed pressure |
| POST   | `/api/v1/agent/financial/state` | Update financial state |
| GET    | `/api/v1/agent/financial/pressure` | Read current pressure only |

### Audit Events
| Event | When |
|-------|------|
| `financial.state_updated` | POST /financial/state |
| `financial.pressure_computed` | After state update |
| `financial.pressure_applied` | During decision graph scoring |

---

## 6. Tests Summary

| Category | Count | Status |
|----------|-------|--------|
| Pressure calculation | 8 | PASS |
| Urgency level mapping | 4 | PASS |
| Income scoring integration | 5 | PASS |
| Adapter nil-safety | 2 | PASS |
| IsIncomeRelated | 2 | PASS |
| Clamp utility | 3 | PASS |
| Boundary conditions | 3 | PASS |
| Path boost safety | 2 | PASS |
| Constants validation | 1 | PASS |
| **Total** | **29** | **ALL PASS** |

Existing test suites:
- `internal/agent/decision_graph` — all PASS (no regressions)
- `internal/agent/income` — all PASS (no regressions)
- Full `go build ./...` — clean

---

## 7. Risks

| Risk | Mitigation |
|------|------------|
| Pressure dominates decisions | PressurePathBoostMax = 0.20 caps influence; goal alignment `forbid_actions` override still enforced |
| Stale financial state | State has `updated_at` timestamp; API allows manual refresh; zero state = zero pressure (fail-open) |
| Division by zero | `target ≤ 0` and `expenses = 0` both produce zero for their respective components |
| Race conditions on single-row UPSERT | PostgreSQL UPSERT with ON CONFLICT is atomic; single-row semantics enforced by CHECK constraint |
| Import cycles | FinancialPressureProvider interface defined in decision_graph; income action types duplicated (not imported) |

---

## 8. Design Principles Verified

- **Bus-first:** Financial pressure is a local computation; no cross-service calls
- **Explicit state:** Pressure is deterministic from FinancialState inputs
- **No silent failure:** Fail-open returns zero pressure; audit events trace all applications
- **Observability:** 3 audit events; API endpoints for state + pressure inspection
- **Deterministic recovery:** Single-row UPSERT; no stuck states possible
- **Minimal magic:** Pure formula, no hidden side effects
- **Pressure guides, not dominates:** Max income boost 50%, max path boost 20%, always clamped to [0,1]
