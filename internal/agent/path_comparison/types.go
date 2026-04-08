package pathcomparison

import "time"

// --- Decision Snapshot (captured at selection time) ---

// DecisionSnapshot captures all candidate paths and scores at the moment of path selection.
type DecisionSnapshot struct {
	DecisionID            string                  `json:"decision_id"`
	GoalType              string                  `json:"goal_type"`
	SelectedPathSignature string                  `json:"selected_path_signature"`
	SelectedScore         float64                 `json:"selected_score"`
	Candidates            []PathCandidateSnapshot `json:"candidates"`
	CreatedAt             time.Time               `json:"created_at"`
}

// PathCandidateSnapshot records a single candidate path's score and rank at decision time.
type PathCandidateSnapshot struct {
	PathSignature string  `json:"path_signature"`
	Score         float64 `json:"score"`
	Rank          int     `json:"rank"` // 1 = best, 2 = second, etc.
}

// --- Comparative Outcome (evaluated after outcome is known) ---

// ComparativeOutcome captures the comparison between the selected path and alternatives.
type ComparativeOutcome struct {
	DecisionID            string `json:"decision_id"`
	GoalType              string `json:"goal_type"`
	SelectedPathSignature string `json:"selected_path_signature"`

	SelectedOutcome string `json:"selected_outcome"` // success | neutral | failure

	RankingError            bool `json:"ranking_error"`
	Overestimated           bool `json:"overestimated"`
	Underestimated          bool `json:"underestimated"`
	BetterAlternativeExists bool `json:"better_alternative_exists"`

	CreatedAt time.Time `json:"created_at"`
}

// --- Comparative Memory Record ---

// ComparativeMemoryRecord holds accumulated win/loss/miss statistics for a path.
type ComparativeMemoryRecord struct {
	PathSignature  string    `json:"path_signature"`
	GoalType       string    `json:"goal_type"`
	SelectionCount int       `json:"selection_count"`
	WinCount       int       `json:"win_count"`        // chosen and performed well
	LossCount      int       `json:"loss_count"`       // chosen but worse than expected
	MissedWinCount int       `json:"missed_win_count"` // not chosen but likely better
	WinRate        float64   `json:"win_rate"`
	LossRate       float64   `json:"loss_rate"`
	LastUpdated    time.Time `json:"last_updated"`
}

// --- Comparative Feedback ---

// ComparativeRecommendation is a deterministic signal derived from comparative memory.
type ComparativeRecommendation string

const (
	ComparativePreferPath    ComparativeRecommendation = "prefer_path"
	ComparativeAvoidPath     ComparativeRecommendation = "avoid_path"
	ComparativeUnderexplored ComparativeRecommendation = "underexplored_path"
	ComparativeNeutral       ComparativeRecommendation = "neutral"
)

// ComparativeFeedback captures the comparative learning signal for a path.
type ComparativeFeedback struct {
	PathSignature  string                    `json:"path_signature"`
	GoalType       string                    `json:"goal_type"`
	WinRate        float64                   `json:"win_rate"`
	LossRate       float64                   `json:"loss_rate"`
	MissedWinCount int                       `json:"missed_win_count"`
	SelectionCount int                       `json:"selection_count"`
	Recommendation ComparativeRecommendation `json:"recommendation"`
}

// --- Outcome Status Constants ---

const (
	OutcomeSuccess = "success"
	OutcomeNeutral = "neutral"
	OutcomeFailure = "failure"
)

// --- Feedback Thresholds ---

const (
	// Comparative feedback thresholds.
	ComparativePreferWinRate     float64 = 0.7
	ComparativeAvoidLossRate     float64 = 0.5
	ComparativeMinSampleSize     int     = 5
	ComparativeUnderexploredMins int     = 3 // missed_win_count threshold

	// Score proximity threshold for "better alternative exists" detection.
	AlternativeScoreThreshold float64 = 0.1

	// Score thresholds for ranking error classification.
	HighScoreThreshold float64 = 0.5
	LowScoreThreshold  float64 = 0.3
)

// --- Score Adjustment Constants ---

const (
	ComparativePreferAdjustment        float64 = 0.10
	ComparativeAvoidAdjustment         float64 = -0.20
	ComparativeUnderexploredAdjustment float64 = 0.05
)

// --- Pure Feedback Generation ---

// GenerateComparativeFeedback produces a deterministic recommendation from a comparative memory record.
func GenerateComparativeFeedback(record *ComparativeMemoryRecord) ComparativeFeedback {
	fb := ComparativeFeedback{
		PathSignature:  record.PathSignature,
		GoalType:       record.GoalType,
		WinRate:        record.WinRate,
		LossRate:       record.LossRate,
		MissedWinCount: record.MissedWinCount,
		SelectionCount: record.SelectionCount,
		Recommendation: ComparativeNeutral,
	}

	// Underexplored: high missed wins regardless of sample size.
	if record.MissedWinCount >= ComparativeUnderexploredMins {
		fb.Recommendation = ComparativeUnderexplored
		return fb
	}

	if record.SelectionCount < ComparativeMinSampleSize {
		return fb
	}

	if record.WinRate >= ComparativePreferWinRate {
		fb.Recommendation = ComparativePreferPath
	} else if record.LossRate >= ComparativeAvoidLossRate {
		fb.Recommendation = ComparativeAvoidPath
	}

	return fb
}

// --- Classification Functions ---

// ClassifyRankingError determines if the selection was a ranking error.
//
// Overestimated: path scored high but outcome was failure.
// Underestimated: path scored low but outcome was success.
func ClassifyRankingError(selectedScore float64, selectedOutcome string) (rankingError, overestimated, underestimated bool) {
	overestimated = selectedScore >= HighScoreThreshold && selectedOutcome == OutcomeFailure
	underestimated = selectedScore <= LowScoreThreshold && selectedOutcome == OutcomeSuccess
	rankingError = overestimated || underestimated
	return
}

// DetectBetterAlternative checks if a better alternative may have existed.
//
// Conservative: only flags when selected path failed or was neutral AND
// another candidate had a similar score (within threshold).
func DetectBetterAlternative(snapshot DecisionSnapshot, selectedOutcome string) bool {
	if selectedOutcome == OutcomeSuccess {
		return false
	}

	for _, c := range snapshot.Candidates {
		if c.PathSignature == snapshot.SelectedPathSignature {
			continue
		}
		scoreDiff := snapshot.SelectedScore - c.Score
		if scoreDiff < AlternativeScoreThreshold {
			return true
		}
	}

	return false
}

// ClassifyWinLoss determines win/loss for the selected path.
//
// Win: selected path succeeded and no strong alternative.
// Loss: selected path failed OR better alternative existed.
func ClassifyWinLoss(selectedOutcome string, betterAlternativeExists bool) (win, loss bool) {
	if selectedOutcome == OutcomeSuccess && !betterAlternativeExists {
		win = true
		return
	}
	if selectedOutcome == OutcomeFailure || betterAlternativeExists {
		loss = true
		return
	}
	return // neutral: neither win nor loss
}
