package calibration

import (
	"testing"
	"time"
)

// --- 1. Bucket assignment correct ---

func TestBucketIndex(t *testing.T) {
	tests := []struct {
		name       string
		confidence float64
		wantIdx    int
	}{
		{"zero", 0.0, 0},
		{"low", 0.1, 0},
		{"boundary_0.2", 0.2, 1},
		{"mid_low", 0.3, 1},
		{"boundary_0.4", 0.4, 2},
		{"mid", 0.5, 2},
		{"boundary_0.6", 0.6, 3},
		{"mid_high", 0.7, 3},
		{"boundary_0.8", 0.8, 4},
		{"high", 0.9, 4},
		{"max", 1.0, 4},
		{"negative", -0.1, 0},
		{"above_max", 1.5, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BucketIndex(tt.confidence)
			if got != tt.wantIdx {
				t.Errorf("BucketIndex(%v) = %d, want %d", tt.confidence, got, tt.wantIdx)
			}
		})
	}
}

// --- 2. Accuracy calculation correct ---

func TestBuildBuckets_Accuracy(t *testing.T) {
	records := []CalibrationRecord{
		{PredictedConfidence: 0.9, ActualOutcome: "success"},
		{PredictedConfidence: 0.85, ActualOutcome: "success"},
		{PredictedConfidence: 0.82, ActualOutcome: "failure"},
		{PredictedConfidence: 0.81, ActualOutcome: "success"},
	}

	buckets := BuildBuckets(records)

	// All records go to bucket 4 (0.8–1.0).
	bucket := buckets[4]
	if bucket.Count != 4 {
		t.Fatalf("expected count=4, got %d", bucket.Count)
	}
	if bucket.SuccessCount != 3 {
		t.Fatalf("expected success_count=3, got %d", bucket.SuccessCount)
	}

	expectedAccuracy := 3.0 / 4.0
	if bucket.Accuracy != expectedAccuracy {
		t.Errorf("expected accuracy=%v, got %v", expectedAccuracy, bucket.Accuracy)
	}

	expectedAvg := (0.9 + 0.85 + 0.82 + 0.81) / 4.0
	if abs(bucket.AvgConfidence-expectedAvg) > 1e-9 {
		t.Errorf("expected avg_confidence=%v, got %v", expectedAvg, bucket.AvgConfidence)
	}
}

func TestBuildBuckets_EmptyBuckets(t *testing.T) {
	buckets := BuildBuckets(nil)
	if len(buckets) != BucketCount {
		t.Fatalf("expected %d buckets, got %d", BucketCount, len(buckets))
	}
	for i, b := range buckets {
		if b.Count != 0 {
			t.Errorf("bucket %d: expected count=0, got %d", i, b.Count)
		}
		if b.Accuracy != 0 {
			t.Errorf("bucket %d: expected accuracy=0, got %v", i, b.Accuracy)
		}
	}
}

// --- 3. ECE calculation correct ---

func TestComputeECE(t *testing.T) {
	// Scenario: well-calibrated — accuracy matches avg confidence.
	wellCalibrated := []CalibrationBucket{
		{MinConfidence: 0.0, MaxConfidence: 0.2, Count: 10, SuccessCount: 1, Accuracy: 0.1, AvgConfidence: 0.1},
		{MinConfidence: 0.2, MaxConfidence: 0.4, Count: 0},
		{MinConfidence: 0.4, MaxConfidence: 0.6, Count: 10, SuccessCount: 5, Accuracy: 0.5, AvgConfidence: 0.5},
		{MinConfidence: 0.6, MaxConfidence: 0.8, Count: 0},
		{MinConfidence: 0.8, MaxConfidence: 1.0, Count: 10, SuccessCount: 9, Accuracy: 0.9, AvgConfidence: 0.9},
	}
	ece := ComputeECE(wellCalibrated)
	if ece != 0 {
		t.Errorf("well-calibrated ECE should be 0, got %v", ece)
	}

	// Scenario: overconfident — bucket says 0.9 avg but accuracy is 0.5.
	overconfident := []CalibrationBucket{
		{MinConfidence: 0.0, MaxConfidence: 0.2, Count: 0},
		{MinConfidence: 0.2, MaxConfidence: 0.4, Count: 0},
		{MinConfidence: 0.4, MaxConfidence: 0.6, Count: 0},
		{MinConfidence: 0.6, MaxConfidence: 0.8, Count: 0},
		{MinConfidence: 0.8, MaxConfidence: 1.0, Count: 10, SuccessCount: 5, Accuracy: 0.5, AvgConfidence: 0.9},
	}
	ece = ComputeECE(overconfident)
	// ECE = |0.5 - 0.9| * (10/10) = 0.4
	if abs(ece-0.4) > 1e-9 {
		t.Errorf("overconfident ECE should be 0.4, got %v", ece)
	}
}

