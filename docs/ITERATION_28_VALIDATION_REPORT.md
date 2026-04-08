# Iteration 28 — Mode-Specific Calibration Layer: Validation Report

**Date:** 2026-04-08
**Status:** Complete — All tests pass, zero regressions

---

## 1. Architecture

### New Files

| File | Purpose |
|------|---------|
| `internal/agent/calibration/mode_types.go` | Core types: `ModeCalibrationRecord`, `ModeCalibrationBucket`, `ModeCalibrationSummary`, constants |
| `internal/agent/calibration/mode_tracker.go` | PostgreSQL persistence with per-mode sliding window |
| `internal/agent/calibration/mode_evaluator.go` | Pure functions: `BuildModeBuckets`, `ComputeModeECE`, `ComputeModeCalibrationScores`, `BuildModeSummary` |
| `internal/agent/calibration/mode_calibrator.go` | Orchestrator: `RecordOutcome`, `CalibrateConfidenceForMode`, `CalibrateConfidenceFromModeSummary` |
| `internal/agent/calibration/mode_adapter.go` | Adapters: `ModeGraphAdapter`, `ModeOutcomeAdapter` |
| `internal/agent/calibration/mode_calibration_test.go` | 23 tests covering all requirements |
| `internal/db/migrations/000032_create_agent_mode_calibration.up.sql` | Migration: `agent_mode_calibration_records` + `agent_mode_calibration_summary` |
| `internal/db/migrations/000032_create_agent_mode_calibration.down.sql` | Rollback migration |

### Modified Files

| File | Change |
|------|--------|
| `internal/agent/decision_graph/planner_adapter.go` | Added `ModeCalibrationProvider` interface, `modeCalibration` field, `WithModeCalibration()`, mode-specific calibration step in confidence pipeline, `calibration.mode_applied` audit event |
| `internal/agent/outcome/handler.go` | Added `ModeCalibrationRecorder` interface, `modeCalibrationRecorder` field, `WithModeCalibrationRecorder()`, `recordModeCalibration()` |
| `internal/api/handlers.go` | Added `modeCalibrator`/`modeCalTracker` fields, `WithModeCalibration()`, 3 handler methods |
| `internal/api/router.go` | Added 3 routes for mode calibration API |
| `cmd/api-gateway/main.go` | Wired `ModeTracker`, `ModeCalibrator`, adapters into graph adapter, outcome handler, and API handlers |

---

## 2. Calibration Model

### Confidence Correction Formula

```
adjustment = (mode_bucket_accuracy - mode_bucket_avg_confidence) × ModeCalibrationWeight
clamped to [-ModeMaxAdjustment, +ModeMaxAdjustment]
adjusted = clamp(rawConfidence + adjustment, 0, 1)
```

### Constants

| Constant | Value | Purpose |
|----------|-------|---------|
| `ModeCalibrationWeight` | 0.25 | Strength of mode-specific correction |
| `ModeMaxAdjustment` | 0.15 | Maximum absolute correction bound |
| `ModeMinBucketSamples` | 3 | Minimum samples before bucket is used |
| `ModeMaxTrackerRecords` | 500 | Sliding window bound per mode |

### Known Modes

`graph`, `direct`, `conservative`, `exploratory`

Unknown modes are persisted but do not trigger summary updates or correction.

---

## 3. Bucket Model

Same 5-bucket model as Iteration 25:

| Bucket | Range |
|--------|-------|
| 0 | [0.0, 0.2) |
| 1 | [0.2, 0.4) |
| 2 | [0.4, 0.6) |
| 3 | [0.6, 0.8) |
| 4 | [0.8, 1.0] |

Each mode has its own independent set of 5 buckets. No cross-mode data sharing.

`BucketIndex()` uses epsilon `1e-9` for IEEE 754 boundary precision (reused from Iteration 25).

---

## 4. Integration Order

The confidence pipeline order is:

