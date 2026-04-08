# Iteration 18.1 — Continuation Learning Separation: Validation Report

**Date**: 2025-07-26  
**Status**: COMPLETE — all tests passing, full regression clean

---

## Summary

Iteration 18.1 separates **strategy effectiveness** from **continuation (step 2) effectiveness** so the system independently learns:

1. **When a strategy works** (existing success/failure rates)
2. **When continuing a strategy is beneficial** (new continuation gain tracking)

The key insight: a strategy can have a high success rate (step 1 works) but continuation (step 2) may add no additional value. Previously these were conflated. Now they are tracked and gated independently.

---

## Core Model: Continuation Gain

```
ContinuationGain = step2Status == "success" AND step1Status != "success"
```

This captures "step 2 added value beyond what step 1 achieved alone."

- Step 1 success + Step 2 neutral → **no gain** (step 2 was wasted)
- Step 1 neutral + Step 2 success → **gain** (step 2 rescued the execution)
- Both neutral → **no gain**
- Both success → **no gain** (step 1 already succeeded)

---

## Changes Made

### 1. Types (`strategy_learning/types.go`)

**StrategyOutcome** extended with:
- `Step1Status`, `Step2Status` — per-step granularity
- `ContinuationUsed` — whether step 2 was actually executed  
- `ContinuationGain` — whether step 2 added value beyond step 1

**StrategyMemoryRecord** extended with:
- `Step1SuccessRuns`, `Step2SuccessRuns` — per-step success counters
- `ContinuationUsedRuns`, `ContinuationGainRuns` — continuation tracking
- `Step1SuccessRate`, `Step2SuccessRate`, `ContinuationGainRate` — derived rates

**StrategyFeedback** extended with:
- `PreferContinuation` — signal to boost multi-step scoring
- `AvoidContinuation` — signal to penalize multi-step scoring

**New constants:**
| Constant | Value | Purpose |
|---|---|---|
| `PreferContinuationGainRate` | 0.60 | Gain rate above which continuation is preferred |
| `AvoidContinuationGainRate` | 0.30 | Gain rate below which continuation is avoided |
| `MinContinuationSampleSize` | 5 | Minimum runs before continuation gating activates |
| `ContinuationPreferBoost` | 0.10 | Scoring boost for preferred continuation |
| `ContinuationAvoidPenalty` | -0.15 | Scoring penalty for avoided continuation |

### 2. Memory Model (`strategy_learning/memory.go`)

- `UpdateMemory()` now delegates to `UpdateMemoryWithContinuation()` (backward compat)
- New UPSERT SQL handles all step-level and continuation counters atomically
- Division-by-zero protection via SQL `CASE WHEN ... > 0 THEN ... / ... ELSE 0 END`
- `SaveOutcome`, `ListOutcomes`, `GetMemory`, `ListMemory` all scan new columns

### 3. Migration (`000024_extend_strategy_learning_continuation`)

- `ALTER TABLE agent_strategy_memory` — 7 new columns (counters + rates, all `DEFAULT 0`)
- `ALTER TABLE agent_strategy_outcomes` — 4 new columns (step statuses + booleans)
- All columns have sensible defaults → existing rows unaffected

### 4. Evaluator (`strategy_learning/evaluator.go`)

- `EvaluateOutcome()` delegates to `EvaluateOutcomeWithSteps()` with empty step statuses
- `EvaluateOutcomeWithSteps()` computes `ContinuationGain` from step-level signals
- `GenerateFeedback()` now computes continuation signals:
  - `ContinuationUsedRuns >= 5` AND `ContinuationGainRate >= 0.60` → `PreferContinuation`
  - `ContinuationUsedRuns >= 5` AND `ContinuationGainRate <= 0.30` → `AvoidContinuation`

### 5. Continuation Engine (`strategy_learning/continuation.go`)

- Extracted `MemoryReader` interface (for testability without DB)
- Added **Gate 8**: `low_continuation_gain` — blocks continuation when `ContinuationUsedRuns >= 5` AND `ContinuationGainRate < 0.30`
- Gate 5 error handling changed to **fail-open** (was: return skip on error → now: log warning, allow continuation)

### 6. Planner Scoring (`strategy/scorer.go`)

- `StrategyFeedbackSignal` extended with `PreferContinuation` / `AvoidContinuation`
- **Section G**: for multi-step strategies (`stepCount > 1`):
  - `AvoidContinuation` → `avgQuality += ContinuationAvoidPenalty` (-0.15)
  - `PreferContinuation` → `avgQuality += ContinuationPreferBoost` (+0.10)
- Single-step strategies unaffected by continuation signals

### 7. Adapter (`strategy_learning/adapter.go` + `strategy/planner_adapter.go`)

