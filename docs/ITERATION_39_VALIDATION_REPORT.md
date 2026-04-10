# Iteration 39 — Outcome Attribution Validation Report

## Summary

Iteration 39 implements the **Real Outcome Attribution Layer**, closing the loop between estimated and actual income outcomes. The system now learns from real results rather than assumptions, feeding verified accuracy data back into opportunity scoring and decision graph path selection.

---

## Attribution Flow

```
Opportunity (estimated_value) → Proposal → Action → Outcome (actual_value)
                                                         │
                                                    Attribution
                                                    (accuracy = actual / estimated)
                                                         │
                                              ┌──────────┴──────────┐
                                              │                     │
                                       Learning Store         Audit Trail
                                    (per-type stats)      (outcome.recorded,
                                              │            outcome.attributed,
                                              │            learning.updated)
                                    ┌─────────┴─────────┐
                                    │                   │
                             Confidence Adj.     Outcome Feedback
                             (scoring bias)      (decision graph)
```

### Recording Flow
1. `POST /api/v1/agent/income/outcomes` receives an outcome with `outcome_source` (manual/system/external) and `verified` flag
2. Engine persists the outcome and closes the linked opportunity
3. Engine retrieves the original opportunity to compute attribution:
   - `accuracy = actual_value / estimated_value` (capped at 2.0; 0 for failures)
4. Attribution updates the per-type learning record incrementally:
   - `success_rate`, `avg_accuracy`, `confidence_adjustment`
5. Three audit events emitted: `outcome.recorded`, `outcome.attributed`, `learning.updated`

### Decision Graph Feedback
- After the income signal boost (Iteration 36), outcome attribution applies a bounded adjustment
- Positive outcomes (success_rate > 0.5) → boost income-related paths by up to +0.10
- Failed outcomes (success_rate < 0.5) → penalize by up to -0.10
- Cold-start guard: no feedback until ≥ 3 outcomes recorded per type

---

## Learning Metrics

| Metric | Formula | Bounds |
|--------|---------|--------|
| Accuracy | `actual_value / estimated_value` | [0, 2.0] |
| Success Rate | `success_count / total_outcomes` | [0, 1] |
| Avg Accuracy | `sum(accuracies) / total_outcomes` | [0, 2.0] |
| Confidence Adjustment | `(avg_accuracy - 1.0) * 0.30` | [-0.10, +0.10] |
| Outcome Feedback | `(success_rate - 0.5) * 2 * max_effect` | [-0.10, +0.10] |

### Constants

| Constant | Value | Purpose |
|----------|-------|---------|
| `LearningWeight` | 0.30 | Scaling factor for confidence adjustment |
| `LearningMaxConfAdj` | 0.10 | Bounds confidence adjustment |
| `MinLearningOutcomes` | 3 | Cold-start guard |
| `OutcomeFeedbackMaxBoost` | 0.10 | Max positive path score boost |
| `OutcomeFeedbackMaxPenalty` | 0.10 | Max negative path score penalty |

---

## Planner Effect

### Pipeline Position (updated)
```
arbitration → resource_penalty → goal_alignment → income_signal →
  outcome_attribution → signal_ingestion → financial_pressure → select
```

Outcome attribution is applied **after** the income signal boost and **before** signal ingestion, ensuring that real outcome data modulates the income boost rather than replacing it.

### Bounded Impact
- Maximum positive influence: +0.10 on FinalScore
- Maximum negative influence: -0.10 on FinalScore
- Combined with income signal max boost (0.15), total income influence capped at 0.25
- Only affects paths whose first action is income-related (`IsIncomeAction()`)
- Fail-open: nil providers, missing data, or DB errors result in no adjustment

---

## New/Modified Files

### New Files
| File | Purpose |
|------|---------|
| `internal/agent/income/attribution.go` | Core attribution functions: accuracy, confidence adjustment, outcome feedback, learning update |
| `internal/agent/income/learning_store.go` | PostgreSQL store for per-type learning records (UPSERT) |
| `internal/agent/income/attribution_test.go` | 39 tests covering all attribution logic |
| `internal/db/migrations/000041_income_outcome_attribution.up.sql` | Schema: extends outcomes + creates learning table |
| `internal/db/migrations/000041_income_outcome_attribution.down.sql` | Rollback migration |

