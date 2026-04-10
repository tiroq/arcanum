package financialtruth

import (
	"testing"
	"time"
)

// --- Event / Fact Normalization Tests ---

func TestNormalize_InflowToIncomeFact(t *testing.T) {
	e := FinancialEvent{
		ID:         "evt-1",
		Source:     "bank",
		EventType:  EventPaymentReceived,
		Direction:  DirectionInflow,
		Amount:     500.0,
		Currency:   "USD",
		OccurredAt: time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
	}
	f := NormalizeEvent(e)
	if f.FactType != FactTypeIncome {
		t.Fatalf("expected income, got %s", f.FactType)
	}
	if f.Amount != 500.0 {
		t.Fatalf("expected 500, got %f", f.Amount)
	}
	if !f.Verified {
		t.Fatal("bank source should be verified")
	}
	if f.Confidence != VerifiedConfidence {
		t.Fatalf("expected confidence %f, got %f", VerifiedConfidence, f.Confidence)
	}
}

func TestNormalize_OutflowToExpenseFact(t *testing.T) {
	e := FinancialEvent{
		ID:         "evt-2",
		Source:     "manual",
		EventType:  EventExpenseRecorded,
		Direction:  DirectionOutflow,
		Amount:     200.0,
		Currency:   "USD",
		OccurredAt: time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
	}
	f := NormalizeEvent(e)
	if f.FactType != FactTypeExpense {
		t.Fatalf("expected expense, got %s", f.FactType)
	}
	if f.Amount != 200.0 {
		t.Fatalf("expected 200, got %f", f.Amount)
	}
	if !f.Verified {
		t.Fatal("manual source should be verified")
	}
}

func TestNormalize_TransferDoesNotCountAsIncome(t *testing.T) {
	e := FinancialEvent{
		ID:         "evt-3",
		Source:     "bank",
		EventType:  EventTransferIn,
		Direction:  DirectionInflow,
		Amount:     1000.0,
		Currency:   "USD",
		OccurredAt: time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
	}
	f := NormalizeEvent(e)
	if f.FactType != FactTypeTransfer {
		t.Fatalf("expected transfer, got %s", f.FactType)
	}
	if f.FactType == FactTypeIncome {
		t.Fatal("transfers should not be treated as income by default")
	}
}

func TestNormalize_VerifiedPreserved(t *testing.T) {
	// Bank source → verified.
	bankEvent := FinancialEvent{
		ID: "evt-4", Source: "bank", EventType: EventPaymentReceived,
		Direction: DirectionInflow, Amount: 100, Currency: "USD",
		OccurredAt: time.Now(),
	}
	if !NormalizeEvent(bankEvent).Verified {
		t.Fatal("bank should be verified")
	}

	// System source → not verified.
	sysEvent := FinancialEvent{
		ID: "evt-5", Source: "system", EventType: EventPaymentReceived,
		Direction: DirectionInflow, Amount: 100, Currency: "USD",
		OccurredAt: time.Now(),
	}
	if NormalizeEvent(sysEvent).Verified {
		t.Fatal("system source should not be verified")
	}

	// External → not verified.
	extEvent := FinancialEvent{
		ID: "evt-6", Source: "external", EventType: EventPaymentReceived,
		Direction: DirectionInflow, Amount: 100, Currency: "USD",
		OccurredAt: time.Now(),
	}
	if NormalizeEvent(extEvent).Verified {
		t.Fatal("external source should not be verified")
	}
}

func TestNormalize_ConfidenceDeterministic(t *testing.T) {
	sources := map[string]float64{
		"bank":     VerifiedConfidence,
		"manual":   ManualSourceConfidence,
		"invoice":  ManualSourceConfidence,
		"system":   SystemSourceConfidence,
		"external": ExternalSourceConfidence,
		"unknown":  ExternalSourceConfidence,
	}
	for src, expected := range sources {
		e := FinancialEvent{
			ID: "evt-" + src, Source: src, EventType: EventPaymentReceived,
			Direction: DirectionInflow, Amount: 100, Currency: "USD",
			OccurredAt: time.Now(),
		}
		f := NormalizeEvent(e)
		if f.Confidence != expected {
			t.Errorf("source %q: expected confidence %f, got %f", src, expected, f.Confidence)
		}
	}
}

