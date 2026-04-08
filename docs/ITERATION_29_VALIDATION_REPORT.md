# Iteration 29 Validation Report

## Resource / Cost / Latency-Aware Optimization Layer

**Date:** 2025-07-21  
**Status:** ✅ COMPLETE  
**Package:** `internal/agent/resource_optimization/`

---

## 1. Architecture

Iteration 29 adds a resource-aware optimization layer that tracks the cost of the agent's own cognition and execution, then feeds bounded penalty signals back into the decision graph pipeline.

### Package Structure

```
internal/agent/resource_optimization/
├── types.go       — Core types, constants, thresholds
├── tracker.go     — Database persistence + rolling averages
├── scorer.go      — Penalty computation + mode adjustment
├── adapter.go     — Bridges to decision_graph + outcome handler
└── resource_optimization_test.go — 36 tests
```

### Integration Points

| Integration Point | File | Position |
|---|---|---|
| Path penalty application | `decision_graph/planner_adapter.go` | After arbitration, before `SelectBestPath` |
| Resource outcome recording | `decision_graph/planner_adapter.go` | After path selection, before return |
| Outcome metric extraction | `outcome/handler.go` | After existing outcome processing |
| API endpoints (3) | `api/router.go` + `api/handlers.go` | New routes under `/api/v1/agent/resource/` |
| Component wiring | `cmd/api-gateway/main.go` | After mode calibration block |

### Adapter Pattern

All integrations use interface-based adapters defined in the consuming packages:

- `decision_graph.ResourceOptimizationProvider` — consumed by `GraphPlannerAdapter`
- `outcome.ResourceOutcomeRecorder` — consumed by `Handler`

Both are nil-safe and fail-open. If the resource optimization layer is absent or errors, the pipeline continues without adjustment.

---

## 2. Resource Model

### Resource Profiles

Each `(mode, goal_type)` pair accumulates a `ResourceProfile` via rolling averages:

| Field | Description |
|---|---|
| `avg_latency_ms` | Average execution latency in milliseconds |
| `avg_reasoning_depth` | Average number of reasoning steps |
| `avg_path_length` | Average path length (decision graph nodes) |
| `avg_token_cost` | Average token cost (prompt + completion) |
| `avg_execution_cost` | Average total execution cost |
| `sample_count` | Number of observations recorded |

Rolling average formula:
```
new_avg = (old_avg * old_count + new_value) / (old_count + 1)
```

Implemented via PostgreSQL UPSERT with atomic increment. No data loss on concurrent writes.

### Minimum Sample Gate

Signals are only emitted when `sample_count >= MinSamplesForSignals (3)`. Below this threshold, all penalties return zero (fail-open).

---

## 3. Penalty Formulas

### Latency Penalty

```
base = clamp((avg_latency_ms - 500) / (5000 - 500), 0, 1)
depth_amplifier = clamp(avg_reasoning_depth / 5.0, 0, 1) * 0.2
path_amplifier = clamp(avg_path_length / 5.0, 0, 1) * 0.1
LatencyPenalty = clamp(base + depth_amplifier + path_amplifier, 0, 1)
```

- Linear interpolation between `LatencyThresholdMs=500` and `HighLatencyMs=5000`
- Amplified by reasoning depth and path length (bounded contributions)

### Cost Penalty

```
token_penalty = clamp((avg_token_cost - 1.0) / (10.0 - 1.0), 0, 1)
execution_penalty = clamp((avg_execution_cost - 1.0) / (10.0 - 1.0), 0, 1)
CostPenalty = max(token_penalty, execution_penalty)
```

- Takes the worse of token cost and execution cost
- Linear interpolation between `CostThreshold=1.0` and `HighCost=10.0`

### Depth Penalty

```
DepthPenalty = clamp((avg_reasoning_depth - 1.0) / (5.0 - 1.0), 0, 1)
```

- Linear between `DepthThreshold=1.0` and `HighDepth=5.0`

### Efficiency Score

```
EfficiencyScore = clamp(1 - (LatencyPenalty*0.4 + CostPenalty*0.4 + DepthPenalty*0.2), 0, 1)
```

- Weighted composite: latency 40%, cost 40%, depth 20%
- Higher is better (1.0 = fully efficient, 0.0 = fully penalized)

### Path Resource Penalty

```
PathResourcePenalty = (1 - EfficiencyScore) * ResourcePenaltyWeight * pathLengthFactor
```

Where `pathLengthFactor = clamp(pathLength / 3.0, 0, 1)`.  
`ResourcePenaltyWeight = 0.10` — maximum influence bounded at 10%.

Applied as: `FinalScore = FinalScore - PathResourcePenalty`, clamped to [0, 1].

**Protected paths:** Single-step paths (length ≤ 1) and safe_mode paths receive zero penalty.

