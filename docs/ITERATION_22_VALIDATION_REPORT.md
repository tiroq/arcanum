# Iteration 22 — Comparative Path Selection Learning: Validation Report

## 1. Overview

Iteration 22 introduces comparative path selection learning to the Arcanum decision graph. The system now captures full decision snapshots (all scored candidate paths at the moment of selection), then evaluates retroactively whether the selection decision was correct by comparing the chosen path's outcome against the alternatives that were available.

**Core concept**: Move from "did this path work?" (Iteration 21) to "was the decision to choose this path over others correct?" The system learns ranking errors (overestimation/underestimation), detects missed opportunities (better alternatives existed), and adjusts future scoring based on comparative win/loss rates.

**Key constraints**: NO execution change, NO graph expansion, NO stochastic logic, FAIL-OPEN on all errors, NO breaking changes to existing behavior.

---

## 2. Architecture

### Data Flow

```
EvaluateForPlanner()
  ├── EvaluateAllPaths → ApplyPathLearningAdjustments
  ├── ApplyComparativeLearningAdjustments (NEW — adjusts based on historical comparative feedback)
  ├── SelectBestPath
  └── CaptureAndSave decision snapshot (NEW — records all candidates + scores + selected path)

Action Executed → Outcome Handler
  ├── evaluatePathOutcome (Iteration 21)
  └── evaluateComparativeOutcome (NEW)
         ├── Retrieve decision snapshot by DecisionID
         ├── ClassifyRankingError (overestimated/underestimated/none)
         ├── DetectBetterAlternative (close-scoring alt existed + failure)
         ├── ClassifyWinLoss (selection decision quality)
         ├── Update comparative memory (win/loss rates per path+goal)
         ├── Record missed wins for alternatives
         └── Emit audit events
```

### Integration Points

1. **Decision Graph** — `ApplyComparativeLearningAdjustments` applied between `ApplyPathLearningAdjustments` and `SelectBestPath`
2. **Decision Graph** — `SnapshotCapturer.CaptureAndSave` called after path selection to record all candidates
3. **Outcome Handler** — `evaluateComparativeOutcome` called after `evaluatePathOutcome` to evaluate decision quality
4. **Planner** — `StrategyOverride.DecisionID` links decisions to snapshots via `_ctx_decision_id` param
5. **API** — 3 new endpoints for observability

---

## 3. Files Created

| File | Purpose |
|------|---------|
| `internal/db/migrations/000027_create_agent_path_comparison.up.sql` | Creates `agent_path_decision_snapshots`, `agent_path_comparative_outcomes`, `agent_path_comparative_memory` tables |
| `internal/db/migrations/000027_create_agent_path_comparison.down.sql` | Rollback migration |
| `internal/agent/path_comparison/types.go` | Core types, constants, pure functions (GenerateComparativeFeedback, ClassifyRankingError, DetectBetterAlternative, ClassifyWinLoss) |
| `internal/agent/path_comparison/snapshot.go` | SnapshotStore — decision snapshot persistence; CaptureSnapshot pure function |
| `internal/agent/path_comparison/evaluator.go` | Evaluator — evaluates comparative outcomes, updates memory, records missed wins, emits audit events |
| `internal/agent/path_comparison/memory.go` | OutcomeStore + MemoryStore — comparative outcome and memory persistence with UPSERT |
| `internal/agent/path_comparison/adapter.go` | GraphAdapter (implements ComparativeLearningProvider) + SnapshotCapturerAdapter (implements SnapshotCapturer) |
| `internal/agent/path_comparison/path_comparison_test.go` | 34 unit tests covering all pure functions, feedback generation, thresholds, edge cases |

---

## 4. Files Modified

| File | Changes |
|------|---------|
| `internal/agent/decision_graph/evaluator.go` | Added `ComparativeLearningSignals`, `ApplyComparativeLearningAdjustments`, comparative adjustment constants |
| `internal/agent/decision_graph/planner_adapter.go` | Added `ComparativeLearningProvider` interface, `SnapshotCapturer` interface, `ScoredPathExport` type, `WithComparativeLearning`, `WithSnapshotCapturer`, snapshot capture + comparative adjustments in `EvaluateForPlanner` |
| `internal/agent/decision_graph/decision_graph_test.go` | Added 12 tests for `ApplyComparativeLearningAdjustments` |
| `internal/agent/planning/planner.go` | Added `DecisionID` to `StrategyOverride`; planner tags `_ctx_decision_id` on action params |
| `internal/agent/outcome/handler.go` | Added `ComparativeEvaluator` interface, `WithComparativeEvaluator`, `evaluateComparativeOutcome` |
| `internal/api/handlers.go` | Added `PathSnapshotsList`, `PathComparativeList`, `PathComparativeMemoryList` handlers; `WithPathComparison` method |
| `internal/api/router.go` | Added 3 routes under `/api/v1/agent/` |
| `cmd/api-gateway/main.go` | Wired comparative stores, adapters, evaluator, snapshot capturer |

