package pathcomparison

import (
	"testing"
	"time"
)

// --- Snapshot Capture Tests ---

func TestCaptureSnapshot_AllCandidatesCaptured(t *testing.T) {
	paths := []ScoredPathInfo{
		{PathSignature: "retry_job", Score: 0.8},
		{PathSignature: "log_recommendation", Score: 0.6},
		{PathSignature: "noop", Score: 0.01},
	}

	snap := CaptureSnapshot("d1", "reduce_retry_rate", paths, "retry_job", 0.8)

	if snap.DecisionID != "d1" {
		t.Errorf("expected decision_id d1, got %s", snap.DecisionID)
	}
	if snap.GoalType != "reduce_retry_rate" {
		t.Errorf("expected goal_type reduce_retry_rate, got %s", snap.GoalType)
	}
	if snap.SelectedPathSignature != "retry_job" {
		t.Errorf("expected selected retry_job, got %s", snap.SelectedPathSignature)
	}
	if snap.SelectedScore != 0.8 {
		t.Errorf("expected selected_score 0.8, got %f", snap.SelectedScore)
	}
	if len(snap.Candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(snap.Candidates))
	}
}

func TestCaptureSnapshot_RankingOrder(t *testing.T) {
	paths := []ScoredPathInfo{
		{PathSignature: "noop", Score: 0.01},
		{PathSignature: "retry_job", Score: 0.8},
		{PathSignature: "log_recommendation", Score: 0.6},
	}

	snap := CaptureSnapshot("d1", "test", paths, "retry_job", 0.8)

	// Rank 1 should be highest score.
	if snap.Candidates[0].Rank != 1 || snap.Candidates[0].PathSignature != "retry_job" {
		t.Errorf("rank 1 should be retry_job, got %s rank %d", snap.Candidates[0].PathSignature, snap.Candidates[0].Rank)
	}
	if snap.Candidates[1].Rank != 2 || snap.Candidates[1].PathSignature != "log_recommendation" {
		t.Errorf("rank 2 should be log_recommendation, got %s rank %d", snap.Candidates[1].PathSignature, snap.Candidates[1].Rank)
	}
	if snap.Candidates[2].Rank != 3 || snap.Candidates[2].PathSignature != "noop" {
		t.Errorf("rank 3 should be noop, got %s rank %d", snap.Candidates[2].PathSignature, snap.Candidates[2].Rank)
	}
}

func TestCaptureSnapshot_Deterministic(t *testing.T) {
	paths := []ScoredPathInfo{
		{PathSignature: "a", Score: 0.5},
		{PathSignature: "b", Score: 0.5},
	}

	snap1 := CaptureSnapshot("d1", "test", paths, "a", 0.5)
	snap2 := CaptureSnapshot("d1", "test", paths, "a", 0.5)

	// Tie-break: alphabetical by PathSignature.
	if snap1.Candidates[0].PathSignature != snap2.Candidates[0].PathSignature {
		t.Error("snapshot capture should be deterministic on tied scores")
	}
}

// --- Ranking Error Classification Tests ---

func TestClassifyRankingError_Overestimated(t *testing.T) {
	rankingErr, over, under := ClassifyRankingError(0.7, OutcomeFailure)

	if !rankingErr {
		t.Error("expected ranking error")
	}
	if !over {
		t.Error("expected overestimated")
	}
	if under {
		t.Error("expected not underestimated")
	}
}

func TestClassifyRankingError_Underestimated(t *testing.T) {
	rankingErr, over, under := ClassifyRankingError(0.2, OutcomeSuccess)

	if !rankingErr {
		t.Error("expected ranking error")
	}
	if over {
		t.Error("expected not overestimated")
	}
	if !under {
		t.Error("expected underestimated")
	}
}

func TestClassifyRankingError_NoError(t *testing.T) {
	rankingErr, over, under := ClassifyRankingError(0.7, OutcomeSuccess)

	if rankingErr {
		t.Error("expected no ranking error")
	}
	if over {
		t.Error("expected not overestimated")
	}
	if under {
		t.Error("expected not underestimated")
	}
}

func TestClassifyRankingError_NeutralOutcome(t *testing.T) {
	rankingErr, _, _ := ClassifyRankingError(0.7, OutcomeNeutral)

	if rankingErr {
		t.Error("neutral outcome should not be a ranking error")
	}
}

// --- Better Alternative Detection Tests ---

