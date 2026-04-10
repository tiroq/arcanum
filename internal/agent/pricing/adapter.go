package pricing

import (
"context"

"go.uber.org/zap"
)

// GraphAdapter provides pricing intelligence for the API layer.
// Nil-safe and fail-open.
type GraphAdapter struct {
engine *Engine
logger *zap.Logger
}

// NewGraphAdapter creates a new GraphAdapter.
func NewGraphAdapter(engine *Engine, logger *zap.Logger) *GraphAdapter {
return &GraphAdapter{
engine: engine,
logger: logger,
}
}

// ComputeProfile computes a pricing profile for an opportunity.
func (a *GraphAdapter) ComputeProfile(ctx context.Context, input PricingInput) (PricingProfile, error) {
if a == nil || a.engine == nil {
return PricingProfile{}, nil
}
return a.engine.ComputeProfile(ctx, input)
}

// Recommend generates a pricing recommendation for an opportunity.
func (a *GraphAdapter) Recommend(ctx context.Context, opportunityID string) (PricingRecommendation, error) {
if a == nil || a.engine == nil {
return PricingRecommendation{}, nil
}
return a.engine.Recommend(ctx, opportunityID)
}

// TransitionNegotiation transitions a negotiation to a new state.
func (a *GraphAdapter) TransitionNegotiation(ctx context.Context, opportunityID, newState string, offeredPrice float64) (NegotiationRecord, error) {
if a == nil || a.engine == nil {
return NegotiationRecord{}, nil
}
return a.engine.TransitionNegotiation(ctx, opportunityID, newState, offeredPrice)
}

// RecordOutcome records a pricing outcome.
func (a *GraphAdapter) RecordOutcome(ctx context.Context, outcome PricingOutcome) (PricingOutcome, error) {
if a == nil || a.engine == nil {
return PricingOutcome{}, nil
}
return a.engine.RecordOutcome(ctx, outcome)
}

// ListProfiles returns all pricing profiles.
func (a *GraphAdapter) ListProfiles(ctx context.Context) ([]PricingProfile, error) {
if a == nil || a.engine == nil {
return nil, nil
}
return a.engine.ListProfiles(ctx)
}

// ListNegotiations returns all negotiation records.
func (a *GraphAdapter) ListNegotiations(ctx context.Context) ([]NegotiationRecord, error) {
if a == nil || a.engine == nil {
return nil, nil
}
return a.engine.ListNegotiations(ctx)
}

// ListPerformance returns all pricing performance records.
func (a *GraphAdapter) ListPerformance(ctx context.Context) ([]PricingPerformance, error) {
if a == nil || a.engine == nil {
return nil, nil
}
return a.engine.ListPerformance(ctx)
}

// GetProfile retrieves a pricing profile by opportunity ID.
func (a *GraphAdapter) GetProfile(ctx context.Context, opportunityID string) (PricingProfile, error) {
if a == nil || a.engine == nil {
return PricingProfile{}, nil
}
return a.engine.GetProfile(ctx, opportunityID)
}

// GetNegotiation retrieves a negotiation by opportunity ID.
func (a *GraphAdapter) GetNegotiation(ctx context.Context, opportunityID string) (NegotiationRecord, error) {
if a == nil || a.engine == nil {
return NegotiationRecord{}, nil
}
return a.engine.GetNegotiation(ctx, opportunityID)
}
