package financialtruth

import (
	"math"
	"time"
)

// LinkResult carries the result of an attempted heuristic link.
type LinkResult struct {
	Linked     bool
	MatchType  string
	Confidence float64
	Reason     string
}

// AttemptExactLink tries to link a fact to an opportunity using exact identifiers.
// Returns a link result with exact match type if external_ref matches opportunity ID
// or if the fact already has a linked_opportunity_id.
func AttemptExactLink(fact FinancialFact, opportunityID, externalRef string) LinkResult {
	if fact.LinkedOpportunityID != "" && fact.LinkedOpportunityID == opportunityID {
		return LinkResult{
			Linked:     true,
			MatchType:  MatchTypeExact,
			Confidence: ExactMatchConfidence,
			Reason:     "fact already linked to this opportunity",
		}
	}
	// Check if external_ref on the originating event matches.
	if externalRef != "" && externalRef == opportunityID {
		return LinkResult{
			Linked:     true,
			MatchType:  MatchTypeExact,
			Confidence: ExactMatchConfidence,
			Reason:     "external_ref matches opportunity_id",
		}
	}
	return LinkResult{Linked: false, Reason: "no exact identifier match"}
}

// AttemptHeuristicLink tries to link a fact to an opportunity/outcome based on
// amount similarity and temporal proximity. Only links if confidence >= MinLinkConfidence.
//
// Heuristic: amount within 5% AND dates within 7 days → HeuristicMatchConfidence.
// Does NOT aggressively link; prefers false negatives over false positives.
func AttemptHeuristicLink(factAmount float64, factDate time.Time, outcomeAmount float64, outcomeDate time.Time) LinkResult {
	if factAmount <= 0 || outcomeAmount <= 0 {
		return LinkResult{Linked: false, Reason: "zero or negative amounts"}
	}

	// Amount similarity: within 5%.
	ratio := factAmount / outcomeAmount
	amountMatch := ratio >= 0.95 && ratio <= 1.05

	// Temporal proximity: within 7 days.
	daysDiff := math.Abs(factDate.Sub(outcomeDate).Hours() / 24)
	dateMatch := daysDiff <= 7.0

	if amountMatch && dateMatch {
		conf := HeuristicMatchConfidence
		if conf < MinLinkConfidence {
			return LinkResult{
				Linked: false,
				Reason: "heuristic confidence below minimum threshold",
			}
		}
		return LinkResult{
			Linked:     true,
			MatchType:  MatchTypeHeuristic,
			Confidence: conf,
			Reason:     "amount and date match within tolerances",
		}
	}

	if amountMatch && !dateMatch {
		return LinkResult{Linked: false, Reason: "amount matches but dates too far apart"}
	}
	if dateMatch && !amountMatch {
		return LinkResult{Linked: false, Reason: "dates match but amounts differ"}
	}
	return LinkResult{Linked: false, Reason: "no heuristic match"}
}

// BuildManualLink creates a manual link result (always succeeds).
func BuildManualLink() LinkResult {
	return LinkResult{
		Linked:     true,
		MatchType:  MatchTypeManual,
		Confidence: ManualMatchConfidence,
		Reason:     "manually linked by user",
	}
}
