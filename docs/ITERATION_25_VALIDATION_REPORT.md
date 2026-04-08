# Iteration 25 — Self-Calibration Layer — Validation Report

## 1. Architecture

The self-calibration layer lives in `internal/agent/calibration/` with 5 source files and 1 test file:

| File | Purpose |
|------|---------|
| `types.go` | Core types, constants, bucket utilities |
| `tracker.go` | Persistence — records + sliding window + summary UPSERT |
| `evaluator.go` | Pure computation — buckets, ECE, calibration scores |
| `calibrator.go` | Orchestrator — record outcomes, rebuild summaries, calibrate confidence |
| `adapter.go` | Integration adapters for decision graph, meta-reasoning, counterfactual, outcome |

Design principles:
- **Fail-open**: All adapters return raw/default values when calibrator is nil or errors occur.
- **Deterministic**: No randomness. Same inputs always produce same outputs.
- **Stateless computation**: `evaluator.go` contains only pure functions.
- **Sliding window**: Tracker enforces `MaxTrackerRecords = 500` for bounded memory.

## 2. Calibration Model

### Bucketing

5 equal-width buckets spanning [0.0, 1.0):

| Bucket | Range | Index |
|--------|-------|-------|
| 0 | [0.0, 0.2) | 0 |
| 1 | [0.2, 0.4) | 1 |
| 2 | [0.4, 0.6) | 2 |
| 3 | [0.6, 0.8) | 3 |
| 4 | [0.8, 1.0] | 4 |

Boundary precision handled via epsilon addition (`confidence/0.2 + 1e-9`) to avoid IEEE 754 truncation at exact boundaries (e.g., 0.6).

### Record Structure

```go
type CalibrationRecord struct {
    ID                  int
    DecisionID          string
    PredictedConfidence float64
    ActualOutcome       string    // "success" or other
    CreatedAt           time.Time
}
```

### Summary Structure

```go
type CalibrationSummary struct {
    Buckets              []CalibrationBucket
    ECE                  float64
    OverconfidenceScore  float64
    UnderconfidenceScore float64
    TotalRecords         int
    LastUpdated          time.Time
}
```

## 3. Error Computation

### Expected Calibration Error (ECE)

$$ECE = \sum_{b \in B} \frac{|B_b|}{N} \cdot |accuracy_b - avgConfidence_b|$$

Where:
- $B_b$ = records in bucket $b$ with count ≥ `MinBucketSamples` (3)
- $N$ = total records across qualifying buckets
- $accuracy_b$ = success rate in bucket
- $avgConfidence_b$ = mean predicted confidence in bucket

Buckets with fewer than `MinBucketSamples` records are excluded from ECE computation.

### Overconfidence / Underconfidence Scores

Weighted averages across qualifying buckets:
- **Overconfidence**: `max(0, avgConfidence - accuracy)` weighted by bucket proportion
- **Underconfidence**: `max(0, accuracy - avgConfidence)` weighted by bucket proportion

## 4. Confidence Correction

```go
adjustment = (accuracy - avgConfidence) * CalibrationWeight
calibrated = rawConfidence + adjustment
```

Where:
- `CalibrationWeight = 0.3` — conservative correction factor
- Lookup uses the bucket matching `rawConfidence`
- Result clamped to [0.0, 1.0]
- Returns raw confidence if: nil summary, empty buckets, or insufficient samples in bucket

## 5. Integration Points

### 5a. Decision Graph (`planner_adapter.go`)

Three injection points via `CalibrationProvider` interface:

1. **Signal confidence**: After enriching candidate signals, calibrates `sig.Confidence` before graph building
2. **Meta-reasoning confidence**: Before `SelectMode`, calibrates `avgConfidence` used for mode selection
3. **Counterfactual confidence**: After counterfactual predictions, calibrates each `cfPredictions.Confidences[sig]`

### 5b. Outcome Handler (`outcome/handler.go`)

Via `CalibrationRecorder` interface:
- After outcome evaluation, extracts `_ctx_decision_id` and `_ctx_predicted_confidence` from action params
- Calls `RecordCalibrationOutcome(ctx, decisionID, predictedConfidence, actualOutcome)`
- Fail-open: skips silently if recorder is nil or params are missing

### 5c. Planning (`planner.go`)

- `StrategyOverride` extended with `PredictedConfidence float64`
- Propagated as `_ctx_predicted_confidence` in resolved action params
- Enables outcome handler to retrieve the original prediction for calibration tracking

### 5d. Adapter Layer

| Adapter | Target Interface | Purpose |
|---------|-----------------|---------|
| `GraphAdapter` | `CalibrationProvider` | Confidence calibration for graph signals |
| `MetaReasoningAdapter` | Returns `(over, under)` scores | Calibration quality input to meta-reasoning |
| `CounterfactualAdapter` | Returns `quality float64` | Maps ECE → quality: `max(0, 1 - ECE*2)` |
| `OutcomeAdapter` | `CalibrationRecorder` | Records calibration data points on outcome |

## 6. Database Schema

Migration `000029_create_agent_calibration`:

```sql
-- Sliding window of calibration records
CREATE TABLE agent_calibration_records (
    id SERIAL PRIMARY KEY,
    decision_id TEXT NOT NULL,
    predicted_confidence DOUBLE PRECISION NOT NULL,
    actual_outcome TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_calibration_records_created ON agent_calibration_records(created_at DESC);

-- Single-row summary (UPSERT pattern)
CREATE TABLE agent_calibration_summary (
    id INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    ece DOUBLE PRECISION NOT NULL DEFAULT 0,
    overconfidence_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    underconfidence_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    total_records INT NOT NULL DEFAULT 0,
    last_updated TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

## 7. API Endpoints

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| GET | `/api/v1/agent/calibration/summary` | `CalibrationSummary` | Full summary with ECE, scores, total records |
| GET | `/api/v1/agent/calibration/buckets` | `CalibrationBuckets` | Per-bucket accuracy and confidence data |
| GET | `/api/v1/agent/calibration/errors` | `CalibrationErrors` | ECE + over/underconfidence scores |

All endpoints return 503 if calibration is not wired, 500 on database errors.

## 8. Audit Events

| Event | Trigger | Entity Type | Details |
|-------|---------|-------------|---------|
| `calibration.recorded` | New calibration record persisted | `calibration` | decision_id, predicted_confidence, actual_outcome |
| `calibration.updated` | Summary recomputed | `calibration` | ece, overconfidence_score, underconfidence_score, total_records |

## 9. Wiring (`cmd/api-gateway/main.go`)

```
calibrationTracker → calibrator → graphAdapter (via GraphAdapter)
                                → outcomeHandler (via OutcomeAdapter)
                                → handlers (via WithCalibration)
```

All wiring uses the established `With*` pattern. CalibrationProvider is optional — nil means no calibration applied.

## 10. Tests

20 unit tests covering all required scenarios:

| # | Test | Validates |
|---|------|-----------|
| 1 | `TestBucketIndex` (13 subtests) | Bucket assignment including boundaries, edge cases |
| 2 | `TestBuildBuckets_Accuracy` | Accuracy = successes / total per bucket |
| 3 | `TestBuildBuckets_EmptyBuckets` | Empty input produces 5 zeroed buckets |
| 4 | `TestComputeECE` | ECE formula with known inputs |
| 5 | `TestComputeECE_InsufficientSamples` | Buckets below MinBucketSamples excluded |
| 6 | `TestComputeCalibrationScores_Overconfident` | High confidence + low accuracy → overconfidence |
| 7 | `TestComputeCalibrationScores_Underconfident` | Low confidence + high accuracy → underconfidence |
| 8 | `TestCalibrateConfidenceFromSummary` | Correction applied within tolerance |
| 9 | `TestCalibrateConfidenceFromSummary_Clamped` | Result stays in [0, 1] |
| 10 | `TestCalibrateConfidenceFromSummary_NilSummary` | Nil summary → raw confidence |
| 11 | `TestCalibrateConfidenceFromSummary_EmptyBuckets` | Empty buckets → raw confidence |
| 12 | `TestCalibrateConfidenceFromSummary_InsufficientSamples` | Below MinBucketSamples → raw confidence |
| 13 | `TestBuildSummary_Integration` | Full pipeline: records → buckets → ECE → scores |
| 14 | `TestBuildSummary_Deterministic` | Same inputs produce identical outputs |
| 15 | `TestOutcomeIsSuccess` | Outcome classification |
| 16 | `TestApplyCalibrationToConfidence` | End-to-end calibration correction |
| 17 | `TestGraphAdapter_NilCalibrator` | Nil adapter returns raw confidence |
| 18 | `TestMetaReasoningAdapter_NilCalibrator` | Nil adapter returns (0, 0) |
| 19 | `TestCounterfactualAdapter_NilCalibrator` | Nil adapter returns quality 1.0 |
| 20 | `TestCounterfactualAdapter_QualityMapping` | ECE → quality: max(0, 1 - ECE×2) |
| 21 | `TestBuildSummary_MixedScenario` | Mixed over/underconfidence across buckets |

## 11. Validation Results

```
$ go build ./...                    → PASS (zero errors)
$ go test ./...                     → PASS (all 24 test packages, zero failures)
$ go test -v ./internal/agent/calibration/... → 20/20 PASS
```

No regressions in any existing package.

## 12. Constants

| Constant | Value | Purpose |
|----------|-------|---------|
| `BucketCount` | 5 | Number of confidence buckets |
| `CalibrationWeight` | 0.3 | Conservative correction scaling |
| `MinBucketSamples` | 3 | Minimum records for bucket to qualify |
| `MaxTrackerRecords` | 500 | Sliding window size |

## 13. Risks

| Risk | Mitigation |
|------|-----------|
| Insufficient data early on | MinBucketSamples gate + fail-open returns raw confidence |
| Overcorrection | CalibrationWeight = 0.3 (conservative) + clamping to [0, 1] |
| Database growth | MaxTrackerRecords = 500 sliding window with DELETE |
| Floating point precision at bucket boundaries | Epsilon addition (1e-9) in BucketIndex |
| Integration failure | All adapters are nil-safe and fail-open |

## 14. Files Changed

### New Files (7)
- `internal/agent/calibration/types.go`
- `internal/agent/calibration/tracker.go`
- `internal/agent/calibration/evaluator.go`
- `internal/agent/calibration/calibrator.go`
- `internal/agent/calibration/adapter.go`
- `internal/agent/calibration/calibration_test.go`
- `internal/db/migrations/000029_create_agent_calibration.up.sql`
- `internal/db/migrations/000029_create_agent_calibration.down.sql`

### Modified Files (6)
- `internal/agent/decision_graph/planner_adapter.go` — CalibrationProvider interface + 3 injection points
- `internal/agent/outcome/handler.go` — CalibrationRecorder interface + recordCalibration method
- `internal/agent/planning/planner.go` — PredictedConfidence field + param propagation
- `internal/api/handlers.go` — WithCalibration + 3 handler methods
- `internal/api/router.go` — 3 new routes
- `cmd/api-gateway/main.go` — Calibration wiring

## 15. Next Step

Iteration 26 should consider:
- **Calibration-aware exploration**: Bias exploration toward underexplored confidence ranges
- **Temporal calibration drift**: Track ECE over time to detect calibration degradation
- **Per-action calibration**: Separate calibration curves per action type for finer correction
