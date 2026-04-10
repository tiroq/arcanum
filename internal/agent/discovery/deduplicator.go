package discovery

import "context"

// CandidateChecker is the subset of CandidateStore that the Deduplicator needs.
type CandidateChecker interface {
	FindByDedupeKey(ctx context.Context, dedupeKey, candidateType string, windowHours int) (*DiscoveryCandidate, error)
	IncrementEvidence(ctx context.Context, id string) error
}

// Deduplicator checks for existing candidates and prevents duplicates.
type Deduplicator struct {
	store              CandidateChecker
	opportunityChecker OpportunityProvider
	windowHours        int
}

// NewDeduplicator creates a Deduplicator with the given store and window.
func NewDeduplicator(store CandidateChecker, oppChecker OpportunityProvider, windowHours int) *Deduplicator {
	return &Deduplicator{
		store:              store,
		opportunityChecker: oppChecker,
		windowHours:        windowHours,
	}
}

// DedupeResult is the outcome of a deduplication check.
type DedupeResult struct {
	IsDuplicate bool
	ExistingID  string
	Action      string // "skipped" | "incremented" | "created"
}

// Check determines whether a candidate already exists within the dedup window.
// If an existing candidate is found, its evidence count is incremented.
// If an active income opportunity already exists for this type+key, it is skipped entirely.
func (d *Deduplicator) Check(ctx context.Context, candidate DiscoveryCandidate) DedupeResult {
	if d.store == nil {
		return DedupeResult{IsDuplicate: false, Action: "created"}
	}

	// 1. Check if an active income opportunity already covers this.
	if d.opportunityChecker != nil {
		oppType := CandidateToOpportunityType[candidate.CandidateType]
		if oppType != "" {
			hasActive, err := d.opportunityChecker.HasActiveOpportunity(ctx, oppType, candidate.DedupeKey)
			if err == nil && hasActive {
				return DedupeResult{IsDuplicate: true, ExistingID: "", Action: "skipped"}
			}
		}
	}

	// 2. Check if a recent candidate with the same dedupe key exists.
	existing, err := d.store.FindByDedupeKey(ctx, candidate.DedupeKey, candidate.CandidateType, d.windowHours)
	if err != nil || existing == nil {
		return DedupeResult{IsDuplicate: false, Action: "created"}
	}

	// Existing candidate found → increment evidence.
	_ = d.store.IncrementEvidence(ctx, existing.ID)
	return DedupeResult{
		IsDuplicate: true,
		ExistingID:  existing.ID,
		Action:      "incremented",
	}
}
