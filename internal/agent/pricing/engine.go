package pricing

import (
"context"
"fmt"

"github.com/google/uuid"
"go.uber.org/zap"

"github.com/tiroq/arcanum/internal/audit"
)

// FinancialPressureProvider reads current financial pressure.
// Defined here to avoid import cycles.
type FinancialPressureProvider interface {
GetPressure(ctx context.Context) (pressureScore float64, urgencyLevel string)
}

// CapacityProvider reads current owner capacity penalty.
// Defined here to avoid import cycles.
type CapacityProvider interface {
GetCapacityPenalty(ctx context.Context) float64
}

// GovernanceProvider checks governance state for review requirements.
// Defined here to avoid import cycles.
type GovernanceProvider interface {
RequiresHumanReview(ctx context.Context) bool
}

// Engine orchestrates the pricing intelligence lifecycle:
// compute profiles, manage negotiations, record outcomes, update performance.
type Engine struct {
profiles     *ProfileStore
negotiations *NegotiationStore
outcomes     *OutcomeStore
performances *PerformanceStore
auditor      audit.AuditRecorder
logger       *zap.Logger

pressure   FinancialPressureProvider
capacity   CapacityProvider
governance GovernanceProvider
}

// NewEngine creates a new pricing engine.
func NewEngine(
profiles *ProfileStore,
negotiations *NegotiationStore,
outcomes *OutcomeStore,
performances *PerformanceStore,
auditor audit.AuditRecorder,
logger *zap.Logger,
) *Engine {
return &Engine{
profiles:     profiles,
negotiations: negotiations,
outcomes:     outcomes,
performances: performances,
auditor:      auditor,
logger:       logger,
}
}

// WithPressure sets the financial pressure provider.
func (e *Engine) WithPressure(p FinancialPressureProvider) *Engine {
e.pressure = p
return e
}

// WithCapacity sets the capacity provider.
func (e *Engine) WithCapacity(c CapacityProvider) *Engine {
e.capacity = c
return e
}

// WithGovernance sets the governance provider.
func (e *Engine) WithGovernance(g GovernanceProvider) *Engine {
e.governance = g
return e
}

// ComputeProfile computes a pricing profile for an opportunity.
func (e *Engine) ComputeProfile(ctx context.Context, input PricingInput) (PricingProfile, error) {
if input.OpportunityID == "" {
return PricingProfile{}, fmt.Errorf("opportunity_id is required")
}
if input.EstimatedEffortHours <= 0 {
return PricingProfile{}, fmt.Errorf("estimated_effort_hours must be > 0")
}

// Gather financial pressure if available.
if e.pressure != nil {
pressure, _ := e.pressure.GetPressure(ctx)
input.FinancialPressure = pressure
}

// Gather capacity penalty if available.
if e.capacity != nil {
input.CapacityPenalty = e.capacity.GetCapacityPenalty(ctx)
}

// Load historical performance for strategy.
var perf PricingPerformance
if input.StrategyID != "" {
p, err := e.performances.Get(ctx, input.StrategyID)
if err != nil {
e.logger.Warn("pricing: failed to load performance",
zap.String("strategy_id", input.StrategyID),
zap.Error(err),
)
} else {
perf = p
}
}

profile := ComputePriceBands(input, perf)

// Persist.
saved, err := e.profiles.Upsert(ctx, profile)
if err != nil {
return PricingProfile{}, err
}

e.auditEvent(ctx, "pricing_profile", saved.ID, "pricing.profile_computed", map[string]any{
"opportunity_id":  saved.OpportunityID,
"strategy_id":    saved.StrategyID,
"cost_basis":     saved.CostBasis,
"minimum_price":  saved.MinimumPrice,
"target_price":   saved.TargetPrice,
"stretch_price":  saved.StretchPrice,
"confidence":     saved.Confidence,
"effort_hours":   saved.EstimatedEffortHours,
"pressure":       input.FinancialPressure,
"capacity":       input.CapacityPenalty,
})

// Create initial negotiation record if none exists.
_, err = e.negotiations.GetByOpportunity(ctx, saved.OpportunityID)
if err != nil {
neg := NegotiationRecord{
OpportunityID:        saved.OpportunityID,
NegotiationState:     StateUnpriced,
CurrentOfferedPrice:  0,
RecommendedNextPrice: saved.TargetPrice,
ConcessionCount:      0,
RequiresReview:       true,
}
_, _ = e.negotiations.Upsert(ctx, neg)
}

return saved, nil
}