func TestComputeECE_InsufficientSamples(t *testing.T) {
	// Buckets with < MinBucketSamples should be excluded.
	buckets := []CalibrationBucket{
		{MinConfidence: 0.8, MaxConfidence: 1.0, Count: 2, SuccessCount: 0, Accuracy: 0.0, AvgConfidence: 0.9},
	}
	ece := ComputeECE(buckets)
	if ece != 0 {
		t.Errorf("ECE with insufficient samples should be 0, got %v", ece)
	}
}

// --- 4. Overconfidence detected ---

func TestComputeCalibrationScores_Overconfident(t *testing.T) {
	buckets := []CalibrationBucket{
		{MinConfidence: 0.8, MaxConfidence: 1.0, Count: 10, SuccessCount: 5, Accuracy: 0.5, AvgConfidence: 0.9},
	}
	over, under := ComputeCalibrationScores(buckets)
	// Overconfidence: 0.9 - 0.5 = 0.4
	if abs(over-0.4) > 1e-9 {
		t.Errorf("expected overconfidence=0.4, got %v", over)
	}
	if under != 0 {
		t.Errorf("expected underconfidence=0, got %v", under)
	}
}

// --- 5. Underconfidence detected ---

func TestComputeCalibrationScores_Underconfident(t *testing.T) {
	buckets := []CalibrationBucket{
		{MinConfidence: 0.0, MaxConfidence: 0.2, Count: 10, SuccessCount: 7, Accuracy: 0.7, AvgConfidence: 0.1},
	}
	over, under := ComputeCalibrationScores(buckets)
	if over != 0 {
		t.Errorf("expected overconfidence=0, got %v", over)
	}
	// Underconfidence: 0.7 - 0.1 = 0.6
	if abs(under-0.6) > 1e-9 {
		t.Errorf("expected underconfidence=0.6, got %v", under)
	}
}

// --- 6. Confidence correction works ---

func TestCalibrateConfidenceFromSummary(t *testing.T) {
	summary := &CalibrationSummary{
		Buckets: []CalibrationBucket{
			{MinConfidence: 0.0, MaxConfidence: 0.2, Count: 0},
			{MinConfidence: 0.2, MaxConfidence: 0.4, Count: 0},
			{MinConfidence: 0.4, MaxConfidence: 0.6, Count: 0},
			{MinConfidence: 0.6, MaxConfidence: 0.8, Count: 0},
			{MinConfidence: 0.8, MaxConfidence: 1.0, Count: 10, SuccessCount: 5, Accuracy: 0.5, AvgConfidence: 0.9},
		},
	}

	// Overconfident bucket: accuracy=0.5, avg_confidence=0.9.
	// adjustment = (0.5 - 0.9) * 0.3 = -0.12
	// raw 0.85 → 0.85 - 0.12 = 0.73
	adjusted := CalibrateConfidenceFromSummary(0.85, summary)
	expected := 0.85 + (0.5-0.9)*CalibrationWeight
	if abs(adjusted-expected) > 1e-9 {
		t.Errorf("expected adjusted=%v, got %v", expected, adjusted)
	}

	// Underconfident bucket (low confidence, high accuracy).
	summary.Buckets[0] = CalibrationBucket{
		MinConfidence: 0.0, MaxConfidence: 0.2, Count: 10, SuccessCount: 8, Accuracy: 0.8, AvgConfidence: 0.1,
	}
	// adjustment = (0.8 - 0.1) * 0.3 = 0.21
	// raw 0.15 → 0.15 + 0.21 = 0.36
	adjusted = CalibrateConfidenceFromSummary(0.15, summary)
	expected = 0.15 + (0.8-0.1)*CalibrationWeight
	if abs(adjusted-expected) > 1e-9 {
		t.Errorf("expected adjusted=%v, got %v", expected, adjusted)
	}
}

