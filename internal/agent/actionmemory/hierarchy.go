package actionmemory

import (
	"fmt"
	"math"
	"time"
)

// --- Hierarchy Levels ---
// 4 levels from most specific (L0) to most general (L3).
// Each level progressively drops dimensions, aggregating more records.

// HierarchyLevel identifies the generalization level of a hierarchical candidate.
type HierarchyLevel int

const (
	// HierarchyExact (L0): all available dimensions match.
	// Provider path: action+goal+provider+model+failure+backlog.
	// Context path: action+goal+failure+backlog.
	HierarchyExact HierarchyLevel = 0

	// HierarchyReduced (L1): action+goal+failure_bucket.
	// Aggregates across provider, model_role, backlog_bucket.
	HierarchyReduced HierarchyLevel = 1

	// HierarchyGeneralized (L2): action+goal only.
	// Aggregates across all other dimensions.
	HierarchyGeneralized HierarchyLevel = 2

	// HierarchyGlobal (L3): action only (from global feedback map).
	HierarchyGlobal HierarchyLevel = 3
)

// String returns a human-readable label for the hierarchy level.
func (l HierarchyLevel) String() string {
	switch l {
	case HierarchyExact:
		return "L0_exact"
	case HierarchyReduced:
		return "L1_reduced"
	case HierarchyGeneralized:
		return "L2_generalized"
	case HierarchyGlobal:
		return "L3_global"
	default:
		return "unknown"
	}
}

// --- Specificity Bonuses ---
// Small tie-breaking bonuses for more specific levels. Intentionally small —
// never enough to overcome a meaningful confidence gap. Only relevant when
// confidence is NOT within the similarity threshold.

const (
	hierBonusExact       = 0.03
	hierBonusReduced     = 0.02
	hierBonusGeneralized = 0.01
	hierBonusGlobal      = 0.00

	// hierSimilarityThreshold: when two candidates' confidence values
	// differ by less than this, the simpler (more general) level is preferred.
	// This prevents exact matches with marginally better confidence from
	// dominating when a generalized signal is nearly as strong.
	hierSimilarityThreshold = 0.05
)

func hierarchySpecificityBonus(level HierarchyLevel) float64 {
	switch level {
	case HierarchyExact:
		return hierBonusExact
	case HierarchyReduced:
		return hierBonusReduced
	case HierarchyGeneralized:
		return hierBonusGeneralized
	case HierarchyGlobal:
		return hierBonusGlobal
	default:
		return 0.00
	}
}

// --- Hierarchical Candidate ---

// HierarchicalCandidate represents a feedback signal at one hierarchy level,
// with its computed confidence, specificity bias, and final score.
type HierarchicalCandidate struct {
	Level           HierarchyLevel `json:"level"`
	LevelName       string         `json:"level_name"`
	Recommendation  Recommendation `json:"recommendation"`
	Confidence      float64        `json:"confidence"`
	RecencyWeight   float64        `json:"recency_weight"`
	SampleWeight    float64        `json:"sample_weight"`
	SpecificityBias float64        `json:"specificity_bias"`
	FinalScore      float64        `json:"final_score"` // confidence + specificity_bias
	SampleSize      int            `json:"sample_size"`
	SuccessRate     float64        `json:"success_rate"`
	FailureRate     float64        `json:"failure_rate"`
	ActionType      string         `json:"action_type"`
	MatchDimensions []string       `json:"match_dimensions"`
}

// --- Record Aggregator ---

// recordAggregator accumulates run statistics across multiple records
// at a given hierarchy level.
type recordAggregator struct {
	totalRuns   int
	successRuns int
	failureRuns int
	neutralRuns int
	latest      time.Time
}

func (a *recordAggregator) addProvider(r *ProviderContextMemoryRecord) {
	a.totalRuns += r.TotalRuns
	a.successRuns += r.SuccessRuns
	a.failureRuns += r.FailureRuns
	a.neutralRuns += r.NeutralRuns
	if r.LastUpdated.After(a.latest) {
		a.latest = r.LastUpdated
	}
}

func (a *recordAggregator) addContext(r *ContextMemoryRecord) {
	a.totalRuns += r.TotalRuns
	a.successRuns += r.SuccessRuns
	a.failureRuns += r.FailureRuns
	a.neutralRuns += r.NeutralRuns
	if r.LastUpdated.After(a.latest) {
		a.latest = r.LastUpdated
	}
}

