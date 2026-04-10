package pricing

import (
"testing"
)

// --- Price Band Computation Tests ---

func TestComputePriceBands_BasicConsulting(t *testing.T) {
input := PricingInput{
OpportunityID:        "opp-1",
EstimatedEffortHours: 10,
StrategyType:         "consulting",
StrategyID:           "strat-1",
}
perf := PricingPerformance{} // no history

p := ComputePriceBands(input, perf)

// cost_basis = 10 * 100 = 1000
// minimum = 1000 * 1.2 = 1200
// target  = 1000 * 1.8 = 1800
// stretch = 1000 * 2.4 = 2400
if p.CostBasis != 1000 {
t.Errorf("cost_basis: got %v, want 1000", p.CostBasis)
}
if p.MinimumPrice != 1200 {
t.Errorf("minimum: got %v, want 1200", p.MinimumPrice)
}
if p.TargetPrice != 1800 {
t.Errorf("target: got %v, want 1800", p.TargetPrice)
}
if p.StretchPrice != 2400 {
t.Errorf("stretch: got %v, want 2400", p.StretchPrice)
}
if p.Confidence != PricingConfidenceDefault {
t.Errorf("confidence: got %v, want %v", p.Confidence, PricingConfidenceDefault)
}
}

func TestComputePriceBands_HigherEffortRaisesFloor(t *testing.T) {
small := PricingInput{
OpportunityID:        "opp-small",
EstimatedEffortHours: 5,
}
large := PricingInput{
OpportunityID:        "opp-large",
EstimatedEffortHours: 20,
}
perf := PricingPerformance{}

ps := ComputePriceBands(small, perf)
pl := ComputePriceBands(large, perf)

if pl.MinimumPrice <= ps.MinimumPrice {
t.Errorf("higher effort should raise floor: small=%v large=%v", ps.MinimumPrice, pl.MinimumPrice)
}
if pl.TargetPrice <= ps.TargetPrice {
t.Errorf("higher effort should raise target: small=%v large=%v", ps.TargetPrice, pl.TargetPrice)
}
}

func TestComputePriceBands_CapacityOverloadRaisesFloor(t *testing.T) {
base := PricingInput{
OpportunityID:        "opp-1",
EstimatedEffortHours: 10,
CapacityPenalty:      0,
}
overloaded := PricingInput{
OpportunityID:        "opp-2",
EstimatedEffortHours: 10,
CapacityPenalty:      0.15, // max capacity penalty
}
perf := PricingPerformance{}

pb := ComputePriceBands(base, perf)
po := ComputePriceBands(overloaded, perf)

if po.MinimumPrice <= pb.MinimumPrice {
t.Errorf("capacity overload should raise floor: base=%v overloaded=%v", pb.MinimumPrice, po.MinimumPrice)
}
}

func TestComputePriceBands_PressureCannotPushBelowMinimum(t *testing.T) {
input := PricingInput{
OpportunityID:        "opp-1",
EstimatedEffortHours: 10,
FinancialPressure:    1.0, // max pressure
}
perf := PricingPerformance{}

p := ComputePriceBands(input, perf)

// Pressure should raise floor, not lower it.
baseMin := 10 * BaseHourlyRate * MinimumPriceMultiplier
if p.MinimumPrice < baseMin {
t.Errorf("pressure cannot push below base minimum: got %v, base=%v", p.MinimumPrice, baseMin)
}
// Invariant: minimum <= target <= stretch.
if p.TargetPrice < p.MinimumPrice {
t.Errorf("target (%v) must be >= minimum (%v)", p.TargetPrice, p.MinimumPrice)
}
if p.StretchPrice < p.TargetPrice {
t.Errorf("stretch (%v) must be >= target (%v)", p.StretchPrice, p.TargetPrice)
}
}

func TestComputePriceBands_StrategyPerformanceAdjustsTarget(t *testing.T) {
input := PricingInput{
OpportunityID:        "opp-1",
EstimatedEffortHours: 10,
StrategyID:           "strat-1",
}

highWin := PricingPerformance{
StrategyID:    "strat-1",
WinRate:       0.90,
TotalOutcomes: 10,
}
lowWin := PricingPerformance{
StrategyID:    "strat-1",
WinRate:       0.20,
TotalOutcomes: 10,
}

ph := ComputePriceBands(input, highWin)
pl := ComputePriceBands(input, lowWin)

if ph.TargetPrice <= pl.TargetPrice {
t.Errorf("high win rate should raise target: high=%v low=%v", ph.TargetPrice, pl.TargetPrice)
}
}

