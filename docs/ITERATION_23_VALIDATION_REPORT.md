# Iteration 23 — Counterfactual Simulation Layer: Validation Report

## 1. Overview

Iteration 23 introduces a counterfactual simulation layer to the Arcanum decision graph. Before each decision, the system now simulates "what would likely happen if I chose path X?" for up to 3 alternative paths, producing expected value predictions from existing signal sources. After execution, it compares predictions against actual outcomes to learn prediction accuracy over time.

**Core concept**: Move from reactive learning ("what happened?") to predictive evaluation ("what would happen?"). The system synthesizes path learning, transition learning, and comparative learning signals into per-path predictions, applies bounded score adjustments, and tracks prediction accuracy for self-calibration.

**Key constraints**: NO real execution of alternatives, NO randomness, NO ML models, DETERMINISTIC from same inputs, FAIL-OPEN on all errors, MAX 3 simulated paths, bounded influence (±20% weight).

---

## 2. Architecture

### Data Flow

```
EvaluateForPlanner()
  ├── EvaluateAllPaths → ApplyPathLearningAdjustments
  ├── ApplyComparativeLearningAdjustments (Iteration 22)
  ├── ApplyCounterfactualAdjustments (NEW — adjusts based on simulated predictions)
  │      ├── SimulateAndSave: gather signals → simulate top-K paths → save simulation
  │      └── AdjustedScore = OriginalScore + (PredictedValue - OriginalScore) × 0.20
  ├── SelectBestPath
  └── CaptureAndSave decision snapshot (Iteration 22)

Action Executed → Outcome Handler
  ├── evaluatePathOutcome (Iteration 21)
  ├── evaluateComparativeOutcome (Iteration 22)
  └── evaluateCounterfactualPrediction (NEW)
         ├── Retrieve simulation by DecisionID
         ├── Find prediction for executed path
         ├── Compute absolute error + direction correctness
         ├── Save prediction outcome
         ├── Update prediction memory (UPSERT avg error + direction accuracy)
         └── Emit audit events
```

### Integration Points

1. **Decision Graph Evaluator** — `ApplyCounterfactualAdjustments` applied between `ApplyComparativeLearningAdjustments` and `SelectBestPath`
2. **Decision Graph Planner Adapter** — `CounterfactualSimulator.SimulateAndSave` called after comparative adjustments with path scores, lengths, and decision ID
3. **Outcome Handler** — `evaluateCounterfactualPrediction` called after comparative evaluation to assess prediction accuracy

---

## 3. New Package: `internal/agent/counterfactual/`

### Files

| File | Purpose |
|------|---------|
| `types.go` | Core types: `PathPrediction`, `SimulationResult`, `PredictionOutcome`, `PredictionMemoryRecord`, `SimulationSignals`; constants: `MaxSimulatedPaths=3`, `PredictionWeight=0.20`, signal weights (0.30/0.30/0.20/0.20) |
| `simulator.go` | Deterministic simulation engine: `SimulateTopKPaths()`, `AdjustScoresWithPredictions()`, `selectTopK()` with insertion sort |
| `predictor.go` | Post-outcome evaluation: `Predictor.EvaluatePrediction()` compares prediction vs actual, updates memory |
| `memory.go` | Database stores: `SimulationStore`, `PredictionOutcomeStore`, `PredictionMemoryStore` with PostgreSQL UPSERT |
| `adapter.go` | Bridge: `GraphAdapter` implements `decision_graph.CounterfactualSimulator` via `SimulateAndSave()` |
| `counterfactual_test.go` | 45 unit tests covering all pure functions |

### Simulation Pipeline

