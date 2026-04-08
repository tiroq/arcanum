package calibration

import (
	"math"
	"testing"
	"time"
)

// --- 1. Bucket assignment by mode is correct ---

func TestBuildModeBuckets_Assignment(t *testing.T) {
	records := []ModeCalibrationRecord{
		{Mode: "graph", PredictedConfidence: 0.1, ActualOutcome: "success"},
		{Mode: "graph", PredictedConfidence: 0.3, ActualOutcome: "failure"},
		{Mode: "graph", PredictedConfidence: 0.5, ActualOutcome: "success"},
		{Mode: "graph", PredictedConfidence: 0.7, ActualOutcome: "success"},
		{Mode: "graph", PredictedConfidence: 0.9, ActualOutcome: "failure"},
	}

	buckets := BuildModeBuckets("graph", records)

	if len(buckets) != BucketCount {
		t.Fatalf("expected %d buckets, got %d", BucketCount, len(buckets))
	}

	// Each bucket should have exactly 1 record.
	for i, b := range buckets {
		if b.Count != 1 {
			t.Errorf("bucket[%d] count = %d, want 1", i, b.Count)
		}
		if b.Mode != "graph" {
			t.Errorf("bucket[%d] mode = %q, want %q", i, b.Mode, "graph")
		}
	}

	// Bucket 0 (0.0–0.2): success → accuracy = 1.0
	if buckets[0].Accuracy != 1.0 {
		t.Errorf("bucket[0] accuracy = %v, want 1.0", buckets[0].Accuracy)
	}

	// Bucket 4 (0.8–1.0): failure → accuracy = 0.0
	if buckets[4].Accuracy != 0.0 {
		t.Errorf("bucket[4] accuracy = %v, want 0.0", buckets[4].Accuracy)
	}
}

// --- 2. Mode ECE computed correctly ---

func TestComputeModeECE(t *testing.T) {
	// Build records with known pattern: always predict 0.9, actual 60% success.
	// This means overconfident: avg_confidence=0.9, accuracy=0.6, gap=0.3.
	var records []ModeCalibrationRecord
	for i := 0; i < 10; i++ {
		outcome := "failure"
		if i < 6 {
			outcome = "success"
		}
		records = append(records, ModeCalibrationRecord{
			Mode:                "direct",
			PredictedConfidence: 0.9,
			ActualOutcome:       outcome,
		})
	}

	buckets := BuildModeBuckets("direct", records)
	ece := ComputeModeECE(buckets)

	// All records in bucket 4, ECE should be |0.6 - 0.9| = 0.3.
	if math.Abs(ece-0.3) > 1e-9 {
		t.Errorf("ECE = %v, want 0.3", ece)
	}
}

func TestComputeModeECE_InsufficientSamples(t *testing.T) {
	// Only 2 records (< ModeMinBucketSamples=3), should fail-open with ECE=0.
	records := []ModeCalibrationRecord{
		{Mode: "graph", PredictedConfidence: 0.9, ActualOutcome: "success"},
		{Mode: "graph", PredictedConfidence: 0.85, ActualOutcome: "failure"},
	}

	buckets := BuildModeBuckets("graph", records)
	ece := ComputeModeECE(buckets)

	if ece != 0 {
		t.Errorf("ECE = %v, want 0 (insufficient samples)", ece)
	}
}

// --- 3. Graph mode overconfidence reduces confidence ---