func TestCalibrateConfidenceFromSummary_Clamped(t *testing.T) {
	summary := &CalibrationSummary{
		Buckets: []CalibrationBucket{
			{MinConfidence: 0.0, MaxConfidence: 0.2, Count: 10, SuccessCount: 10, Accuracy: 1.0, AvgConfidence: 0.1},
			{MinConfidence: 0.2, MaxConfidence: 0.4, Count: 0},
			{MinConfidence: 0.4, MaxConfidence: 0.6, Count: 0},
			{MinConfidence: 0.6, MaxConfidence: 0.8, Count: 0},
			{MinConfidence: 0.8, MaxConfidence: 1.0, Count: 0},
		},
	}
	// adjustment = (1.0 - 0.1) * 0.3 = 0.27
	// raw 0.19 → 0.19 + 0.27 = 0.46, no clamping needed
	adjusted := CalibrateConfidenceFromSummary(0.19, summary)
	if adjusted < 0 || adjusted > 1 {
		t.Errorf("adjusted confidence out of [0,1] range: %v", adjusted)
	}
}

// --- 7. No calibration → no change ---

func TestCalibrateConfidenceFromSummary_NilSummary(t *testing.T) {
	adjusted := CalibrateConfidenceFromSummary(0.85, nil)
	if adjusted != 0.85 {
		t.Errorf("expected raw confidence 0.85, got %v", adjusted)
	}
}

func TestCalibrateConfidenceFromSummary_EmptyBuckets(t *testing.T) {
	summary := &CalibrationSummary{Buckets: nil}
	adjusted := CalibrateConfidenceFromSummary(0.85, summary)
	if adjusted != 0.85 {
		t.Errorf("expected raw confidence 0.85, got %v", adjusted)
	}
}

func TestCalibrateConfidenceFromSummary_InsufficientSamples(t *testing.T) {
	summary := &CalibrationSummary{
		Buckets: []CalibrationBucket{
			{MinConfidence: 0.0, MaxConfidence: 0.2, Count: 0},
			{MinConfidence: 0.2, MaxConfidence: 0.4, Count: 0},
			{MinConfidence: 0.4, MaxConfidence: 0.6, Count: 0},
			{MinConfidence: 0.6, MaxConfidence: 0.8, Count: 0},
			{MinConfidence: 0.8, MaxConfidence: 1.0, Count: 2, SuccessCount: 0, Accuracy: 0.0, AvgConfidence: 0.9},
		},
	}
	adjusted := CalibrateConfidenceFromSummary(0.85, summary)
	if adjusted != 0.85 {
		t.Errorf("expected raw confidence 0.85 with insufficient samples, got %v", adjusted)
	}
}

// --- 8. BuildSummary from records (integration) ---

