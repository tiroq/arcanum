# Iteration 6 — Adaptive Planning: Validation Report

## 1. Architecture

```
GoalEngine.Evaluate()
      │
      ▼ goals[]
AdaptivePlanner.PlanActions()
      │
      ├─ ContextCollector.Collect()      → PlanningContext (queue state + rates + feedback)
      │
      ├─ For each goal:
      │    ├─ CandidatesForGoal()        → explicit candidate set
      │    ├─ ScoreCandidate() per type  → scored, reasoned candidates
      │    ├─ Sort + select              → highest safe candidate
      │    ├─ buildExplanation()         → concise rationale
      │    ├─ auditPlanningEvaluated()   → planning.evaluated event
      │    └─ auditActionSelected()      → planning.action_selected event
      │
      ├─ resolveActions()               → delegate to TargetResolver for concrete targets
      │
      ▼ actions[]
Engine (unchanged)
      │
      ├─ Guardrails.EvaluateSafety()    → unchanged, still applies feedback + dedup + load checks
      ├─ Executor.ExecuteAction()       → unchanged
      └─ OutcomeHandler.HandleOutcome() → unchanged (Iter 4+5)
```

### Key integration change

The `Engine` now accepts an `ActionPlanner` interface instead of a concrete `*Planner`:

```go
type ActionPlanner interface {
    PlanActions(ctx context.Context, goals []goals.Goal) ([]Action, error)
}
```

Both the static `Planner` and the new `AdaptivePlanner` implement this interface.
The `AdaptivePlanner` uses a `TargetResolver` interface (also implemented by the static `Planner`) to find specific job/task targets after action type selection.

---

## 2. Planning Model

### PlanningContext (collected once per cycle)

| Field | Source |
|-------|--------|
| QueueBacklog | `COUNT(*) WHERE status='queued'` |
| RetryScheduledCount | `COUNT(*) WHERE status='retry_scheduled'` |
| LeasedCount | `COUNT(*) WHERE status='leased'` |
| FailureRate | `(failed+dead_letter) / total` from last 24h |
| AcceptanceRate | `approved / (approved+rejected)` from last 24h |
| RecentActionFeedback | `actionmemory.GenerateFeedback()` for all memory records |

### Candidate mappings

| Goal Type | Candidates |
|-----------|------------|
| reduce_retry_rate | retry_job, log_recommendation, noop |
| investigate_failed_jobs | retry_job, log_recommendation, noop |
| resolve_queue_backlog | trigger_resync, log_recommendation, noop |
| increase_reliability | log_recommendation, noop |
| increase_model_quality | log_recommendation, noop |
| reduce_latency | log_recommendation, noop |

### PlanningDecision output

Each goal produces a `PlanningDecision` with:
- All candidates with scores, confidence, reasoning trail
- Selected action type
- Human-readable explanation
- Timestamp

---

## 3. Scoring Rules

All constants defined explicitly in `scorer.go`:

| Constant | Value | Purpose |
|----------|-------|---------|
| `baseScoreDefault` | 0.50 | Starting score for every candidate |
| `feedbackAvoidPenalty` | 0.40 | Penalty when history says avoid |
| `feedbackPreferBoost` | 0.25 | Boost when history says prefer |
| `highBacklogResyncPenalty` | 0.30 | trigger_resync penalty when backlog > 50 |
| `highBacklogRetryPenalty` | 0.10 | retry_job minor penalty when backlog > 50 |
| `highRetryBoost` | 0.15 | retry_job boost when retries > 10 and feedback healthy |
| `highFailureRateBadHistoryPenalty` | 0.20 | Extra penalty for bad-history actions when system failure rate > 0.20 |
| `highFailureRateRecommendBoost` | 0.15 | log_recommendation boost when failure rate > 0.20 |
| `lowAcceptanceAdvisoryBoost` | 0.10 | Advisory actions boosted when acceptance < 0.40 |
| `lowAcceptanceDestructivePenalty` | 0.10 | Destructive actions penalized when acceptance < 0.40 |
| `safetyPreferenceBoost` | 0.05 | Small edge for lower-risk log_recommendation |
| `noopBasePenalty` | 0.20 | Ensures noop only wins when everything else is worse |

### Scoring flow

