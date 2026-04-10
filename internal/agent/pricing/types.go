package pricing

import "time"

// --- Constants ---

const (
// BaseHourlyRate is the default hourly rate when no strategy data is available.
BaseHourlyRate = 100.0

// MinimumPriceMultiplier defines cost_basis * multiplier = minimum_price.
MinimumPriceMultiplier = 1.2
// TargetPriceMultiplier defines cost_basis * multiplier = target_price.
TargetPriceMultiplier = 1.8
// StretchPriceMultiplier defines cost_basis * multiplier = stretch_price.
StretchPriceMultiplier = 2.4

// MaxConcessionCount caps the number of stepwise concessions per negotiation.
MaxConcessionCount = 5

// DefaultConcessionStepFraction is the fraction of (current - minimum) given per step.
DefaultConcessionStepFraction = 0.15

// MaxPressureFloorBoost caps how much financial pressure can raise the minimum price.
// Applied as: minimum *= (1 + pressure * MaxPressureFloorBoost).
MaxPressureFloorBoost = 0.20

// MaxCapacityFloorBoost caps how much capacity overload can raise the minimum price.
// Applied as: minimum *= (1 + normalisedCapacity * MaxCapacityFloorBoost).
MaxCapacityFloorBoost = 0.25

// StrategyPerformanceMaxAdjust bounds how much strategy win-rate adjusts target price.
StrategyPerformanceMaxAdjust = 0.15

// MinOutcomesForLearning is the cold-start guard — no learning adjustments below this.
MinOutcomesForLearning = 3

// PricingConfidenceDefault is the confidence when no historical data exists.
PricingConfidenceDefault = 0.50
)

// --- Negotiation states ---

const (
StateUnpriced             = "unpriced"
StateInitialOfferPrepared = "initial_offer_prepared"
StateCounterOfferNeeded   = "counter_offer_needed"
StateAwaitingResponse     = "awaiting_response"
StateWon                  = "won"
StateLost                 = "lost"
)

// ValidNegotiationStates is the set of accepted negotiation states.
var ValidNegotiationStates = map[string]bool{
StateUnpriced:             true,
StateInitialOfferPrepared: true,
StateCounterOfferNeeded:   true,
StateAwaitingResponse:     true,
StateWon:                  true,
StateLost:                 true,
}

// ValidTransitions maps each state to the set of states it can move to.
var ValidTransitions = map[string]map[string]bool{
StateUnpriced: {
StateInitialOfferPrepared: true,
},
StateInitialOfferPrepared: {
StateAwaitingResponse:   true,
StateCounterOfferNeeded: true,
StateLost:               true,
},
StateAwaitingResponse: {
StateCounterOfferNeeded: true,
StateWon:                true,
StateLost:               true,
},
StateCounterOfferNeeded: {
StateAwaitingResponse: true,
StateLost:             true,
},
StateWon:  {},
StateLost: {},
}

// --- Recommended message types ---

const (
MessageTypeInitialQuote  = "initial_quote"
MessageTypeCounterOffer  = "counter_offer"
MessageTypeFollowUp      = "follow_up"
MessageTypeAcceptance    = "acceptance"
MessageTypeDraftProposal = "draft_proposal"
)

// --- Entities ---

// PricingProfile is a structured pricing view for an opportunity.
type PricingProfile struct {
ID                   string    `json:"id"`
OpportunityID        string    `json:"opportunity_id"`
StrategyID           string    `json:"strategy_id"`
EstimatedEffortHours float64   `json:"estimated_effort_hours"`
CostBasis            float64   `json:"cost_basis"`
TargetPrice          float64   `json:"target_price"`
MinimumPrice         float64   `json:"minimum_price"`
StretchPrice         float64   `json:"stretch_price"`
Confidence           float64   `json:"confidence"`
CreatedAt            time.Time `json:"created_at"`
UpdatedAt            time.Time `json:"updated_at"`
}

// NegotiationRecord tracks the negotiation state for an opportunity.
type NegotiationRecord struct {
ID                   string    `json:"id"`
OpportunityID        string    `json:"opportunity_id"`
NegotiationState     string    `json:"negotiation_state"`
CurrentOfferedPrice  float64   `json:"current_offered_price"`
RecommendedNextPrice float64   `json:"recommended_next_price"`
ConcessionCount      int       `json:"concession_count"`
RequiresReview       bool      `json:"requires_review"`
CreatedAt            time.Time `json:"created_at"`
UpdatedAt            time.Time `json:"updated_at"`
}

// PricingOutcome records the final pricing result for an opportunity.
type PricingOutcome struct {
ID            string    `json:"id"`
OpportunityID string    `json:"opportunity_id"`
QuotedPrice   float64   `json:"quoted_price"`
AcceptedPrice float64   `json:"accepted_price"`
Won           bool      `json:"won"`
Notes         string    `json:"notes"`
CreatedAt     time.Time `json:"created_at"`
}

// PricingPerformance aggregates pricing outcomes per strategy.
type PricingPerformance struct {
StrategyID       string    `json:"strategy_id"`
AvgQuotedPrice   float64   `json:"avg_quoted_price"`
AvgAcceptedPrice float64   `json:"avg_accepted_price"`
AvgDiscountRate  float64   `json:"avg_discount_rate"`
WinRate          float64   `json:"win_rate"`
TotalOutcomes    int       `json:"total_outcomes"`
UpdatedAt        time.Time `json:"updated_at"`
}

// PricingRecommendation is the system's suggested pricing action.
type PricingRecommendation struct {
OpportunityID          string  `json:"opportunity_id"`
TargetPrice            float64 `json:"target_price"`
MinimumPrice           float64 `json:"minimum_price"`
StretchPrice           float64 `json:"stretch_price"`
ConcessionStep         float64 `json:"concession_step"`
RecommendedMessageType string  `json:"recommended_message_type"`
Rationale              string  `json:"rationale"`
RequiresReview         bool    `json:"requires_review"`
Confidence             float64 `json:"confidence"`
}

// PricingInput carries the inputs to price computation.
type PricingInput struct {
OpportunityID        string  `json:"opportunity_id"`
EstimatedEffortHours float64 `json:"estimated_effort_hours"`
StrategyType         string  `json:"strategy_type"`
StrategyID           string  `json:"strategy_id"`
FinancialPressure    float64 `json:"financial_pressure"`
CapacityPenalty      float64 `json:"capacity_penalty"`
}

// ConcessionResult captures a single concession step.
type ConcessionResult struct {
NewOfferedPrice float64 `json:"new_offered_price"`
StepSize        float64 `json:"step_size"`
ConcessionCount int     `json:"concession_count"`
AtFloor         bool    `json:"at_floor"`
Reason          string  `json:"reason"`
}
