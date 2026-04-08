# Iteration 19 Validation Report — Strategy Portfolio + Competition Layer

**Date:** 2026-04-08
**Status:** PASS — All tests pass, zero regressions

---

## Objective

Move from fixed strategy selection to competitive portfolio evaluation:

```
BEFORE: goal → strategy → action
AFTER:  goal → multiple strategies → enrich → score → select best → action
```

System now **chooses** strategies through a scored competition, not just executes them.

---

## Architecture

### New Files

| File | Purpose |
|------|---------|
| `internal/agent/strategy/portfolio.go` | StrategyCandidate model, portfolio scoring formula, signal enrichment |
| `internal/agent/strategy/portfolio_selector.go` | Portfolio selection engine with exploration + stability overrides |
| `internal/agent/strategy/strategy_portfolio_test.go` | 19 test scenarios |
| `internal/db/migrations/000025_add_strategy_portfolio_tracking.up.sql` | Selection count + win rate tracking columns |
| `internal/db/migrations/000025_add_strategy_portfolio_tracking.down.sql` | Rollback migration |

### Modified Files

| File | Change |
|------|--------|
| `internal/agent/strategy/engine.go` | Added `EvaluatePortfolio()` method, `lastPortfolioSelection` field |
| `internal/agent/strategy/planner_adapter.go` | Uses portfolio evaluation, `WithExplorationTrigger()`, continuation gain extraction |
| `internal/agent/strategy_learning/types.go` | Added `SelectionCount`, `WinCount`, `WinRate` to `StrategyMemoryRecord` |
| `internal/agent/strategy_learning/memory.go` | Added `RecordSelection()`, updated `GetMemory`/`ListMemory` scans |

---

## Scoring Formula

```
FinalScore = ExpectedValue × 0.5 + Confidence × 0.3 - RiskScore × 0.2
```

**Properties:**
- Deterministic: same inputs → same output
- Bounded: [0, 1]
- Verified by `TestPortfolio_ScoringFormula` and `TestPortfolio_FinalScoreBounded`

### Scoring Breakdown Example

| Strategy | ExpectedValue | RiskScore | Confidence | FinalScore |
|-----|-----|-----|-----|-----|
| direct_retry | 0.80 | 0.00 | 0.90 | **0.67** |
| observe_then_retry | 0.50 | 0.15 | 0.70 | **0.46** |
| recommendation_only | 0.40 | 0.00 | 0.60 | **0.38** |
| noop | 0.10 | 0.00 | 1.00 | **0.35** |

Direct retry wins with highest FinalScore, as expected with high expected value and confidence.

---

## Signal Sources

Each candidate is enriched from 7 signal sources:

| Signal | Source | Adjustment Range |
|--------|--------|-----------------|
| Strategy memory | `strategy_learning.MemoryStore` | [-0.10, +0.08] |
| Continuation gain | `strategy_learning` gain rates | [-0.05, +0.05] |
| Action memory (aggregate) | `actionmemory.ActionFeedback` | [-0.05, +0.05] |
| Stability state | `StabilityProvider.GetMode()` | [0, +0.8] risk |
| Policy adjustments | Via `ScoreInput.CandidateScores` | Already in base |
| Contextual memory | Via `ScoreInput.ActionFeedback` | Already in base |
| Provider-context memory | Via `ScoreInput.ActionFeedback` | Already in base |

---

## Selection Rules

1. **Sort** by FinalScore DESC
2. **Reject** FinalScore ≤ 0 (noop always kept)
3. **Tie** → simpler strategy wins (fewer steps, within SimplicityBias=0.05)
4. **All invalid** → fallback to noop
5. **Exploration override** → deterministic toggle selects second-best
6. **Safe mode** → only noop and recommendation_only allowed

---

## Stability Integration

| Mode | Effect |
|------|--------|
| `normal` | No adjustment |
| `throttled` | +0.3 risk for multi-step strategies |
| `safe_mode` | Only noop/recommendation_only allowed; +0.8 risk for aggressive strategies |

Verified by:
- `TestPortfolio_SafeMode_OnlySafeStrategies`
- `TestPortfolio_ThrottledPenalizesMultiStep`
- `TestPortfolio_StabilityAdjustment`

---

## Exploration Integration

Deterministic override mechanism:
- `WithExplorationTrigger(fn)` on `PlannerAdapter`
- When triggered: selects second-best candidate from portfolio
- No randomness — trigger function is deterministic

