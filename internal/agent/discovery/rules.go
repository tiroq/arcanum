package discovery

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// DiscoveryInput aggregates all input data for a discovery run.
type DiscoveryInput struct {
	Signals   []SignalRecord
	Outcomes  []OutcomeRecord
	Proposals []ProposalRecord
}

// Rule is a deterministic discovery rule that evaluates operational patterns
// and produces discovery candidates.
type Rule interface {
	Name() string
	Evaluate(ctx context.Context, input DiscoveryInput) (DiscoveryRuleResult, []DiscoveryCandidate)
}

// --- Rule 1: Repeated Manual Work ---

// RepeatedManualWorkRule detects repeated manual task patterns and creates
// automation candidates.
type RepeatedManualWorkRule struct {
	threshold int
}

// NewRepeatedManualWorkRule creates a rule with the given threshold.
func NewRepeatedManualWorkRule(threshold int) *RepeatedManualWorkRule {
	return &RepeatedManualWorkRule{threshold: threshold}
}

func (r *RepeatedManualWorkRule) Name() string { return "repeated_manual_work" }

func (r *RepeatedManualWorkRule) Evaluate(_ context.Context, input DiscoveryInput) (DiscoveryRuleResult, []DiscoveryCandidate) {
	// Count outcomes grouped by action type where mode indicates manual/operator work.
	counts := map[string]int{}
	for _, o := range input.Outcomes {
		if isManualAction(o.ActionType) {
			counts[o.ActionType]++
		}
	}

	var candidates []DiscoveryCandidate
	totalEvidence := 0

	for actionType, count := range counts {
		if count >= r.threshold {
			totalEvidence += count
			candidates = append(candidates, DiscoveryCandidate{
				ID:              uuid.New().String(),
				CandidateType:   CandidateAutomation,
				SourceType:      SourceRepeatedManualWork,
				SourceRefs:      []string{fmt.Sprintf("action_type:%s", actionType)},
				Title:           fmt.Sprintf("Automate repeated manual work: %s", actionType),
				Description:     fmt.Sprintf("Action type %q has been executed %d times in the discovery window, suggesting automation potential.", actionType, count),
				Confidence:      confidenceFromCount(count, r.threshold),
				EstimatedValue:  estimateAutomationValue(count),
				EstimatedEffort: 0.4, // moderate effort default
				DedupeKey:       fmt.Sprintf("manual_work:%s", actionType),
				EvidenceCount:   count,
			})
		}
	}

	return DiscoveryRuleResult{
		RuleName:               r.Name(),
		Matched:                len(candidates) > 0,
		EvidenceCount:          totalEvidence,
		Confidence:             bestConfidence(candidates),
		GeneratedCandidateType: CandidateAutomation,
	}, candidates
}

// --- Rule 2: Repeated Solved Issue ---

// RepeatedSolvedIssueRule detects repeated resolution of similar issues.
type RepeatedSolvedIssueRule struct {
	threshold int
}

// NewRepeatedSolvedIssueRule creates a rule with the given threshold.
func NewRepeatedSolvedIssueRule(threshold int) *RepeatedSolvedIssueRule {
	return &RepeatedSolvedIssueRule{threshold: threshold}
}

func (r *RepeatedSolvedIssueRule) Name() string { return "repeated_solved_issue" }

func (r *RepeatedSolvedIssueRule) Evaluate(_ context.Context, input DiscoveryInput) (DiscoveryRuleResult, []DiscoveryCandidate) {
	// Count succeeded outcomes grouped by goal type (problem category).
	counts := map[string]int{}
	for _, o := range input.Outcomes {
		if o.Status == "succeeded" && o.GoalType != "" {
			counts[o.GoalType]++
		}
	}

	var candidates []DiscoveryCandidate
	totalEvidence := 0

	for goalType, count := range counts {
		if count >= r.threshold {
			totalEvidence += count
			candidates = append(candidates, DiscoveryCandidate{
				ID:              uuid.New().String(),
				CandidateType:   CandidateResaleRepackage,
				SourceType:      SourceRepeatedSolvedIssue,
				SourceRefs:      []string{fmt.Sprintf("goal_type:%s", goalType)},
				Title:           fmt.Sprintf("Package repeatable solution: %s", goalType),
				Description:     fmt.Sprintf("Goal type %q has been successfully resolved %d times, suggesting a repackageable or automatable solution.", goalType, count),
				Confidence:      confidenceFromCount(count, r.threshold),
				EstimatedValue:  estimateRepackageValue(count),
				EstimatedEffort: 0.5,
				DedupeKey:       fmt.Sprintf("solved_issue:%s", goalType),
				EvidenceCount:   count,
			})
		}
	}

	return DiscoveryRuleResult{
		RuleName:               r.Name(),
		Matched:                len(candidates) > 0,
		EvidenceCount:          totalEvidence,
		Confidence:             bestConfidence(candidates),
		GeneratedCandidateType: CandidateResaleRepackage,
	}, candidates
}