---

## 5. Data Model

### agent_path_decision_snapshots
| Column | Type | Description |
|--------|------|-------------|
| id | SERIAL PK | Auto-increment |
| decision_id | TEXT UNIQUE | Links to StrategyOverride.StrategyID (= DecisionID) |
| goal_type | TEXT | Goal family context |
| selected_path_signature | TEXT | Path that was chosen |
| selected_score | DOUBLE | Score of selected path at decision time |
| candidates | JSONB | All candidate paths with scores and ranks |
| candidate_count | INT | Number of candidates considered |
| score_spread | DOUBLE | Max score - min score |
| created_at | TIMESTAMPTZ | Timestamp |

**Unique constraint**: `(decision_id)`

### agent_path_comparative_outcomes
| Column | Type | Description |
|--------|------|-------------|
| id | SERIAL PK | Auto-increment |
| decision_id | TEXT UNIQUE | Links back to snapshot |
| goal_type | TEXT | Goal family |
| selected_path_signature | TEXT | Path that was chosen |
| selected_outcome | TEXT | success/failure/neutral |
| ranking_error | TEXT | overestimated/underestimated/none |
| better_alternative_existed | BOOLEAN | Close-scoring alternative existed during failure |
| win | BOOLEAN | Selection decision was correct |
| loss | BOOLEAN | Selection decision was wrong |
| created_at | TIMESTAMPTZ | Timestamp |

**Unique constraint**: `(decision_id)`

### agent_path_comparative_memory
| Column | Type | Description |
|--------|------|-------------|
| id | SERIAL PK | Auto-increment |
| path_signature | TEXT | Path being tracked |
| goal_type | TEXT | Goal family context |
| selection_count | INT | Times this path was selected |
| win_count | INT | Correct selection decisions |
| loss_count | INT | Wrong selection decisions |
| win_rate | DOUBLE | win_count / selection_count |
| loss_rate | DOUBLE | loss_count / selection_count |
| missed_win_count | INT | Times this path was NOT selected but would have been better |
| last_selected_at | TIMESTAMPTZ | Most recent selection |
| created_at / updated_at | TIMESTAMPTZ | Timestamps |

**Unique constraint**: `(path_signature, goal_type)`

---

## 6. Score Adjustment Model

### Comparative Path-Level Adjustments

| Feedback | Condition | Score Δ |
|----------|-----------|---------|
| `prefer_path` | win_rate ≥ 0.7, min 5 selections | +0.10 |
| `avoid_path` | loss_rate ≥ 0.5, min 5 selections | -0.20 |
| `underexplored_path` | missed_win_count ≥ 3, min 5 selections | +0.05 |
| `neutral` | None of the above | +0.00 |

**Priority**: `underexplored_path` takes precedence over `prefer_path` and `avoid_path` when its condition is met.

### Application Order

1. `EvaluateAllPaths` computes base FinalScore per path
2. `ApplyPathLearningAdjustments` adds path-level + transition deltas (Iteration 21)
3. `ApplyComparativeLearningAdjustments` adds comparative decision quality deltas (Iteration 22)
4. Result clamped to [0, 1] at each step
5. `SelectBestPath` picks the highest adjusted score

### Ranking Error Classification

| Classification | Condition |
|---------------|-----------|
| `overestimated` | Selected path scored ≥ 0.5 but outcome was failure |
| `underestimated` | Selected path scored < 0.3 but outcome was success |
| `none` | Neither condition applies |

### Better Alternative Detection

A "better alternative" exists when:
- Selected path outcome is NOT success
- At least one non-selected candidate scored within `AlternativeScoreThreshold` (0.1) of the selected path

### Win/Loss Classification

| Result | Condition |
|--------|-----------|
| Win | outcome = success AND no better alternative |
| Loss | outcome = failure OR (outcome ≠ success AND better alternative exists) |
| Neutral | outcome = success but better alternative exists, OR outcome = neutral without better alternative |

---

## 7. Audit Events

| Event Type | Entity Type | When |
|------------|-------------|------|
| `comparative.snapshot_captured` | `path_comparison` | Decision snapshot persisted |
| `comparative.outcome_evaluated` | `path_comparison` | Comparative outcome evaluation completed |
| `comparative.missed_win_recorded` | `path_comparison` | Non-selected alternative could have been better |

---