1. Base score = `0.50 + (goal_priority × 0.30)`
2. noop: apply penalty and return immediately
3. Historical feedback: avoid → -0.40, prefer → +0.25
4. System context: backlog, retry count, failure rate, acceptance rate adjustments
5. Safety preference: +0.05 for log_recommendation

### Selection

1. Sort candidates by score descending (stable sort)
2. Reject any candidate with score ≤ 0
3. Pick highest non-rejected candidate
4. If none found → noop

---

## 4. Integration Point

### Files created

| File | Purpose |
|------|---------|
| `internal/agent/planning/types.go` | PlanningContext, PlannedActionCandidate, PlanningDecision, goalCandidateMap |
| `internal/agent/planning/scorer.go` | ScoreCandidate(), all scoring constants, applyFeedbackRules(), applyContextRules() |
| `internal/agent/planning/context.go` | ContextCollector — gathers queue state, rates, action feedback |
| `internal/agent/planning/planner.go` | AdaptivePlanner — main planning logic, audit events, target resolution |
| `internal/agent/planning/planner_test.go` | 21 unit tests |

### Files modified

| File | Change |
|------|--------|
| `internal/agent/actions/engine.go` | Added `ActionPlanner` interface; changed `planner` field from `*Planner` to `ActionPlanner` |
| `internal/agent/actions/types.go` | Added `TargetResolver` interface |
| `internal/agent/actions/planner.go` | Added `FindRetryTargets()`, `FindResyncTargets()` exported methods |
| `internal/api/handlers.go` | Added `adaptivePlanner` field, `WithAdaptivePlanner()`, `AgentPlanningDecisions()` handler |
| `internal/api/router.go` | Added `GET /api/v1/agent/planning-decisions` route |
| `cmd/api-gateway/main.go` | Wired `ContextCollector`, `AdaptivePlanner`, `WithAdaptivePlanner` |

---

## 5. Example Planning Decisions

### Scenario A — Historical preference

Given: `retry_job` has `success_rate=0.80, recommendation=prefer_action`

```
Goal: reduce_retry_rate (priority=0.60)
Candidates:
  retry_job:          score=0.93 (base 0.68 + prefer 0.25)      ← SELECTED
  log_recommendation: score=0.73 (base 0.68 + safety 0.05)
  noop:               score=0.48 (base 0.68 - noop 0.20)

Selected: retry_job
Explanation: "selected retry_job (score=0.93); feedback prefer_action: +0.25 ..."
```

### Scenario B — Historical avoidance

Given: `retry_job` has `failure_rate=0.60, recommendation=avoid_action`

```
Goal: reduce_retry_rate (priority=0.60)
Candidates:
  log_recommendation: score=0.73 (base 0.68 + safety 0.05)      ← SELECTED
  noop:               score=0.48 (base 0.68 - noop 0.20)
  retry_job:          score=0.28 (base 0.68 - avoid 0.40)

Selected: log_recommendation
Explanation: "selected log_recommendation (score=0.73); retry_job: feedback avoid_action ..."
```

### Scenario C — High queue backlog

Given: `queue_backlog=60`

```
Goal: resolve_queue_backlog (priority=0.60)
Candidates:
  log_recommendation: score=0.73 (base 0.68 + safety 0.05)      ← SELECTED
  noop:               score=0.48 (base 0.68 - noop 0.20)
  trigger_resync:     score=0.38 (base 0.68 - backlog 0.30)

Selected: log_recommendation
Explanation: "selected log_recommendation ...; context: high backlog (60)"
```

---

## 6. Tests Added

### planning package — 21 tests, all PASS

**Scorer tests:**
- `TestScoreCandidate_BaseScore` — base score = 0.50 + priority×0.30
- `TestScoreCandidate_NoopPenalty` — noop gets -0.20 penalty
- `TestScoreCandidate_FeedbackPreferBoost` — prefer_action +0.25
- `TestScoreCandidate_FeedbackAvoidPenalty` — avoid_action -0.40
- `TestScoreCandidate_HighBacklogPenalizesResync` — trigger_resync -0.30 when backlog > 50
- `TestScoreCandidate_HighBacklogPenalizesRetrySlightly` — retry_job -0.10 when backlog > 50
- `TestScoreCandidate_HighRetryBoostsRetryIfHealthy` — retry_job +0.15 when retries > 10
- `TestScoreCandidate_HighRetryNoBoostIfAvoid` — no boost when feedback says avoid
- `TestScoreCandidate_HighFailureRateBoostsRecommendation` — log_recommendation +0.15
- `TestScoreCandidate_LowAcceptancePrefersAdvisory` — advisory > destructive
- `TestScoreCandidate_SafetyPreference` — log_recommendation +0.05