func TestNormalize_InvoicePaidClassifiedAsExpense(t *testing.T) {
	e := FinancialEvent{
		ID: "evt-inv", Source: "invoice", EventType: EventInvoicePaid,
		Direction: DirectionOutflow, Amount: 350, Currency: "USD",
		OccurredAt: time.Now(),
	}
	f := NormalizeEvent(e)
	if f.FactType != FactTypeExpense {
		t.Fatalf("expected expense for outflow invoice, got %s", f.FactType)
	}
}

func TestNormalize_SubscriptionChargeClassifiedAsExpense(t *testing.T) {
	e := FinancialEvent{
		ID: "evt-sub", Source: "system", EventType: EventSubscriptionCharge,
		Direction: DirectionOutflow, Amount: 15, Currency: "USD",
		OccurredAt: time.Now(),
	}
	f := NormalizeEvent(e)
	if f.FactType != FactTypeExpense {
		t.Fatalf("expected expense for subscription charge, got %s", f.FactType)
	}
}

func TestNormalize_TransferOutClassifiedAsTransfer(t *testing.T) {
	e := FinancialEvent{
		ID: "evt-tout", Source: "bank", EventType: EventTransferOut,
		Direction: DirectionOutflow, Amount: 500, Currency: "USD",
		OccurredAt: time.Now(),
	}
	f := NormalizeEvent(e)
	if f.FactType != FactTypeTransfer {
		t.Fatalf("expected transfer, got %s", f.FactType)
	}
}

// --- Linking Tests ---

func TestLink_ExactIdentifierMatch(t *testing.T) {
	fact := FinancialFact{
		ID:                  "fact-1",
		LinkedOpportunityID: "opp-123",
	}
	result := AttemptExactLink(fact, "opp-123", "")
	if !result.Linked {
		t.Fatal("expected exact link")
	}
	if result.MatchType != MatchTypeExact {
		t.Fatalf("expected exact match type, got %s", result.MatchType)
	}
	if result.Confidence != ExactMatchConfidence {
		t.Fatalf("expected confidence %f, got %f", ExactMatchConfidence, result.Confidence)
	}
}

func TestLink_ExternalRefMatch(t *testing.T) {
	fact := FinancialFact{ID: "fact-2"}
	result := AttemptExactLink(fact, "opp-456", "opp-456")
	if !result.Linked {
		t.Fatal("expected exact link via external_ref")
	}
	if result.MatchType != MatchTypeExact {
		t.Fatalf("expected exact, got %s", result.MatchType)
	}
}

func TestLink_HeuristicAmountAndDateMatch(t *testing.T) {
	factDate := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	outcomeDate := time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC)
	result := AttemptHeuristicLink(500.0, factDate, 500.0, outcomeDate)
	if !result.Linked {
		t.Fatal("expected heuristic link (exact amounts, 2 days apart)")
	}
	if result.MatchType != MatchTypeHeuristic {
		t.Fatalf("expected heuristic, got %s", result.MatchType)
	}
}

func TestLink_LowConfidenceDoesNotAutoLink(t *testing.T) {
	// Amounts differ by more than 5%.
	factDate := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	outcomeDate := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	result := AttemptHeuristicLink(500.0, factDate, 600.0, outcomeDate)
	if result.Linked {
		t.Fatal("should not link when amounts differ by >5%")
	}
}

func TestLink_DatesTooFarApart(t *testing.T) {
	factDate := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	outcomeDate := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	result := AttemptHeuristicLink(500.0, factDate, 500.0, outcomeDate)
	if result.Linked {
		t.Fatal("should not link when dates >7 days apart")
	}
}

func TestLink_ManualLinkAlwaysSucceeds(t *testing.T) {
	result := BuildManualLink()
	if !result.Linked {
		t.Fatal("manual link should always succeed")
	}
	if result.MatchType != MatchTypeManual {
		t.Fatalf("expected manual, got %s", result.MatchType)
	}
	if result.Confidence != ManualMatchConfidence {
		t.Fatalf("expected confidence %f, got %f", ManualMatchConfidence, result.Confidence)
	}
}