## 8. API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/agent/path-snapshots` | List decision snapshots |
| GET | `/api/v1/agent/path-comparative` | List comparative outcomes |
| GET | `/api/v1/agent/path-comparative-memory` | List comparative memory records |

All endpoints support optional `?goal_type=` query parameter for filtering.

---

## 9. Test Results

### Test Counts

| Package | Tests | New in Iteration 22 |
|---------|-------|---------------------|
| `path_comparison` | 34 | 34 (new package) |
| `decision_graph` | 48 | 12 (comparative adjustments) |
| **Total** | **82** | **46** |

### Full Test Suite

```
ok  github.com/tiroq/arcanum/internal/agent/actionmemory    0.005s
ok  github.com/tiroq/arcanum/internal/agent/actions          0.009s
ok  github.com/tiroq/arcanum/internal/agent/causal           0.013s
ok  github.com/tiroq/arcanum/internal/agent/decision_graph   0.013s
ok  github.com/tiroq/arcanum/internal/agent/exploration      0.006s
ok  github.com/tiroq/arcanum/internal/agent/goals            0.004s
ok  github.com/tiroq/arcanum/internal/agent/outcome          0.004s
ok  github.com/tiroq/arcanum/internal/agent/path_comparison  0.004s
ok  github.com/tiroq/arcanum/internal/agent/path_learning    0.007s
ok  github.com/tiroq/arcanum/internal/agent/planning         0.006s
ok  github.com/tiroq/arcanum/internal/agent/policy           0.006s
ok  github.com/tiroq/arcanum/internal/agent/reflection       0.007s
ok  github.com/tiroq/arcanum/internal/agent/scheduler        1.303s
ok  github.com/tiroq/arcanum/internal/agent/stability        0.005s
ok  github.com/tiroq/arcanum/internal/agent/strategy         0.004s
ok  github.com/tiroq/arcanum/internal/agent/strategy_learning 0.005s
ok  github.com/tiroq/arcanum/internal/api                    0.010s
ok  github.com/tiroq/arcanum/internal/config                 0.010s
ok  github.com/tiroq/arcanum/internal/contracts              0.053s
ok  github.com/tiroq/arcanum/internal/control                0.061s
ok  github.com/tiroq/arcanum/internal/db/models              0.009s
ok  github.com/tiroq/arcanum/internal/jobs                   0.007s
ok  github.com/tiroq/arcanum/internal/processors             0.005s
ok  github.com/tiroq/arcanum/internal/prompts                0.004s
ok  github.com/tiroq/arcanum/internal/providers              0.011s
ok  github.com/tiroq/arcanum/internal/providers/execution    0.008s
ok  github.com/tiroq/arcanum/internal/providers/profile      0.003s
ok  github.com/tiroq/arcanum/internal/providers/routing      0.005s
ok  github.com/tiroq/arcanum/internal/source                 0.004s
ok  github.com/tiroq/arcanum/internal/worker                 0.004s
```

**All 28 test packages pass. 0 failures.**

---

## 10. Design Decisions

1. **DecisionID = StrategyID**: The UUID generated for `StrategyOverride.StrategyID` serves as the `DecisionID`, flowing through `_ctx_decision_id` action param to the outcome handler.

2. **Fail-open everywhere**: If comparative learning stores fail, the system logs warnings and continues. `GetAllComparativeFeedbackMap` returns empty map on error. `evaluateComparativeOutcome` logs and returns nil on error.

3. **Interface-based decoupling**: `ComparativeLearningProvider` and `SnapshotCapturer` interfaces are defined in the `decision_graph` package. Implementations in `path_comparison` avoid import cycles.

4. **Underexplored priority**: When a path has high `missed_win_count`, the `underexplored_path` recommendation takes priority over `prefer_path`/`avoid_path`, encouraging the system to revisit paths it may have unfairly dismissed.

5. **Success overrides loss**: A successful outcome is never classified as a loss, even if a better alternative existed. This prevents punishing correct selections.

6. **Floating-point threshold tolerance**: `DetectBetterAlternative` uses a 1e-9 epsilon in threshold comparison to avoid IEEE 754 edge cases (e.g., `0.5 - 0.4 ≠ 0.1` in float64).

7. **Deterministic ordering**: Insertion sort used for candidate ranking (by score DESC, signature ASC) ensuring stable, reproducible snapshots.

8. **Additive layering**: Comparative adjustments layer additively on top of path learning adjustments, each clamped to [0, 1], preserving the existing scoring pipeline.

---

## 11. Breaking Changes

None. All changes are additive:
- New tables (migration 000027)
- New interfaces with optional wiring (`WithComparativeLearning`, `WithSnapshotCapturer`, `WithComparativeEvaluator`)
- Existing path learning and decision graph behavior unchanged when comparative components are nil