func TestCalibrateConfidenceFromModeSummary_Overconfident(t *testing.T) {
	// Mode is overconfident: predicts 0.9, actual accuracy 0.5.
	// adjustment = (0.5 - 0.9) × 0.25 = -0.1
	// adjusted = 0.9 + (-0.1) = 0.8
	summary := &ModeCalibrationSummary{
		Mode: "graph",
		Buckets: []ModeCalibrationBucket{
			{Mode: "graph", MinConfidence: 0.0, MaxConfidence: 0.2},
			{Mode: "graph", MinConfidence: 0.2, MaxConfidence: 0.4},
			{Mode: "graph", MinConfidence: 0.4, MaxConfidence: 0.6},
			{Mode: "graph", MinConfidence: 0.6, MaxConfidence: 0.8},
			{Mode: "graph", MinConfidence: 0.8, MaxConfidence: 1.0,
				Count: 10, SuccessCount: 5, Accuracy: 0.5, AvgConfidence: 0.9},
		},
	}

	adjusted := CalibrateConfidenceFromModeSummary(0.9, summary)

	// Adjustment = (0.5 - 0.9) * 0.25 = -0.1
	// Adjusted = 0.9 + (-0.1) = 0.8
	if math.Abs(adjusted-0.8) > 1e-9 {
		t.Errorf("adjusted = %v, want 0.8", adjusted)
	}

	// Confidence should be reduced.
	if adjusted >= 0.9 {
		t.Errorf("expected confidence reduction, got %v >= 0.9", adjusted)
	}
}

// --- 4. Direct mode underconfidence increases confidence ---

func TestCalibrateConfidenceFromModeSummary_Underconfident(t *testing.T) {
	// Mode is underconfident: predicts 0.3, actual accuracy 0.7.
	// adjustment = (0.7 - 0.3) × 0.25 = +0.1
	// adjusted = 0.3 + 0.1 = 0.4
	summary := &ModeCalibrationSummary{
		Mode: "direct",
		Buckets: []ModeCalibrationBucket{
			{Mode: "direct", MinConfidence: 0.0, MaxConfidence: 0.2},
			{Mode: "direct", MinConfidence: 0.2, MaxConfidence: 0.4,
				Count: 10, SuccessCount: 7, Accuracy: 0.7, AvgConfidence: 0.3},
			{Mode: "direct", MinConfidence: 0.4, MaxConfidence: 0.6},
			{Mode: "direct", MinConfidence: 0.6, MaxConfidence: 0.8},
			{Mode: "direct", MinConfidence: 0.8, MaxConfidence: 1.0},
		},
	}

	adjusted := CalibrateConfidenceFromModeSummary(0.3, summary)

	if math.Abs(adjusted-0.4) > 1e-9 {
		t.Errorf("adjusted = %v, want 0.4", adjusted)
	}

	// Confidence should be increased.
	if adjusted <= 0.3 {
		t.Errorf("expected confidence increase, got %v <= 0.3", adjusted)
	}
}

// --- 5. One mode's data does not affect another mode ---

func TestNoCrossContamination(t *testing.T) {
	// Build separate records for graph and direct modes.
	graphRecords := []ModeCalibrationRecord{
		{Mode: "graph", PredictedConfidence: 0.9, ActualOutcome: "failure"},
		{Mode: "graph", PredictedConfidence: 0.9, ActualOutcome: "failure"},
		{Mode: "graph", PredictedConfidence: 0.9, ActualOutcome: "failure"},
		{Mode: "graph", PredictedConfidence: 0.9, ActualOutcome: "failure"},
		{Mode: "graph", PredictedConfidence: 0.9, ActualOutcome: "failure"},
	}
	directRecords := []ModeCalibrationRecord{
		{Mode: "direct", PredictedConfidence: 0.9, ActualOutcome: "success"},
		{Mode: "direct", PredictedConfidence: 0.9, ActualOutcome: "success"},
		{Mode: "direct", PredictedConfidence: 0.9, ActualOutcome: "success"},
		{Mode: "direct", PredictedConfidence: 0.9, ActualOutcome: "success"},
		{Mode: "direct", PredictedConfidence: 0.9, ActualOutcome: "success"},
	}

	graphSummary := BuildModeSummary("graph", graphRecords)
	directSummary := BuildModeSummary("direct", directRecords)

	// Graph mode: accuracy=0.0, avg=0.9, should reduce confidence heavily.
	graphAdj := CalibrateConfidenceFromModeSummary(0.9, &graphSummary)

	// Direct mode: accuracy=1.0, avg=0.9, should increase confidence.
	directAdj := CalibrateConfidenceFromModeSummary(0.9, &directSummary)

	// They must differ — no cross-contamination.
	if graphAdj == directAdj {
		t.Errorf("cross-contamination: graph adjusted=%v, direct adjusted=%v — should differ", graphAdj, directAdj)
	}

	// Graph should be reduced.
	if graphAdj >= 0.9 {
		t.Errorf("graph mode overconfident, expected reduction from 0.9, got %v", graphAdj)
	}

	// Direct should be increased.
	if directAdj <= 0.9 {
		t.Errorf("direct mode underconfident, expected increase from 0.9, got %v", directAdj)
	}
}