func TestComputePriceBands_NoHistoricalData_DeterministicDefault(t *testing.T) {
input := PricingInput{
OpportunityID:        "opp-1",
EstimatedEffortHours: 10,
}
perf := PricingPerformance{} // zero value

p := ComputePriceBands(input, perf)

// Should produce deterministic default pricing.
if p.CostBasis != 1000 {
t.Errorf("cost_basis: got %v, want 1000", p.CostBasis)
}
if p.MinimumPrice != 1200 {
t.Errorf("minimum: got %v, want 1200", p.MinimumPrice)
}
if p.TargetPrice != 1800 {
t.Errorf("target: got %v, want 1800", p.TargetPrice)
}
if p.Confidence != PricingConfidenceDefault {
t.Errorf("confidence: got %v, want %v", p.Confidence, PricingConfidenceDefault)
}
}

func TestComputePriceBands_ZeroEffort_FallsBackToMinimumHour(t *testing.T) {
input := PricingInput{
OpportunityID:        "opp-1",
EstimatedEffortHours: 0,
}
perf := PricingPerformance{}

p := ComputePriceBands(input, perf)

if p.CostBasis != BaseHourlyRate {
t.Errorf("zero effort should default to 1 hr cost: got %v", p.CostBasis)
}
}

// --- Concession Tests ---

func TestConcession_DecreasesTargetButNotBelowFloor(t *testing.T) {
currentOffer := 1800.0
minimumPrice := 1200.0

result := ComputeConcession(currentOffer, minimumPrice, 0)

if result.NewOfferedPrice >= currentOffer {
t.Errorf("concession should decrease price: got %v, current %v", result.NewOfferedPrice, currentOffer)
}
if result.NewOfferedPrice < minimumPrice {
t.Errorf("concession should not breach floor: got %v, floor %v", result.NewOfferedPrice, minimumPrice)
}
}

func TestConcession_CountCapEnforced(t *testing.T) {
result := ComputeConcession(1800, 1200, MaxConcessionCount)

if result.NewOfferedPrice != 1800 {
t.Errorf("at max concessions, price should not change: got %v", result.NewOfferedPrice)
}
if result.ConcessionCount != MaxConcessionCount {
t.Errorf("concession count should remain at max: got %v", result.ConcessionCount)
}
}

func TestConcession_ConvergesToFloor(t *testing.T) {
offer := 1800.0
floor := 1200.0

for i := 0; i < MaxConcessionCount; i++ {
result := ComputeConcession(offer, floor, i)
if result.NewOfferedPrice < floor {
t.Errorf("step %d: price %v breached floor %v", i, result.NewOfferedPrice, floor)
}
offer = result.NewOfferedPrice
}

if offer < floor {
t.Errorf("after all concessions, price %v should be >= floor %v", offer, floor)
}
}

func TestConcession_AtFloorNoChange(t *testing.T) {
result := ComputeConcession(1200, 1200, 0)

if result.NewOfferedPrice != 1200 {
t.Errorf("at floor, price should not change: got %v", result.NewOfferedPrice)
}
if !result.AtFloor {
t.Error("should flag at_floor when price equals minimum")
}
}

// --- Negotiation State Machine Tests ---

func TestValidateTransition_ValidTransitions(t *testing.T) {
valid := []struct {
from, to string
}{
{StateUnpriced, StateInitialOfferPrepared},
{StateInitialOfferPrepared, StateAwaitingResponse},
{StateInitialOfferPrepared, StateCounterOfferNeeded},
{StateInitialOfferPrepared, StateLost},
{StateAwaitingResponse, StateCounterOfferNeeded},
{StateAwaitingResponse, StateWon},
{StateAwaitingResponse, StateLost},
{StateCounterOfferNeeded, StateAwaitingResponse},
{StateCounterOfferNeeded, StateLost},
}

for _, tt := range valid {
if err := ValidateTransition(tt.from, tt.to); err != nil {
t.Errorf("expected valid: %s -> %s, got error: %v", tt.from, tt.to, err)
}
}
}

func TestValidateTransition_InvalidTransitions(t *testing.T) {
invalid := []struct {
from, to string
}{
{StateUnpriced, StateWon},
{StateUnpriced, StateLost},
{StateUnpriced, StateCounterOfferNeeded},
{StateWon, StateUnpriced},
{StateWon, StateLost},
{StateLost, StateWon},
{StateLost, StateUnpriced},
{StateAwaitingResponse, StateUnpriced},
{StateAwaitingResponse, StateInitialOfferPrepared},
}

for _, tt := range invalid {
if err := ValidateTransition(tt.from, tt.to); err == nil {
t.Errorf("expected invalid: %s -> %s, got nil error", tt.from, tt.to)
}
}
}

func TestValidateTransition_InvalidStates(t *testing.T) {
err := ValidateTransition("bogus", StateWon)
if err == nil {
t.Error("expected error for invalid source state")
}

err = ValidateTransition(StateUnpriced, "bogus")
if err == nil {
t.Error("expected error for invalid target state")
}
}

// --- Learning / Performance Tests ---

