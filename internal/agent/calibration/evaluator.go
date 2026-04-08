package calibration

import "time"

// BuildBuckets computes calibration buckets from a list of records.
// Deterministic: same records always produce the same buckets.
func BuildBuckets(records []CalibrationRecord) []CalibrationBucket {
	boundaries := BucketBoundaries()

	buckets := make([]CalibrationBucket, BucketCount)
	for i, b := range boundaries {
		buckets[i] = CalibrationBucket{
			MinConfidence: b[0],
			MaxConfidence: b[1],
		}
	}

	// Accumulate confidence sums for computing averages.
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

// ComputeECE calculates Expected Calibration Error.
//
// ECE = Σ |accuracy - avg_confidence| × (bucket_count / total_count)
//
// Only buckets with at least MinBucketSamples are included.
// If no qualifying buckets exist, returns 0 (fail-open).
func ComputeECE(buckets []CalibrationBucket) float64 {
	total := 0
	for _, b := range buckets {
		if b.Count >= MinBucketSamples {
			total += b.Count
		}
	}
	if total == 0 {
		return 0
	}

	ece := 0.0
	for _, b := range buckets {
		if b.Count < MinBucketSamples {
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

// ComputeCalibrationScores returns overconfidence and underconfidence scores.
//
// Overconfidence: weighted average of (avg_confidence - accuracy) where confidence > accuracy.
// Underconfidence: weighted average of (accuracy - avg_confidence) where accuracy > confidence.
//
// Only buckets with >= MinBucketSamples are considered.
func ComputeCalibrationScores(buckets []CalibrationBucket) (overconfidence, underconfidence float64) {
	overTotal := 0
	underTotal := 0

	for _, b := range buckets {
		if b.Count < MinBucketSamples {
			continue
		}
		gap := b.AvgConfidence - b.Accuracy
		if gap > 0 {
			// Overconfident: predicted higher than actual.
			overconfidence += gap * float64(b.Count)
			overTotal += b.Count
		} else if gap < 0 {
			// Underconfident: actual higher than predicted.
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

// BuildSummary computes a full CalibrationSummary from records.
// Deterministic: same records always produce the same summary.
func BuildSummary(records []CalibrationRecord) CalibrationSummary {
	buckets := BuildBuckets(records)
	ece := ComputeECE(buckets)
	over, under := ComputeCalibrationScores(buckets)

	lastUpdated := time.Time{}
	for _, r := range records {
		if r.CreatedAt.After(lastUpdated) {
			lastUpdated = r.CreatedAt
		}
	}

	return CalibrationSummary{
		Buckets:                  buckets,
		ExpectedCalibrationError: ece,
		OverconfidenceScore:      over,
		UnderconfidenceScore:     under,
		TotalRecords:             len(records),
		LastUpdated:              lastUpdated,
	}
}