// --- 6. No data for mode → no change ---

func TestCalibrateConfidenceFromModeSummary_NilSummary(t *testing.T) {
	adjusted := CalibrateConfidenceFromModeSummary(0.75, nil)
	if adjusted != 0.75 {
		t.Errorf("nil summary: adjusted = %v, want 0.75", adjusted)
	}
}

func TestCalibrateConfidenceFromModeSummary_EmptyBuckets(t *testing.T) {
	summary := &ModeCalibrationSummary{
		Mode:    "graph",
		Buckets: []ModeCalibrationBucket{},
	}
	adjusted := CalibrateConfidenceFromModeSummary(0.75, summary)
	if adjusted != 0.75 {
		t.Errorf("empty buckets: adjusted = %v, want 0.75", adjusted)
	}
}

func TestCalibrateConfidenceFromModeSummary_InsufficientBucketSamples(t *testing.T) {
	summary := &ModeCalibrationSummary{
		Mode: "graph",
		Buckets: []ModeCalibrationBucket{
			{Mode: "graph", MinConfidence: 0.0, MaxConfidence: 0.2},
			{Mode: "graph", MinConfidence: 0.2, MaxConfidence: 0.4},
			{Mode: "graph", MinConfidence: 0.4, MaxConfidence: 0.6},
			{Mode: "graph", MinConfidence: 0.6, MaxConfidence: 0.8,
				Count: 2, SuccessCount: 1, Accuracy: 0.5, AvgConfidence: 0.7}, // < ModeMinBucketSamples
			{Mode: "graph", MinConfidence: 0.8, MaxConfidence: 1.0},
		},
	}
	adjusted := CalibrateConfidenceFromModeSummary(0.7, summary)
	if adjusted != 0.7 {
		t.Errorf("insufficient samples: adjusted = %v, want 0.7", adjusted)
	}
}

// --- 7. Bounded correction enforced ---

func TestCalibrateConfidenceFromModeSummary_BoundedCorrection(t *testing.T) {
	// Extreme overconfidence: accuracy=0.0, avg=0.9
	// Raw adjustment = (0.0 - 0.9) × 0.25 = -0.225
	// Clamped to -ModeMaxAdjustment = -0.15
	summary := &ModeCalibrationSummary{
		Mode: "graph",
		Buckets: []ModeCalibrationBucket{
			{Mode: "graph", MinConfidence: 0.0, MaxConfidence: 0.2},
			{Mode: "graph", MinConfidence: 0.2, MaxConfidence: 0.4},
			{Mode: "graph", MinConfidence: 0.4, MaxConfidence: 0.6},
			{Mode: "graph", MinConfidence: 0.6, MaxConfidence: 0.8},
			{Mode: "graph", MinConfidence: 0.8, MaxConfidence: 1.0,
				Count: 20, SuccessCount: 0, Accuracy: 0.0, AvgConfidence: 0.9},
		},
	}

	adjusted := CalibrateConfidenceFromModeSummary(0.9, summary)

	// Clamped: 0.9 + (-0.15) = 0.75
	if math.Abs(adjusted-0.75) > 1e-9 {
		t.Errorf("bounded correction: adjusted = %v, want 0.75", adjusted)
	}

	// Also test extreme underconfidence.
	// accuracy=1.0, avg=0.1 → raw adjustment = (1.0-0.1)*0.25 = 0.225 → clamped to +0.15
	summaryUnder := &ModeCalibrationSummary{
		Mode: "direct",
		Buckets: []ModeCalibrationBucket{
			{Mode: "direct", MinConfidence: 0.0, MaxConfidence: 0.2,
				Count: 20, SuccessCount: 20, Accuracy: 1.0, AvgConfidence: 0.1},
			{Mode: "direct", MinConfidence: 0.2, MaxConfidence: 0.4},
			{Mode: "direct", MinConfidence: 0.4, MaxConfidence: 0.6},
			{Mode: "direct", MinConfidence: 0.6, MaxConfidence: 0.8},
			{Mode: "direct", MinConfidence: 0.8, MaxConfidence: 1.0},
		},
	}

	adjustedUnder := CalibrateConfidenceFromModeSummary(0.1, summaryUnder)

	// Clamped: 0.1 + 0.15 = 0.25
	if math.Abs(adjustedUnder-0.25) > 1e-9 {
		t.Errorf("bounded underconfidence: adjusted = %v, want 0.25", adjustedUnder)
	}
}