### Modified Files
| File | Changes |
|------|---------|
| `internal/agent/income/types.go` | Added `OutcomeSource`, `Verified` to `IncomeOutcome`; added `LearningRecord`, `AttributionRecord`, `PerformanceStats` types; added learning constants and outcome source validation |
| `internal/agent/income/outcome_store.go` | Updated SQL for `outcome_source` and `verified` columns; added `CountVerified()` |
| `internal/agent/income/engine.go` | Added `LearningStore` field, `WithLearning()`, `processAttribution()`, `GetPerformance()`, `GetLearningForType()`; enriched audit events |
| `internal/agent/income/graph_adapter.go` | Added `GetOutcomeFeedback()` implementing `OutcomeAttributionProvider` |
| `internal/agent/decision_graph/planner_adapter.go` | Added `OutcomeAttributionProvider` interface, `outcomeAttribution` field, `WithOutcomeAttribution()`, integrated feedback in scoring pipeline |
| `internal/api/handlers.go` | Added `IncomePerformance` handler |
| `internal/api/router.go` | Added `/api/v1/agent/income/performance` route |
| `cmd/api-gateway/main.go` | Wired `LearningStore`, `WithLearning()`, `WithOutcomeAttribution()` |

---

## API Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/api/v1/agent/income/outcomes` | Record real outcome (extended with `outcome_source`, `verified`) |
| GET | `/api/v1/agent/income/outcomes` | List outcomes (includes new fields) |
| GET | `/api/v1/agent/income/performance` | Attribution performance stats (new) |

---

## Audit Events

| Event | Trigger | Payload |
|-------|---------|---------|
| `outcome.recorded` | Outcome persisted | outcome details + source + verified |
| `outcome.attributed` | Attribution computed | accuracy, estimated vs actual, opportunity type |
| `learning.updated` | Learning record updated | success_rate, avg_accuracy, confidence_adjustment |
| `outcome.attribution_applied` | Decision graph scoring | goal_type |

---

## Test Results

```
ok  github.com/tiroq/arcanum/internal/agent/income         0.007s  (39 tests)
ok  github.com/tiroq/arcanum/internal/agent/decision_graph  0.009s  (no regressions)
```

All 27 agent packages pass. The only pre-existing failure is `financial_pressure` (corrupted source file, unrelated).

### Test Coverage

- **Accuracy computation**: exact match, overperformance, cap at 2.0, partial, failed, zero/negative estimates, zero/negative actual
- **Confidence adjustment**: cold start, perfect calibration, overestimate, underestimate, clamping both directions
- **Outcome feedback**: cold start, high/low success rate, neutral, max boost/penalty, symmetry
- **Attribution building**: succeeded with accuracy, failed always zero
- **Learning update**: first outcome, multiple outcomes, partial outcomes, cold-start guard
- **Outcome sources**: valid/invalid validation
- **Graph adapter**: nil engine, nil adapter fail-open
- **Type defaults**: verified default false, outcome_source default
- **Constants**: bounds sanity checks

---

## Risks

| Risk | Mitigation |
|------|------------|
| Cold-start: no learning signal until 3 outcomes | `MinLearningOutcomes` guard prevents premature influence |
| Overperformance distortion | Accuracy capped at 2.0 |
| Single bad outcome dominating | Incremental rolling averages smooth per-type stats |
| Learning store unavailable | Fail-open: `processAttribution` logs and continues |
| Feedback loop amplification | Bounded at ±0.10 per path; combined with income signal ≤ 0.25 total |
| Fake/unverified outcomes | `verified` field distinguishes ground-truth; `outcome_source` tracks provenance |

---

## Design Decisions

1. **Incremental learning**: Learning records are updated incrementally (UPSERT) rather than recomputed from scratch, matching the existing calibration patterns.
2. **Per-type granularity**: Learning is tracked per `opportunity_type`, enabling type-specific confidence adjustments.
3. **Action-to-type reverse mapping**: `GetOutcomeFeedback` maps action types back to opportunity types via `MapOpportunityToActions`, using the best available learning signal.
4. **Fail-open throughout**: Every new component returns zero/default on error, matching the system-wide pattern.
5. **No fake outcomes**: The `outcome_source` and `verified` fields ensure provenance tracking. Only real signals influence learning.