```
SimulateTopKPaths(scores, lengths, signals)
  ├── selectTopK: deterministic top-3 by score DESC, then alphabetical tie-break
  ├── For each selected path:
  │     ├── computeExpectedValue: weighted blend of 4 signal sources
  │     │     ├── PathFeedback (0.30): recommendationToValue → prefer=0.80, avoid=0.20, neutral=0.50
  │     │     ├── TransitionFeedback (0.30): aggregateTransitionSignal across path edges
  │     │     ├── ComparativeFeedback (0.20): classifyComparativeFeedback from win/loss rates
  │     │     └── BaseScore (0.20): original decision graph score
  │     ├── computeExpectedRisk: historical failure rate + length penalty (0.02 per extra node)
  │     └── computeConfidence: weighted from signal source coverage
  └── Return []PathPrediction (deterministic, bounded)
```

### Score Adjustment Formula

```
AdjustedScore = OriginalScore + (PredictedValue - OriginalScore) × PredictionWeight
```

Where `PredictionWeight = 0.20`. Only applied when prediction confidence ≥ `counterfactualMinConfidence` (0.01).

---

## 4. Integration Changes

### `decision_graph/evaluator.go`

- Added `CounterfactualPredictions` struct with `Predictions` and `Confidences` maps
- Added `ApplyCounterfactualAdjustments()` function: iterates paths, looks up prediction by first node action type as path signature, applies additive adjustment clamped to [0,1]
- Constants: `counterfactualPredictionWeight = 0.20`, `counterfactualMinConfidence = 0.01`

### `decision_graph/planner_adapter.go`

- Added `CounterfactualSimulator` interface with `SimulateAndSave()` method
- Added `CounterfactualPredictionExport` type with `Predictions` + `Confidences` maps
- Added `WithCounterfactual()` setter on `GraphPlannerAdapter`
- Modified `EvaluateForPlanner()`: after comparative adjustments, builds pathScores/pathLengths maps, calls `SimulateAndSave`, applies `ApplyCounterfactualAdjustments`

### `outcome/handler.go`

- Added `CounterfactualPredictionEvaluator` interface
- Added `WithCounterfactualEvaluator()` setter
- Added `evaluateCounterfactualPrediction()` method: extracts `_ctx_decision_id` and `_ctx_path_signature` from action params
- Called in `HandleOutcome()` after comparative evaluation

### `path_comparison/adapter.go`

- Added `GetAllComparativeWinRates()` method returning `map[string]float64`
- Added `GetAllComparativeLossRates()` method returning `map[string]float64`

### `api/handlers.go`

- Added 3 handler functions: `CounterfactualPredictionsList`, `CounterfactualMemoryList`, `CounterfactualErrorsList`
- Added `WithCounterfactual()` method accepting simulation, outcome, and memory stores

### `api/router.go`

- Added 3 routes:
  - `GET /api/v1/agent/counterfactual/predictions` — list simulations
  - `GET /api/v1/agent/counterfactual/memory` — list prediction memory
  - `GET /api/v1/agent/counterfactual/errors` — list prediction outcomes

### `cmd/api-gateway/main.go`

- Created 3 counterfactual stores
- Created `GraphAdapter` with path learning + comparative learning signal sources
- Wired `graphAdapter.WithCounterfactual(cfAdapter)`
- Created `Predictor` and wired `outcomeHandler.WithCounterfactualEvaluator(cfPredictor)`
- Added `.WithCounterfactual()` to handlers chain

---

## 5. Database Migration

Migration `000028_create_agent_counterfactual`:

```sql
-- Simulations: stores pre-decision predictions
agent_counterfactual_simulations (
    id SERIAL PRIMARY KEY,
    decision_id TEXT NOT NULL UNIQUE,
    goal_type TEXT NOT NULL DEFAULT '',
    predictions JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)

-- Prediction outcomes: post-execution accuracy records
agent_counterfactual_prediction_outcomes (
    id SERIAL PRIMARY KEY,
    decision_id TEXT NOT NULL UNIQUE,
    path_signature TEXT NOT NULL DEFAULT '',
    goal_type TEXT NOT NULL DEFAULT '',
    predicted_value DOUBLE PRECISION NOT NULL DEFAULT 0,
    actual_value DOUBLE PRECISION NOT NULL DEFAULT 0,
    absolute_error DOUBLE PRECISION NOT NULL DEFAULT 0,
    direction_correct BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)

-- Prediction memory: aggregated accuracy per path+goal
agent_counterfactual_prediction_memory (
    id SERIAL PRIMARY KEY,
    path_signature TEXT NOT NULL DEFAULT '',
    goal_type TEXT NOT NULL DEFAULT '',
    total_predictions INT NOT NULL DEFAULT 0,
    avg_error DOUBLE PRECISION NOT NULL DEFAULT 0,
    direction_accuracy DOUBLE PRECISION NOT NULL DEFAULT 0,
    direction_correct_count INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(path_signature, goal_type)
)
```

