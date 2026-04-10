package financialtruth

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EventStore persists and retrieves financial events.
type EventStore struct {
	pool *pgxpool.Pool
}

// NewEventStore creates an EventStore backed by the given pool.
func NewEventStore(pool *pgxpool.Pool) *EventStore {
	return &EventStore{pool: pool}
}

// Create inserts a new financial event. ID and CreatedAt are set if empty.
func (s *EventStore) Create(ctx context.Context, e FinancialEvent) (FinancialEvent, error) {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	if e.Currency == "" {
		e.Currency = "USD"
	}

	const q = `
INSERT INTO agent_financial_events
  (id, source, event_type, direction, amount, currency, description, external_ref, occurred_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, source, event_type, direction, amount, currency, description, external_ref, occurred_at, created_at`

	row := s.pool.QueryRow(ctx, q,
		e.ID, e.Source, e.EventType, e.Direction,
		e.Amount, e.Currency, e.Description, e.ExternalRef,
		e.OccurredAt, e.CreatedAt,
	)
	return scanEvent(row)
}

// List returns recent financial events ordered by occurred_at DESC.
func (s *EventStore) List(ctx context.Context, limit, offset int) ([]FinancialEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `
SELECT id, source, event_type, direction, amount, currency, description, external_ref, occurred_at, created_at
FROM agent_financial_events
ORDER BY occurred_at DESC
LIMIT $1 OFFSET $2`

	rows, err := s.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list financial events: %w", err)
	}
	defer rows.Close()

	var out []FinancialEvent
	for rows.Next() {
		var e FinancialEvent
		if err := rows.Scan(
			&e.ID, &e.Source, &e.EventType, &e.Direction,
			&e.Amount, &e.Currency, &e.Description, &e.ExternalRef,
			&e.OccurredAt, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan financial event: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetByExternalRef retrieves an event by its external reference.
func (s *EventStore) GetByExternalRef(ctx context.Context, ref string) (FinancialEvent, error) {
	const q = `
SELECT id, source, event_type, direction, amount, currency, description, external_ref, occurred_at, created_at
FROM agent_financial_events
WHERE external_ref = $1
LIMIT 1`

	row := s.pool.QueryRow(ctx, q, ref)
	return scanEvent(row)
}

// --- FactStore ---

// FactStore persists and retrieves financial facts.
type FactStore struct {
	pool *pgxpool.Pool
}

// NewFactStore creates a FactStore backed by the given pool.
func NewFactStore(pool *pgxpool.Pool) *FactStore {
	return &FactStore{pool: pool}
}

// Create inserts a new financial fact.
func (s *FactStore) Create(ctx context.Context, f FinancialFact) (FinancialFact, error) {
	if f.CreatedAt.IsZero() {
		f.CreatedAt = time.Now().UTC()
	}
	if f.Currency == "" {
		f.Currency = "USD"
	}

	const q = `
INSERT INTO agent_financial_facts
  (id, fact_type, amount, currency, verified, confidence, source, event_id,
   linked_opportunity_id, linked_outcome_id, linked_proposal_id, financially_verified,
   occurred_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING id, fact_type, amount, currency, verified, confidence, source, event_id,
  COALESCE(linked_opportunity_id, ''), COALESCE(linked_outcome_id, ''), COALESCE(linked_proposal_id, ''),
  financially_verified, occurred_at, created_at`

	row := s.pool.QueryRow(ctx, q,
		f.ID, f.FactType, f.Amount, f.Currency, f.Verified, f.Confidence,
		f.Source, f.EventID,
		nullIfEmpty(f.LinkedOpportunityID), nullIfEmpty(f.LinkedOutcomeID), nullIfEmpty(f.LinkedProposalID),
		f.FinanciallyVerified, f.OccurredAt, f.CreatedAt,
	)
	return scanFact(row)
}

// UpdateLinks updates the linked IDs and financially_verified flag on a fact.
func (s *FactStore) UpdateLinks(ctx context.Context, factID, oppID, outcomeID, proposalID string, financiallyVerified bool) error {
	const q = `
UPDATE agent_financial_facts
SET linked_opportunity_id = $2,
    linked_outcome_id = $3,
    linked_proposal_id = $4,
    financially_verified = $5
WHERE id = $1`

	_, err := s.pool.Exec(ctx, q,
		factID, nullIfEmpty(oppID), nullIfEmpty(outcomeID), nullIfEmpty(proposalID),
		financiallyVerified,
	)
	if err != nil {
		return fmt.Errorf("update fact links: %w", err)
	}
	return nil
}

// List returns financial facts ordered by occurred_at DESC.
func (s *FactStore) List(ctx context.Context, limit, offset int) ([]FinancialFact, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `
SELECT id, fact_type, amount, currency, verified, confidence, source, event_id,
  COALESCE(linked_opportunity_id, ''), COALESCE(linked_outcome_id, ''), COALESCE(linked_proposal_id, ''),
  financially_verified, occurred_at, created_at
FROM agent_financial_facts
ORDER BY occurred_at DESC
LIMIT $1 OFFSET $2`

	rows, err := s.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list financial facts: %w", err)
	}
	defer rows.Close()

	var out []FinancialFact
	for rows.Next() {
		f, err := scanFactRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetByID retrieves a single fact by ID.
func (s *FactStore) GetByID(ctx context.Context, id string) (FinancialFact, error) {
	const q = `
SELECT id, fact_type, amount, currency, verified, confidence, source, event_id,
  COALESCE(linked_opportunity_id, ''), COALESCE(linked_outcome_id, ''), COALESCE(linked_proposal_id, ''),
  financially_verified, occurred_at, created_at
FROM agent_financial_facts
WHERE id = $1`

	row := s.pool.QueryRow(ctx, q, id)
	return scanFact(row)
}

// ListByOpportunityID returns facts linked to a specific opportunity.
func (s *FactStore) ListByOpportunityID(ctx context.Context, oppID string) ([]FinancialFact, error) {
	const q = `
SELECT id, fact_type, amount, currency, verified, confidence, source, event_id,
  COALESCE(linked_opportunity_id, ''), COALESCE(linked_outcome_id, ''), COALESCE(linked_proposal_id, ''),
  financially_verified, occurred_at, created_at
FROM agent_financial_facts
WHERE linked_opportunity_id = $1
ORDER BY occurred_at DESC`

	rows, err := s.pool.Query(ctx, q, oppID)
	if err != nil {
		return nil, fmt.Errorf("list facts by opportunity: %w", err)
	}
	defer rows.Close()

	var out []FinancialFact
	for rows.Next() {
		f, err := scanFactRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SumVerifiedByMonth computes verified income and expense totals for a given month.
// month is in "2006-01" format.
func (s *FactStore) SumVerifiedByMonth(ctx context.Context, month string) (income, expenses float64, err error) {
	startDate, err := time.Parse("2006-01", month)
	if err != nil {
		return 0, 0, fmt.Errorf("parse month: %w", err)
	}
	endDate := startDate.AddDate(0, 1, 0)

	const q = `
SELECT fact_type, COALESCE(SUM(amount), 0) as total
FROM agent_financial_facts
WHERE verified = true
  AND occurred_at >= $1 AND occurred_at < $2
  AND fact_type IN ('income', 'expense')
GROUP BY fact_type`

	rows, err := s.pool.Query(ctx, q, startDate, endDate)
	if err != nil {
		return 0, 0, fmt.Errorf("sum verified by month: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var factType string
		var total float64
		if err := rows.Scan(&factType, &total); err != nil {
			return 0, 0, fmt.Errorf("scan sum: %w", err)
		}
		switch factType {
		case FactTypeIncome:
			income = total
		case FactTypeExpense:
			expenses = total
		}
	}
	return income, expenses, rows.Err()
}

// SumUnverifiedByMonth computes unverified inflow and outflow for a given month.
func (s *FactStore) SumUnverifiedByMonth(ctx context.Context, month string) (inflow, outflow float64, err error) {
	startDate, err := time.Parse("2006-01", month)
	if err != nil {
		return 0, 0, fmt.Errorf("parse month: %w", err)
	}
	endDate := startDate.AddDate(0, 1, 0)

	const q = `
SELECT fact_type, COALESCE(SUM(amount), 0) as total
FROM agent_financial_facts
WHERE verified = false
  AND occurred_at >= $1 AND occurred_at < $2
GROUP BY fact_type`

	rows, err := s.pool.Query(ctx, q, startDate, endDate)
	if err != nil {
		return 0, 0, fmt.Errorf("sum unverified by month: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var factType string
		var total float64
		if err := rows.Scan(&factType, &total); err != nil {
			return 0, 0, fmt.Errorf("scan sum: %w", err)
		}
		switch factType {
		case FactTypeIncome, FactTypeRefund:
			inflow += total
		case FactTypeExpense:
			outflow += total
		case FactTypeTransfer:
			// transfers go to whichever side based on context;
			// for now neutral
		}
	}
	return inflow, outflow, rows.Err()
}

// CountByMonth returns fact counts for a month.
func (s *FactStore) CountByMonth(ctx context.Context, month string) (total, verified int, err error) {
	startDate, err := time.Parse("2006-01", month)
	if err != nil {
		return 0, 0, fmt.Errorf("parse month: %w", err)
	}
	endDate := startDate.AddDate(0, 1, 0)

	const q = `
SELECT COUNT(*), COUNT(*) FILTER (WHERE verified = true)
FROM agent_financial_facts
WHERE occurred_at >= $1 AND occurred_at < $2`

	err = s.pool.QueryRow(ctx, q, startDate, endDate).Scan(&total, &verified)
	if err != nil {
		return 0, 0, fmt.Errorf("count by month: %w", err)
	}
	return total, verified, nil
}

// --- MatchStore ---

// MatchStore persists attribution matches between facts and opportunities/outcomes.
type MatchStore struct {
	pool *pgxpool.Pool
}

// NewMatchStore creates a MatchStore backed by the given pool.
func NewMatchStore(pool *pgxpool.Pool) *MatchStore {
	return &MatchStore{pool: pool}
}

// Create inserts a new attribution match.
func (s *MatchStore) Create(ctx context.Context, m AttributionMatch) (AttributionMatch, error) {
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}

	const q = `
INSERT INTO agent_financial_matches
  (id, fact_id, outcome_id, opportunity_id, match_type, match_confidence, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, fact_id, COALESCE(outcome_id, ''), COALESCE(opportunity_id, ''),
  match_type, match_confidence, created_at`

	row := s.pool.QueryRow(ctx, q,
		m.ID, m.FactID, nullIfEmpty(m.OutcomeID), nullIfEmpty(m.OpportunityID),
		m.MatchType, m.MatchConfidence, m.CreatedAt,
	)
	return scanMatch(row)
}

// ListByFactID returns matches for a given fact.
func (s *MatchStore) ListByFactID(ctx context.Context, factID string) ([]AttributionMatch, error) {
	const q = `
SELECT id, fact_id, COALESCE(outcome_id, ''), COALESCE(opportunity_id, ''),
  match_type, match_confidence, created_at
FROM agent_financial_matches
WHERE fact_id = $1
ORDER BY created_at DESC`

	rows, err := s.pool.Query(ctx, q, factID)
	if err != nil {
		return nil, fmt.Errorf("list matches by fact: %w", err)
	}
	defer rows.Close()

	var out []AttributionMatch
	for rows.Next() {
		var m AttributionMatch
		if err := rows.Scan(
			&m.ID, &m.FactID, &m.OutcomeID, &m.OpportunityID,
			&m.MatchType, &m.MatchConfidence, &m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan match: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListByOpportunityID returns matches for a given opportunity.
func (s *MatchStore) ListByOpportunityID(ctx context.Context, oppID string) ([]AttributionMatch, error) {
	const q = `
SELECT id, fact_id, COALESCE(outcome_id, ''), COALESCE(opportunity_id, ''),
  match_type, match_confidence, created_at
FROM agent_financial_matches
WHERE opportunity_id = $1
ORDER BY created_at DESC`

	rows, err := s.pool.Query(ctx, q, oppID)
	if err != nil {
		return nil, fmt.Errorf("list matches by opportunity: %w", err)
	}
	defer rows.Close()

	var out []AttributionMatch
	for rows.Next() {
		var m AttributionMatch
		if err := rows.Scan(
			&m.ID, &m.FactID, &m.OutcomeID, &m.OpportunityID,
			&m.MatchType, &m.MatchConfidence, &m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan match: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ExistsByFactAndOpportunity checks if a match already exists for the given fact+opportunity pair.
func (s *MatchStore) ExistsByFactAndOpportunity(ctx context.Context, factID, oppID string) (bool, error) {
	const q = `
SELECT EXISTS(SELECT 1 FROM agent_financial_matches WHERE fact_id = $1 AND opportunity_id = $2)`

	var exists bool
	err := s.pool.QueryRow(ctx, q, factID, oppID).Scan(&exists)
	return exists, err
}

// --- helpers ---

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEvent(row rowScanner) (FinancialEvent, error) {
	var e FinancialEvent
	err := row.Scan(
		&e.ID, &e.Source, &e.EventType, &e.Direction,
		&e.Amount, &e.Currency, &e.Description, &e.ExternalRef,
		&e.OccurredAt, &e.CreatedAt,
	)
	if err != nil {
		return FinancialEvent{}, fmt.Errorf("scan financial event: %w", err)
	}
	return e, nil
}

func scanFact(row rowScanner) (FinancialFact, error) {
	var f FinancialFact
	err := row.Scan(
		&f.ID, &f.FactType, &f.Amount, &f.Currency, &f.Verified, &f.Confidence,
		&f.Source, &f.EventID,
		&f.LinkedOpportunityID, &f.LinkedOutcomeID, &f.LinkedProposalID,
		&f.FinanciallyVerified, &f.OccurredAt, &f.CreatedAt,
	)
	if err != nil {
		return FinancialFact{}, fmt.Errorf("scan financial fact: %w", err)
	}
	return f, nil
}

type rowsScanner interface {
	Scan(dest ...any) error
}

func scanFactRow(rows rowsScanner) (FinancialFact, error) {
	var f FinancialFact
	err := rows.Scan(
		&f.ID, &f.FactType, &f.Amount, &f.Currency, &f.Verified, &f.Confidence,
		&f.Source, &f.EventID,
		&f.LinkedOpportunityID, &f.LinkedOutcomeID, &f.LinkedProposalID,
		&f.FinanciallyVerified, &f.OccurredAt, &f.CreatedAt,
	)
	if err != nil {
		return FinancialFact{}, fmt.Errorf("scan financial fact row: %w", err)
	}
	return f, nil
}

func scanMatch(row rowScanner) (AttributionMatch, error) {
	var m AttributionMatch
	err := row.Scan(
		&m.ID, &m.FactID, &m.OutcomeID, &m.OpportunityID,
		&m.MatchType, &m.MatchConfidence, &m.CreatedAt,
	)
	if err != nil {
		return AttributionMatch{}, fmt.Errorf("scan attribution match: %w", err)
	}
	return m, nil
}

// nullIfEmpty returns nil for empty strings (for nullable DB columns).
func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