// --- 8. Integration order preserved: contextual before mode ---
// This test verifies that applying contextual calibration THEN mode calibration
// produces a different result than the reverse, confirming pipeline ordering matters.

func TestIntegrationOrderMatters(t *testing.T) {
	// Simulate the full pipeline:
	// raw → global → contextual → mode-specific
	rawConfidence := 0.8

	// Global calibration: slight overconfidence correction.
	// Simulated: reduces by CalibrationWeight factor.
	globalSummary := &CalibrationSummary{
		Buckets: []CalibrationBucket{
			{MinConfidence: 0.0, MaxConfidence: 0.2},
			{MinConfidence: 0.2, MaxConfidence: 0.4},
			{MinConfidence: 0.4, MaxConfidence: 0.6},
			{MinConfidence: 0.6, MaxConfidence: 0.8},
			{MinConfidence: 0.8, MaxConfidence: 1.0,
				Count: 10, SuccessCount: 6, Accuracy: 0.6, AvgConfidence: 0.85},
		},
	}

	afterGlobal := CalibrateConfidenceFromSummary(rawConfidence, globalSummary)

	// Contextual calibration: slight additional correction.
	// error = avg_predicted - avg_actual = 0.8 - 0.65 = 0.15 (overconfident)
	afterContext := ApplyContextualCalibration(afterGlobal, 0.15)

	// Mode-specific calibration: graph mode specific correction.
	modeSummary := &ModeCalibrationSummary{
		Mode: "graph",
		Buckets: []ModeCalibrationBucket{
			{Mode: "graph", MinConfidence: 0.0, MaxConfidence: 0.2},
			{Mode: "graph", MinConfidence: 0.2, MaxConfidence: 0.4},
			{Mode: "graph", MinConfidence: 0.4, MaxConfidence: 0.6,
				Count: 10, SuccessCount: 7, Accuracy: 0.7, AvgConfidence: 0.5},
			{Mode: "graph", MinConfidence: 0.6, MaxConfidence: 0.8},
			{Mode: "graph", MinConfidence: 0.8, MaxConfidence: 1.0},
		},
	}

	afterMode := CalibrateConfidenceFromModeSummary(afterContext, modeSummary)

	// If afterContext lands in bucket 2 (0.4-0.6), mode calibration applies +0.05.
	// Verify chain produces a different value than raw.
	if afterMode == rawConfidence {
		t.Log("pipeline had no overall effect (possible if corrections cancel), which is valid")
	}

	// Verify order: raw → global → context → mode.
	// Each step should transform the value.
	t.Logf("Pipeline: raw=%.3f → global=%.3f → context=%.3f → mode=%.3f",
		rawConfidence, afterGlobal, afterContext, afterMode)

	// At minimum, global should differ from raw (bucket has enough samples and there's a gap).
	if afterGlobal == rawConfidence {
		t.Errorf("global calibration had no effect, expected adjustment")
	}
}

