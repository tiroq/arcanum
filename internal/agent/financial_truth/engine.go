package financialtruth

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// Engine orchestrates the financial truth pipeline:
//
//	ingest event → normalize → persist fact → link → compute summary
type Engine struct {
	eventStore *EventStore
	factStore  *FactStore
	matchStore *MatchStore
	auditor    audit.AuditRecorder
	logger     *zap.Logger
}

// NewEngine creates an Engine with required stores.
func NewEngine(
	eventStore *EventStore,
	factStore *FactStore,
	matchStore *MatchStore,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Engine {
	return &Engine{
		eventStore: eventStore,
		factStore:  factStore,
		matchStore: matchStore,
		auditor:    auditor,
		logger:     logger,
	}
}

// IngestEvent records a raw financial event and normalizes it into a fact.
// Returns both the persisted event and the normalized fact.
func (e *Engine) IngestEvent(ctx context.Context, event FinancialEvent) (FinancialEvent, FinancialFact, error) {
	// Validate.
	if err := validateEvent(event); err != nil {
		return FinancialEvent{}, FinancialFact{}, fmt.Errorf("validate event: %w", err)
	}

	// Assign ID if not set.
	if event.ID == "" {
		event.ID = uuid.New().String()
	}

	// Persist raw event.
	saved, err := e.eventStore.Create(ctx, event)
	if err != nil {
		return FinancialEvent{}, FinancialFact{}, fmt.Errorf("persist event: %w", err)
	}

	e.auditEvent(ctx, "financial.event_recorded", map[string]any{
		"event_id":   saved.ID,
		"event_type": saved.EventType,
		"direction":  saved.Direction,
		"amount":     saved.Amount,
		"currency":   saved.Currency,
		"source":     saved.Source,
	})

	// Normalize into fact.
	fact := NormalizeEvent(saved)
	fact.ID = uuid.New().String()

	persistedFact, err := e.factStore.Create(ctx, fact)
	if err != nil {
		return saved, FinancialFact{}, fmt.Errorf("persist fact: %w", err)
	}

	e.auditEvent(ctx, "financial.fact_normalized", map[string]any{
		"fact_id":    persistedFact.ID,
		"event_id":   saved.ID,
		"fact_type":  persistedFact.FactType,
		"amount":     persistedFact.Amount,
		"verified":   persistedFact.Verified,
		"confidence": persistedFact.Confidence,
		"source":     persistedFact.Source,
	})

	e.logger.Info("financial event ingested",
		zap.String("event_id", saved.ID),
		zap.String("fact_id", persistedFact.ID),
		zap.String("type", persistedFact.FactType),
		zap.Float64("amount", persistedFact.Amount),
		zap.Bool("verified", persistedFact.Verified),
	)

	return saved, persistedFact, nil
}

// LinkFactToOpportunity manually links a financial fact to an opportunity/outcome.
// Creates an attribution match and updates the fact's linked IDs.
func (e *Engine) LinkFactToOpportunity(ctx context.Context, req LinkRequest) (AttributionMatch, error) {
	if req.FactID == "" {
		return AttributionMatch{}, fmt.Errorf("fact_id is required")
	}
	if req.OpportunityID == "" && req.OutcomeID == "" {
		return AttributionMatch{}, fmt.Errorf("at least one of opportunity_id or outcome_id is required")
	}

	// Verify fact exists.
	fact, err := e.factStore.GetByID(ctx, req.FactID)
	if err != nil {
		return AttributionMatch{}, fmt.Errorf("get fact: %w", err)
	}
	if fact.ID == "" {
		return AttributionMatch{}, fmt.Errorf("fact not found: %s", req.FactID)
	}

	// Check for duplicate links.
	if req.OpportunityID != "" {
		exists, err := e.matchStore.ExistsByFactAndOpportunity(ctx, req.FactID, req.OpportunityID)
		if err != nil {
			return AttributionMatch{}, fmt.Errorf("check duplicate: %w", err)
		}
		if exists {
			return AttributionMatch{}, fmt.Errorf("fact %s is already linked to opportunity %s", req.FactID, req.OpportunityID)
		}
	}

	link := BuildManualLink()

	match := AttributionMatch{
		ID:              uuid.New().String(),
		FactID:          req.FactID,
		OutcomeID:       req.OutcomeID,
		OpportunityID:   req.OpportunityID,
		MatchType:       link.MatchType,
		MatchConfidence: link.Confidence,
	}

	saved, err := e.matchStore.Create(ctx, match)
	if err != nil {
		return AttributionMatch{}, fmt.Errorf("create match: %w", err)
	}

	// Update fact with links.
	oppID := req.OpportunityID
	if oppID == "" {
		oppID = fact.LinkedOpportunityID
	}
	outcomeID := req.OutcomeID
	if outcomeID == "" {
		outcomeID = fact.LinkedOutcomeID
	}
	financiallyVerified := fact.Verified && (oppID != "" || outcomeID != "")

	if err := e.factStore.UpdateLinks(ctx, req.FactID, oppID, outcomeID, fact.LinkedProposalID, financiallyVerified); err != nil {
		e.logger.Warn("failed to update fact links", zap.Error(err))
	}

	e.auditEvent(ctx, "financial.fact_linked", map[string]any{
		"fact_id":              req.FactID,
		"opportunity_id":       req.OpportunityID,
		"outcome_id":           req.OutcomeID,
		"match_type":           saved.MatchType,
		"match_confidence":     saved.MatchConfidence,
		"financially_verified": financiallyVerified,
	})

	e.logger.Info("financial fact linked",
		zap.String("fact_id", req.FactID),
		zap.String("opportunity_id", req.OpportunityID),
		zap.String("match_type", saved.MatchType),
	)

	return saved, nil
}

// GetSummary returns the current month's financial truth summary.
func (e *Engine) GetSummary(ctx context.Context) FinancialSummary {
	month := CurrentMonth()
	summary := ComputeSummary(ctx, e.factStore, month)

	e.auditEvent(ctx, "financial.summary_updated", map[string]any{
		"month":                      month,
		"income_verified":            summary.CurrentMonthIncomeVerified,
		"expenses_verified":          summary.CurrentMonthExpensesVerified,
		"net_verified":               summary.CurrentMonthNetVerified,
		"pending_unverified_inflow":  summary.PendingUnverifiedInflow,
		"pending_unverified_outflow": summary.PendingUnverifiedOutflow,
		"total_facts":                summary.TotalFacts,
		"verified_facts":             summary.VerifiedFacts,
	})

	return summary
}

// GetTruthSignal returns a verified financial truth signal for upstream consumers
// (financial pressure, learning). Fail-open: returns empty signal if no data.
func (e *Engine) GetTruthSignal(ctx context.Context) FinancialTruthSignal {
	month := CurrentMonth()
	income, expenses, err := e.factStore.SumVerifiedByMonth(ctx, month)
	if err != nil {
		return FinancialTruthSignal{}
	}
	hasData := income > 0 || expenses > 0
	return FinancialTruthSignal{
		VerifiedMonthlyIncome:   income,
		VerifiedMonthlyExpenses: expenses,
		VerifiedNetIncome:       income - expenses,
		HasVerifiedData:         hasData,
	}
}

// GetVerifiedValueForOpportunity returns the total verified income amount
// linked to a specific opportunity. Returns 0 if no verified facts exist.
func (e *Engine) GetVerifiedValueForOpportunity(ctx context.Context, oppID string) float64 {
	facts, err := e.factStore.ListByOpportunityID(ctx, oppID)
	if err != nil {
		return 0
	}
	var total float64
	for _, f := range facts {
		if f.Verified && f.FactType == FactTypeIncome {
			total += f.Amount
		}
	}
	return total
}

// ListEvents returns recent financial events.
func (e *Engine) ListEvents(ctx context.Context, limit, offset int) ([]FinancialEvent, error) {
	return e.eventStore.List(ctx, limit, offset)
}

// ListFacts returns recent financial facts.
func (e *Engine) ListFacts(ctx context.Context, limit, offset int) ([]FinancialFact, error) {
	return e.factStore.List(ctx, limit, offset)
}

// RecomputeSummary forces a summary recomputation for the current month.
func (e *Engine) RecomputeSummary(ctx context.Context) FinancialSummary {
	return e.GetSummary(ctx)
}

// --- validation ---

func validateEvent(e FinancialEvent) error {
	if e.EventType == "" {
		return fmt.Errorf("event_type is required")
	}
	if !validEventTypes[e.EventType] {
		return fmt.Errorf("invalid event_type %q", e.EventType)
	}
	if e.Direction == "" {
		return fmt.Errorf("direction is required")
	}
	if !validDirections[e.Direction] {
		return fmt.Errorf("invalid direction %q", e.Direction)
	}
	if e.Amount <= 0 {
		return fmt.Errorf("amount must be positive")
	}
	if e.Source == "" {
		return fmt.Errorf("source is required")
	}
	if e.OccurredAt.IsZero() {
		return fmt.Errorf("occurred_at is required")
	}
	return nil
}

// --- audit helper ---

func (e *Engine) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if e.auditor == nil {
		return
	}
	e.auditor.RecordEvent(ctx, "financial_truth", uuid.Nil, //nolint:errcheck
		eventType, "financial_truth_engine", "system", payload)
}
