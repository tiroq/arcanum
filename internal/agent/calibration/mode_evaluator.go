package calibration

import "time"

// --- Mode-Specific Calibration Evaluator (Iteration 28) ---
// Pure functions for building mode-specific calibration buckets and summaries.
// Reuses the same bucket model as Iteration 25 but scoped to individual modes.

// BuildModeBuckets computes calibration buckets from mode-specific records.
// Deterministic: same records always produce the same buckets.
func BuildModeBuckets(mode string, records []ModeCalibrationRecord) []ModeCalibrationBucket {
	boundaries := BucketBoundaries()

	buckets := make([]ModeCalibrationBucket, BucketCount)
	for i, b := range boundaries {
		buckets[i] = ModeCalibrationBucket{
			Mode:          mode,
			MinConfidence: b[0],
			MaxConfidence: b[1],
		}
	}

	confidenceSums := make([]float64, BucketCount)

	for _, r := range records {
		idx := BucketIndex(r.PredictedConfidence)
		buckets[idx].Count++
		confidenceSums[idx] += r.PredictedConfidence
		if OutcomeIsSuccess(r.ActualOutcome) {
			buckets[idx].SuccessCount++
		}
	}

	for i := range buckets {
		if buckets[i].Count > 0 {
			buckets[i].Accuracy = float64(buckets[i].SuccessCount) / float64(buckets[i].Count)
			buckets[i].AvgConfidence = confidenceSums[i] / float64(buckets[i].Count)
		}
	}

	return buckets
}

// ComputeModeECE calculates Expected Calibration Error for mode-specific buckets.
// Only buckets with at least ModeMinBucketSamples are included.
// If no qualifying buckets exist, returns 0 (fail-open).
func ComputeModeECE(buckets []ModeCalibrationBucket) float64 {
	total := 0
	for _, b := range buckets {
		if b.Count >= ModeMinBucketSamples {
			total += b.Count
		}
	}
	if total == 0 {
		return 0
	}

	ece := 0.0
	for _, b := range buckets {
		if b.Count < ModeMinBucketSamples {
			continue
		}
		weight := float64(b.Count) / float64(total)
		gap := b.Accuracy - b.AvgConfidence
		if gap < 0 {
			gap = -gap
		}
		ece += gap * weight
	}
	return ece
}

// ComputeModeCalibrationScores returns overconfidence and underconfidence scores
// for mode-specific buckets.
// Only buckets with >= ModeMinBucketSamples are considered.
func ComputeModeCalibrationScores(buckets []ModeCalibrationBucket) (overconfidence, underconfidence float64) {
	overTotal := 0
	underTotal := 0

	for _, b := range buckets {
		if b.Count < ModeMinBucketSamples {
			continue
		}
		gap := b.AvgConfidence - b.Accuracy
		if gap > 0 {
			overconfidence += gap * float64(b.Count)
			overTotal += b.Count
		} else if gap < 0 {
			underconfidence += (-gap) * float64(b.Count)
			underTotal += b.Count
		}
	}

	if overTotal > 0 {
		overconfidence /= float64(overTotal)
	}
	if underTotal > 0 {
		underconfidence /= float64(underTotal)
	}
	return
}

// BuildModeSummary computes a full ModeCalibrationSummary from mode-specific records.
// Deterministic: same records always produce the same summary.
func BuildModeSummary(mode string, records []ModeCalibrationRecord) ModeCalibrationSummary {
	buckets := BuildModeBuckets(mode, records)
	ece := ComputeModeECE(buckets)
	over, under := ComputeModeCalibrationScores(buckets)

	lastUpdated := time.Time{}
	for _, r := range records {
		if r.CreatedAt.After(lastUpdated) {
			lastUpdated = r.CreatedAt
		}
	}

	return ModeCalibrationSummary{
		Mode:                     mode,
		Buckets:                  buckets,
		ExpectedCalibrationError: ece,
		OverconfidenceScore:      over,
		UnderconfidenceScore:     under,
		TotalRecords:             len(records),
		LastUpdated:              lastUpdated,
	}
}