---

## 6. Audit Events

| Event Type | Entity Type | When |
|------------|-------------|------|
| `counterfactual.simulated` | `counterfactual` | After simulation saved (predictions + signal summary) |
| `counterfactual.prediction_evaluated` | `counterfactual` | After prediction accuracy computed |
| `counterfactual.memory_updated` | `counterfactual` | After prediction memory UPSERT |

---

## 7. Test Results

### `internal/agent/counterfactual/` — 45 tests

| Test | Status |
|------|--------|
| `TestSimulateTopKPaths_NilSignals` | PASS |
| `TestSimulateTopKPaths_EmptyScores` | PASS |
| `TestSimulateTopKPaths_Deterministic` | PASS |
| `TestSimulateTopKPaths_BoundedToMaxK` | PASS |
| `TestSimulateTopKPaths_TopPathsSelected` | PASS |
| `TestSimulateTopKPaths_PreferPathIncreasesValue` | PASS |
| `TestSimulateTopKPaths_AvoidPathDecreasesValue` | PASS |
| `TestSimulateTopKPaths_PathLengthIncreasesRisk` | PASS |
| `TestSimulateTopKPaths_TransitionSignalContributes` | PASS |
| `TestAdjustScoresWithPredictions_NilPredictions` | PASS |
| `TestAdjustScoresWithPredictions_EmptyPredictions` | PASS |
| `TestAdjustScoresWithPredictions_HighPredictionIncreasesScore` | PASS |
| `TestAdjustScoresWithPredictions_LowPredictionDecreasesScore` | PASS |
| `TestAdjustScoresWithPredictions_BoundedInfluence` | PASS |
| `TestAdjustScoresWithPredictions_LowConfidenceIgnored` | PASS |
| `TestAdjustScoresWithPredictions_UnpredictedPathUnchanged` | PASS |
| `TestAdjustScoresWithPredictions_ClampedToZeroOne` | PASS |
| `TestSelectTopK_SortsDescending` | PASS |
| `TestSelectTopK_TieBreakAlphabetical` | PASS |
| `TestSelectTopK_LimitsToK` | PASS |
| `TestOutcomeToValue_Success` | PASS |
| `TestOutcomeToValue_Failure` | PASS |
| `TestOutcomeToValue_Neutral` | PASS |
| `TestOutcomeToValue_Unknown` | PASS |
| `TestRecommendationToValue_Prefer` | PASS |
| `TestRecommendationToValue_Avoid` | PASS |
| `TestRecommendationToValue_Underexplored` | PASS |
| `TestRecommendationToValue_Neutral` | PASS |
| `TestSplitPathSignature_Single` | PASS |
| `TestSplitPathSignature_Multi` | PASS |
| `TestSplitPathSignature_Empty` | PASS |
| `TestAggregateTransitionSignal_SingleTransition` | PASS |
| `TestAggregateTransitionSignal_NoTransitions` | PASS |
| `TestAggregateTransitionSignal_MixedTransitions` | PASS |
| `TestComputeConfidence_NoSignals` | PASS |
| `TestComputeConfidence_AllSignals` | PASS |
| `TestComputeConfidence_PartialSignals` | PASS |
| `TestClassifyComparativeFeedback_Underexplored` | PASS |
| `TestClassifyComparativeFeedback_Prefer` | PASS |
| `TestClassifyComparativeFeedback_Avoid` | PASS |
| `TestClassifyComparativeFeedback_Neutral_LowSamples` | PASS |
| `TestComputeExpectedRisk_HighFailureRate` | PASS |
| `TestComputeExpectedRisk_LongPath` | PASS |
| `TestClamp01_Below` | PASS |
| `TestClamp01_Above` | PASS |
| `TestClamp01_InRange` | PASS |