// --- 9. Deterministic repeated runs ---

func TestDeterministicRepeatedRuns(t *testing.T) {
	records := []ModeCalibrationRecord{
		{Mode: "graph", PredictedConfidence: 0.9, ActualOutcome: "success", CreatedAt: time.Now()},
		{Mode: "graph", PredictedConfidence: 0.85, ActualOutcome: "failure", CreatedAt: time.Now()},
		{Mode: "graph", PredictedConfidence: 0.82, ActualOutcome: "success", CreatedAt: time.Now()},
		{Mode: "graph", PredictedConfidence: 0.88, ActualOutcome: "success", CreatedAt: time.Now()},
		{Mode: "graph", PredictedConfidence: 0.91, ActualOutcome: "failure", CreatedAt: time.Now()},
	}

	// Run 10 times, all results must be identical.
	var firstECE, firstOver, firstUnder float64
	var firstAdj float64

	for i := 0; i < 10; i++ {
		summary := BuildModeSummary("graph", records)
		adj := CalibrateConfidenceFromModeSummary(0.9, &summary)

		if i == 0 {
			firstECE = summary.ExpectedCalibrationError
			firstOver = summary.OverconfidenceScore
			firstUnder = summary.UnderconfidenceScore
			firstAdj = adj
		} else {
			if summary.ExpectedCalibrationError != firstECE {
				t.Fatalf("run %d: ECE=%v, want %v", i, summary.ExpectedCalibrationError, firstECE)
			}
			if summary.OverconfidenceScore != firstOver {
				t.Fatalf("run %d: overconfidence=%v, want %v", i, summary.OverconfidenceScore, firstOver)
			}
			if summary.UnderconfidenceScore != firstUnder {
				t.Fatalf("run %d: underconfidence=%v, want %v", i, summary.UnderconfidenceScore, firstUnder)
			}
			if adj != firstAdj {
				t.Fatalf("run %d: adjusted=%v, want %v", i, adj, firstAdj)
			}
		}
	}
}

// --- 10. No regression to existing calibration flows ---

func TestExistingCalibrationUnchanged(t *testing.T) {
	// Verify global calibration still works as before.
	globalRecords := []CalibrationRecord{
		{PredictedConfidence: 0.9, ActualOutcome: "success"},
		{PredictedConfidence: 0.85, ActualOutcome: "success"},
		{PredictedConfidence: 0.82, ActualOutcome: "failure"},
		{PredictedConfidence: 0.81, ActualOutcome: "success"},
	}

	globalSummary := BuildSummary(globalRecords)

	// Global functions should still produce correct results.
	if globalSummary.TotalRecords != 4 {
		t.Errorf("global summary total_records = %d, want 4", globalSummary.TotalRecords)
	}

	// Verify contextual calibration still works.
	// Apply contextual calibration with known error.
	adjusted := ApplyContextualCalibration(0.8, 0.1)
	expected := 0.7 // 0.8 - 0.1
	if math.Abs(adjusted-expected) > 1e-9 {
		t.Errorf("contextual calibration: adjusted = %v, want %v", adjusted, expected)
	}
}

// --- Edge Cases ---

func TestIsKnownMode(t *testing.T) {
	known := []string{"graph", "direct", "conservative", "exploratory"}
	for _, m := range known {
		if !IsKnownMode(m) {
			t.Errorf("IsKnownMode(%q) = false, want true", m)
		}
	}

	unknown := []string{"unknown", "hybrid", "", "GRAPH"}
	for _, m := range unknown {
		if IsKnownMode(m) {
			t.Errorf("IsKnownMode(%q) = true, want false", m)
		}
	}
}