// --- Rule 3: Inbound Need ---

// InboundNeedRule detects inbound request signals that suggest consulting leads.
type InboundNeedRule struct {
	threshold int
}

// NewInboundNeedRule creates a rule with the given threshold.
func NewInboundNeedRule(threshold int) *InboundNeedRule {
	return &InboundNeedRule{threshold: threshold}
}

func (r *InboundNeedRule) Name() string { return "inbound_need" }

func (r *InboundNeedRule) Evaluate(_ context.Context, input DiscoveryInput) (DiscoveryRuleResult, []DiscoveryCandidate) {
	// Look for new_opportunity signals or manually ingested signals.
	var matchingSignals []SignalRecord
	for _, s := range input.Signals {
		if s.SignalType == "new_opportunity" || s.Source == "manual" || s.Source == "external" {
			matchingSignals = append(matchingSignals, s)
		}
	}

	var candidates []DiscoveryCandidate

	if len(matchingSignals) >= r.threshold {
		// Group by source for dedup.
		sourceGroups := map[string]int{}
		for _, s := range matchingSignals {
			key := s.Source
			if s.SignalType == "new_opportunity" {
				key = "new_opportunity"
			}
			sourceGroups[key]++
		}

		for source, count := range sourceGroups {
			candidates = append(candidates, DiscoveryCandidate{
				ID:              uuid.New().String(),
				CandidateType:   CandidateConsultingLead,
				SourceType:      SourceInboundRequest,
				SourceRefs:      []string{fmt.Sprintf("source:%s", source)},
				Title:           fmt.Sprintf("Consulting lead from %s", source),
				Description:     fmt.Sprintf("Detected %d inbound request signals from %s, suggesting a consulting engagement.", count, source),
				Confidence:      confidenceFromCount(count, r.threshold),
				EstimatedValue:  estimateConsultingValue(count),
				EstimatedEffort: 0.3,
				DedupeKey:       fmt.Sprintf("inbound:%s", source),
				EvidenceCount:   count,
			})
		}
	}

	return DiscoveryRuleResult{
		RuleName:               r.Name(),
		Matched:                len(candidates) > 0,
		EvidenceCount:          len(matchingSignals),
		Confidence:             bestConfidence(candidates),
		GeneratedCandidateType: CandidateConsultingLead,
	}, candidates
}

// --- Rule 4: Cost Waste ---

// CostWasteRule detects repeated cost spikes and inefficiency signals.
type CostWasteRule struct {
	threshold int
}

// NewCostWasteRule creates a rule with the given threshold.
func NewCostWasteRule(threshold int) *CostWasteRule {
	return &CostWasteRule{threshold: threshold}
}

func (r *CostWasteRule) Name() string { return "cost_waste" }

func (r *CostWasteRule) Evaluate(_ context.Context, input DiscoveryInput) (DiscoveryRuleResult, []DiscoveryCandidate) {
	// Count cost_spike signals.
	costSpikeCount := 0
	for _, s := range input.Signals {
		if s.SignalType == "cost_spike" {
			costSpikeCount++
		}
	}

	var candidates []DiscoveryCandidate

	if costSpikeCount >= r.threshold {
		candidates = append(candidates, DiscoveryCandidate{
			ID:              uuid.New().String(),
			CandidateType:   CandidateCostSaving,
			SourceType:      SourceCostInefficiency,
			SourceRefs:      []string{"signal_type:cost_spike"},
			Title:           "Reduce repeated cost spikes",
			Description:     fmt.Sprintf("Detected %d cost spike signals in the discovery window, suggesting cost optimisation opportunity.", costSpikeCount),
			Confidence:      confidenceFromCount(costSpikeCount, r.threshold),
			EstimatedValue:  estimateCostSavingValue(costSpikeCount),
			EstimatedEffort: 0.3,
			DedupeKey:       "cost_spike:recurring",
			EvidenceCount:   costSpikeCount,
		})
	}

	return DiscoveryRuleResult{
		RuleName:               r.Name(),
		Matched:                len(candidates) > 0,
		EvidenceCount:          costSpikeCount,
		Confidence:             bestConfidence(candidates),
		GeneratedCandidateType: CandidateCostSaving,
	}, candidates
}

// --- Rule 5: Reusable Success ---

// ReusableSuccessRule detects repeated successful internal execution patterns.
type ReusableSuccessRule struct {
	threshold int
}

// NewReusableSuccessRule creates a rule with the given threshold.
func NewReusableSuccessRule(threshold int) *ReusableSuccessRule {
	return &ReusableSuccessRule{threshold: threshold}
}

func (r *ReusableSuccessRule) Name() string { return "reusable_success" }