```
raw confidence
  → global calibration        (Iteration 25: CalibrationWeight=0.3)
  → contextual calibration    (Iteration 26: ContextMaxAdjustment=±0.20)
  → mode-specific calibration (Iteration 28: ModeMaxAdjustment=±0.15)
  → final confidence
```

This order is explicit in `planner_adapter.go` lines where each calibration step is applied sequentially:
1. `a.calibration.CalibrateConfidence()` — global
2. `a.contextCalibration.CalibrateConfidenceForContext()` — contextual
3. `a.modeCalibration.CalibrateConfidenceForMode()` — mode-specific

Mode-specific calibration is applied last because it is the most specific correction layer.

---

## 5. Example Confidence Corrections per Mode

### Example 1: Graph Mode (Overconfident)

- Raw confidence: 0.90
- Global calibration adjusts to: 0.725
- Contextual calibration adjusts to: 0.575
- Graph mode: bucket accuracy = 0.7, avg_confidence = 0.5
  - adjustment = (0.7 - 0.5) × 0.25 = +0.05
- Final confidence: 0.625

### Example 2: Direct Mode (Underconfident)

- Predicted: 0.30
- Direct mode bucket: accuracy = 0.70, avg_confidence = 0.30
  - adjustment = (0.70 - 0.30) × 0.25 = +0.10
- Adjusted: 0.40 (confidence increased)

### Example 3: Mode with Extreme Overconfidence

- Predicted: 0.90
- Mode bucket: accuracy = 0.0, avg_confidence = 0.90
  - raw adjustment = (0.0 - 0.9) × 0.25 = -0.225
  - clamped to -0.15 (ModeMaxAdjustment)
- Adjusted: 0.75 (bounded reduction)

### Example 4: Unknown Mode

- Predicted: 0.80, mode = "hybrid"
- `IsKnownMode("hybrid")` → false
- Result: 0.80 unchanged (fail-open)

---

## 6. Tests

23 tests in `mode_calibration_test.go`:

| # | Test | Status |
|---|------|--------|
| 1 | `TestBuildModeBuckets_Assignment` — bucket assignment by mode | PASS |
| 2 | `TestComputeModeECE` — ECE computed correctly | PASS |
| 3 | `TestComputeModeECE_InsufficientSamples` — insufficient samples → ECE=0 | PASS |
| 4 | `TestCalibrateConfidenceFromModeSummary_Overconfident` — overconfidence reduces confidence | PASS |
| 5 | `TestCalibrateConfidenceFromModeSummary_Underconfident` — underconfidence increases confidence | PASS |
| 6 | `TestNoCrossContamination` — one mode's data doesn't affect another | PASS |
| 7 | `TestCalibrateConfidenceFromModeSummary_NilSummary` — no data → no change | PASS |
| 8 | `TestCalibrateConfidenceFromModeSummary_EmptyBuckets` — empty buckets → no change | PASS |
| 9 | `TestCalibrateConfidenceFromModeSummary_InsufficientBucketSamples` — insufficient → no change | PASS |
| 10 | `TestCalibrateConfidenceFromModeSummary_BoundedCorrection` — bounded to ±0.15 | PASS |
| 11 | `TestIntegrationOrderMatters` — contextual before mode produces correct pipeline | PASS |
| 12 | `TestDeterministicRepeatedRuns` — 10 identical runs produce identical results | PASS |
| 13 | `TestExistingCalibrationUnchanged` — global + contextual calibration unaffected | PASS |
| 14 | `TestIsKnownMode` — known/unknown mode classification | PASS |
| 15 | `TestCalibrateConfidenceFromModeSummary_UnknownModeSkipped` — pure function behavior | PASS |
| 16 | `TestBuildModeSummary_EmptyRecords` — empty records → zero summary | PASS |
| 17 | `TestBuildModeSummary_OnlyFailures` — all failures → high ECE | PASS |
| 18 | `TestBuildModeSummary_OnlySuccesses` — all successes → underconfidence | PASS |
| 19 | `TestExactBoundaryConfidenceValues` — 0.0, 0.2, 0.4, 0.6, 0.8, 1.0 boundaries | PASS |
| 20 | `TestCalibrateClampedTo01` — output always in [0, 1] | PASS |
| 21 | `TestModeCalibrationScores` — overconfidence/underconfidence scores | PASS |
| 22 | `TestModeGraphAdapter_NilCalibrator` — nil adapter → fail-open | PASS |
| 23 | `TestModeOutcomeAdapter_NilCalibrator` — nil adapter → no error | PASS |

