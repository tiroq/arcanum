# Iteration 5 — Action Memory + Feedback Loop: Validation Report

## 1. Objective

Build a **persistent learning layer** on top of the Outcome Verification Layer (Iteration 4). After each outcome evaluation, the system accumulates aggregate statistics by action type and derives deterministic feedback signals. The guardrails layer then uses these signals to reject actions with consistently poor outcomes — creating a closed feedback loop where the agent learns from its own results.

---

## 2. Files Created

| File | Purpose |
|------|---------|
| `internal/agent/actionmemory/types.go` | `ActionMemoryRecord`, `TargetMemoryRecord` data models with explicit JSON tags |
| `internal/agent/actionmemory/memory.go` | `Store` — PostgreSQL persistence with atomic UPSERT, `OutcomeInput` struct (breaks import cycle) |
| `internal/agent/actionmemory/feedback.go` | `GenerateFeedback()` — pure deterministic function, `Recommendation` type, thresholds |
| `internal/agent/actionmemory/adapter.go` | `FeedbackAdapter` — implements `actions.FeedbackProvider` interface |
| `internal/agent/actionmemory/feedback_test.go` | 15 unit tests for feedback classification, rate calculation, aggregation |
| `internal/agent/actionmemory/adapter_test.go` | 4 unit tests for adapter/guardrail rejection logic |
| `internal/db/migrations/000013_create_agent_action_memory.up.sql` | Creates `agent_action_memory` + `agent_action_memory_targets` tables |
| `internal/db/migrations/000013_create_agent_action_memory.down.sql` | Drops both tables (reversible) |

## 3. Files Modified

| File | Change |
|------|--------|
| `internal/agent/outcome/handler.go` | Added `WithMemoryStore()`, memory update after outcome persistence, `auditFeedback()` for `action.feedback_generated` event |
| `internal/agent/actions/guardrails.go` | Added `FeedbackProvider` interface, `WithFeedback()`, step 0 in `EvaluateSafety` that checks `ShouldAvoid()` before dedup/load/retry checks |
| `internal/api/handlers.go` | Added `actionMemoryStore` field, `WithActionMemoryStore()`, `AgentActionMemory()` handler |
| `internal/api/router.go` | Added `GET /api/v1/agent/action-memory` route |
| `cmd/api-gateway/main.go` | Wired `actionmemory.NewStore`, `actionmemory.NewFeedbackAdapter`, `guardrails.WithFeedback`, `outcomeHandler.WithMemoryStore`, `handlers.WithActionMemoryStore` |

---

## 4. Architecture

```
Action Executed
      │
      ▼
OutcomeHandler.HandleOutcome()
      │
      ├─ evaluator.Evaluate()       → Determine outcome (success/neutral/failure)
      ├─ store.Save()               → Persist outcome (Iteration 4)
      ├─ memoryStore.Update()       → UPSERT counters + recompute rates
      ├─ GenerateFeedback()         → Derive recommendation
      └─ auditFeedback()            → Record action.feedback_generated event
      
Next Cycle:
      │
      ▼
Guardrails.EvaluateSafety()
      │
      ├─ Step 0: feedback.ShouldAvoid()  → Reject if failure_rate ≥ 0.5 (n≥5)
      ├─ Step 1: dedupe check
      ├─ Step 2: system load check
      └─ Step 3: retry count check
```

### Import Cycle Resolution

```
actions ──defines──► FeedbackProvider interface
   ▲                        │
   │                        │
guardrails uses ◄─── actionmemory.FeedbackAdapter implements

outcome.handler ──uses──► actionmemory.OutcomeInput (primitive struct)
                           (NOT outcome.ActionOutcome — breaks cycle)
```

---

## 5. Data Model

### `agent_action_memory` — Aggregate by (action_type, target_type)

| Column | Type | Constraint |
|--------|------|------------|
| id | UUID | PRIMARY KEY |
| action_type | TEXT | NOT NULL |
| target_type | TEXT | NOT NULL |
| total_runs | INT | NOT NULL DEFAULT 0 |
| success_runs | INT | NOT NULL DEFAULT 0 |
| failure_runs | INT | NOT NULL DEFAULT 0 |
| neutral_runs | INT | NOT NULL DEFAULT 0 |
| success_rate | DOUBLE PRECISION | NOT NULL DEFAULT 0 |
| failure_rate | DOUBLE PRECISION | NOT NULL DEFAULT 0 |
| last_updated | TIMESTAMPTZ | NOT NULL DEFAULT NOW() |
| | | **UNIQUE (action_type, target_type)** |

### `agent_action_memory_targets` — Per-target detail

Same columns plus `target_id UUID NOT NULL`, with `UNIQUE (action_type, target_type, target_id)`.

UPSERT computes rates atomically: `success_rate = (success_runs + inc) / (total_runs + 1)`.

---

## 6. Feedback Classification

Pure deterministic function — `GenerateFeedback(record *ActionMemoryRecord) ActionFeedback`:

| Condition | Recommendation |
|-----------|----------------|
| record == nil | `insufficient_data` |
| total_runs < 5 | `insufficient_data` |
| failure_rate ≥ 0.5 | `avoid_action` |
| success_rate ≥ 0.7 | `prefer_action` |
| otherwise | `neutral` |

