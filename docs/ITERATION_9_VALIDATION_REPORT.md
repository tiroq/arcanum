# Iteration 9 — Self-Stability Layer: Validation Report

## 1. Objective

Implement a self-stability layer that detects and damps harmful autonomous
repetition patterns. The full agent pipeline becomes:

```
goal → plan → act → outcome → memory → reflection → stability control → repeat safely
```

Hard constraints:
- Preserve scheduler safety (single-flight, timeout, panic recovery)
- Manual operator endpoints for override/reset
- Fully reversible controls
- Observable via API
- Prefer throttling over hard shutdown

## 2. Architecture

### New Package: `internal/agent/stability/`

| File                      | Purpose                                         |
|---------------------------|------------------------------------------------|
| `types.go`                | Mode (normal/throttled/safe_mode), State, Finding, DetectionResult |
| `store.go`                | PostgreSQL persistence (single-row `agent_stability_state`) |
| `detector.go`             | 5 deterministic detection rules (A–E)          |
| `policy.go`               | Maps findings → state changes (deterministic)  |
| `engine.go`               | Orchestration: collect → detect → policy → persist → audit |
| `adapter.go`              | `GuardrailAdapter` — implements `actions.StabilityChecker` |
| `scheduler_adapter.go`    | `SchedulerAdapter` — implements `scheduler.StabilityProvider` |

### Detection Rules

| Rule | ID                            | Threshold                                      | Action              |
|------|-------------------------------|------------------------------------------------|---------------------|
| A    | `noop_loop_detected`          | noop ≥60% of ≥5 recent decisions               | → throttled (2×)    |
| B    | `low_value_loop_detected`     | same action ≥3 times, success_rate ≤30%        | → throttled + block  |
| C    | `cycle_instability_detected`  | ≥2 errors in ≥3 recent cycles                  | → safe_mode (3×)    |
| D    | `retry_amplification_detected`| retry_job ≥3 selections, success_rate ≤30%     | → throttled + block  |
| E    | `stability_recovered`         | noop_ratio ≤30%, 0 cycle errors, ≥5 decisions  | → normal (1×)       |

### Modes

| Mode        | Throttle | Behavior                                            |
|-------------|----------|-----------------------------------------------------|
| `normal`    | 1.0×     | Full operation                                      |
| `throttled` | 2.0×     | Slowed interval + specific actions blocked          |
| `safe_mode` | 3.0×     | Only `noop` and `log_recommendation` allowed        |

### Integration Points

1. **Guardrails** (`internal/agent/actions/guardrails.go`):
   - New `StabilityChecker` interface
   - Step 0a in `EvaluateSafety()` — before feedback check
   - In safe_mode: blocks all actions except noop/log_recommendation
   - In throttled: blocks explicitly listed action types

2. **Scheduler** (`internal/agent/scheduler/scheduler.go`):
   - New `StabilityProvider` interface
   - `effectiveInterval()` multiplies base interval by throttle
   - `recordAndEvaluateStability()` after every cycle
   - Ticker reset after each tick for dynamic throttling

3. **API** (3 new endpoints):
   - `GET  /api/v1/agent/stability` — current state
   - `POST /api/v1/agent/stability/reset` — manual operator reset
   - `POST /api/v1/agent/stability/evaluate` — trigger evaluation

4. **Audit Events**: `stability.evaluated`, `stability.mode_changed`, `stability.recovered`

## 3. Database Migration

**000016_create_agent_stability_state**