---

## 4. Mode-Aware Optimization

### Mode Complexity Weights

| Mode | Weight | Rationale |
|---|---|---|
| `safe_mode` | 0.5 | Conservative, low resource usage expected |
| `conservative` | 0.7 | Moderate resource usage |
| `balanced` | 1.0 | Baseline |
| `exploratory` | 1.3 | Higher resource usage acceptable |
| `aggressive` | 1.5 | Highest resource tolerance |

### Mode Adjustment Rules

| Condition | Adjustment | Description |
|---|---|---|
| `confidence ≥ 0.8` AND `efficiency ≥ 0.7` | `+ModeDirectBoost (0.05)` | Reward efficient high-confidence direct execution |
| `efficiency < 0.5` AND `successRate < 0.6` | `-ModeGraphPenalty (0.08)` | Penalize expensive failing graph traversals |
| Mode = `conservative` or `safe_mode` | `0.0` (never adjusted) | Protected modes never receive resource-based adjustments |

All adjustments are mode-complexity-weighted: `adjustment * ModeComplexityWeight(mode)`.

---

## 5. Integration Pipeline Position

The resource optimization layer integrates at two points in the decision pipeline:

### During Path Evaluation (planner_adapter.go)

```
EvaluateForPlanner
  ├── Record decision start time
  ├── Build graph
  ├── Enumerate paths
  ├── Evaluate all paths
  ├── Apply path learning adjustments
  ├── Apply comparative learning adjustments
  ├── Apply counterfactual adjustments
  ├── Apply calibration
  ├── Apply contextual calibration
  ├── Apply arbitrated signals
  ├── ★ Apply resource path penalties ← NEW (Iteration 29)
  ├── Select best path
  ├── ★ Record resource outcome     ← NEW (Iteration 29)
  └── Return strategy override
```

### During Outcome Processing (outcome/handler.go)

```
HandleOutcome
  ├── ... existing outcome processing ...
  ├── ★ Record resource outcome ← NEW (Iteration 29)
  └── Return
```

Extracts `_ctx_meta_mode`, `_ctx_goal_type`, `_ctx_path_length` from action params.

---

## 6. Example: Decision Before/After Resource Optimization

### Before (no resource awareness)

| Path | FinalScore (post-arbitration) | Length |
|---|---|---|
| analyze→synthesize→validate | 0.85 | 3 |
| analyze→summarize | 0.82 | 2 |

Selected: `analyze→synthesize→validate` (highest score)

### After (with resource profiles showing high latency/cost)

Profile for this mode+goal: `avg_latency_ms=3000, avg_token_cost=6.0, avg_reasoning_depth=3.0`

Computed signals:
- `LatencyPenalty = 0.556 + depth_amp(0.12) + path_amp(0.06) = 0.736`
- `CostPenalty = 0.556`
- `DepthPenalty = 0.500`
- `EfficiencyScore = clamp(1 - (0.736*0.4 + 0.556*0.4 + 0.500*0.2)) = 0.384`

Path penalties:
- 3-node path: `(1 - 0.384) * 0.10 * clamp(3/3, 0, 1) = 0.062`
- 2-node path: `(1 - 0.384) * 0.10 * clamp(2/3, 0, 1) = 0.041`

| Path | Pre-Resource Score | Penalty | Post-Resource Score |
|---|---|---|---|
| analyze→synthesize→validate | 0.85 | -0.062 | 0.788 |
| analyze→summarize | 0.82 | -0.041 | 0.779 |

Selected: `analyze→synthesize→validate` still wins (penalty is bounded), but the gap narrowed from 0.030 to 0.009. Under slightly different conditions, the shorter path could be preferred.

**Key insight:** Resource penalties are deliberately small (max 10%) to influence selection only at the margins, not override primary learning signals.

---

## 7. API Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/agent/resource/profiles` | All resource profiles |
| `GET` | `/api/v1/agent/resource/summary` | Aggregated summary + pressure status |
| `GET` | `/api/v1/agent/resource/decisions` | Recent resource decisions (FIFO, max 100) |

All endpoints return JSON. No query parameters required.

---

## 8. Audit Events

| Event Type | Entity Type | When |
|---|---|---|
| `resource.profile_updated` | `resource_optimization` | After recording an outcome |
| `resource.adjustment_applied` | `resource_optimization` | After applying mode/path adjustments |
| `resource.pressure_detected` | `resource_optimization` | When resource pressure is detected |

Payloads include all computed signals, penalties, and the mode/goal_type context.

---

## 9. Database Migration

**Migration 000032:** `agent_resource_profiles` table