func TestLink_ZeroAmountsDoNotLink(t *testing.T) {
	now := time.Now()
	result := AttemptHeuristicLink(0, now, 500, now)
	if result.Linked {
		t.Fatal("should not link with zero fact amount")
	}
	result = AttemptHeuristicLink(500, now, 0, now)
	if result.Linked {
		t.Fatal("should not link with zero outcome amount")
	}
}

func TestLink_NoExactMatchWhenIDsDiffer(t *testing.T) {
	fact := FinancialFact{ID: "fact-x", LinkedOpportunityID: "opp-999"}
	result := AttemptExactLink(fact, "opp-111", "")
	if result.Linked {
		t.Fatal("should not match when IDs differ")
	}
}

// --- Validation Tests ---

func TestValidateEvent_MissingEventType(t *testing.T) {
	e := FinancialEvent{
		Direction: DirectionInflow, Amount: 100, Source: "manual",
		OccurredAt: time.Now(),
	}
	if err := validateEvent(e); err == nil {
		t.Fatal("expected error for missing event_type")
	}
}

func TestValidateEvent_InvalidEventType(t *testing.T) {
	e := FinancialEvent{
		EventType: "magic_money", Direction: DirectionInflow,
		Amount: 100, Source: "manual", OccurredAt: time.Now(),
	}
	if err := validateEvent(e); err == nil {
		t.Fatal("expected error for invalid event_type")
	}
}

func TestValidateEvent_MissingDirection(t *testing.T) {
	e := FinancialEvent{
		EventType: EventPaymentReceived, Amount: 100, Source: "manual",
		OccurredAt: time.Now(),
	}
	if err := validateEvent(e); err == nil {
		t.Fatal("expected error for missing direction")
	}
}

func TestValidateEvent_InvalidDirection(t *testing.T) {
	e := FinancialEvent{
		EventType: EventPaymentReceived, Direction: "sideways",
		Amount: 100, Source: "manual", OccurredAt: time.Now(),
	}
	if err := validateEvent(e); err == nil {
		t.Fatal("expected error for invalid direction")
	}
}

func TestValidateEvent_ZeroAmount(t *testing.T) {
	e := FinancialEvent{
		EventType: EventPaymentReceived, Direction: DirectionInflow,
		Amount: 0, Source: "manual", OccurredAt: time.Now(),
	}
	if err := validateEvent(e); err == nil {
		t.Fatal("expected error for zero amount")
	}
}

func TestValidateEvent_NegativeAmount(t *testing.T) {
	e := FinancialEvent{
		EventType: EventPaymentReceived, Direction: DirectionInflow,
		Amount: -50, Source: "manual", OccurredAt: time.Now(),
	}
	if err := validateEvent(e); err == nil {
		t.Fatal("expected error for negative amount")
	}
}