**Failure check takes precedence**: even if success_rate ≥ 0.7, a failure_rate ≥ 0.5 still classifies as `avoid_action`.

---

## 7. Test Results

### actionmemory package — 19 tests, all PASS

```
=== RUN   TestAdapterShouldAvoid_NoHistory               --- PASS
=== RUN   TestAdapterShouldAvoid_InsufficientData         --- PASS
=== RUN   TestAdapterShouldAvoid_HighFailure              --- PASS
=== RUN   TestAdapterShouldAvoid_HealthyAction            --- PASS
=== RUN   TestGenerateFeedback_InsufficientData_Nil       --- PASS
=== RUN   TestGenerateFeedback_InsufficientData_LowSample --- PASS
=== RUN   TestGenerateFeedback_AvoidAction                --- PASS
=== RUN   TestGenerateFeedback_AvoidAction_HighFailure    --- PASS
=== RUN   TestGenerateFeedback_PreferAction               --- PASS
=== RUN   TestGenerateFeedback_PreferAction_Threshold     --- PASS
=== RUN   TestGenerateFeedback_Neutral                    --- PASS
=== RUN   TestOutcomeIncrements_Success                   --- PASS
=== RUN   TestOutcomeIncrements_Failure                   --- PASS
=== RUN   TestOutcomeIncrements_Neutral                   --- PASS
=== RUN   TestOutcomeIncrements_Unknown                   --- PASS
=== RUN   TestRateCalculation_AllSuccess                  --- PASS
=== RUN   TestRateCalculation_AllFailure                  --- PASS
=== RUN   TestRateCalculation_Mixed                       --- PASS
=== RUN   TestFeedback_FailurePrecedence                  --- PASS
```

### Full test suite — all packages PASS, zero regressions

```
ok  github.com/tiroq/arcanum/internal/agent/actionmemory
ok  github.com/tiroq/arcanum/internal/agent/actions
ok  github.com/tiroq/arcanum/internal/agent/goals
ok  github.com/tiroq/arcanum/internal/agent/outcome
ok  github.com/tiroq/arcanum/internal/api
ok  github.com/tiroq/arcanum/internal/config
ok  github.com/tiroq/arcanum/internal/contracts
ok  github.com/tiroq/arcanum/internal/control
ok  github.com/tiroq/arcanum/internal/db/models
ok  github.com/tiroq/arcanum/internal/jobs
ok  github.com/tiroq/arcanum/internal/processors
ok  github.com/tiroq/arcanum/internal/prompts
ok  github.com/tiroq/arcanum/internal/providers
ok  github.com/tiroq/arcanum/internal/providers/execution
ok  github.com/tiroq/arcanum/internal/providers/profile
ok  github.com/tiroq/arcanum/internal/providers/routing
ok  github.com/tiroq/arcanum/internal/source
ok  github.com/tiroq/arcanum/internal/worker
```

---

## 8. DB Migration Validation

| Check | Result |
|-------|--------|
| Migration version | **13** (clean, dirty=false) |
| `agent_action_memory` table | ✅ Exists |
| `agent_action_memory_targets` table | ✅ Exists |
| Down migration | ✅ Drops both tables |

---

## 9. Observability

| Event | When |
|-------|------|
| `action.outcome_evaluated` | After each outcome is persisted (Iteration 4) |
| `action.feedback_generated` | After each memory update, with success_rate, failure_rate, sample_size, recommendation |
| Guardrail rejection log | When `ShouldAvoid()` blocks an action — logged with reason string |
| `action_memory_update_failed` | If memory UPSERT fails (best-effort, non-blocking) |

### API Endpoint

`GET /api/v1/agent/action-memory` — Returns all memory records with computed feedback recommendations:

```json
{
  "memory": [
    {
      "action_type": "retry_job",
      "target_type": "job",
      "total_runs": 10,
      "success_runs": 8,
      "failure_runs": 1,
      "neutral_runs": 1,
      "success_rate": 0.8,
      "failure_rate": 0.1,
      "recommendation": "prefer_action"
    }
  ]
}
```

---

## 10. Design Decisions

1. **OutcomeInput struct in actionmemory** — Breaks the `actionmemory ↔ outcome` import cycle by defining a local struct with primitive fields instead of referencing `outcome.ActionOutcome`.

2. **FeedbackProvider interface in actions** — Same pattern as Iteration 4's `OutcomeHandler` — consumer defines the interface, provider implements it.

3. **Best-effort memory updates** — Memory write failures are logged but don't fail the outcome handling pipeline. This keeps the critical path (evaluate → persist outcome → audit) reliable.

4. **Feedback before all other guardrails** — The feedback check runs as step 0, before dedup/load/retry checks. If historical data says "avoid," there's no point checking further.

5. **Log recommendations exempt** — `log_recommendation` actions bypass the feedback check (same as system load check), since they have no side effects.

6. **Atomic UPSERT** — Success/failure rate recomputation occurs inside the PostgreSQL `ON CONFLICT DO UPDATE` clause, eliminating race conditions between concurrent outcomes.

7. **Two-tier storage** — Aggregate records (action_type + target_type) enable global feedback. Per-target records (+ target_id) preserve granular history for future use without increasing current complexity.