```sql
CREATE TABLE IF NOT EXISTS agent_resource_profiles (
    id              SERIAL PRIMARY KEY,
    mode            TEXT NOT NULL,
    goal_type       TEXT NOT NULL,
    avg_latency_ms       DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_reasoning_depth  DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_path_length      DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_token_cost       DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_execution_cost   DOUBLE PRECISION NOT NULL DEFAULT 0,
    sample_count         INTEGER NOT NULL DEFAULT 0,
    last_updated    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_resource_profiles_mode_goal ON agent_resource_profiles(mode, goal_type);
```

Rolling average UPSERT ensures atomic, lock-free updates.

---

## 10. Tests

### Test Summary

**36 tests, 0 failures, 0.006s runtime**

### Test Categories

| # | Category | Tests | Status |
|---|---|---|---|
| 1 | Rolling average correctness | 2 | ✅ |
| 2 | Penalty monotonicity (latency) | 1 | ✅ |
| 3 | Penalty monotonicity (cost) | 1 | ✅ |
| 4 | Efficiency bounded + deterministic | 2 | ✅ |
| 5 | Direct mode boost conditions | 1 | ✅ |
| 6 | Graph mode penalty conditions | 1 | ✅ |
| 7 | Conservative/safe_mode protection | 6 | ✅ |
| 8 | Nil/missing profile fail-open | 1 | ✅ |
| 9 | Path resource penalties | 4 | ✅ |
| 10 | 100-iteration determinism | 1 | ✅ |
| — | Edge cases (zero costs, spikes, etc.) | 10 | ✅ |
| — | Pressure detection + FIFO | 3 | ✅ |
| — | Mode complexity weights | 3 | ✅ |

### Regression Tests

| Package | Tests | Status |
|---|---|---|
| `resource_optimization` | 36 | ✅ PASS |
| `decision_graph` | 29 | ✅ PASS |
| Full project `go build ./...` | — | ✅ Compiles cleanly |

---

## 11. Validation Results

### Hard Constraints

| Constraint | Status | Evidence |
|---|---|---|
| No randomness | ✅ | No `rand` import; `TestDeterminism_100Iterations` confirms identical output |
| Deterministic | ✅ | All computations are pure functions of input; no time-dependent logic |
| Fail-open | ✅ | Nil adapters, nil profiles, insufficient samples all return zero adjustment |
| No breaking changes | ✅ | All integration via `With*` optional methods; zero decision_graph test regressions |
| Bounded influence | ✅ | `ResourcePenaltyWeight=0.10` caps penalty at 10%; mode adjustments ≤ 0.08 |
| Explicit formulas | ✅ | All formulas documented in code comments and this report |

### Behavioral Properties

| Property | Status | Evidence |
|---|---|---|
| Expensive paths penalized more | ✅ | Longer paths get higher `pathLengthFactor` |
| Cheap efficient paths rewarded | ✅ | `TestModeAdjustment_ExploratoryNoPenaltyWhenCheap` |
| Safe_mode never adjusted | ✅ | `TestModeAdjustment_SafeModeNeverAdjusted`, `TestPathPenalty_SafeModeZero` |
| Conservative never adjusted | ✅ | `TestModeAdjustment_ConservativeNeverAdjusted` |
| Single-step paths unpenalized | ✅ | `TestPathPenalty_SingleStepZero` |
| Rolling averages smooth spikes | ✅ | `TestEdgeCase_ExtremeSpike` |
| Pressure detection works | ✅ | `TestSummary_PressureDetection` |

---

## 12. Risks

| Risk | Mitigation |
|---|---|
| Penalty weight too low to matter | `ResourcePenaltyWeight=0.10` is intentionally conservative; can be tuned after observation |
| Cold start (no profiles) | `MinSamplesForSignals=3` gate ensures no adjustments until data exists |
| Mode complexity weight too aggressive for `aggressive` (1.5x) | All adjustments still bounded by `ResourcePenaltyWeight`; 1.5× of 0.10 = 0.15 max |
| Rolling averages slow to adapt | Deliberate design for stability; extreme outliers are dampened |

---

## 13. Next Step Recommendation

**Iteration 30** (Governance) is partially present. The natural next evolution would be:

- **Resource budgets**: Per-goal-type cost budgets with alerts when exceeded
- **Adaptive thresholds**: Use calibration layer (Iteration 25/26) to tune `LatencyThresholdMs` and `CostThreshold` dynamically
- **Resource arbitration signals**: Feed resource pressure as a signal into the arbitration layer (Iteration 27)
- **Dashboard integration**: Surface resource profiles and pressure status in the admin UI

---

## Conclusion

The system is now capable of choosing **not only what is likely correct, but what is operationally justified given latency, cost, and reasoning depth**.

Resource awareness is integrated as a bounded, deterministic, fail-open signal in the decision pipeline — consistent with Arcanum's architecture principles of explicit state, observable behavior, and no silent side effects.