- `PreferContinuation` / `AvoidContinuation` threaded through:
  - `StrategyFeedback` → `planning.StrategyLearningFeedback` → `StrategyFeedbackSignal`

---

## Test Coverage

### 9 New Tests (all passing)

| # | Test | Validates |
|---|---|---|
| 1 | `TestContinuationGain_Step1SuccessStep2Neutral` | Step1 success + Step2 neutral → no gain → AvoidContinuation |
| 2 | `TestContinuationGain_Step1NeutralStep2Success` | Step1 neutral + Step2 success → gain → PreferContinuation |
| 3 | `TestContinuationGain_AlwaysNeutral_LearnsToStop` | Low gain rate → Gate 8 blocks continuation |
| 4 | `TestContinuationGain_MixedOutcomes` | Mixed outcomes → correct intermediate rate (0.42) → no signal |
| 5 | `TestContinuationGain_InsufficientData` | < 5 continuation runs → no gating, no feedback signals |
| 6 | `TestContinuationGain_BackwardCompatibility` | Empty step statuses → no continuation signals |
| 7 | `TestContinuationGain_DivisionByZeroSafe` | Zero continuation runs → no panic, no signals |
| 8 | `TestStrategyScoring_ContinuationPreferBoost` | Multi-step: PreferContinuation boosts utility, AvoidContinuation penalizes |
| 9 | `TestStrategyScoring_ContinuationSignals_SingleStep_NoEffect` | Single-step: continuation signals have zero effect |

### Existing Tests (all still passing)

10 tests from Iteration 18 continue to pass unchanged:
- `TestClassifyOutcome_Success/Failure/Neutral`
- `TestGenerateFeedback_MemoryUpdates`
- `TestContinuation_AllGatesPassed/BlockedInSafeMode/BlockedOnHighFailureRate`
- `TestStrategyScoring_PreferBoost/AvoidPenalty/NoRegression_ActionOnlyMode`

### Full Suite

```
go test ./... -count=1
```
All packages pass. Zero failures. Zero regressions.

---

## Backward Compatibility

| Scenario | Behavior |
|---|---|
| Empty `Step1Status` / `Step2Status` | No continuation signals generated |
| `ContinuationUsedRuns = 0` | No gating (division-by-zero safe) |
| `ContinuationUsedRuns < 5` | No gating (insufficient data) |
| `EvaluateOutcome()` (legacy caller) | Delegates with empty step statuses → no step-level effects |
| `UpdateMemory()` (legacy caller) | Delegates with empty step fields → only legacy counters updated |
| Existing DB rows (pre-migration) | All new columns default to 0/""/"false" |

---

## Edge Cases Handled

1. **Division by zero**: SQL CASE prevents division when `continuation_used_runs = 0`
2. **Fail-open on error**: Memory lookup failure in Gate 5 logs warning and allows continuation
3. **Threshold boundaries**: Gain rate exactly at 0.30 → AvoidContinuation. Gain rate exactly at 0.60 → PreferContinuation
4. **Single-step immunity**: Continuation signals only affect strategies with `stepCount > 1`
5. **Concurrent evolution**: Step-level learning and overall learning accumulate independently — one does not override the other

---

## Design Principles Upheld

- **No silent failure**: All gate blocks and continuation outcomes are audited
- **Explicit state transitions**: Continuation gain is deterministically computed from step outcomes
- **Observability**: All new signals included in audit events and feedback structs
- **Deterministic recovery**: Gate 8 learned blocking is reversible — if gain rate improves above threshold, continuation resumes
- **Minimal magic**: Pure functions for evaluation and feedback, explicit thresholds, no hidden state

---

## Files Modified

| File | Change |
|---|---|
| `internal/agent/strategy_learning/types.go` | Extended StrategyOutcome, StrategyMemoryRecord, StrategyFeedback; 5 new constants |
| `internal/agent/strategy_learning/memory.go` | UpdateMemoryWithContinuation, scan updates |
| `internal/agent/strategy_learning/evaluator.go` | EvaluateOutcomeWithSteps, continuation feedback in GenerateFeedback |
| `internal/agent/strategy_learning/continuation.go` | MemoryReader interface, Gate 8, fail-open Gate 5 |
| `internal/agent/strategy_learning/adapter.go` | PreferContinuation/AvoidContinuation passthrough |
| `internal/agent/strategy/scorer.go` | Section G continuation scoring |
| `internal/agent/strategy/types.go` | ContinuationPreferBoost, ContinuationAvoidPenalty constants |
| `internal/agent/strategy/planner_adapter.go` | Thread continuation signals |
| `internal/agent/planning/planner.go` | StrategyLearningFeedback continuation fields |
| `internal/db/migrations/000024_*` | ALTER TABLE for both strategy tables |
| `internal/agent/strategy_learning/strategy_learning_continuation_test.go` | 9 new tests |
