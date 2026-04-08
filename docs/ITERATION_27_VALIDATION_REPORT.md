# Iteration 27 — Signal Arbitration Layer: Validation Report

## 1. Signal Priority Table

| Priority | Signal Type         | Description                                    |
|----------|---------------------|------------------------------------------------|
| 1        | Stability           | System stability mode (safe_mode, throttled)   |
| 2        | Calibration         | Calibrated confidence assessment               |
| 3        | Causal              | Counterfactual prediction signals              |
| 4        | Comparative         | Comparative path selection learning             |
| 5        | PathLearning        | Historical path outcome memory                 |
| 6        | TransitionLearning  | Historical transition outcome memory           |
| 7        | Exploration         | Exploration/exploitation balance               |

Higher priority ALWAYS overrides lower in conflict. No exceptions.

---

## 2. Arbitration Flow Diagram

```
Input: Raw signals from all learning subsystems
         │
         ▼
┌─────────────────────────────┐
│  Phase 1: Exploration       │
│  Isolation (Rule 5)         │
│  ─ If Stability/Calibration │
│    has directional intent,  │
│    suppress Exploration     │
└──────────┬──────────────────┘
           ▼
┌─────────────────────────────┐
│  Phase 2: Confidence        │
│  Suppression (Rule 2)       │
│  ─ If calibratedConf < 0.4  │
│    suppress PathLearning,   │
│    TransitionLearning,      │
│    Comparative              │
└──────────┬──────────────────┘
           ▼
┌─────────────────────────────┐
│  Phase 3: Hard Override     │
│  (Rule 1)                   │
│  ─ Higher priority signal   │
│    contradicting lower →    │
│    lower suppressed         │
└──────────┬──────────────────┘
           ▼
┌─────────────────────────────┐
│  Phase 4: Conflict Check    │
│  ─ Remaining active signals │
│    have both prefer+avoid?  │
│    YES → Rule 4 neutralize  │
│    NO  → Rule 3 reinforce   │
└──────────┬──────────────────┘
           ▼
Output: FinalAdjustment + ArbitrationTrace
```

---

## 3. Conflict Resolution Examples

### Example 1: Stability Overrides Path Learning
- **Stability** = avoid (-0.20)
- **PathLearning** = prefer (+0.10)
- **Result**: PathLearning suppressed by hard_override. FinalAdjustment = -0.20

### Example 2: Low Confidence Suppresses Learning
- **Calibrated confidence** = 0.30 (below 0.4 threshold)
- **PathLearning** = prefer (+0.10)
- **Comparative** = prefer (+0.10)
- **Stability** = neutral (0.00)
- **Result**: PathLearning and Comparative suppressed by confidence_suppression. FinalAdjustment = 0.00

### Example 3: Reinforcement
- **PathLearning** = prefer (+0.10)
- **TransitionLearning** = prefer (+0.05)
- **Comparative** = prefer (+0.10)
- **Result**: All agree → sum = +0.25

### Example 4: Conflict Neutralization
- **Causal_A** = prefer (+0.15)
- **Causal_B** = avoid (-0.10)
- **Result**: Same priority, conflict → neutralized = 0.05 * 0.7 = 0.035

---

## 4. Before vs After Behavior

### Before (Iterations 21–23)
Scoring pipeline applied adjustments sequentially:
1. `ApplyPathLearningAdjustments` → additive
2. `ApplyComparativeLearningAdjustments` → additive
3. `ApplyCounterfactualAdjustments` → additive

**Problem**: No conflict resolution. If path learning said "prefer" and stability mode was "throttled", both adjustments could be applied simultaneously, causing oscillation.

### After (Iteration 27)
All signals collected into a unified set per path, then resolved through deterministic arbitration:
1. Collect all signals (stability, calibration, path, transition, comparative, counterfactual, exploration)
2. `ResolveSignals()` applies 5 rules in fixed order
3. Single `FinalAdjustment` applied per path

**Improvement**: Conflicting signals are explicitly resolved. Higher-priority signals always win. Low-confidence suppression prevents unreliable learning from affecting decisions.

---

## 5. Oscillation Reduction Proof

Oscillation occurs when conflicting signals alternate dominance across cycles. The arbitration layer prevents this via:

1. **Strict priority ordering**: Stability (priority 1) always dominates. If the system enters throttled/safe_mode, all lower signals that contradict it are suppressed deterministically.

2. **Confidence suppression**: When calibrated confidence drops below 0.4, learning signals (path, transition, comparative) are suppressed entirely, preventing unreliable signals from pulling scores back and forth.

3. **Conflict neutralization**: When signals of equal priority conflict (prefer vs avoid), the adjustment is pulled toward zero rather than alternating between the two extremes.

4. **Deterministic output**: Same inputs always produce the same output (verified by 100-iteration determinism test).

---

## 6. Test Results

All 24 arbitration tests pass. All 30 test packages pass with zero regressions.