func TestValidateEvent_MissingSource(t *testing.T) {
	e := FinancialEvent{
		EventType: EventPaymentReceived, Direction: DirectionInflow,
		Amount: 100, OccurredAt: time.Now(),
	}
	if err := validateEvent(e); err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestValidateEvent_MissingOccurredAt(t *testing.T) {
	e := FinancialEvent{
		EventType: EventPaymentReceived, Direction: DirectionInflow,
		Amount: 100, Source: "manual",
	}
	if err := validateEvent(e); err == nil {
		t.Fatal("expected error for missing occurred_at")
	}
}

func TestValidateEvent_ValidEvent(t *testing.T) {
	e := FinancialEvent{
		EventType: EventPaymentReceived, Direction: DirectionInflow,
		Amount: 500, Source: "bank", OccurredAt: time.Now(),
	}
	if err := validateEvent(e); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Adapter Nil Safety Tests ---

func TestAdapter_NilSafety(t *testing.T) {
	var adapter *GraphAdapter
	sig := adapter.GetTruthSignal(nil) //nolint:staticcheck
	if sig.HasVerifiedData {
		t.Fatal("nil adapter should return empty signal")
	}
	val := adapter.GetVerifiedIncomeForOpportunity(nil, "opp-1") //nolint:staticcheck
	if val != 0 {
		t.Fatalf("nil adapter should return 0, got %f", val)
	}
	summary := adapter.GetSummary(nil) //nolint:staticcheck
	if summary.Month != "" {
		t.Fatal("nil adapter should return empty summary")
	}
}

func TestAdapter_NilEngine(t *testing.T) {
	adapter := &GraphAdapter{engine: nil}
	sig := adapter.GetTruthSignal(nil) //nolint:staticcheck
	if sig.HasVerifiedData {
		t.Fatal("nil engine should return empty signal")
	}
}

// --- Financial Truth Signal Tests ---

func TestTruthSignal_VerifiedSourcePrefersBank(t *testing.T) {
	if !isVerifiedSource("bank") {
		t.Fatal("bank should be verified source")
	}
	if !isVerifiedSource("manual") {
		t.Fatal("manual should be verified source")
	}
	if !isVerifiedSource("invoice") {
		t.Fatal("invoice should be verified source")
	}
	if isVerifiedSource("system") {
		t.Fatal("system should NOT be verified source")
	}
	if isVerifiedSource("external") {
		t.Fatal("external should NOT be verified source")
	}
}

// --- Heuristic Linking Edge Cases ---

func TestLink_HeuristicWithin5PercentAmount(t *testing.T) {
	now := time.Now()
	// 4% difference → should link (1000/960 = 1.0417).
	result := AttemptHeuristicLink(1000, now, 960, now)
	if !result.Linked {
		t.Fatal("expected link for 4% difference")
	}
	// 6% difference → should not link (1000/940 = 1.0638).
	result = AttemptHeuristicLink(1000, now, 940, now)
	if result.Linked {
		t.Fatal("should not link for >5% difference")
	}
}

func TestLink_HeuristicWithin7Days(t *testing.T) {
	base := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	// 7 days → should link.
	result := AttemptHeuristicLink(100, base, 100, base.AddDate(0, 0, -7))
	if !result.Linked {
		t.Fatal("expected link for exactly 7 days apart")
	}
	// 8 days → should not link.
	result = AttemptHeuristicLink(100, base, 100, base.AddDate(0, 0, -8))
	if result.Linked {
		t.Fatal("should not link for 8 days apart")
	}
}

// --- Determinism Tests ---

func TestNormalize_Deterministic(t *testing.T) {
	e := FinancialEvent{
		ID: "evt-det", Source: "bank", EventType: EventPaymentReceived,
		Direction: DirectionInflow, Amount: 750, Currency: "USD",
		OccurredAt: time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC),
	}
	f1 := NormalizeEvent(e)
	f2 := NormalizeEvent(e)
	f3 := NormalizeEvent(e)
	if f1.FactType != f2.FactType || f2.FactType != f3.FactType {
		t.Fatal("normalization not deterministic: fact types differ")
	}
	if f1.Confidence != f2.Confidence || f2.Confidence != f3.Confidence {
		t.Fatal("normalization not deterministic: confidences differ")
	}
	if f1.Verified != f2.Verified || f2.Verified != f3.Verified {
		t.Fatal("normalization not deterministic: verified flags differ")
	}
}

// --- Fail-Open Tests ---

func TestFailOpen_NilAdapterReturnsZero(t *testing.T) {
	var a *GraphAdapter
	sig := a.GetTruthSignal(nil) //nolint:staticcheck
	if sig.VerifiedMonthlyIncome != 0 {
		t.Fatal("expected zero income from nil adapter")
	}
	if sig.VerifiedMonthlyExpenses != 0 {
		t.Fatal("expected zero expenses from nil adapter")
	}
}

// --- Constants Sanity ---

func TestConstants_Sanity(t *testing.T) {
	if VerifiedConfidence < ManualSourceConfidence {
		t.Fatal("verified confidence should be >= manual")
	}
	if ManualSourceConfidence < SystemSourceConfidence {
		t.Fatal("manual confidence should be >= system")
	}
	if SystemSourceConfidence < ExternalSourceConfidence {
		t.Fatal("system confidence should be >= external")
	}
	if MinLinkConfidence > HeuristicMatchConfidence {
		t.Fatal("MinLinkConfidence should not exceed HeuristicMatchConfidence")
	}
	if ExactMatchConfidence < HeuristicMatchConfidence {
		t.Fatal("exact match confidence should be >= heuristic")
	}
}