---

## 7. Validation Results

```
$ go test ./internal/agent/calibration/ -count=1
PASS
ok  github.com/tiroq/arcanum/internal/agent/calibration  0.004s

$ go test ./internal/agent/decision_graph/ -count=1
PASS
ok  github.com/tiroq/arcanum/internal/agent/decision_graph  0.006s

$ go test ./... 2>&1 | grep "FAIL"
(no output — all tests pass)

$ go build ./...
(clean build, no errors)
```

---

## 8. Regression Summary

| Area | Status |
|------|--------|
| Global calibration (Iteration 25) | No changes, all tests pass |
| Contextual calibration (Iteration 26) | No changes, all tests pass |
| Signal arbitration (Iteration 27) | No changes, all tests pass |
| Decision graph evaluation | No changes, all tests pass |
| Path learning | No changes, all tests pass |
| Comparative learning | No changes, all tests pass |
| Counterfactual simulation | No changes, all tests pass |
| Meta-reasoning | No changes, all tests pass |
| Outcome handler | Extended with mode recorder, all existing tests pass |
| API endpoints | 3 new routes added, existing routes unchanged |
| Full project build | Clean build, zero errors |
| Full test suite | All packages pass |

---

## 9. Risks

| Risk | Mitigation |
|------|------------|
| Cold start — no mode data initially | Fail-open: raw confidence returned unchanged |
| Mode with only failures → always reduces confidence | Bounded by ModeMaxAdjustment (±0.15), never exceeds cap |
| Unknown mode submitted | Persisted but ignored for calibration; `IsKnownMode()` guard |
| Database table growth | Per-mode sliding window enforced (500 records per mode) |
| Pipeline order dependency | Explicit sequential application in planner_adapter.go, tested |
| Correction stacking (global + context + mode) | Mode correction bounded to ±0.15, total maximum cumulative adjustment is bounded |

---

## 10. Next Step Recommendation

Potential follow-up iterations:

1. **Mode selection influence** — Use mode-specific calibration scores to influence meta-reasoning mode selection (currently mode calibration only adjusts post-selection confidence; it could also inform mode choice)
2. **Context + mode joint calibration** — Track calibration at the (goal_type, mode) joint level for finer granularity
3. **Mode calibration decay** — Apply time-based weighting so recent records have more influence than older ones
4. **Dashboard visualization** — Surface mode calibration trends in the admin web UI using the new API endpoints

---

## Audit Events

| Event | Payload |
|-------|---------|
| `calibration.mode_recorded` | `decision_id`, `goal_type`, `mode`, `predicted_confidence`, `actual_outcome` |
| `calibration.mode_updated` | `mode`, `ece`, `overconfidence_score`, `underconfidence_score`, `total_records` |
| `calibration.mode_applied` | `mode`, `original_confidence`, `adjusted_confidence`, `goal_type` |

---

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/agent/calibration/mode-summary` | Mode calibration summaries for all modes |
| GET | `/api/v1/agent/calibration/mode-buckets` | Bucket-level calibration by mode |
| GET | `/api/v1/agent/calibration/mode-records` | Recent mode calibration records (supports `?limit=` and `?offset=`) |

---

## Definition of Done Checklist

- [x] Mode-specific calibration records stored
- [x] Mode summaries computed correctly
- [x] Mode-specific correction applied deterministically
- [x] Existing calibration still works
- [x] No mode cross-contamination
- [x] API endpoints added (3 endpoints)
- [x] Audit events added (3 events)
- [x] Full tests pass (23 new tests)
- [x] Zero regressions
- [x] Validation report created
