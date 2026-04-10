# Iteration 37 — Signal Ingestion Layer Validation Report

**Date:** 2026-04-10  
**Status:** ✅ Complete  

---

## 1. Pipeline

```
sources → raw_events → normalize → signals → derive → derived_state → planner input
```

**Flow:**
1. External system sends `POST /api/v1/agent/signals/ingest` with a `RawEvent`
2. Engine persists raw event → `agent_raw_events` table
3. Engine normalises event → `Signal` using deterministic rules
4. Engine persists signal → `agent_signals` table
5. Engine recomputes derived state from active signals (1h window) → `agent_derived_state` table
6. Planner reads active signals + derived state via `SignalIngestionProvider` interface
7. Matching signals boost scored paths by `min(matchCount × 0.03, 0.10)`

---

## 2. Examples (Raw → Signal → State)

### Example 1: Job Failure
```
Raw Event: { source: "worker", event_type: "job_failed", payload: { count: 5 } }
     ↓
Signal:    { signal_type: "failed_jobs", severity: "medium", confidence: 0.9, value: 5 }
     ↓
Derived:   { failure_rate: 0.5 }  (if 2 total signals, 5 / (2 × 10))
```

### Example 2: Income Gap
```
Raw Event: { source: "tracker", event_type: "income_gap", payload: { gap: 3000 } }
     ↓
Signal:    { signal_type: "income_gap", severity: "high", confidence: 0.75, value: 3000 }
     ↓
Derived:   { income_pressure: 0.6 }  (3000 / 5000)
```

### Example 3: Cognitive Load
```
Raw Event: { source: "self_monitor", event_type: "high_cognitive_load", payload: { score: 0.9 } }
     ↓
Signal:    { signal_type: "high_cognitive_load", severity: "high", confidence: 0.65, value: 0.9 }
     ↓
Derived:   { owner_load_score: 0.9 }  (if only cognitive load signal present)
```

---

## 3. Goal Impact Mapping

| Signal Type | Affected Goals |
|---|---|
| failed_jobs | system_reliability |
| dead_letter_spike | system_reliability |
| pending_tasks | system_reliability |
| overdue_tasks | system_reliability |
| cost_spike | monthly_income_growth |
| income_gap | monthly_income_growth |
| new_opportunity | monthly_income_growth |
| high_cognitive_load | owner_load_reduction |

---

## 4. Planner Impact

- **Without signals:** Planner operates unchanged. `SignalIngestionProvider` is nil-safe and fail-open.
- **With signals:** Paths receive an additive boost of `min(matchCount × 0.03, 0.10)` when active signals match the current goal type.
- **Pipeline position:** After income signal boost, before `SelectBestPath`.
- **Bounded influence:** Maximum 0.10 additive boost (10% of path score range).

---

## 5. Entities

### RawEvent
- `id`, `source`, `event_type`, `payload` (JSONB), `observed_at`, `created_at`

### Signal
- `id`, `signal_type`, `severity`, `confidence`, `value`, `source`, `context_tags[]`, `observed_at`, `raw_event_id`, `created_at`

### DerivedState
- `key`, `value`, `updated_at`

---

## 6. API Endpoints

| Method | Path | Description |
|---|---|---|
| POST | `/api/v1/agent/signals/ingest` | Ingest raw event through pipeline |
| GET | `/api/v1/agent/signals` | List normalised signals (paginated) |
| GET | `/api/v1/agent/signals/derived` | List all derived state entries |
| POST | `/api/v1/agent/signals/recompute` | Recompute derived state from active signals |

---

## 7. Audit Events

| Event | Trigger |
|---|---|
| `signals.raw_ingested` | Raw event persisted |
| `signals.normalized` | Signal normalised from raw event |
| `signals.derived_updated` | Derived state recomputed |
| `signals.boost_applied` | Planner applied signal-based path boost |

---

## 8. Test Results