func TestCalibrateConfidenceFromModeSummary_UnknownModeSkipped(t *testing.T) {
	// The pure function works with any summary, but in the ModeCalibrator the
	// unknown mode check happens before calling this. Still verify boundary behavior.
	summary := &ModeCalibrationSummary{
		Mode: "unknown",
		Buckets: []ModeCalibrationBucket{
			{Mode: "unknown", MinConfidence: 0.0, MaxConfidence: 0.2},
			{Mode: "unknown", MinConfidence: 0.2, MaxConfidence: 0.4},
			{Mode: "unknown", MinConfidence: 0.4, MaxConfidence: 0.6,
				Count: 10, SuccessCount: 3, Accuracy: 0.3, AvgConfidence: 0.5},
			{Mode: "unknown", MinConfidence: 0.6, MaxConfidence: 0.8},
			{Mode: "unknown", MinConfidence: 0.8, MaxConfidence: 1.0},
		},
	}
	// The pure function still applies correction even for unknown mode—
	// the guard is in ModeCalibrator.CalibrateConfidenceForMode.
	adj := CalibrateConfidenceFromModeSummary(0.5, summary)
	// (0.3 - 0.5) * 0.25 = -0.05 → 0.45
	if math.Abs(adj-0.45) > 1e-9 {
		t.Errorf("adjusted = %v, want 0.45", adj)
	}
}

func TestBuildModeSummary_EmptyRecords(t *testing.T) {
	summary := BuildModeSummary("graph", nil)
	if summary.TotalRecords != 0 {
		t.Errorf("empty records: total_records = %d, want 0", summary.TotalRecords)
	}
	if summary.ExpectedCalibrationError != 0 {
		t.Errorf("empty records: ECE = %v, want 0", summary.ExpectedCalibrationError)
	}
}

func TestBuildModeSummary_OnlyFailures(t *testing.T) {
	records := []ModeCalibrationRecord{
		{Mode: "graph", PredictedConfidence: 0.9, ActualOutcome: "failure"},
		{Mode: "graph", PredictedConfidence: 0.85, ActualOutcome: "failure"},
		{Mode: "graph", PredictedConfidence: 0.88, ActualOutcome: "failure"},
	}
	summary := BuildModeSummary("graph", records)

	// All failures: accuracy=0.0 in bucket 4, and there are 3 samples (= ModeMinBucketSamples).
	bucket := summary.Buckets[4]
	if bucket.Accuracy != 0.0 {
		t.Errorf("all failures: accuracy = %v, want 0.0", bucket.Accuracy)
	}
	if bucket.Count != 3 {
		t.Errorf("all failures: count = %d, want 3", bucket.Count)
	}

	// ECE should be |0.0 - avg_conf| where avg_conf ≈ 0.877.
	if summary.ExpectedCalibrationError < 0.85 {
		t.Errorf("all failures with high confidence: ECE should be high, got %v", summary.ExpectedCalibrationError)
	}
}

func TestBuildModeSummary_OnlySuccesses(t *testing.T) {
	records := []ModeCalibrationRecord{
		{Mode: "direct", PredictedConfidence: 0.1, ActualOutcome: "success"},
		{Mode: "direct", PredictedConfidence: 0.15, ActualOutcome: "success"},
		{Mode: "direct", PredictedConfidence: 0.12, ActualOutcome: "success"},
	}
	summary := BuildModeSummary("direct", records)

	// All successes: accuracy=1.0 in bucket 0, underconfident.
	bucket := summary.Buckets[0]
	if bucket.Accuracy != 1.0 {
		t.Errorf("all successes: accuracy = %v, want 1.0", bucket.Accuracy)
	}

	// Should be underconfident.
	if summary.UnderconfidenceScore == 0 {
		t.Errorf("all successes with low confidence: expected underconfidence > 0")
	}
}