func TestComputePerformance_AcceptedQuotedRatio(t *testing.T) {
outcomes := []PricingOutcome{
{QuotedPrice: 2000, AcceptedPrice: 1800, Won: true},
{QuotedPrice: 3000, AcceptedPrice: 2700, Won: true},
}

perf := ComputePerformanceFromOutcomes("strat-1", outcomes)

// avg_quoted = (2000+3000)/2 = 2500
// avg_accepted = (1800+2700)/2 = 2250
// discount = 1 - 2250/2500 = 0.10
if perf.AvgQuotedPrice != 2500 {
t.Errorf("avg_quoted: got %v, want 2500", perf.AvgQuotedPrice)
}
if perf.AvgAcceptedPrice != 2250 {
t.Errorf("avg_accepted: got %v, want 2250", perf.AvgAcceptedPrice)
}
if perf.AvgDiscountRate != 0.10 {
t.Errorf("avg_discount: got %v, want 0.10", perf.AvgDiscountRate)
}
}

func TestComputePerformance_WinLossUpdatesWinRate(t *testing.T) {
outcomes := []PricingOutcome{
{QuotedPrice: 2000, AcceptedPrice: 1800, Won: true},
{QuotedPrice: 3000, AcceptedPrice: 0, Won: false},
{QuotedPrice: 1500, AcceptedPrice: 1500, Won: true},
{QuotedPrice: 2500, AcceptedPrice: 0, Won: false},
}

perf := ComputePerformanceFromOutcomes("strat-1", outcomes)

// win_rate = 2/4 = 0.5
if perf.WinRate != 0.5 {
t.Errorf("win_rate: got %v, want 0.5", perf.WinRate)
}
if perf.TotalOutcomes != 4 {
t.Errorf("total_outcomes: got %v, want 4", perf.TotalOutcomes)
}
}

func TestComputePerformance_UpdatedFromVerifiedOutcomes(t *testing.T) {
outcomes := []PricingOutcome{
{QuotedPrice: 1000, AcceptedPrice: 900, Won: true},
{QuotedPrice: 1000, AcceptedPrice: 800, Won: true},
{QuotedPrice: 1000, AcceptedPrice: 0, Won: false},
}

perf := ComputePerformanceFromOutcomes("strat-1", outcomes)

// win_rate = 2/3 ≈ 0.6667
expectedWinRate := roundToFourDecimals(2.0 / 3.0)
if perf.WinRate != expectedWinRate {
t.Errorf("win_rate: got %v, want %v", perf.WinRate, expectedWinRate)
}
// avg_accepted = (900+800)/2 = 850
if perf.AvgAcceptedPrice != 850 {
t.Errorf("avg_accepted: got %v, want 850", perf.AvgAcceptedPrice)
}
}

func TestComputePerformance_Empty(t *testing.T) {
perf := ComputePerformanceFromOutcomes("strat-1", nil)
if perf.StrategyID != "strat-1" {
t.Errorf("strategy_id: got %v, want strat-1", perf.StrategyID)
}
if perf.TotalOutcomes != 0 {
t.Errorf("total_outcomes: got %v, want 0", perf.TotalOutcomes)
}
}

func TestComputePerformance_AllLost(t *testing.T) {
outcomes := []PricingOutcome{
{QuotedPrice: 2000, Won: false},
{QuotedPrice: 3000, Won: false},
}

perf := ComputePerformanceFromOutcomes("strat-1", outcomes)

if perf.WinRate != 0 {
t.Errorf("win_rate: got %v, want 0", perf.WinRate)
}
if perf.AvgAcceptedPrice != 0 {
t.Errorf("avg_accepted: got %v, want 0", perf.AvgAcceptedPrice)
}
if perf.AvgDiscountRate != 0 {
t.Errorf("avg_discount: got %v, want 0", perf.AvgDiscountRate)
}
}

// --- Message Type Tests ---

func TestRecommendMessageType(t *testing.T) {
tests := []struct {
state    string
expected string
}{
{StateUnpriced, MessageTypeInitialQuote},
{StateInitialOfferPrepared, MessageTypeInitialQuote},
{StateCounterOfferNeeded, MessageTypeCounterOffer},
{StateAwaitingResponse, MessageTypeFollowUp},
{StateWon, MessageTypeAcceptance},
{StateLost, MessageTypeDraftProposal},
}

for _, tt := range tests {
got := RecommendMessageType(tt.state)
if got != tt.expected {
t.Errorf("state %s: got %s, want %s", tt.state, got, tt.expected)
}
}
}

// --- Confidence Tests ---

func TestConfidence_BelowMinOutcomes_ReturnsDefault(t *testing.T) {
conf := computeConfidence(0)
if conf != PricingConfidenceDefault {
t.Errorf("got %v, want %v", conf, PricingConfidenceDefault)
}

conf = computeConfidence(2)
if conf != PricingConfidenceDefault {
t.Errorf("got %v, want %v", conf, PricingConfidenceDefault)
}
}