// Recommend generates a pricing recommendation for an opportunity.
func (e *Engine) Recommend(ctx context.Context, opportunityID string) (PricingRecommendation, error) {
profile, err := e.profiles.GetByOpportunity(ctx, opportunityID)
if err != nil {
return PricingRecommendation{}, fmt.Errorf("no pricing profile: %w", err)
}

neg, err := e.negotiations.GetByOpportunity(ctx, opportunityID)
if err != nil {
// No negotiation yet — create one.
neg = NegotiationRecord{
OpportunityID:    opportunityID,
NegotiationState: StateUnpriced,
ConcessionCount:  0,
}
}

// Determine recommended price based on state.
recommendedPrice := profile.TargetPrice
concessionStep := 0.0
messageType := RecommendMessageType(neg.NegotiationState)

if neg.NegotiationState == StateCounterOfferNeeded && neg.CurrentOfferedPrice > 0 {
concession := ComputeConcession(neg.CurrentOfferedPrice, profile.MinimumPrice, neg.ConcessionCount)
recommendedPrice = concession.NewOfferedPrice
concessionStep = concession.StepSize
}

requiresReview := true
if e.governance != nil {
requiresReview = e.governance.RequiresHumanReview(ctx)
}

rationale := buildRationale(profile, neg)

rec := PricingRecommendation{
OpportunityID:          opportunityID,
TargetPrice:            profile.TargetPrice,
MinimumPrice:           profile.MinimumPrice,
StretchPrice:           profile.StretchPrice,
ConcessionStep:         concessionStep,
RecommendedMessageType: messageType,
Rationale:              rationale,
RequiresReview:         requiresReview,
Confidence:             profile.Confidence,
}

// Transition negotiation state.
newState := neg.NegotiationState
if neg.NegotiationState == StateUnpriced {
newState = StateInitialOfferPrepared
}

neg.NegotiationState = newState
neg.RecommendedNextPrice = recommendedPrice
neg.RequiresReview = requiresReview
_, _ = e.negotiations.Upsert(ctx, neg)

e.auditEvent(ctx, "pricing_recommendation", opportunityID, "pricing.recommendation_created", map[string]any{
"opportunity_id":  opportunityID,
"target_price":    rec.TargetPrice,
"minimum_price":   rec.MinimumPrice,
"stretch_price":   rec.StretchPrice,
"message_type":    rec.RecommendedMessageType,
"requires_review": rec.RequiresReview,
"confidence":      rec.Confidence,
})

return rec, nil
}

// TransitionNegotiation transitions a negotiation to a new state.
func (e *Engine) TransitionNegotiation(ctx context.Context, opportunityID, newState string, offeredPrice float64) (NegotiationRecord, error) {
neg, err := e.negotiations.GetByOpportunity(ctx, opportunityID)
if err != nil {
return NegotiationRecord{}, fmt.Errorf("negotiation not found: %w", err)
}

if err := ValidateTransition(neg.NegotiationState, newState); err != nil {
return NegotiationRecord{}, err
}

oldState := neg.NegotiationState
neg.NegotiationState = newState

if offeredPrice > 0 {
neg.CurrentOfferedPrice = offeredPrice
}

// If transitioning to counter_offer_needed, compute concession.
if newState == StateCounterOfferNeeded && neg.CurrentOfferedPrice > 0 {
profile, err := e.profiles.GetByOpportunity(ctx, opportunityID)
if err == nil {
concession := ComputeConcession(neg.CurrentOfferedPrice, profile.MinimumPrice, neg.ConcessionCount)
neg.RecommendedNextPrice = concession.NewOfferedPrice
neg.ConcessionCount = concession.ConcessionCount

e.auditEvent(ctx, "pricing_concession", opportunityID, "pricing.concession_suggested", map[string]any{
"opportunity_id":    opportunityID,
"current_offer":     neg.CurrentOfferedPrice,
"recommended_next":  concession.NewOfferedPrice,
"step_size":         concession.StepSize,
"concession_count":  concession.ConcessionCount,
"at_floor":          concession.AtFloor,
"reason":            concession.Reason,
})
}
}

neg.RequiresReview = true
saved, err := e.negotiations.Upsert(ctx, neg)
if err != nil {
return NegotiationRecord{}, err
}

e.auditEvent(ctx, "negotiation", saved.ID, "pricing.negotiation_transitioned", map[string]any{
"opportunity_id": opportunityID,
"from_state":     oldState,
"to_state":       newState,
"offered_price":  offeredPrice,
})

return saved, nil
}