func (r *ReusableSuccessRule) Evaluate(_ context.Context, input DiscoveryInput) (DiscoveryRuleResult, []DiscoveryCandidate) {
	// Count succeeded proposals grouped by opportunity type.
	successByType := map[string]int{}
	for _, p := range input.Proposals {
		if p.Status == "executed" || p.Status == "approved" {
			successByType[p.OpportunityType]++
		}
	}

	// Also count succeeded outcomes by action type.
	successByAction := map[string]int{}
	for _, o := range input.Outcomes {
		if o.Status == "succeeded" {
			successByAction[o.ActionType]++
		}
	}

	var candidates []DiscoveryCandidate
	totalEvidence := 0

	for oppType, count := range successByType {
		if count >= r.threshold {
			totalEvidence += count
			candidates = append(candidates, DiscoveryCandidate{
				ID:              uuid.New().String(),
				CandidateType:   CandidateProductFeature,
				SourceType:      SourceReusableSuccess,
				SourceRefs:      []string{fmt.Sprintf("opportunity_type:%s", oppType)},
				Title:           fmt.Sprintf("Productise reusable capability: %s", oppType),
				Description:     fmt.Sprintf("Opportunity type %q has been successfully executed %d times, suggesting a productisable feature.", oppType, count),
				Confidence:      confidenceFromCount(count, r.threshold),
				EstimatedValue:  estimateProductValue(count),
				EstimatedEffort: 0.6,
				DedupeKey:       fmt.Sprintf("reusable:%s", oppType),
				EvidenceCount:   count,
			})
		}
	}

	for actionType, count := range successByAction {
		if count >= r.threshold {
			totalEvidence += count
			candidates = append(candidates, DiscoveryCandidate{
				ID:              uuid.New().String(),
				CandidateType:   CandidateProductFeature,
				SourceType:      SourceReusableSuccess,
				SourceRefs:      []string{fmt.Sprintf("action_type:%s", actionType)},
				Title:           fmt.Sprintf("Productise reusable action: %s", actionType),
				Description:     fmt.Sprintf("Action type %q has succeeded %d times, suggesting a productisable pattern.", actionType, count),
				Confidence:      confidenceFromCount(count, r.threshold),
				EstimatedValue:  estimateProductValue(count),
				EstimatedEffort: 0.6,
				DedupeKey:       fmt.Sprintf("reusable_action:%s", actionType),
				EvidenceCount:   count,
			})
		}
	}

	return DiscoveryRuleResult{
		RuleName:               r.Name(),
		Matched:                len(candidates) > 0,
		EvidenceCount:          totalEvidence,
		Confidence:             bestConfidence(candidates),
		GeneratedCandidateType: CandidateProductFeature,
	}, candidates
}

// --- Default rules ---

// DefaultRules returns the standard set of discovery rules with default thresholds.
func DefaultRules() []Rule {
	return []Rule{
		NewRepeatedManualWorkRule(RepeatedWorkThreshold),
		NewRepeatedSolvedIssueRule(RepeatedIssueThreshold),
		NewInboundNeedRule(InboundRequestThreshold),
		NewCostWasteRule(CostSpikeThreshold),
		NewReusableSuccessRule(ReusableSuccessThreshold),
	}
}

// --- Helper functions ---

// isManualAction reports whether an action type represents manual/operator work.
func isManualAction(actionType string) bool {
	manualIndicators := []string{
		"summarize_state",
		"generate_report",
		"manual_",
		"operator_",
		"review_",
		"triage_",
	}
	lower := strings.ToLower(actionType)
	for _, indicator := range manualIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}

// confidenceFromCount computes a confidence score in [0.3, 0.95] based on evidence count.
func confidenceFromCount(count, threshold int) float64 {
	if count <= 0 || threshold <= 0 {
		return 0.3
	}
	ratio := float64(count) / float64(threshold)
	conf := 0.3 + (ratio-1.0)*0.15
	if conf < 0.3 {
		return 0.3
	}
	if conf > 0.95 {
		return 0.95
	}
	return conf
}

func bestConfidence(candidates []DiscoveryCandidate) float64 {
	best := 0.0
	for _, c := range candidates {
		if c.Confidence > best {
			best = c.Confidence
		}
	}
	return best
}

func estimateAutomationValue(count int) float64 {
	// Each manual intervention avoided ≈ $50 in saved time.
	v := float64(count) * 50.0
	if v > MaxOpValue {
		return MaxOpValue
	}
	return v
}

func estimateRepackageValue(count int) float64 {
	v := float64(count) * 100.0
	if v > MaxOpValue {
		return MaxOpValue
	}
	return v
}

func estimateConsultingValue(count int) float64 {
	v := float64(count) * 500.0
	if v > MaxOpValue {
		return MaxOpValue
	}
	return v
}

func estimateCostSavingValue(count int) float64 {
	v := float64(count) * 200.0
	if v > MaxOpValue {
		return MaxOpValue
	}
	return v
}

func estimateProductValue(count int) float64 {
	v := float64(count) * 300.0
	if v > MaxOpValue {
		return MaxOpValue
	}
	return v
}

// MaxOpValue mirrors the income engine max op value for value estimation capping.
const MaxOpValue = 10_000.0
