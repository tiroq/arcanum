# Iteration 24 — Meta-Reasoning Layer: Validation Report

## 1. Overview

Iteration 24 introduces meta-reasoning to the Arcanum decision graph. The system now dynamically selects **how** to reason (which mode to use) before deciding **what** to do. This moves the pipeline from `goal → decision_graph → action` to `goal → select_reasoning_mode → execute_mode → action`.

**Four reasoning modes**:
- **graph** (default): Full decision graph evaluation (Iteration 20 pipeline)
- **direct**: Single-step best action, skip graph expansion — used when confidence is high and risk is low
- **conservative**: Restrict to safe actions (noop, log_recommendation) — triggered by safe_mode or high failure rate
- **exploratory**: Force non-top choice (exploration bias) — triggered by missed wins or stagnation

**Key constraints**: Deterministic mode selection, FAIL-OPEN on all errors, NO breaking changes, NO stochastic logic, mode selection is pure function + memory lookup.

---

## 2. Architecture

### Data Flow

```
EvaluateForPlanner()
  ├── MetaReasoningProvider.SelectMode()  (NEW — choose reasoning mode)
  │     ├── Rule-based selection (hard rules: conservative > direct > exploratory > graph)
  │     ├── Scoring overlay (memory-based success rates + inertia)
  │     └── Record decision to memory + history + audit
  ├── Mode-specific graph config:
  │     ├── graph:        default pipeline (full graph)
  │     ├── direct:       MaxDepth=1 (single-step)
  │     ├── conservative: MaxDepth=1, SafeMode=true, filter to safe actions
  │     └── exploratory:  ShouldExplore=true (second-best selection)
  ├── BuildGraph → EnumeratePaths → EvaluateAllPaths
  ├── ApplyPathLearningAdjustments
  ├── ApplyComparativeLearningAdjustments
  └── SelectBestPath

Action Executed → Outcome Handler
  ├── evaluatePathOutcome (Iteration 21)
  ├── evaluateComparativeOutcome (Iteration 22)
  └── evaluateMetaReasoningOutcome (NEW)
         ├── Extract _ctx_meta_mode and _ctx_goal_type from action params
         ├── Update mode memory (success rate per mode+goal)
         ├── Update history record outcome
         └── Emit audit events
```

### Integration Points

1. **Decision Graph** — `MetaReasoningProvider.SelectMode()` called before graph evaluation to configure mode-specific behavior
2. **Planning** — `StrategyOverride.MetaMode` carries mode through to action context via `_ctx_meta_mode` param
3. **Outcome Handler** — `evaluateMetaReasoningOutcome()` called after comparative outcome to update mode learning
4. **API** — 3 new endpoints for observability

---

## 3. Mode Selection Model

### Rule-Based Selection (Hard Rules)

Priority order (first match wins):

| Priority | Condition | Mode | Confidence | Reason |
|----------|-----------|------|------------|--------|
| 1 | `stability_mode == "safe_mode"` | conservative | 1.0 | stability in safe_mode |
| 2 | `failure_rate >= 0.5` | conservative | 0.9 | high failure rate |
| 3 | `confidence >= 0.8 AND risk < 0.2 AND path_samples >= 5` | direct | 0.85 | strong signal, low risk, sufficient data |
| 4 | `missed_wins >= 3` | exploratory | 0.8 | missed comparative wins |
| 5 | `noop_rate > 0.6 OR low_value_rate > 0.5` | exploratory | 0.7 | stagnation detected |
| default | none of the above | graph | 0.75 | default reasoning mode |

### Scoring Overlay

When no hard rule triggers (default graph), the scoring system can override:

- **Score formula**: `SuccessRate * 0.5 + Confidence * 0.3 - Risk * 0.2`
- **Inertia**: +0.07 boost to last-used mode when gap < 0.15 (prevents oscillation)
- All scores clamped to [0, 1]

### Execution Model

Each mode configures the decision graph differently:
- **graph**: No config changes — full `MaxDepth=3` evaluation
- **direct**: `MaxDepth=1` — only single-step paths evaluated
- **conservative**: `MaxDepth=1` + `SafeMode=true` + path filtering to safe actions only
- **exploratory**: `ShouldExplore=true` — selector picks second-best path

---

## 4. File Inventory

### New Files (6)

| File | Lines | Purpose |
|------|-------|---------|
| `internal/agent/meta_reasoning/types.go` | 103 | DecisionMode type, ModeDecision, ModeMemoryRecord, ModeHistoryRecord, MetaInput, ModeScore, threshold constants |
| `internal/agent/meta_reasoning/scorer.go` | 83 | ScoreMode, ScoreAllModes, ApplyInertia, clamp01 |
| `internal/agent/meta_reasoning/selector.go` | 115 | SelectMode (rule-based), SelectModeWithScoring (rules + scoring + inertia) |
| `internal/agent/meta_reasoning/memory.go` | 199 | MemoryStore (UPSERT for agent_meta_reasoning_memory), HistoryStore (agent_meta_reasoning_history) |
| `internal/agent/meta_reasoning/adapter.go` | 205 | Engine (orchestration + audit + inertia tracking), GraphAdapter (implements MetaReasoningProvider) |
| `internal/agent/meta_reasoning/meta_reasoning_test.go` | 428 | 28 tests covering all selection rules, scoring, inertia, determinism |

### New Migrations