func TestConfidence_IncreasesWithOutcomes(t *testing.T) {
c3 := computeConfidence(3)
c10 := computeConfidence(10)
c100 := computeConfidence(100)

if c10 <= c3 {
t.Errorf("more outcomes should increase confidence: c3=%v c10=%v", c3, c10)
}
if c100 <= c10 {
t.Errorf("more outcomes should increase confidence: c10=%v c100=%v", c10, c100)
}
if c100 > 1.0 {
t.Errorf("confidence must be <= 1.0: got %v", c100)
}
}

// --- Invariant Tests ---

func TestPriceBands_MinTargetStretchInvariant(t *testing.T) {
cases := []PricingInput{
{OpportunityID: "1", EstimatedEffortHours: 1},
{OpportunityID: "2", EstimatedEffortHours: 100},
{OpportunityID: "3", EstimatedEffortHours: 10, FinancialPressure: 1.0},
{OpportunityID: "4", EstimatedEffortHours: 10, CapacityPenalty: 0.15},
{OpportunityID: "5", EstimatedEffortHours: 10, FinancialPressure: 1.0, CapacityPenalty: 0.15},
}

for _, input := range cases {
p := ComputePriceBands(input, PricingPerformance{})
if p.MinimumPrice > p.TargetPrice {
t.Errorf("opp %s: minimum (%v) > target (%v)", input.OpportunityID, p.MinimumPrice, p.TargetPrice)
}
if p.TargetPrice > p.StretchPrice {
t.Errorf("opp %s: target (%v) > stretch (%v)", input.OpportunityID, p.TargetPrice, p.StretchPrice)
}
}
}

// --- Nil-safe Adapter Tests ---

func TestGraphAdapter_NilSafe(t *testing.T) {
var a *GraphAdapter

// All methods should return zero values without panic.
p, err := a.ComputeProfile(nil, PricingInput{})
if err != nil {
t.Errorf("nil adapter ComputeProfile error: %v", err)
}
if p.ID != "" {
t.Errorf("nil adapter should return zero value")
}

r, err := a.Recommend(nil, "opp-1")
if err != nil {
t.Errorf("nil adapter Recommend error: %v", err)
}
if r.OpportunityID != "" {
t.Errorf("nil adapter should return zero value")
}

profiles, err := a.ListProfiles(nil)
if err != nil {
t.Errorf("nil adapter ListProfiles error: %v", err)
}
if profiles != nil {
t.Errorf("nil adapter should return nil")
}
}

// --- Error Type Tests ---

func TestInvalidStateError(t *testing.T) {
e := &InvalidStateError{State: "bogus"}
if e.Error() != "invalid negotiation state: bogus" {
t.Errorf("unexpected message: %s", e.Error())
}
}

func TestInvalidTransitionError(t *testing.T) {
e := &InvalidTransitionError{From: "a", To: "b"}
if e.Error() != "invalid transition: a -> b" {
t.Errorf("unexpected message: %s", e.Error())
}
}

// --- Pressure Floor Boost Bounded Test ---

func TestComputePriceBands_PressureFloorBoostBounded(t *testing.T) {
noPressure := PricingInput{
OpportunityID:        "opp-1",
EstimatedEffortHours: 10,
FinancialPressure:    0,
}
maxPressure := PricingInput{
OpportunityID:        "opp-2",
EstimatedEffortHours: 10,
FinancialPressure:    1.0,
}
perf := PricingPerformance{}

pn := ComputePriceBands(noPressure, perf)
pm := ComputePriceBands(maxPressure, perf)

// Max boost = 1.0 * 0.20 = 20% above base minimum.
expectedMaxMin := pn.MinimumPrice * (1 + MaxPressureFloorBoost)
if pm.MinimumPrice != roundToTwoDecimals(expectedMaxMin) {
t.Errorf("max pressure floor: got %v, want %v", pm.MinimumPrice, roundToTwoDecimals(expectedMaxMin))
}
}

// --- Cold-Start Performance Ignored Test ---

func TestComputePriceBands_ColdStartPerformanceIgnored(t *testing.T) {
input := PricingInput{
OpportunityID:        "opp-1",
EstimatedEffortHours: 10,
}
// Too few outcomes — should be ignored.
perf := PricingPerformance{
WinRate:       0.90,
TotalOutcomes: 2, // below MinOutcomesForLearning
}

cold := ComputePriceBands(input, perf)
noHistory := ComputePriceBands(input, PricingPerformance{})

if cold.TargetPrice != noHistory.TargetPrice {
t.Errorf("cold-start should equal no-history: got %v vs %v", cold.TargetPrice, noHistory.TargetPrice)
}
}
