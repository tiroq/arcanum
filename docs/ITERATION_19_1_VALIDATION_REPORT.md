# Iteration 19.1 Validation Report: Scoring Stabilization + Signal Arbitration Layer

**Date:** 2025-07-25  
**Status:** COMPLETE  
**Regressions:** 0 (all 29 testable packages pass)

---

## Objective

Address scoring instability from heterogeneous signals feeding a flat weighted sum.
Decompose confidence and risk into sub-components, normalize all signals to [0,1],
add inertia (anti-oscillation) to the portfolio selector, and introduce a
`DecisionSignals` structure for full auditability.

---

## Changes Summary

### 1. Signal Decomposition (`portfolio.go`)

**ConfidenceComponents** — decomposes confidence into evidence-quality sub-signals:
- `SampleConfidence` (0.6 weight): from action memory sample sizes + strategy memory evidence
- `RecencyConfidence` (0.4 weight): from plan confidence + multi-step degradation
- Composed via: `clamp01(SampleConfidence * 0.6 + RecencyConfidence * 0.4)`

**RiskComponents** — decomposes risk into source-specific sub-signals:
- `StabilityRisk` (0.5 weight): from stability mode (safe_mode → 1.0 for non-safe, throttled + multi-step → 0.6)
- `HistoricalRisk` (0.3 weight): from strategy memory failure rate
- `PolicyRisk` (0.2 weight): from plan risk + step count penalty
- Composed via: `clamp01(StabilityRisk * 0.5 + HistoricalRisk * 0.3 + PolicyRisk * 0.2)`

**DecisionSignals** — wraps EV, Confidence, Risk + decomposed components for audit trail.

### 2. Signal Normalization (`portfolio.go`)

- Added `clamp01()` helper used throughout the enrichment pipeline
- All intermediate and final values normalized to [0, 1]
- Replaced inline clamping with consistent `clamp01()` calls

### 3. Enrichment Pipeline Refactor (`portfolio.go`)

Refactored `enrichCandidate()` to a 5-phase pipeline:
1. **Collect raw EV signals** (strategy memory, continuation gain, action memory)
2. **Decompose confidence** via `decomposeConfidence()`
3. **Decompose risk** via `decomposeRisk()`
4. **Normalize** all signals with `clamp01()`
5. **Compute FinalScore** from normalized composites

Old stability adjustment function retained for backward compatibility.

### 4. Inertia / Anti-Oscillation (`portfolio_selector.go`)

- `InertiaBoost = 0.05`: added to incumbent candidate's FinalScore
- `InertiaThreshold = 0.10`: inertia only applies when gap to leader < threshold
- `PortfolioSelectConfig.LastSelectedStrategy`: new field, wired from `engine.lastPortfolioSelection`
- `applyInertia()`: applied after initial sort, then re-sorted
- Effect: prevents flip-flopping between closely-scored strategies across cycles

### 5. Engine Wiring (`engine.go`)

- `EvaluatePortfolio()` now passes `LastSelectedStrategy` from previous selection to `SelectFromPortfolio()`

---

## Test Results

### New Tests (strategy_scoring_stability_test.go): 10/10 PASS

| # | Test | Status |
|---|------|--------|
| 1 | Deterministic (10 runs identical) | PASS |
| 2 | Small noise + inertia prevents oscillation | PASS |
| 3 | High EV + low confidence penalized | PASS |
| 4 | High confidence + low EV not dominating | PASS |
| 5 | Risk decomposition correct components | PASS |
| 6 | Inertia prevents flip-flop | PASS |
| 7 | Normalization clamps extremes | PASS |
| 8 | All signals zero edge case | PASS |
| 9 | Confidence decomposition weights correct | PASS |
| 10 | Inertia disabled for large gap | PASS |

### Existing Tests (strategy_portfolio_test.go): 19/19 PASS

All Iteration 19 portfolio tests continue to pass with zero regressions.

### Full Suite: 29/29 testable packages PASS

Pre-existing issue in `internal/agent/decision_graph` (conflicting package names) is unrelated.

---

## Scoring Traces

### Before (Iteration 19) — Flat Signals

```
enrichCandidate:
  ev = plan.ExpectedUtility + stratMemAdj + contGainAdj + actionMemAdj
  risk = plan.RiskScore + stabilityAdj
  confidence = plan.Confidence  (pass-through)
  FinalScore = ev*0.5 + conf*0.3 - risk*0.2
```

Problem: confidence is a single opaque value, risk conflates stability/historical/policy sources,
small signal differences cause oscillation between strategies.

### After (Iteration 19.1) — Decomposed + Normalized

```
enrichCandidate:
  Phase 1: ev = plan.ExpectedUtility + stratMemAdj + contGainAdj + actionMemAdj
  Phase 2: confidence = ConfidenceComponents{
    SampleConfidence:  f(action_memory_samples, strategy_memory_samples),
    RecencyConfidence: f(plan.Confidence, step_degradation),
  }.Compose()  // 0.6*Sample + 0.4*Recency
  Phase 3: risk = RiskComponents{
    StabilityRisk:  f(stability_mode, step_count),
    HistoricalRisk: f(strategy_failure_rate),
    PolicyRisk:     f(plan_risk, step_count),
  }.Compose()  // 0.5*Stability + 0.3*Historical + 0.2*Policy
  Phase 4: clamp01(ev), clamp01(confidence), clamp01(risk)
  Phase 5: FinalScore = clamp01(ev*0.5 + conf*0.3 - risk*0.2)

Selection:
  applyInertia: if lastSelected == candidate && gap < 0.10 → +0.05
```

---

## Oscillation Reduction Evidence

**Test 6 (InertiaPreventFlipFlop):**
- PlanA (DirectRetry): EV=0.55, PlanB (DirectResync): EV=0.53
- Without inertia: A wins every time
- With inertia favoring B: B wins (gap < InertiaThreshold)
- With inertia favoring A: A wins
- → System "sticks" to incumbent when scores are close

**Test 10 (InertiaDisabledForLargeGap):**
- PlanA: EV=0.8, PlanB: EV=0.3
- Even with inertia favoring B: A still wins
- → Inertia doesn't override genuinely better strategies

---

## Edge Cases Verified

1. **All signals zero** → FinalScore = 0, Signals correctly populated, no panic
2. **Extreme values** (EV=1.5, risk=-0.5, conf=2.0) → all clamped to [0,1]
3. **Empty action feedback** → SampleConfidence defaults to 0.5
4. **No strategy memory** → HistoricalRisk = 0, SampleConfidence not blended
5. **Single-step plan** → no multi-step degradation, no continuation gain

---

## Architecture Compliance

| Principle | Status |
|-----------|--------|
| Deterministic scoring | ✅ Same inputs → same outputs (Test 1) |
| Observable signals | ✅ DecisionSignals struct on every candidate |
| No silent failure | ✅ All values bounded, no panic paths |
| Explicit state | ✅ Decomposed components visible in audit |
| Backward compatible | ✅ stabilityAdjustment() retained, existing tests pass |