func TestBuildSummary_Integration(t *testing.T) {
	now := time.Now().UTC()
	records := make([]CalibrationRecord, 0, 20)

	// 10 overconfident records: confidence 0.9 but only 50% success
	for i := 0; i < 10; i++ {
		outcome := "success"
		if i%2 == 0 {
			outcome = "failure"
		}
		records = append(records, CalibrationRecord{
			DecisionID:          "d-over-" + string(rune('0'+i)),
			PredictedConfidence: 0.9,
			ActualOutcome:       outcome,
			CreatedAt:           now.Add(time.Duration(i) * time.Second),
		})
	}

	summary := BuildSummary(records)

	if summary.TotalRecords != 10 {
		t.Errorf("expected total_records=10, got %d", summary.TotalRecords)
	}
	if len(summary.Buckets) != BucketCount {
		t.Fatalf("expected %d buckets, got %d", BucketCount, len(summary.Buckets))
	}

	// Bucket 4 should have all records.
	bucket := summary.Buckets[4]
	if bucket.Count != 10 {
		t.Errorf("expected bucket 4 count=10, got %d", bucket.Count)
	}

	// ECE should be non-zero (overconfident).
	if summary.ExpectedCalibrationError == 0 {
		t.Error("expected non-zero ECE for overconfident records")
	}
	if summary.OverconfidenceScore == 0 {
		t.Error("expected non-zero overconfidence score")
	}
}

// --- 9. Deterministic ---

func TestBuildSummary_Deterministic(t *testing.T) {
	records := []CalibrationRecord{
		{PredictedConfidence: 0.3, ActualOutcome: "success", CreatedAt: time.Unix(100, 0)},
		{PredictedConfidence: 0.3, ActualOutcome: "failure", CreatedAt: time.Unix(101, 0)},
		{PredictedConfidence: 0.3, ActualOutcome: "success", CreatedAt: time.Unix(102, 0)},
		{PredictedConfidence: 0.7, ActualOutcome: "success", CreatedAt: time.Unix(103, 0)},
		{PredictedConfidence: 0.7, ActualOutcome: "success", CreatedAt: time.Unix(104, 0)},
		{PredictedConfidence: 0.7, ActualOutcome: "failure", CreatedAt: time.Unix(105, 0)},
	}

	s1 := BuildSummary(records)
	s2 := BuildSummary(records)

	if s1.ExpectedCalibrationError != s2.ExpectedCalibrationError {
		t.Errorf("ECE not deterministic: %v vs %v", s1.ExpectedCalibrationError, s2.ExpectedCalibrationError)
	}
	if s1.OverconfidenceScore != s2.OverconfidenceScore {
		t.Error("overconfidence score not deterministic")
	}
	if s1.UnderconfidenceScore != s2.UnderconfidenceScore {
		t.Error("underconfidence score not deterministic")
	}
}

// --- 10. Outcome mapping ---

func TestOutcomeIsSuccess(t *testing.T) {
	if !OutcomeIsSuccess("success") {
		t.Error("expected success=true")
	}
	if OutcomeIsSuccess("failure") {
		t.Error("expected failure=false")
	}
	if OutcomeIsSuccess("neutral") {
		t.Error("expected neutral=false")
	}
}

// --- Decision graph integration test (pure function) ---

func TestApplyCalibrationToConfidence(t *testing.T) {
	// Simulate decision graph integration: calibrate node confidence before scoring.
	summary := &CalibrationSummary{
		Buckets: []CalibrationBucket{
			{MinConfidence: 0.0, MaxConfidence: 0.2, Count: 0},
			{MinConfidence: 0.2, MaxConfidence: 0.4, Count: 0},
			{MinConfidence: 0.4, MaxConfidence: 0.6, Count: 0},
			{MinConfidence: 0.6, MaxConfidence: 0.8, Count: 10, SuccessCount: 5, Accuracy: 0.5, AvgConfidence: 0.7},
			{MinConfidence: 0.8, MaxConfidence: 1.0, Count: 10, SuccessCount: 9, Accuracy: 0.9, AvgConfidence: 0.9},
		},
	}

	// Node with confidence 0.75 → bucket 3 (0.6–0.8).
	// adjustment = (0.5 - 0.7) * 0.3 = -0.06
	raw := 0.75
	calibrated := CalibrateConfidenceFromSummary(raw, summary)
	expected := 0.75 + (0.5-0.7)*CalibrationWeight
	if abs(calibrated-expected) > 1e-9 {
		t.Errorf("expected calibrated=%v, got %v", expected, calibrated)
	}

	// Node with confidence 0.9 → bucket 4 (0.8–1.0).
	// adjustment = (0.9 - 0.9) * 0.3 = 0 (well-calibrated)
	raw = 0.9
	calibrated = CalibrateConfidenceFromSummary(raw, summary)
	if abs(calibrated-0.9) > 1e-9 {
		t.Errorf("well-calibrated bucket should not change: expected 0.9, got %v", calibrated)
	}
}