func TestDetectBetterAlternative_FailureWithCloseAlternative(t *testing.T) {
	snap := DecisionSnapshot{
		SelectedPathSignature: "retry_job",
		SelectedScore:         0.6,
		Candidates: []PathCandidateSnapshot{
			{PathSignature: "retry_job", Score: 0.6, Rank: 1},
			{PathSignature: "log_recommendation", Score: 0.55, Rank: 2},
		},
	}

	result := DetectBetterAlternative(snap, OutcomeFailure)
	if !result {
		t.Error("expected better alternative when selected failed and close alternative exists")
	}
}

func TestDetectBetterAlternative_SuccessOutcome(t *testing.T) {
	snap := DecisionSnapshot{
		SelectedPathSignature: "retry_job",
		SelectedScore:         0.6,
		Candidates: []PathCandidateSnapshot{
			{PathSignature: "retry_job", Score: 0.6, Rank: 1},
			{PathSignature: "log_recommendation", Score: 0.55, Rank: 2},
		},
	}

	result := DetectBetterAlternative(snap, OutcomeSuccess)
	if result {
		t.Error("should not detect better alternative when selected succeeded")
	}
}

func TestDetectBetterAlternative_NoCloseAlternative(t *testing.T) {
	snap := DecisionSnapshot{
		SelectedPathSignature: "retry_job",
		SelectedScore:         0.8,
		Candidates: []PathCandidateSnapshot{
			{PathSignature: "retry_job", Score: 0.8, Rank: 1},
			{PathSignature: "log_recommendation", Score: 0.3, Rank: 2},
		},
	}

	result := DetectBetterAlternative(snap, OutcomeFailure)
	if result {
		t.Error("should not detect better alternative when gap is large")
	}
}

// --- Win/Loss Classification Tests ---

func TestClassifyWinLoss_Win(t *testing.T) {
	win, loss := ClassifyWinLoss(OutcomeSuccess, false)

	if !win {
		t.Error("expected win")
	}
	if loss {
		t.Error("expected not loss")
	}
}

func TestClassifyWinLoss_LossFromFailure(t *testing.T) {
	win, loss := ClassifyWinLoss(OutcomeFailure, false)

	if win {
		t.Error("expected not win")
	}
	if !loss {
		t.Error("expected loss")
	}
}

func TestClassifyWinLoss_LossFromBetterAlternative(t *testing.T) {
	win, loss := ClassifyWinLoss(OutcomeNeutral, true)

	if win {
		t.Error("expected not win")
	}
	if !loss {
		t.Error("expected loss when better alternative exists")
	}
}

func TestClassifyWinLoss_SuccessButBetterAlternative(t *testing.T) {
	// Success overrides better alternative: still not a loss, but not a clean win.
	win, loss := ClassifyWinLoss(OutcomeSuccess, true)

	// When outcome is success but better alternative exists: not a win, not a loss.
	if win {
		t.Error("expected not win when better alternative exists despite success")
	}
	if loss {
		t.Error("success outcome should not be a loss even with better alternative")
	}
}

func TestClassifyWinLoss_Neutral(t *testing.T) {
	win, loss := ClassifyWinLoss(OutcomeNeutral, false)

	if win {
		t.Error("expected not win for neutral")
	}
	if loss {
		t.Error("expected not loss for neutral without alternative")
	}
}

// --- Comparative Feedback Generation Tests ---

func TestGenerateComparativeFeedback_PreferPath(t *testing.T) {
	record := &ComparativeMemoryRecord{
		PathSignature:  "retry_job",
		GoalType:       "test",
		SelectionCount: 10,
		WinCount:       8,
		LossCount:      1,
		MissedWinCount: 0,
		WinRate:        0.8,
		LossRate:       0.1,
	}

	fb := GenerateComparativeFeedback(record)

	if fb.Recommendation != ComparativePreferPath {
		t.Errorf("expected prefer_path, got %s", fb.Recommendation)
	}
}

func TestGenerateComparativeFeedback_AvoidPath(t *testing.T) {
	record := &ComparativeMemoryRecord{
		PathSignature:  "retry_job",
		GoalType:       "test",
		SelectionCount: 10,
		WinCount:       1,
		LossCount:      6,
		MissedWinCount: 0,
		WinRate:        0.1,
		LossRate:       0.6,
	}

	fb := GenerateComparativeFeedback(record)

	if fb.Recommendation != ComparativeAvoidPath {
		t.Errorf("expected avoid_path, got %s", fb.Recommendation)
	}
}