// RecordOutcome records a pricing outcome and updates performance.
func (e *Engine) RecordOutcome(ctx context.Context, outcome PricingOutcome) (PricingOutcome, error) {
if outcome.OpportunityID == "" {
return PricingOutcome{}, fmt.Errorf("opportunity_id is required")
}

saved, err := e.outcomes.Create(ctx, outcome)
if err != nil {
return PricingOutcome{}, err
}

e.auditEvent(ctx, "pricing_outcome", saved.ID, "pricing.outcome_recorded", map[string]any{
"opportunity_id": saved.OpportunityID,
"quoted_price":   saved.QuotedPrice,
"accepted_price": saved.AcceptedPrice,
"won":            saved.Won,
})

// Transition negotiation state based on outcome.
neg, err := e.negotiations.GetByOpportunity(ctx, outcome.OpportunityID)
if err == nil {
targetState := StateLost
if outcome.Won {
targetState = StateWon
}
if ValidateTransition(neg.NegotiationState, targetState) == nil {
neg.NegotiationState = targetState
_, _ = e.negotiations.Upsert(ctx, neg)
}
}

// Re-compute performance for the linked strategy.
profile, err := e.profiles.GetByOpportunity(ctx, outcome.OpportunityID)
if err == nil && profile.StrategyID != "" {
if err := e.updatePerformance(ctx, profile.StrategyID); err != nil {
e.logger.Warn("pricing: failed to update performance",
zap.String("strategy_id", profile.StrategyID),
zap.Error(err),
)
}
}

return saved, nil
}

// updatePerformance recomputes pricing performance for a strategy from its outcomes.
func (e *Engine) updatePerformance(ctx context.Context, strategyID string) error {
outcomes, err := e.outcomes.ListByStrategy(ctx, strategyID)
if err != nil {
return err
}

perf := ComputePerformanceFromOutcomes(strategyID, outcomes)
if err := e.performances.Upsert(ctx, perf); err != nil {
return err
}

e.auditEvent(ctx, "pricing_performance", strategyID, "pricing.performance_updated", map[string]any{
"strategy_id":      perf.StrategyID,
"avg_quoted_price": perf.AvgQuotedPrice,
"avg_accepted":     perf.AvgAcceptedPrice,
"avg_discount":     perf.AvgDiscountRate,
"win_rate":         perf.WinRate,
"total_outcomes":   perf.TotalOutcomes,
})

return nil
}

// ListProfiles returns all pricing profiles.
func (e *Engine) ListProfiles(ctx context.Context) ([]PricingProfile, error) {
return e.profiles.ListAll(ctx)
}

// ListNegotiations returns all negotiation records.
func (e *Engine) ListNegotiations(ctx context.Context) ([]NegotiationRecord, error) {
return e.negotiations.ListAll(ctx)
}

// ListPerformance returns all pricing performance records.
func (e *Engine) ListPerformance(ctx context.Context) ([]PricingPerformance, error) {
return e.performances.ListAll(ctx)
}

// GetProfile retrieves a pricing profile by opportunity ID.
func (e *Engine) GetProfile(ctx context.Context, opportunityID string) (PricingProfile, error) {
return e.profiles.GetByOpportunity(ctx, opportunityID)
}

// GetNegotiation retrieves a negotiation by opportunity ID.
func (e *Engine) GetNegotiation(ctx context.Context, opportunityID string) (NegotiationRecord, error) {
return e.negotiations.GetByOpportunity(ctx, opportunityID)
}

func (e *Engine) auditEvent(ctx context.Context, entityType, entityID, eventType string, payload map[string]any) {
id, _ := uuid.Parse(entityID)
if id == uuid.Nil {
id = uuid.New()
}
if err := e.auditor.RecordEvent(ctx, entityType, id, eventType, "pricing_engine", "engine", payload); err != nil {
e.logger.Warn("audit event failed",
zap.String("event", eventType),
zap.Error(err),
)
}
}

func buildRationale(profile PricingProfile, neg NegotiationRecord) string {
switch neg.NegotiationState {
case StateUnpriced:
return fmt.Sprintf(
"Initial pricing computed: target $%.2f (cost basis $%.2f x %.1f), floor $%.2f, stretch $%.2f. Confidence: %.0f%%.",
profile.TargetPrice, profile.CostBasis, TargetPriceMultiplier,
profile.MinimumPrice, profile.StretchPrice, profile.Confidence*100,
)
case StateCounterOfferNeeded:
return fmt.Sprintf(
"Counter-offer needed. Current offer $%.2f. Concession %d/%d. Floor $%.2f.",
neg.CurrentOfferedPrice, neg.ConcessionCount, MaxConcessionCount, profile.MinimumPrice,
)
case StateAwaitingResponse:
return fmt.Sprintf(
"Awaiting response on offer $%.2f. Follow-up recommended.",
neg.CurrentOfferedPrice,
)
default:
return fmt.Sprintf(
"Price band: $%.2f-$%.2f-$%.2f. State: %s.",
profile.MinimumPrice, profile.TargetPrice, profile.StretchPrice, neg.NegotiationState,
)
}
}