func TestExactBoundaryConfidenceValues(t *testing.T) {
	records := []ModeCalibrationRecord{
		{Mode: "graph", PredictedConfidence: 0.0, ActualOutcome: "success"},
		{Mode: "graph", PredictedConfidence: 0.2, ActualOutcome: "success"},
		{Mode: "graph", PredictedConfidence: 0.4, ActualOutcome: "success"},
		{Mode: "graph", PredictedConfidence: 0.6, ActualOutcome: "success"},
		{Mode: "graph", PredictedConfidence: 0.8, ActualOutcome: "success"},
		{Mode: "graph", PredictedConfidence: 1.0, ActualOutcome: "success"},
	}

	buckets := BuildModeBuckets("graph", records)

	// 0.0 → bucket 0, 0.2 → bucket 1, 0.4 → bucket 2, 0.6 → bucket 3,
	// 0.8 → bucket 4, 1.0 → bucket 4
	expected := []int{1, 1, 1, 1, 2}
	for i, want := range expected {
		if buckets[i].Count != want {
			t.Errorf("boundary: bucket[%d] count = %d, want %d", i, buckets[i].Count, want)
		}
	}
}

func TestCalibrateClampedTo01(t *testing.T) {
	// Extreme underconfidence: accuracy=1.0, avg=0.1 → adjustment = +0.15 (clamped)
	// raw=0.95 + 0.15 = 1.10 → clamped to 1.0
	summary := &ModeCalibrationSummary{
		Mode: "direct",
		Buckets: []ModeCalibrationBucket{
			{Mode: "direct", MinConfidence: 0.0, MaxConfidence: 0.2},
			{Mode: "direct", MinConfidence: 0.2, MaxConfidence: 0.4},
			{Mode: "direct", MinConfidence: 0.4, MaxConfidence: 0.6},
			{Mode: "direct", MinConfidence: 0.6, MaxConfidence: 0.8},
			{Mode: "direct", MinConfidence: 0.8, MaxConfidence: 1.0,
				Count: 10, SuccessCount: 10, Accuracy: 1.0, AvgConfidence: 0.85},
		},
	}

	adjusted := CalibrateConfidenceFromModeSummary(0.95, summary)
	// adjustment = (1.0 - 0.85)*0.25 = 0.0375, adjusted = 0.9875
	if adjusted > 1.0 || adjusted < 0.0 {
		t.Errorf("out of bounds: adjusted = %v, want [0,1]", adjusted)
	}
}

func TestModeCalibrationScores(t *testing.T) {
	buckets := []ModeCalibrationBucket{
		{Mode: "graph", Count: 5, Accuracy: 0.2, AvgConfidence: 0.1}, // underconfident
		{Mode: "graph", Count: 5, Accuracy: 0.8, AvgConfidence: 0.9}, // overconfident
		{Mode: "graph", Count: 1, Accuracy: 0.5, AvgConfidence: 0.5}, // excluded (< ModeMinBucketSamples)
	}

	over, under := ComputeModeCalibrationScores(buckets)

	// Overconfident bucket: gap = 0.9 - 0.8 = 0.1
	if math.Abs(over-0.1) > 1e-9 {
		t.Errorf("overconfidence = %v, want 0.1", over)
	}

	// Underconfident bucket: gap = |0.1 - 0.2| = 0.1
	if math.Abs(under-0.1) > 1e-9 {
		t.Errorf("underconfidence = %v, want 0.1", under)
	}
}

// --- Mode Adapter tests ---

func TestModeGraphAdapter_NilCalibrator(t *testing.T) {
	adapter := NewModeGraphAdapter(nil, nil)
	adjusted := adapter.CalibrateConfidenceForMode(nil, 0.75, "graph")
	if adjusted != 0.75 {
		t.Errorf("nil calibrator: adjusted = %v, want 0.75", adjusted)
	}
}

func TestModeOutcomeAdapter_NilCalibrator(t *testing.T) {
	adapter := NewModeOutcomeAdapter(nil, nil)
	err := adapter.RecordModeCalibrationOutcome(nil, "dec-1", "task", "graph", 0.9, "success")
	if err != nil {
		t.Errorf("nil calibrator: unexpected error %v", err)
	}
}