Verified by `TestPortfolio_ExplorationOverride_SelectsSecondBest`

---

## Memory Tracking

New columns in `agent_strategy_memory`:

| Column | Type | Purpose |
|--------|------|---------|
| `selection_count` | INTEGER | How many times this strategy was selected |
| `win_count` | INTEGER | How many times selection led to success |
| `win_rate` | DOUBLE PRECISION | `win_count / selection_count` |

Updated via `MemoryStore.RecordSelection(strategyType, goalType, won)`.

---

## Test Results

**19 portfolio tests + 20 existing strategy tests = 39 total, all PASS**

| # | Test | Status |
|---|------|--------|
| 1 | `TestPortfolio_BestStrategySelected` | PASS |
| 2 | `TestPortfolio_HighRiskRejected` | PASS |
| 3 | `TestPortfolio_LowConfidencePenalized` | PASS |
| 4 | `TestPortfolio_EqualScore_SimplerWins` | PASS |
| 5 | `TestPortfolio_AllBadFallbackToNoop` | PASS |
| 6 | `TestPortfolio_SafeMode_OnlySafeStrategies` | PASS |
| 7 | `TestPortfolio_ExplorationOverride_SelectsSecondBest` | PASS |
| 8 | `TestPortfolio_Deterministic` | PASS |
| 9 | `TestPortfolio_MemorySignalsAffectScoring` | PASS |
| 10 | `TestPortfolio_ScoringFormula` | PASS |
| 11 | `TestPortfolio_FinalScoreBounded` (5 sub-tests) | PASS |
| 12 | `TestPortfolio_StrategyMemoryAdjustment` | PASS |
| 13 | `TestPortfolio_ContinuationGainAdjustment` | PASS |
| 14 | `TestPortfolio_StabilityAdjustment` | PASS |
| 15 | `TestPortfolio_EndToEnd` | PASS |
| 16 | `TestPortfolio_ThrottledPenalizesMultiStep` | PASS |
| 17 | `TestPortfolio_EmptyCandidates` | PASS |
| 18 | `TestPortfolio_ActionMemoryAggregate` | PASS |
| 19 | `TestPortfolio_SortDeterministic` | PASS |

**Full regression suite: all 35 packages pass**

---

## Failure Scenarios

| Scenario | Behavior |
|----------|----------|
| All strategies score ≤ 0 | Noop fallback selected |
| All strategies blocked | Noop fallback (blocked strategies get risk=1.0) |
| No candidates generated | `no_candidates` reason returned |
| Strategy memory unavailable | Enrichment skipped, base scores used |
| Stability provider error | Fail-open: treated as normal mode |
| Exploration with only 1 viable candidate | Exploration skipped (needs 2+ viable) |

---

## Comparison vs Previous Behavior

| Aspect | Before (Iteration 18) | After (Iteration 19) |
|--------|----------------------|---------------------|
| Strategy selection | Single-pass scoring → best utility | Portfolio competition with 7 signal sources |
| Scoring formula | `utility = quality - risk × 0.5` | `FinalScore = EV × 0.5 + Conf × 0.3 - Risk × 0.2` |
| Exploration | Planner-level only | Portfolio-level second-best selection |
| Stability integration | Scorer penalties only | Portfolio-level safe_mode filtering + risk adjustments |
| Memory tracking | None for selection | SelectionCount + WinRate per strategy+goal |
| Auditability | strategy.selected | strategy.portfolio_generated + strategy.portfolio_selected |
| Signal enrichment | Strategy feedback only | 7 signal sources: strategy memory, continuation gain, action memory aggregate, stability, policy, contextual, provider-context |

### Backward Compatibility

- `Engine.Evaluate()` still works unchanged (existing callers unaffected)
- `Engine.EvaluatePortfolio()` is additive — used by `PlannerAdapter`
- Existing strategy decision format preserved (StrategyDecision unchanged)
- All 20 pre-existing strategy tests pass without modification

---

## Definition of Done

| Criterion | Status |
|-----------|--------|
| Strategy candidates implemented | DONE |
| Scoring model working | DONE |
| Selection engine deterministic | DONE |
| Planner uses strategy selection | DONE |
| Stability integrated | DONE |
| Exploration integrated | DONE |
| Memory tracking added | DONE |
| Tests pass | DONE (19 new + 20 existing) |
| No regressions | DONE (all 35 packages pass) |
| Validation report created | DONE |