func TestGenerateComparativeFeedback_Underexplored(t *testing.T) {
	record := &ComparativeMemoryRecord{
		PathSignature:  "retry_job",
		GoalType:       "test",
		SelectionCount: 2,
		WinCount:       0,
		LossCount:      0,
		MissedWinCount: 3,
		WinRate:        0,
		LossRate:       0,
	}

	fb := GenerateComparativeFeedback(record)

	if fb.Recommendation != ComparativeUnderexplored {
		t.Errorf("expected underexplored_path, got %s", fb.Recommendation)
	}
}

func TestGenerateComparativeFeedback_Neutral(t *testing.T) {
	record := &ComparativeMemoryRecord{
		PathSignature:  "retry_job",
		GoalType:       "test",
		SelectionCount: 10,
		WinCount:       4,
		LossCount:      3,
		MissedWinCount: 1,
		WinRate:        0.4,
		LossRate:       0.3,
	}

	fb := GenerateComparativeFeedback(record)

	if fb.Recommendation != ComparativeNeutral {
		t.Errorf("expected neutral, got %s", fb.Recommendation)
	}
}

func TestGenerateComparativeFeedback_InsufficientData(t *testing.T) {
	record := &ComparativeMemoryRecord{
		PathSignature:  "retry_job",
		GoalType:       "test",
		SelectionCount: 3,
		WinCount:       3,
		LossCount:      0,
		MissedWinCount: 0,
		WinRate:        1.0,
		LossRate:       0,
	}

	fb := GenerateComparativeFeedback(record)

	if fb.Recommendation != ComparativeNeutral {
		t.Errorf("expected neutral for insufficient data, got %s", fb.Recommendation)
	}
}

func TestGenerateComparativeFeedback_UnderexploredTakesPriority(t *testing.T) {
	// Underexplored should take priority over prefer/avoid even with enough samples.
	record := &ComparativeMemoryRecord{
		PathSignature:  "retry_job",
		GoalType:       "test",
		SelectionCount: 10,
		WinCount:       8,
		LossCount:      1,
		MissedWinCount: 5,
		WinRate:        0.8,
		LossRate:       0.1,
	}

	fb := GenerateComparativeFeedback(record)

	if fb.Recommendation != ComparativeUnderexplored {
		t.Errorf("expected underexplored to take priority, got %s", fb.Recommendation)
	}
}

// --- Score Adjustment Constants Tests ---

func TestComparativeAdjustmentConstants(t *testing.T) {
	if ComparativePreferAdjustment != 0.10 {
		t.Errorf("expected prefer adjustment 0.10, got %f", ComparativePreferAdjustment)
	}
	if ComparativeAvoidAdjustment != -0.20 {
		t.Errorf("expected avoid adjustment -0.20, got %f", ComparativeAvoidAdjustment)
	}
	if ComparativeUnderexploredAdjustment != 0.05 {
		t.Errorf("expected underexplored adjustment 0.05, got %f", ComparativeUnderexploredAdjustment)
	}
}

// --- Threshold Tests ---

func TestComparativeThresholds(t *testing.T) {
	if ComparativePreferWinRate != 0.7 {
		t.Errorf("expected prefer win rate 0.7, got %f", ComparativePreferWinRate)
	}
	if ComparativeAvoidLossRate != 0.5 {
		t.Errorf("expected avoid loss rate 0.5, got %f", ComparativeAvoidLossRate)
	}
	if ComparativeMinSampleSize != 5 {
		t.Errorf("expected min sample size 5, got %d", ComparativeMinSampleSize)
	}
	if ComparativeUnderexploredMins != 3 {
		t.Errorf("expected underexplored mins 3, got %d", ComparativeUnderexploredMins)
	}
}

// --- Edge Cases ---

func TestClassifyRankingError_AtThresholds(t *testing.T) {
	// Exactly at high score threshold with failure → overestimated.
	_, over, _ := ClassifyRankingError(HighScoreThreshold, OutcomeFailure)
	if !over {
		t.Error("expected overestimated at high score threshold with failure")
	}

	// Exactly at low score threshold with success → underestimated.
	_, _, under := ClassifyRankingError(LowScoreThreshold, OutcomeSuccess)
	if !under {
		t.Error("expected underestimated at low score threshold with success")
	}
}

func TestDetectBetterAlternative_ExactThreshold(t *testing.T) {
	// Score diff exactly at threshold (0.1) → not a better alternative.
	snap := DecisionSnapshot{
		SelectedPathSignature: "a",
		SelectedScore:         0.5,
		Candidates: []PathCandidateSnapshot{
			{PathSignature: "a", Score: 0.5, Rank: 1},
			{PathSignature: "b", Score: 0.4, Rank: 2},
		},
	}

	result := DetectBetterAlternative(snap, OutcomeFailure)
	if result {
		t.Error("exactly at threshold should not detect better alternative")
	}
}