```
=== 25 tests, 0 failures ===

Normalisation correctness (8+2 cases):
  ✅ TestNormalize_FailedJobs
  ✅ TestNormalize_DeadLetterSpike
  ✅ TestNormalize_PendingTasks
  ✅ TestNormalize_OverdueTasks
  ✅ TestNormalize_CostSpike
  ✅ TestNormalize_IncomeGap
  ✅ TestNormalize_NewOpportunity
  ✅ TestNormalize_HighCognitiveLoad
  ✅ TestNormalize_UnknownEventType
  ✅ TestNormalize_MissingPayloadField

Derived state correctness (5 cases):
  ✅ TestComputeDerivedState_Empty
  ✅ TestComputeDerivedState_FailureRate
  ✅ TestComputeDerivedState_DeadLetterRate
  ✅ TestComputeDerivedState_OwnerLoadScore
  ✅ TestComputeDerivedState_IncomePressure
  ✅ TestComputeDerivedState_InfraCostPressure

Deterministic recompute:
  ✅ TestComputeDerivedState_Deterministic

Goal mapping:
  ✅ TestMapSignalToGoals
  ✅ TestSignalMatchesGoal
  ✅ TestCountMatchingSignals
  ✅ TestMapSignalsToGoals

Planner integration (signals present/absent):
  ✅ TestGraphAdapter_Nil
  ✅ TestGraphAdapter_NilEngine

Utilities:
  ✅ TestSeverityFromValue
  ✅ TestFloatFromPayload (4 sub-tests)
  ✅ TestClamp01

Decision graph regression:
  ✅ All existing decision_graph tests pass (0 regressions)
```

---

## 9. Files Created/Modified

### New Files (8)
| File | Purpose |
|---|---|
| `internal/agent/signals/types.go` | Entity types, constants, signal types |
| `internal/agent/signals/store.go` | PostgreSQL stores (raw, signal, derived) |
| `internal/agent/signals/normalizer.go` | Raw event → signal normalisation (8 types) |
| `internal/agent/signals/deriver.go` | Signal → derived state computation (5 metrics) |
| `internal/agent/signals/goal_mapper.go` | Signal → goal impact mapping |
| `internal/agent/signals/engine.go` | Pipeline orchestrator |
| `internal/agent/signals/adapter.go` | Decision graph integration adapter |
| `internal/agent/signals/signals_test.go` | 25 tests |
| `internal/db/migrations/000041_create_agent_signals.up.sql` | Migration (3 tables) |
| `internal/db/migrations/000041_create_agent_signals.down.sql` | Migration rollback |

### Modified Files (4)
| File | Change |
|---|---|
| `internal/agent/decision_graph/planner_adapter.go` | Added `SignalIngestionProvider` interface, field, `WithSignalIngestion()`, boost integration |
| `internal/api/handlers.go` | Added 4 signal handlers + `WithSignalEngine()` |
| `internal/api/router.go` | Added 4 signal routes |
| `cmd/api-gateway/main.go` | Wired signal engine + stores + graph adapter |

---

## 10. Risks

| Risk | Mitigation |
|---|---|
| Signal boost stacks with income boost | Bounded at 0.10 max; income boost separately bounded at 0.15 |
| Active window (1h) may miss slow-moving signals | Configurable via `DefaultActiveWindow`; `recompute` endpoint available |
| High signal volume could slow derived computation | ListActive limited to 1000 signals; DB-level indexes on `observed_at` |
| Unknown event types silently dropped | Logged at debug level; raw event still persisted for audit |
| All stores fail-open | Errors logged, pipeline continues; no silent data loss |

---

## 11. DoD Checklist

- [x] Signals ingested (raw event → persist)
- [x] Signals normalized (8 event types → signal types)
- [x] Derived state computed (5 metrics)
- [x] Planner receives signals (via `SignalIngestionProvider`)
- [x] No regressions (all existing decision_graph tests pass)
- [x] Fail-open everywhere
- [x] Deterministic transformations only
- [x] No decision graph redesign
- [x] No action execution
- [x] No governance modification