### `internal/agent/decision_graph/` — 55 tests (7 new)

| New Test | Status |
|----------|--------|
| `TestApplyCounterfactualAdjustments_Nil` | PASS |
| `TestApplyCounterfactualAdjustments_Empty` | PASS |
| `TestApplyCounterfactualAdjustments_HighPrediction` | PASS |
| `TestApplyCounterfactualAdjustments_LowPrediction` | PASS |
| `TestApplyCounterfactualAdjustments_LowConfidenceIgnored` | PASS |
| `TestApplyCounterfactualAdjustments_ClampedToZeroOne` | PASS |
| `TestApplyCounterfactualAdjustments_UnpredictedPathUnchanged` | PASS |

### `internal/agent/path_comparison/` — all tests PASS

### Build

- `go build ./cmd/api-gateway/` — clean (no errors)
- `go build ./internal/agent/counterfactual/` — clean
- `go build ./internal/agent/decision_graph/` — clean
- `go build ./internal/agent/outcome/` — clean
- `go build ./internal/agent/path_comparison/` — clean

---

## 8. Scoring Pipeline Order

After Iteration 23, the decision graph scoring pipeline is:

```
EvaluateAllPaths              (Iteration 20: base scores)
  → ApplyPathLearningAdjustments      (Iteration 21: path + transition signals)
  → ApplyComparativeLearningAdjustments (Iteration 22: comparative win/loss)
  → ApplyCounterfactualAdjustments     (Iteration 23: simulated predictions)  ← NEW
  → SelectBestPath                     (Iteration 20: final selection)
```

Each layer is additive and clamped to [0,1]. Each layer is fail-open — errors log warnings and return unmodified scores.

---

## 9. Constraints Verification

| Constraint | Status |
|------------|--------|
| No real execution of alternatives | ✅ Simulation uses only existing signals |
| No randomness | ✅ Deterministic insertion sort, no rand |
| No ML models | ✅ Pure weighted signal aggregation |
| Deterministic from same inputs | ✅ Verified by determinism test |
| Fail-open on all errors | ✅ All adapter/store calls log+skip on error |
| Max 3 simulated paths | ✅ `MaxSimulatedPaths = 3` enforced |
| Bounded influence ±20% | ✅ `PredictionWeight = 0.20` applied as delta |
| No breaking changes | ✅ All new code layered additively |
| No import cycles | ✅ Interfaces defined in consumer packages |
| Clamped to [0,1] | ✅ clamp01 applied after every adjustment |

---

## 10. Files Changed

### New Files (6)
- `internal/agent/counterfactual/types.go`
- `internal/agent/counterfactual/simulator.go`
- `internal/agent/counterfactual/predictor.go`
- `internal/agent/counterfactual/memory.go`
- `internal/agent/counterfactual/adapter.go`
- `internal/agent/counterfactual/counterfactual_test.go`
- `internal/db/migrations/000028_create_agent_counterfactual.up.sql`
- `internal/db/migrations/000028_create_agent_counterfactual.down.sql`

### Modified Files (6)
- `internal/agent/decision_graph/evaluator.go`
- `internal/agent/decision_graph/planner_adapter.go`
- `internal/agent/decision_graph/decision_graph_test.go`
- `internal/agent/outcome/handler.go`
- `internal/agent/path_comparison/adapter.go`
- `internal/api/handlers.go`
- `internal/api/router.go`
- `cmd/api-gateway/main.go`