// --- Adapter fail-open tests ---

func TestGraphAdapter_NilCalibrator(t *testing.T) {
	adapter := NewGraphAdapter(nil, nil)
	// Should return raw confidence when calibrator is nil.
	result := adapter.CalibrateConfidence(nil, 0.75)
	if result != 0.75 {
		t.Errorf("expected 0.75, got %v", result)
	}
}

func TestMetaReasoningAdapter_NilCalibrator(t *testing.T) {
	adapter := NewMetaReasoningAdapter(nil, nil)
	over, under := adapter.GetCalibrationSignals(nil)
	if over != 0 || under != 0 {
		t.Errorf("expected (0, 0), got (%v, %v)", over, under)
	}
}

func TestCounterfactualAdapter_NilCalibrator(t *testing.T) {
	adapter := NewCounterfactualAdapter(nil, nil)
	quality := adapter.GetCalibrationQuality(nil)
	if quality != 1.0 {
		t.Errorf("expected 1.0, got %v", quality)
	}
}

func TestCounterfactualAdapter_QualityMapping(t *testing.T) {
	// When ECE is 0, quality should be 1.0
	// When ECE is 0.5, quality should be 0.0
	// When ECE is 0.25, quality should be 0.5
	tests := []struct {
		ece         float64
		wantQuality float64
	}{
		{0.0, 1.0},
		{0.25, 0.5},
		{0.5, 0.0},
		{0.75, 0.0}, // clamped to 0
	}

	for _, tt := range tests {
		quality := 1.0 - tt.ece*2
		if quality < 0 {
			quality = 0
		}
		if abs(quality-tt.wantQuality) > 1e-9 {
			t.Errorf("ECE=%v: expected quality=%v, got %v", tt.ece, tt.wantQuality, quality)
		}
	}
}

// --- Mixed calibration scenario ---

func TestBuildSummary_MixedScenario(t *testing.T) {
	now := time.Now().UTC()
	records := []CalibrationRecord{
		// Underconfident: low confidence but high success
		{PredictedConfidence: 0.1, ActualOutcome: "success", CreatedAt: now},
		{PredictedConfidence: 0.15, ActualOutcome: "success", CreatedAt: now},
		{PredictedConfidence: 0.12, ActualOutcome: "success", CreatedAt: now},
		// Overconfident: high confidence but low success
		{PredictedConfidence: 0.9, ActualOutcome: "failure", CreatedAt: now},
		{PredictedConfidence: 0.85, ActualOutcome: "failure", CreatedAt: now},
		{PredictedConfidence: 0.88, ActualOutcome: "failure", CreatedAt: now},
	}

	summary := BuildSummary(records)

	if summary.OverconfidenceScore == 0 {
		t.Error("expected non-zero overconfidence (high-confidence bucket)")
	}
	if summary.UnderconfidenceScore == 0 {
		t.Error("expected non-zero underconfidence (low-confidence bucket)")
	}
	if summary.ExpectedCalibrationError == 0 {
		t.Error("expected non-zero ECE for mixed scenario")
	}
}

// --- Helper ---

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