func TestDetectBetterAlternative_JustBelowThreshold(t *testing.T) {
	// Score diff just below threshold → better alternative exists.
	snap := DecisionSnapshot{
		SelectedPathSignature: "a",
		SelectedScore:         0.5,
		Candidates: []PathCandidateSnapshot{
			{PathSignature: "a", Score: 0.5, Rank: 1},
			{PathSignature: "b", Score: 0.41, Rank: 2},
		},
	}

	result := DetectBetterAlternative(snap, OutcomeFailure)
	if !result {
		t.Error("just below threshold should detect better alternative")
	}
}

func TestCaptureSnapshot_EmptyCandidates(t *testing.T) {
	snap := CaptureSnapshot("d1", "test", nil, "", 0)

	if len(snap.Candidates) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(snap.Candidates))
	}
}

func TestCaptureSnapshot_SingleCandidate(t *testing.T) {
	paths := []ScoredPathInfo{
		{PathSignature: "retry_job", Score: 0.5},
	}

	snap := CaptureSnapshot("d1", "test", paths, "retry_job", 0.5)

	if len(snap.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(snap.Candidates))
	}
	if snap.Candidates[0].Rank != 1 {
		t.Errorf("expected rank 1, got %d", snap.Candidates[0].Rank)
	}
}

func TestGenerateComparativeFeedback_AtPreferThreshold(t *testing.T) {
	record := &ComparativeMemoryRecord{
		PathSignature:  "a",
		GoalType:       "test",
		SelectionCount: 10,
		WinCount:       7,
		LossCount:      0,
		WinRate:        0.7,
		LossRate:       0,
	}

	fb := GenerateComparativeFeedback(record)

	if fb.Recommendation != ComparativePreferPath {
		t.Errorf("expected prefer_path at threshold, got %s", fb.Recommendation)
	}
}

func TestGenerateComparativeFeedback_BelowPreferThreshold(t *testing.T) {
	record := &ComparativeMemoryRecord{
		PathSignature:  "a",
		GoalType:       "test",
		SelectionCount: 10,
		WinCount:       6,
		LossCount:      0,
		WinRate:        0.6,
		LossRate:       0,
	}

	fb := GenerateComparativeFeedback(record)

	if fb.Recommendation != ComparativeNeutral {
		t.Errorf("expected neutral below threshold, got %s", fb.Recommendation)
	}
}

func TestSnapshotTimestamp(t *testing.T) {
	before := time.Now().UTC()
	snap := CaptureSnapshot("d1", "test", nil, "", 0)
	after := time.Now().UTC()

	if snap.CreatedAt.Before(before) || snap.CreatedAt.After(after) {
		t.Error("snapshot timestamp should be within test bounds")
	}
}

// --- Determinism Tests ---

func TestClassifyRankingError_Deterministic(t *testing.T) {
	for i := 0; i < 100; i++ {
		r1, o1, u1 := ClassifyRankingError(0.7, OutcomeFailure)
		r2, o2, u2 := ClassifyRankingError(0.7, OutcomeFailure)

		if r1 != r2 || o1 != o2 || u1 != u2 {
			t.Fatal("ranking error classification must be deterministic")
		}
	}
}

func TestClassifyWinLoss_Deterministic(t *testing.T) {
	for i := 0; i < 100; i++ {
		w1, l1 := ClassifyWinLoss(OutcomeFailure, true)
		w2, l2 := ClassifyWinLoss(OutcomeFailure, true)

		if w1 != w2 || l1 != l2 {
			t.Fatal("win/loss classification must be deterministic")
		}
	}
}

func TestDetectBetterAlternative_Deterministic(t *testing.T) {
	snap := DecisionSnapshot{
		SelectedPathSignature: "a",
		SelectedScore:         0.6,
		Candidates: []PathCandidateSnapshot{
			{PathSignature: "a", Score: 0.6, Rank: 1},
			{PathSignature: "b", Score: 0.55, Rank: 2},
		},
	}

	for i := 0; i < 100; i++ {
		r1 := DetectBetterAlternative(snap, OutcomeFailure)
		r2 := DetectBetterAlternative(snap, OutcomeFailure)

		if r1 != r2 {
			t.Fatal("better alternative detection must be deterministic")
		}
	}
}