// build produces a HierarchicalCandidate from the aggregated data,
// or nil if no records were accumulated.
func (a *recordAggregator) build(level HierarchyLevel, actionType string, dims []string, now time.Time) *HierarchicalCandidate {
	if a.totalRuns == 0 {
		return nil
	}
	successRate := float64(a.successRuns) / float64(a.totalRuns)
	failureRate := float64(a.failureRuns) / float64(a.totalRuns)

	rec := &ActionMemoryRecord{
		ActionType:  actionType,
		TotalRuns:   a.totalRuns,
		SuccessRuns: a.successRuns,
		FailureRuns: a.failureRuns,
		NeutralRuns: a.neutralRuns,
		SuccessRate: successRate,
		FailureRate: failureRate,
		LastUpdated: a.latest,
	}
	fb := GenerateFeedback(rec)

	rw := RecencyWeight(a.latest, now)
	sw := SampleWeight(a.totalRuns)
	conf := EvidenceConfidence(rw, sw)
	bias := hierarchySpecificityBonus(level)

	return &HierarchicalCandidate{
		Level:           level,
		LevelName:       level.String(),
		Recommendation:  fb.Recommendation,
		Confidence:      conf,
		RecencyWeight:   rw,
		SampleWeight:    sw,
		SpecificityBias: bias,
		FinalScore:      conf + bias,
		SampleSize:      a.totalRuns,
		SuccessRate:     successRate,
		FailureRate:     failureRate,
		ActionType:      actionType,
		MatchDimensions: dims,
	}
}

// --- Gather Hierarchical Candidates ---

// GatherHierarchicalCandidates generates candidates at all 4 hierarchy levels
// by progressively dropping dimensions and aggregating records.
//
// Levels:
//
//	L0 (exact):       all available dims match
//	L1 (reduced):     action+goal+failure_bucket (aggregate across provider/model/backlog)
//	L2 (generalized): action+goal (aggregate across all other dims)
//	L3 (global):      action only (from global feedback map)
func GatherHierarchicalCandidates(
	providerRecords []ProviderContextMemoryRecord,
	contextRecords []ContextMemoryRecord,
	globalFeedback map[string]ActionFeedback,
	actionType, goalType string,
	providerName, modelRole string,
	failureBucket, backlogBucket string,
	now time.Time,
) []HierarchicalCandidate {
	var candidates []HierarchicalCandidate

	// L0: Exact match (all available dimensions).
	if c := gatherL0(providerRecords, contextRecords, actionType, goalType,
		providerName, modelRole, failureBucket, backlogBucket, now); c != nil {
		candidates = append(candidates, *c)
	}

	// L1: Reduced (action+goal+failure_bucket).
	if c := gatherL1(providerRecords, contextRecords, actionType, goalType,
		failureBucket, now); c != nil {
		candidates = append(candidates, *c)
	}

	// L2: Generalized (action+goal).
	if c := gatherL2(providerRecords, contextRecords, actionType, goalType, now); c != nil {
		candidates = append(candidates, *c)
	}

	// L3: Global (action only).
	if c := gatherL3(globalFeedback, actionType, now); c != nil {
		candidates = append(candidates, *c)
	}

	return candidates
}

// gatherL0 finds an exact-match record. Tries provider-context exact first
// (6-dim), then context exact (4-dim).
func gatherL0(
	providerRecords []ProviderContextMemoryRecord,
	contextRecords []ContextMemoryRecord,
	actionType, goalType string,
	providerName, modelRole string,
	failureBucket, backlogBucket string,
	now time.Time,
) *HierarchicalCandidate {
	if providerName != "" {
		for i := range providerRecords {
			r := &providerRecords[i]
			if r.ActionType == actionType && r.GoalType == goalType &&
				r.ProviderName == providerName && r.ModelRole == modelRole &&
				r.FailureBucket == failureBucket && r.BacklogBucket == backlogBucket {
				var agg recordAggregator
				agg.addProvider(r)
				return agg.build(HierarchyExact, actionType,
					[]string{"action_type", "goal_type", "provider_name", "model_role", "failure_bucket", "backlog_bucket"}, now)
			}
		}
	}

	for i := range contextRecords {
		r := &contextRecords[i]
		if r.ActionType == actionType && r.GoalType == goalType &&
			r.FailureBucket == failureBucket && r.BacklogBucket == backlogBucket {
			var agg recordAggregator
			agg.addContext(r)
			return agg.build(HierarchyExact, actionType,
				[]string{"action_type", "goal_type", "failure_bucket", "backlog_bucket"}, now)
		}
	}

	return nil
}

// gatherL1 aggregates all records matching action+goal+failure_bucket,
// combining both provider-context and context records.
func gatherL1(
	providerRecords []ProviderContextMemoryRecord,
	contextRecords []ContextMemoryRecord,
	actionType, goalType string,
	failureBucket string,
	now time.Time,
) *HierarchicalCandidate {
	var agg recordAggregator

	for i := range providerRecords {
		r := &providerRecords[i]
		if r.ActionType == actionType && r.GoalType == goalType && r.FailureBucket == failureBucket {
			agg.addProvider(r)
		}
	}

	for i := range contextRecords {
		r := &contextRecords[i]
		if r.ActionType == actionType && r.GoalType == goalType && r.FailureBucket == failureBucket {
			agg.addContext(r)
		}
	}

	return agg.build(HierarchyReduced, actionType, []string{"action_type", "goal_type", "failure_bucket"}, now)
}

