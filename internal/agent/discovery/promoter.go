package discovery

import (
	"context"
	"fmt"

	"github.com/tiroq/arcanum/internal/agent/income"
)

// Promoter converts discovery candidates into income opportunities.
type Promoter struct {
	incomeEngine *income.Engine
}

// NewPromoter creates a Promoter backed by the income engine.
func NewPromoter(incomeEngine *income.Engine) *Promoter {
	return &Promoter{incomeEngine: incomeEngine}
}

// Promote converts a DiscoveryCandidate into an IncomeOpportunity and persists it
// via the income engine. Returns the created opportunity.
func (p *Promoter) Promote(ctx context.Context, candidate DiscoveryCandidate) (income.IncomeOpportunity, error) {
	if p.incomeEngine == nil {
		return income.IncomeOpportunity{}, fmt.Errorf("income engine not available")
	}

	oppType := CandidateToOpportunityType[candidate.CandidateType]
	if oppType == "" {
		oppType = "other"
	}

	opp := income.IncomeOpportunity{
		Source:          "discovery",
		Title:           candidate.Title,
		Description:     candidate.Description,
		OpportunityType: oppType,
		EstimatedValue:  candidate.EstimatedValue,
		EstimatedEffort: candidate.EstimatedEffort,
		Confidence:      candidate.Confidence,
	}

	created, err := p.incomeEngine.CreateOpportunity(ctx, opp)
	if err != nil {
		return income.IncomeOpportunity{}, fmt.Errorf("promote candidate %s: %w", candidate.ID, err)
	}

	return created, nil
}
