package policy

import (
	"context"

	"github.com/tiroq/arcanum/internal/agent/planning"
)

// PlannerAdapter implements planning.PolicyProvider so the policy store
// can supply dynamic scoring parameters without import cycles.
type PlannerAdapter struct {
	store *Store
}

// NewPlannerAdapter creates a PlannerAdapter.
func NewPlannerAdapter(store *Store) *PlannerAdapter {
	return &PlannerAdapter{store: store}
}

// GetScoringParams reads current policy values from the store and returns
// a ScoringParams struct. Falls back to defaults on error.
func (a *PlannerAdapter) GetScoringParams(ctx context.Context) planning.ScoringParams {
	vals, err := a.store.GetAll(ctx)
	if err != nil {
		return planning.DefaultScoringParams()
	}

	params := planning.DefaultScoringParams()

	if v, ok := vals[ParamFeedbackAvoidPenalty]; ok {
		params.FeedbackAvoidPenalty = v
	}
	if v, ok := vals[ParamFeedbackPreferBoost]; ok {
		params.FeedbackPreferBoost = v
	}
	if v, ok := vals[ParamHighBacklogResyncPenalty]; ok {
		params.HighBacklogResyncPenalty = v
	}
	if v, ok := vals[ParamHighRetryBoost]; ok {
		params.HighRetryBoost = v
	}
	if v, ok := vals[ParamSafetyPreferenceBoost]; ok {
		params.SafetyPreferenceBoost = v
	}
	if v, ok := vals[ParamNoopBasePenalty]; ok {
		params.NoopBasePenalty = v
	}

	return params
}