**Planning decision tests:**
- `TestPlanForGoal_PreferHistoricalGood` — **Validation A**
- `TestPlanForGoal_AvoidHistoricalBad` — **Validation B**
- `TestPlanForGoal_HighBacklogPenalizesResync` — **Validation C**
- `TestPlanForGoal_AllCandidatesBad_Noop` — all bad → noop fallback
- `TestPlanForGoal_DeterministicOrdering` — same input → same output
- `TestPlanForGoal_ExplanationsPopulated` — **Validation D**
- `TestPlanForGoal_AdvisoryGoals_LogRecommendation` — advisory goals prefer advisory actions
- `TestPlanForGoal_UnknownGoal_Noop` — unknown goal → noop

**Candidate map tests:**
- `TestCandidatesForGoal_KnownGoals` — all 6 goal types mapped correctly
- `TestCandidatesForGoal_UnknownReturnNoop` — unknown → ["noop"]

**Context-aware combined tests:**
- `TestContextAware_HighFailureRateWithBadHistory` — double penalty stacking

---

## 7. Verification Results

### Required validations

| Validation | Status | Test |
|-----------|--------|------|
| A — Historical preference: retry_job preferred when success_rate ≥ 0.7 | ✅ | `TestPlanForGoal_PreferHistoricalGood` |
| B — Historical avoidance: retry_job avoided when failure_rate ≥ 0.5 | ✅ | `TestPlanForGoal_AvoidHistoricalBad` |
| C — Context-aware: trigger_resync penalized with high backlog | ✅ | `TestPlanForGoal_HighBacklogPenalizesResync` |
| D — Explainability: decisions include concise explanations | ✅ | `TestPlanForGoal_ExplanationsPopulated` |
| E — Regression safety: all existing tests pass | ✅ | Full suite: all 18 packages pass |

### Test suite summary

```
ok  github.com/tiroq/arcanum/internal/agent/actionmemory    (19 tests)
ok  github.com/tiroq/arcanum/internal/agent/actions          (existing tests)
ok  github.com/tiroq/arcanum/internal/agent/goals            (existing tests)
ok  github.com/tiroq/arcanum/internal/agent/outcome          (existing tests)
ok  github.com/tiroq/arcanum/internal/agent/planning         (21 tests — NEW)
ok  github.com/tiroq/arcanum/internal/api                    (existing tests)
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

### Audit events

| Event | Payload |
|-------|---------|
| `planning.evaluated` | goal_id, goal_type, candidates (type+score+rejected), selected_action_type, explanation |
| `planning.action_selected` | goal_id, goal_type, selected_action_type, candidates, explanation |

### API endpoint

`GET /api/v1/agent/planning-decisions` — Returns last computed planning decisions with full candidate scores and explanations.

---

## 8. Remaining Gaps

1. **No persistent storage of decisions** — `LastDecisions()` holds only the most recent cycle in memory. Future iterations could persist to a `planning_decisions` table.
2. **No per-target scoring** — Scoring is at the action-type level, not per-target. The `agent_action_memory_targets` table (Iteration 5) is available for future per-target scoring.
3. **No adaptive scheduling** — Planner is triggered manually via `POST /api/v1/agent/run-actions`. Autonomous scheduling is deferred per task spec.
4. **Advisory goals have limited candidates** — `increase_reliability`, `increase_model_quality`, `reduce_latency` only have `log_recommendation`/`noop`. New action types (model switching, prompt adjustment) would expand these.

---

## 9. Next Step Readiness

The system is ready for:

- **Iteration 7: Autonomous Scheduling** — The adaptive planner can be invoked on a timer since it's stateless and deterministic.
- **LLM-driven planning** — The `ActionPlanner` interface allows swapping in an LLM-backed planner without changing the engine or guardrails.
- **New action types** — Adding entries to `goalCandidateMap` and implementing executor methods is the only requirement.
- **Policy mutation** — The `prefer_action`/`avoid_action` feedback loop creates a natural foundation for automated policy changes.