```sql
CREATE TABLE IF NOT EXISTS agent_stability_state (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    mode TEXT NOT NULL DEFAULT 'normal',
    throttle_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    blocked_action_types JSONB NOT NULL DEFAULT '[]',
    reason TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Single-row design with seed INSERT. Reversible (DROP TABLE in down migration).

## 4. Files Created

| File | Lines | Purpose |
|------|-------|---------|
| `internal/db/migrations/000016_create_agent_stability_state.up.sql` | 11 | Schema migration |
| `internal/db/migrations/000016_create_agent_stability_state.down.sql` | 1 | Drop migration |
| `internal/agent/stability/types.go` | ~65 | Core types |
| `internal/agent/stability/store.go` | ~60 | PostgreSQL persistence |
| `internal/agent/stability/detector.go` | ~245 | 5 detection rules |
| `internal/agent/stability/policy.go` | ~95 | Policy engine |
| `internal/agent/stability/engine.go` | ~150 | Orchestration |
| `internal/agent/stability/adapter.go` | ~45 | Guardrail adapter |
| `internal/agent/stability/scheduler_adapter.go` | ~45 | Scheduler adapter |
| `internal/agent/stability/detector_test.go` | ~155 | Detector tests |
| `internal/agent/stability/policy_test.go` | ~145 | Policy tests |
| `internal/agent/stability/adapter_test.go` | ~115 | Adapter tests |
| `internal/agent/stability/scheduler_adapter_test.go` | ~60 | Scheduler adapter tests |

## 5. Files Modified

| File | Change |
|------|--------|
| `internal/agent/actions/guardrails.go` | Added `StabilityChecker` interface, `stability` field, `WithStability()`, step 0a in `EvaluateSafety()` |
| `internal/agent/scheduler/scheduler.go` | Added `StabilityProvider` interface, `stability` field, `WithStability()`, `effectiveInterval()`, `recordAndEvaluateStability()`, throttle-aware loop |
| `internal/api/handlers.go` | Added `stabilityEngine` field, `WithStabilityEngine()`, 3 handler methods |
| `internal/api/router.go` | Added 3 stability routes |
| `cmd/api-gateway/main.go` | Wired stability store, engine, adapters, handlers |

## 6. Test Results

### Stability Package Tests (20 tests)

```
=== RUN   TestGuardrailAdapter_SafeMode_BlocksAction         PASS
=== RUN   TestGuardrailAdapter_ExplicitBlocklist              PASS
=== RUN   TestGuardrailAdapter_NormalMode_AllowsAll           PASS
=== RUN   TestStabilityChecker_Interface                      PASS
=== RUN   TestDetect_NoopLoop                                 PASS
=== RUN   TestDetect_NoopLoop_BelowThreshold                  PASS
=== RUN   TestDetect_LowValueLoop                             PASS
=== RUN   TestDetect_CycleInstability                         PASS
=== RUN   TestDetect_RetryAmplification                       PASS
=== RUN   TestDetect_Recovery                                 PASS
=== RUN   TestDetect_Recovery_NotTriggeredInNormalMode         PASS
=== RUN   TestApplyPolicy_CycleInstability_EntersSafeMode     PASS
=== RUN   TestApplyPolicy_NoopLoop_EntersThrottled            PASS
=== RUN   TestApplyPolicy_LowValueLoop_BlocksAction           PASS
=== RUN   TestApplyPolicy_RetryAmplification_BlocksRetryJob   PASS
=== RUN   TestApplyPolicy_Recovery_ReturnsToNormal            PASS
=== RUN   TestApplyPolicy_RecoveryIgnoredWithInstability       PASS
=== RUN   TestApplyPolicy_NoFindings_NoChange                 PASS
=== RUN   TestSchedulerAdapter_RecordCycleResult_SlidingWindow PASS
=== RUN   TestSchedulerAdapter_RecordCycleResult_SuccessVsFailure PASS
```

### Full Suite: 0 failures, 0 regressions

All existing test packages continue to pass (`go test ./... -count=1`).

## 7. Validation Checklist

| # | Validation | Status |
|---|-----------|--------|
| 1 | Noop loop detected when noop ≥60% of ≥5 decisions | ✅ `TestDetect_NoopLoop` |
| 2 | Retry amplification detected, retry_job blocked | ✅ `TestDetect_RetryAmplification` + `TestApplyPolicy_RetryAmplification_BlocksRetryJob` |
| 3 | Safe mode blocks all non-noop/log_recommendation actions | ✅ `TestGuardrailAdapter_SafeMode_BlocksAction` + `TestApplyPolicy_CycleInstability_EntersSafeMode` |
| 4 | Recovery returns to normal, clears blocklist | ✅ `TestDetect_Recovery` + `TestApplyPolicy_Recovery_ReturnsToNormal` |
| 5 | Manual reset via API endpoint | ✅ `StabilityReset` handler wired to `POST /api/v1/agent/stability/reset` |
| 6 | No scheduler regression (single-flight, timeout, panic) | ✅ Full scheduler test suite passes (1.305s) |

## 8. Observability

- **Audit**: `stability.evaluated` (every eval), `stability.mode_changed` (transitions), `stability.recovered` (return to normal)
- **API**: `GET /api/v1/agent/stability` returns full state (mode, throttle, blocklist, reason, updated_at)
- **Metrics**: Throttle multiplier visible in state; scheduler effective interval = base × multiplier

## 9. Safety Properties

| Property | Mechanism |
|----------|-----------|
| Fail-open on DB errors | GuardrailAdapter + SchedulerAdapter return defaults |
| No silent mode transitions | All transitions audited via AuditRecorder |
| Reversible controls | POST /reset returns to normal; recovery rule auto-heals |
| Scheduler safety preserved | StabilityProvider is optional (nil-checked); core single-flight/timeout/panic unchanged |
| No import cycles | Interface-based adapters in stability package |

## 10. Design Decisions

1. **Single-row state table**: Simpler than time-series; current state is the only thing that matters for blocking/throttling.
2. **Fail-open defaults**: On any DB error, adapters return "not blocked" / multiplier 1.0 — avoids cascading failure.
3. **Recovery rule requires absence of instability**: `FindingStabilityRecovered` is ignored when `FindingCycleInstability` is also present. Instability always wins.
4. **Sliding-window decay**: Instead of fixed-size ring buffer, halve counters after 10 cycles. Simpler, same effect for Rule C.
5. **Step 0a in guardrails**: Stability check runs before feedback check, since stability represents a higher-level system concern.

## 11. What's Next (Iteration 10)

Potential directions:
- **LLM-driven stability analysis**: Replace deterministic rules with LLM evaluation for nuanced pattern detection
- **Adaptive thresholds**: Auto-tune detection thresholds based on historical patterns
- **Stability dashboard**: Web UI for real-time stability state visualization
- **Multi-dimensional blocking**: Block action+target combinations, not just action types
- **Stability history**: Track mode transitions over time for trend analysis