| File | Purpose |
|------|---------|
| `000028_create_agent_meta_reasoning.up.sql` | Creates `agent_meta_reasoning_memory` and `agent_meta_reasoning_history` tables |
| `000028_create_agent_meta_reasoning.down.sql` | Drops both tables |

### Modified Files (5)

| File | Change |
|------|--------|
| `internal/agent/decision_graph/planner_adapter.go` | `MetaReasoningProvider` interface, `WithMetaReasoning()`, mode-aware `EvaluateForPlanner()`, signal computation helpers |
| `internal/agent/planning/planner.go` | `MetaMode` field on `StrategyOverride`, `_ctx_meta_mode` param tagging |
| `internal/agent/outcome/handler.go` | `MetaReasoningOutcomeEvaluator` interface, `WithMetaReasoningEvaluator()`, `evaluateMetaReasoningOutcome()` |
| `internal/api/handlers.go` | `WithMetaReasoning()`, 3 handler methods (status, memory, history) |
| `internal/api/router.go` | 3 routes: `/api/v1/agent/meta-reasoning/{status,memory,history}` |
| `cmd/api-gateway/main.go` | Wiring: stores, engine, adapter, planner integration, outcome handler integration |

---

## 5. Interface Design

All interfaces follow the established pattern: defined in consumer package with primitive types only.

```go
// In decision_graph/planner_adapter.go
type MetaReasoningProvider interface {
    SelectMode(ctx context.Context, goalType string, failureRate, confidence, risk float64,
        stabilityMode string, missedWinCount int, noopRate, lowValueRate float64) (mode string, conf float64, reason string)
    RecordOutcome(ctx context.Context, mode string, goalType string, success bool)
}

// In outcome/handler.go
type MetaReasoningOutcomeEvaluator interface {
    RecordOutcome(ctx context.Context, mode string, goalType string, success bool)
}
```

---

## 6. Tests

### 28 Tests — All Passing

**SelectMode (12 tests)**:
- Conservative: safe_mode, high failure rate, exact threshold
- Direct: strong signal, requires min samples, blocked by high risk
- Exploratory: missed wins, stagnation (noop rate), stagnation (low-value rate)
- Default: graph fallback on normal conditions
- Precedence: conservative over direct, conservative over exploratory

**ScoreMode (4 tests)**:
- Nil memory defaults, with memory record, clamp to zero, max score = 0.8

**ApplyInertia (4 tests)**:
- No last mode, boost when gap small, no boost when gap large, no boost when already best

**SelectModeWithScoring (4 tests)**:
- Hard rule overrides scoring, default graph with no memory, scoring override when significant, inertia prevents mode switch

**Determinism & Validation (4 tests)**:
- DecisionMode.IsValid, SelectMode deterministic (100 iterations), ScoreAllModes deterministic, fallback to graph

---

## 7. Validation Checklist

| Requirement | Status |
|-------------|--------|
| 4 modes defined (graph, direct, conservative, exploratory) | ✅ |
| Deterministic rule-based selection | ✅ |
| Priority order: conservative > direct > exploratory > graph | ✅ |
| Scoring overlay for default graph case | ✅ |
| Inertia anti-oscillation (boost=0.07, threshold=0.15) | ✅ |
| Mode affects graph config (depth, safe_mode, explore) | ✅ |
| Memory tracking (per mode+goal success rates) | ✅ |
| History tracking (every mode selection event) | ✅ |
| Outcome feedback loop (_ctx_meta_mode → outcome handler) | ✅ |
| Audit events emitted (meta_reasoning.evaluated, outcome) | ✅ |
| Fail-open on nil provider | ✅ |
| Fail-open on DB errors (adapter level) | ✅ |
| No breaking changes to existing tests | ✅ |
| All 28 new tests pass | ✅ |
| All existing test packages pass (except pre-existing calibration issue) | ✅ |
| API endpoints for observability (3 routes) | ✅ |
| Migration files (up + down) | ✅ |

---

## 8. Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Mode selection adds latency to every planning call | Selection is pure function + single DB read; <1ms overhead |
| Conservative mode could suppress useful actions | Only triggered by safe_mode or failure_rate ≥ 0.5 — both indicate genuine danger |
| Direct mode could miss graph-only insights | Requires high confidence (≥0.8), low risk (<0.2), AND sufficient path samples (≥5) |
| Mode oscillation | Inertia mechanism prevents switching when gap is small |
| Memory corruption | UPSERT with ON CONFLICT ensures atomicity; fail-open on errors |

---

## 9. Pipeline Position

```
Iteration 20: Decision Graph (graph-based evaluation)
Iteration 21: Path Learning (path-level feedback)
Iteration 22: Comparative Learning (snapshot-based win/loss)
Iteration 23: Counterfactual Simulation (what-if analysis)
Iteration 24: Meta-Reasoning (mode selection before evaluation) ← THIS
```

The meta-reasoning layer sits **before** the decision graph pipeline, configuring how the graph should operate. This is the first layer that controls the reasoning process itself rather than the reasoning content.

---

## 10. Next Steps

Potential Iteration 25 directions:
- **Meta-Learning**: Learn which mode selection rules work best and adjust thresholds dynamically
- **Mode Composition**: Allow combining modes (e.g., conservative + exploratory for safe exploration)
- **Temporal Mode Patterns**: Detect time-of-day or cycle-count patterns in mode effectiveness