| Test | Description | Status |
|------|-------------|--------|
| TestHardOverride_StabilityOverridesPathLearning | Higher priority overrides lower | PASS |
| TestHardOverride_CalibrationOverridesComparative | Calibration > Comparative | PASS |
| TestConfidenceSuppression_LowConfidence | Low confidence suppresses learning | PASS |
| TestConfidenceSuppression_HighConfidence_NoSuppression | High confidence preserves signals | PASS |
| TestStabilityAlwaysWins | Stability beats all others | PASS |
| TestReinforcement_AgreeingSignals | Agreeing signals sum | PASS |
| TestConflictNeutralization | Equal-priority conflicts neutralized | PASS |
| TestExplorationIsolation_StabilityPresent | Exploration blocked by stability | PASS |
| TestExplorationIsolation_CalibrationPresent | Exploration blocked by calibration | PASS |
| TestExplorationIsolation_NoBlocker | Exploration allowed without blockers | PASS |
| TestDeterministic_RepeatRuns | 100 runs identical output | PASS |
| TestFailOpen_EmptySignals | Nil signals → zero adjustment | PASS |
| TestFailOpen_EmptySlice | Empty slice → zero adjustment | PASS |
| TestNoRegression_SinglePathLearning | Single prefer = +0.10 | PASS |
| TestNoRegression_SingleComparativeAvoid | Single avoid = -0.20 | PASS |
| TestTraceAlwaysPopulated | Trace always has reason + signature | PASS |
| TestEdgeCase_AllConflicting | Same-priority conflict → neutralization | PASS |
| TestEdgeCase_LowConfidenceMultipleSignals | Low conf + mixed signals | PASS |
| TestEdgeCase_IdenticalScores | Identical adjustments sum | PASS |
| TestPriority_Order | Priority numbering correct | PASS |
| TestHigherPriority | Comparison function correct | PASS |
| TestIsLearningSignal | Learning signal classification | PASS |
| TestSignalType_String | Enum string rendering | PASS |
| TestRecommendation_String | Recommendation string rendering | PASS |
| TestNeutralsOnly | All neutral → zero adjustment | PASS |

---

## 7. Regression Summary

Full test suite: **30 packages, 0 failures**

- `internal/agent/arbitration`: 24 tests PASS (new)
- `internal/agent/decision_graph`: All existing tests PASS (no regression)
- `internal/api`: All existing tests PASS (no regression)
- All other packages: PASS (cached, unchanged)

The existing `ApplyPathLearningAdjustments`, `ApplyComparativeLearningAdjustments`, and `ApplyCounterfactualAdjustments` functions are retained for backward compatibility and unit testing but are no longer called in the main pipeline. The `planner_adapter.go` now routes all signal application through `ApplyArbitratedAdjustments`.

---

## 8. Risk Analysis

### Where arbitration can fail

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| All signals suppressed → zero adjustment | Low | Fail-open: no adjustment is safe; system falls back to base graph scoring |
| Stability permanently stuck in throttled → all learning suppressed | Low | Stability engine has its own recovery mechanism; arbitration only reads mode |
| Calibration confidence always below 0.4 → permanent learning suppression | Low | Calibration threshold (0.4) is conservative; requires sustained poor accuracy |
| New signal types added without priority assignment | Medium | Signal types are a Go enum; compilation will fail if switch cases are missing |
| Counterfactual predictions produce NaN/Inf | Low | clamp01 prevents propagation; counterfactual has its own min-confidence gate |

### Design decisions and trade-offs

1. **Priority is static, not learned**: The priority ordering (Stability > Calibration > ... > Exploration) is hardcoded. This prevents meta-learning from destabilizing the arbitration layer itself.

2. **Neutralization strength is fixed at 0.3**: When conflicts are neutralized, 30% of the conflict magnitude is absorbed. This biases toward smaller adjustments under uncertainty.

3. **Exploration is lowest priority by design**: Exploration can never override safety-critical signals. This is intentional — exploration should only activate when the system is confident and stable.

---

## Files Changed

### New files
- `internal/agent/arbitration/types.go` — SignalType enum, Signal, ArbitrationResult, thresholds
- `internal/agent/arbitration/priority.go` — Priority ordering, helper functions
- `internal/agent/arbitration/trace.go` — ArbitrationTrace, SuppressedSignal, AppliedSignal
- `internal/agent/arbitration/resolver.go` — ResolveSignals core function, all 5 rules
- `internal/agent/arbitration/arbitration_test.go` — 24 tests covering all rules + edge cases

### Modified files
- `internal/agent/decision_graph/evaluator.go` — Added ArbitratedSignals, ApplyArbitratedAdjustments, collectSignals
- `internal/agent/decision_graph/planner_adapter.go` — Replaced 3 sequential Apply* calls with unified arbitrated call, added arbitration audit events, added LastArbTraces accessor
- `internal/api/handlers.go` — Added ArbitrationTrace handler
- `internal/api/router.go` — Added `/api/v1/agent/arbitration/trace` route