// gatherL2 aggregates all records matching action+goal,
// combining both provider-context and context records.
func gatherL2(
	providerRecords []ProviderContextMemoryRecord,
	contextRecords []ContextMemoryRecord,
	actionType, goalType string,
	now time.Time,
) *HierarchicalCandidate {
	var agg recordAggregator

	for i := range providerRecords {
		r := &providerRecords[i]
		if r.ActionType == actionType && r.GoalType == goalType {
			agg.addProvider(r)
		}
	}

	for i := range contextRecords {
		r := &contextRecords[i]
		if r.ActionType == actionType && r.GoalType == goalType {
			agg.addContext(r)
		}
	}

	return agg.build(HierarchyGeneralized, actionType, []string{"action_type", "goal_type"}, now)
}

// gatherL3 produces a global-level candidate from the global feedback map.
func gatherL3(
	globalFeedback map[string]ActionFeedback,
	actionType string,
	now time.Time,
) *HierarchicalCandidate {
	gfb, ok := globalFeedback[actionType]
	if !ok {
		return nil
	}

	rw := RecencyWeight(gfb.LastUpdated, now)
	sw := SampleWeight(gfb.SampleSize)
	conf := EvidenceConfidence(rw, sw)
	bias := hierarchySpecificityBonus(HierarchyGlobal)

	return &HierarchicalCandidate{
		Level:           HierarchyGlobal,
		LevelName:       HierarchyGlobal.String(),
		Recommendation:  gfb.Recommendation,
		Confidence:      conf,
		RecencyWeight:   rw,
		SampleWeight:    sw,
		SpecificityBias: bias,
		FinalScore:      conf + bias,
		SampleSize:      gfb.SampleSize,
		SuccessRate:     gfb.SuccessRate,
		FailureRate:     gfb.FailureRate,
		ActionType:      actionType,
		MatchDimensions: []string{"action_type"},
	}
}

// --- Hierarchical Resolution ---

// ResolveHierarchicalFeedback selects the best feedback signal from
// hierarchical candidates using confidence-based selection with
// simplicity preference.
//
// Resolution rules:
//  1. Discard insufficient_data and neutral recommendations.
//  2. When two candidates have similar confidence (within hierSimilarityThreshold),
//     prefer the simpler (more general) level.
//  3. When confidence differs meaningfully, select highest FinalScore.
//  4. Returns nil as best when no actionable feedback exists.
func ResolveHierarchicalFeedback(candidates []HierarchicalCandidate) (*HierarchicalCandidate, []HierarchicalCandidate) {
	if len(candidates) == 0 {
		return nil, nil
	}

	var actionable []HierarchicalCandidate
	for _, c := range candidates {
		if c.Recommendation == RecommendInsufficientData || c.Recommendation == RecommendNeutral {
			continue
		}
		actionable = append(actionable, c)
	}

	if len(actionable) == 0 {
		return nil, candidates
	}

	best := actionable[0]
	for _, c := range actionable[1:] {
		if hierShouldPrefer(c, best) {
			best = c
		}
	}

	return &best, candidates
}

// hierShouldPrefer returns true if candidate c should be preferred over
// the current best. Implements the "prefer simpler when similar" rule.
//
// When recommendations agree and confidence is similar, simpler levels win.
// When recommendations conflict, FinalScore (confidence + specificity bonus)
// decides — giving a small edge to more specific levels.
func hierShouldPrefer(c, best HierarchicalCandidate) bool {
	confDiff := math.Abs(c.Confidence - best.Confidence)

	// When confidence is similar AND recommendations agree,
	// prefer the simpler (higher level number) level.
	if confDiff <= hierSimilarityThreshold && c.Recommendation == best.Recommendation {
		if c.Level > best.Level {
			return true
		}
		if c.Level < best.Level {
			return false
		}
		// Same level: higher FinalScore wins.
		return c.FinalScore > best.FinalScore
	}

	// When recommendations conflict or confidence differs meaningfully,
	// use FinalScore (confidence + specificity bonus).
	return c.FinalScore > best.FinalScore
}

// --- Score Adjustment ---

// HierarchicalScoreAdjustment computes the score adjustment from a resolved
// hierarchical feedback signal. The adjustment is scaled by confidence.
func HierarchicalScoreAdjustment(hc *HierarchicalCandidate, avoidPenalty, preferBoost float64) (float64, string) {
	if hc == nil {
		return 0, ""
	}

	rawAdj := feedbackAdjustment(hc.Recommendation, avoidPenalty, preferBoost)
	adj := rawAdj * hc.Confidence

	return adj, fmt.Sprintf(
		"hierarchical %s %s: raw=%.2f × confidence=%.2f → %.2f (level=%s, samples=%d, score=%.2f)",
		hc.LevelName, hc.Recommendation,
		rawAdj, hc.Confidence, adj,
		hc.LevelName, hc.SampleSize, hc.FinalScore,
	)
}
