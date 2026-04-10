package income

import (
	"fmt"

	"github.com/google/uuid"
)

// GenerateProposals creates IncomeActionProposal records for an opportunity
// based on its score, type, and governance state.
//
// Rules:
//  1. Proposals are generated only if score ≥ ScoreThreshold.
//  2. One proposal is generated per mapped action type.
//  3. Risk level is derived from confidence and effort.
//  4. requires_review is set when: governance is frozen, risk is high, or
//     expected_value exceeds BigTicketThreshold.
func GenerateProposals(opp IncomeOpportunity, score float64, governanceFrozen bool) []IncomeActionProposal {
	if score < ScoreThreshold {
		return nil
	}

	actionTypes := MapOpportunityToActions(opp.OpportunityType)
	riskLevel := deriveRiskLevel(opp.Confidence, opp.EstimatedEffort)
	expectedValue := opp.EstimatedValue * score // bounded estimate

	proposals := make([]IncomeActionProposal, 0, len(actionTypes))
	for _, at := range actionTypes {
		requiresReview := governanceFrozen ||
			riskLevel == RiskHigh ||
			expectedValue > BigTicketThreshold

		proposals = append(proposals, IncomeActionProposal{
			ID:             uuid.New().String(),
			OpportunityID:  opp.ID,
			ActionType:     at,
			Title:          fmt.Sprintf("%s via %s", opp.Title, at),
			Reason:         fmt.Sprintf("score=%.2f type=%s", score, opp.OpportunityType),
			ExpectedValue:  expectedValue,
			RiskLevel:      riskLevel,
			RequiresReview: requiresReview,
			Status:         ProposalStatusPending,
		})
	}
	return proposals
}

// deriveRiskLevel computes a risk level from confidence and estimated effort.
//
//   - low:    confidence > 0.80 and effort < 0.30
//   - medium: confidence ≥ 0.50
//   - high:   otherwise
func deriveRiskLevel(confidence, effort float64) string {
	if confidence > 0.80 && effort < 0.30 {
		return RiskLow
	}
	if confidence >= 0.50 {
		return RiskMedium
	}
	return RiskHigh
}
