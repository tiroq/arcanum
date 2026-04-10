package pricing

import "math"

// ComputePriceBands computes the three-tier price band plus cost basis.
// All adjustments are deterministic and bounded.
//
//cost_basis = estimated_effort_hours * base_hourly_rate
//minimum    = cost_basis * 1.2
//target     = cost_basis * 1.8
//stretch    = cost_basis * 2.4
//
// Then adjusted by:
//   - financial pressure (raises floor)
//   - capacity penalty (raises floor)
//   - strategy performance (adjusts target up/down)
func ComputePriceBands(input PricingInput, perf PricingPerformance) PricingProfile {
hourlyRate := BaseHourlyRate

costBasis := input.EstimatedEffortHours * hourlyRate
if costBasis <= 0 {
costBasis = hourlyRate // minimum 1 hour
}

minimum := costBasis * MinimumPriceMultiplier
target := costBasis * TargetPriceMultiplier
stretch := costBasis * StretchPriceMultiplier

// Adjust: financial pressure raises floor (bounded).
if input.FinancialPressure > 0 {
pressureBoost := input.FinancialPressure * MaxPressureFloorBoost
minimum *= (1 + pressureBoost)
}

// Adjust: capacity overload raises floor (bounded).
if input.CapacityPenalty > 0 {
capacityBoost := input.CapacityPenalty * MaxCapacityFloorBoost / 0.15 // normalise to capacity penalty max
if capacityBoost > MaxCapacityFloorBoost {
capacityBoost = MaxCapacityFloorBoost
}
minimum *= (1 + capacityBoost)
}

// Adjust: strategy performance shifts target (bounded).
if perf.TotalOutcomes >= MinOutcomesForLearning && perf.WinRate > 0 {
// If win rate is high (>70%), push target up; if low (<30%), push down.
winAdjust := (perf.WinRate - 0.5) * 2 * StrategyPerformanceMaxAdjust
winAdjust = clamp(winAdjust, -StrategyPerformanceMaxAdjust, StrategyPerformanceMaxAdjust)
target *= (1 + winAdjust)
}

// Ensure invariant: minimum <= target <= stretch.
if target < minimum {
target = minimum
}
if stretch < target {
stretch = target
}

// Compute confidence from sample count.
confidence := computeConfidence(perf.TotalOutcomes)

return PricingProfile{
OpportunityID:        input.OpportunityID,
StrategyID:           input.StrategyID,
EstimatedEffortHours: input.EstimatedEffortHours,
CostBasis:            costBasis,
TargetPrice:          roundToTwoDecimals(target),
MinimumPrice:         roundToTwoDecimals(minimum),
StretchPrice:         roundToTwoDecimals(stretch),
Confidence:           confidence,
}
}

// ComputeConcession computes the next concession step.
func ComputeConcession(currentOffer, minimumPrice float64, concessionCount int) ConcessionResult {
if concessionCount >= MaxConcessionCount {
return ConcessionResult{
NewOfferedPrice: currentOffer,
ConcessionCount: concessionCount,
AtFloor:         currentOffer <= minimumPrice,
Reason:          "max concession count reached",
}
}

step := (currentOffer - minimumPrice) * DefaultConcessionStepFraction
if step < 0 {
step = 0
}

newPrice := currentOffer - step
if newPrice < minimumPrice {
newPrice = minimumPrice
}

return ConcessionResult{
NewOfferedPrice: roundToTwoDecimals(newPrice),
StepSize:        roundToTwoDecimals(step),
ConcessionCount: concessionCount + 1,
AtFloor:         newPrice <= minimumPrice,
Reason:          formatConcessionReason(concessionCount+1, newPrice <= minimumPrice),
}
}

// ComputePerformanceFromOutcomes aggregates pricing outcomes into performance metrics.
func ComputePerformanceFromOutcomes(strategyID string, outcomes []PricingOutcome) PricingPerformance {
if len(outcomes) == 0 {
return PricingPerformance{StrategyID: strategyID}
}

totalQuoted := 0.0
totalAccepted := 0.0
wonCount := 0

for _, o := range outcomes {
totalQuoted += o.QuotedPrice
if o.Won {
wonCount++
totalAccepted += o.AcceptedPrice
}
}

n := float64(len(outcomes))
avgQuoted := totalQuoted / n

avgAccepted := 0.0
if wonCount > 0 {
avgAccepted = totalAccepted / float64(wonCount)
}

avgDiscount := 0.0
if avgQuoted > 0 && wonCount > 0 {
avgDiscount = 1 - (avgAccepted / avgQuoted)
}

winRate := float64(wonCount) / n

return PricingPerformance{
StrategyID:       strategyID,
AvgQuotedPrice:   roundToTwoDecimals(avgQuoted),
AvgAcceptedPrice: roundToTwoDecimals(avgAccepted),
AvgDiscountRate:  roundToFourDecimals(avgDiscount),
WinRate:          roundToFourDecimals(winRate),
TotalOutcomes:    len(outcomes),
}
}

// ValidateTransition checks whether a state transition is allowed.
func ValidateTransition(from, to string) error {
if !ValidNegotiationStates[from] {
return errInvalidState(from)
}
if !ValidNegotiationStates[to] {
return errInvalidState(to)
}
allowed, ok := ValidTransitions[from]
if !ok || !allowed[to] {
return errInvalidTransition(from, to)
}
return nil
}

// RecommendMessageType returns the recommended message type based on negotiation state.
func RecommendMessageType(state string) string {
switch state {
case StateUnpriced, StateInitialOfferPrepared:
return MessageTypeInitialQuote
case StateCounterOfferNeeded:
return MessageTypeCounterOffer
case StateAwaitingResponse:
return MessageTypeFollowUp
case StateWon:
return MessageTypeAcceptance
default:
return MessageTypeDraftProposal
}
}

// --- helpers ---

func computeConfidence(totalOutcomes int) float64 {
if totalOutcomes < MinOutcomesForLearning {
return PricingConfidenceDefault
}
// Logarithmic growth: confidence approaches 1.0 with more data.
conf := PricingConfidenceDefault + 0.50*(1-1/math.Log2(float64(totalOutcomes)+1))
return clamp(conf, 0, 1)
}

func clamp(v, lo, hi float64) float64 {
if v < lo {
return lo
}
if v > hi {
return hi
}
return v
}

func roundToTwoDecimals(v float64) float64 {
return math.Round(v*100) / 100
}

func roundToFourDecimals(v float64) float64 {
return math.Round(v*10000) / 10000
}

func formatConcessionReason(count int, atFloor bool) string {
if atFloor {
return "concession reached price floor"
}
return "stepwise concession applied"
}

func errInvalidState(s string) error {
return &InvalidStateError{State: s}
}

func errInvalidTransition(from, to string) error {
return &InvalidTransitionError{From: from, To: to}
}

// InvalidStateError indicates an unknown negotiation state.
type InvalidStateError struct {
State string
}

func (e *InvalidStateError) Error() string {
return "invalid negotiation state: " + e.State
}

// InvalidTransitionError indicates a disallowed state transition.
type InvalidTransitionError struct {
From, To string
}

func (e *InvalidTransitionError) Error() string {
return "invalid transition: " + e.From + " -> " + e.To
}
