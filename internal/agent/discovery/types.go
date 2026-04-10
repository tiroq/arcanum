package discovery

import (
	"context"
	"time"
)

// --- Candidate types ---

const (
	CandidateAutomation      = "automation_candidate"
	CandidateResaleRepackage = "resale_or_repackage_candidate"
	CandidateConsultingLead  = "consulting_lead"
	CandidateCostSaving      = "cost_saving_candidate"
	CandidateProductFeature  = "product_feature_candidate"
)

// ValidCandidateTypes enumerates all recognised candidate types.
var ValidCandidateTypes = map[string]bool{
	CandidateAutomation:      true,
	CandidateResaleRepackage: true,
	CandidateConsultingLead:  true,
	CandidateCostSaving:      true,
	CandidateProductFeature:  true,
}

// --- Source types ---

const (
	SourceRepeatedManualWork  = "repeated_manual_work"
	SourceRepeatedSolvedIssue = "repeated_solved_issue"
	SourceInboundRequest      = "inbound_request"
	SourceCostInefficiency    = "cost_inefficiency"
	SourceReusableSuccess     = "reusable_success"
)

// --- Candidate statuses ---

const (
	CandidateStatusNew      = "new"
	CandidateStatusPromoted = "promoted"
	CandidateStatusSkipped  = "skipped"
)

// --- Mapping from candidate type to income opportunity type ---

// CandidateToOpportunityType maps discovery candidate types to
// income engine opportunity types (consulting|automation|service|content|other).
var CandidateToOpportunityType = map[string]string{
	CandidateAutomation:      "automation",
	CandidateResaleRepackage: "service",
	CandidateConsultingLead:  "consulting",
	CandidateCostSaving:      "other",
	CandidateProductFeature:  "service",
}

// --- Threshold constants ---

const (
	// RepeatedWorkThreshold is the minimum number of repeated task patterns to create a candidate.
	RepeatedWorkThreshold = 3
	// RepeatedIssueThreshold is the minimum number of repeated solved issues to create a candidate.
	RepeatedIssueThreshold = 3
	// CostSpikeThreshold is the minimum number of cost spike signals to create a candidate.
	CostSpikeThreshold = 2
	// ReusableSuccessThreshold is the minimum number of reused successful paths/proposals.
	ReusableSuccessThreshold = 3
	// InboundRequestThreshold is the minimum number of inbound request signals.
	InboundRequestThreshold = 1

	// DedupeWindowHours controls how far back to look for existing candidates with the same dedupe key.
	DedupeWindowHours = 72
	// DefaultDiscoveryWindowHours is the default lookback window for discovery signal analysis.
	DefaultDiscoveryWindowHours = 24
)

// --- Entities ---

// DiscoveryCandidate represents a candidate opportunity discovered from operational patterns.
type DiscoveryCandidate struct {
	ID              string    `json:"id"`
	CandidateType   string    `json:"candidate_type"`
	SourceType      string    `json:"source_type"`
	SourceRefs      []string  `json:"source_refs"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	Confidence      float64   `json:"confidence"`
	EstimatedValue  float64   `json:"estimated_value"`
	EstimatedEffort float64   `json:"estimated_effort"`
	DedupeKey       string    `json:"dedupe_key"`
	Status          string    `json:"status"`
	EvidenceCount   int       `json:"evidence_count"`
	CreatedAt       time.Time `json:"created_at"`
}

// DiscoveryRuleResult captures the output of a single discovery rule evaluation.
type DiscoveryRuleResult struct {
	RuleName               string  `json:"rule_name"`
	Matched                bool    `json:"matched"`
	EvidenceCount          int     `json:"evidence_count"`
	Confidence             float64 `json:"confidence"`
	GeneratedCandidateType string  `json:"generated_candidate_type"`
}

// DiscoveryStats aggregates discovery engine statistics.
type DiscoveryStats struct {
	TotalCandidatesGenerated int       `json:"total_candidates_generated"`
	TotalCandidatesPromoted  int       `json:"total_candidates_promoted"`
	TotalCandidatesDeduped   int       `json:"total_candidates_deduped"`
	UpdatedAt                time.Time `json:"updated_at"`
}

// DiscoveryRunResult is the output of a single discovery run.
type DiscoveryRunResult struct {
	RuleResults       []DiscoveryRuleResult `json:"rule_results"`
	CandidatesCreated int                   `json:"candidates_created"`
	CandidatesDeduped int                   `json:"candidates_deduped"`
}

// --- Discovery input providers ---

// SignalProvider gives the discovery engine access to active signals.
type SignalProvider interface {
	ListRecentSignals(ctx context.Context, windowHours int, limit int) ([]SignalRecord, error)
}

// OutcomeProvider gives the discovery engine access to recent action outcomes.
type OutcomeProvider interface {
	ListRecentOutcomes(ctx context.Context, windowHours int, limit int) ([]OutcomeRecord, error)
}

// ProposalProvider gives the discovery engine access to recent proposals.
type ProposalProvider interface {
	ListRecentProposals(ctx context.Context, windowHours int, limit int) ([]ProposalRecord, error)
}

// OpportunityProvider checks whether an active income opportunity already exists for dedup.
type OpportunityProvider interface {
	HasActiveOpportunity(ctx context.Context, opportunityType, dedupeKey string) (bool, error)
}

// --- Lightweight input records (decoupled from other packages) ---

// SignalRecord is a lightweight signal representation for discovery input.
type SignalRecord struct {
	SignalType string    `json:"signal_type"`
	Severity   string    `json:"severity"`
	Value      float64   `json:"value"`
	Source     string    `json:"source"`
	ObservedAt time.Time `json:"observed_at"`
}

// OutcomeRecord is a lightweight outcome representation for discovery input.
type OutcomeRecord struct {
	ActionType string    `json:"action_type"`
	GoalType   string    `json:"goal_type"`
	Status     string    `json:"status"`
	Mode       string    `json:"mode"`
	CreatedAt  time.Time `json:"created_at"`
}

// ProposalRecord is a lightweight proposal representation for discovery input.
type ProposalRecord struct {
	ActionType      string    `json:"action_type"`
	OpportunityType string    `json:"opportunity_type"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
}
