package pricing

import (
"context"
"fmt"
"time"

"github.com/google/uuid"
"github.com/jackc/pgx/v5"
"github.com/jackc/pgx/v5/pgxpool"
)

// ProfileStore manages pricing profile persistence in PostgreSQL.
type ProfileStore struct {
pool *pgxpool.Pool
}

// NewProfileStore creates a new ProfileStore.
func NewProfileStore(pool *pgxpool.Pool) *ProfileStore {
return &ProfileStore{pool: pool}
}

// Upsert creates or updates a pricing profile (keyed by opportunity_id).
func (s *ProfileStore) Upsert(ctx context.Context, p PricingProfile) (PricingProfile, error) {
if p.ID == "" {
p.ID = uuid.New().String()
}
now := time.Now().UTC()
if p.CreatedAt.IsZero() {
p.CreatedAt = now
}
p.UpdatedAt = now

const q = `
INSERT INTO agent_pricing_profiles
(id, opportunity_id, strategy_id, estimated_effort_hours, cost_basis,
 target_price, minimum_price, stretch_price, confidence, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (opportunity_id) DO UPDATE SET
strategy_id = EXCLUDED.strategy_id,
estimated_effort_hours = EXCLUDED.estimated_effort_hours,
cost_basis = EXCLUDED.cost_basis,
target_price = EXCLUDED.target_price,
minimum_price = EXCLUDED.minimum_price,
stretch_price = EXCLUDED.stretch_price,
confidence = EXCLUDED.confidence,
updated_at = EXCLUDED.updated_at`

_, err := s.pool.Exec(ctx, q,
p.ID, p.OpportunityID, p.StrategyID,
p.EstimatedEffortHours, p.CostBasis,
p.TargetPrice, p.MinimumPrice, p.StretchPrice,
p.Confidence, p.CreatedAt, p.UpdatedAt,
)
if err != nil {
return PricingProfile{}, fmt.Errorf("upsert pricing profile: %w", err)
}
return p, nil
}

// GetByOpportunity retrieves the pricing profile for an opportunity.
func (s *ProfileStore) GetByOpportunity(ctx context.Context, opportunityID string) (PricingProfile, error) {
const q = `
SELECT id, opportunity_id, strategy_id, estimated_effort_hours, cost_basis,
   target_price, minimum_price, stretch_price, confidence, created_at, updated_at
FROM agent_pricing_profiles
WHERE opportunity_id = $1`

var p PricingProfile
err := s.pool.QueryRow(ctx, q, opportunityID).Scan(
&p.ID, &p.OpportunityID, &p.StrategyID,
&p.EstimatedEffortHours, &p.CostBasis,
&p.TargetPrice, &p.MinimumPrice, &p.StretchPrice,
&p.Confidence, &p.CreatedAt, &p.UpdatedAt,
)
if err == pgx.ErrNoRows {
return PricingProfile{}, fmt.Errorf("pricing profile not found for opportunity: %s", opportunityID)
}
if err != nil {
return PricingProfile{}, fmt.Errorf("get pricing profile: %w", err)
}
return p, nil
}

// ListAll returns all pricing profiles ordered by updated_at DESC.
func (s *ProfileStore) ListAll(ctx context.Context) ([]PricingProfile, error) {
const q = `
SELECT id, opportunity_id, strategy_id, estimated_effort_hours, cost_basis,
   target_price, minimum_price, stretch_price, confidence, created_at, updated_at
FROM agent_pricing_profiles
ORDER BY updated_at DESC`

rows, err := s.pool.Query(ctx, q)
if err != nil {
return nil, fmt.Errorf("list pricing profiles: %w", err)
}
defer rows.Close()

var result []PricingProfile
for rows.Next() {
var p PricingProfile
if err := rows.Scan(
&p.ID, &p.OpportunityID, &p.StrategyID,
&p.EstimatedEffortHours, &p.CostBasis,
&p.TargetPrice, &p.MinimumPrice, &p.StretchPrice,
&p.Confidence, &p.CreatedAt, &p.UpdatedAt,
); err != nil {
return nil, fmt.Errorf("scan pricing profile: %w", err)
}
result = append(result, p)
}
return result, rows.Err()
}

// NegotiationStore manages negotiation record persistence in PostgreSQL.
type NegotiationStore struct {
pool *pgxpool.Pool
}

// NewNegotiationStore creates a new NegotiationStore.
func NewNegotiationStore(pool *pgxpool.Pool) *NegotiationStore {
return &NegotiationStore{pool: pool}
}

// Upsert creates or updates a negotiation record (keyed by opportunity_id).
func (s *NegotiationStore) Upsert(ctx context.Context, n NegotiationRecord) (NegotiationRecord, error) {
if n.ID == "" {
n.ID = uuid.New().String()
}
now := time.Now().UTC()
if n.CreatedAt.IsZero() {
n.CreatedAt = now
}
n.UpdatedAt = now

const q = `
INSERT INTO agent_negotiation_records
(id, opportunity_id, negotiation_state, current_offered_price,
 recommended_next_price, concession_count, requires_review, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (opportunity_id) DO UPDATE SET
negotiation_state = EXCLUDED.negotiation_state,
current_offered_price = EXCLUDED.current_offered_price,
recommended_next_price = EXCLUDED.recommended_next_price,
concession_count = EXCLUDED.concession_count,
requires_review = EXCLUDED.requires_review,
updated_at = EXCLUDED.updated_at`

_, err := s.pool.Exec(ctx, q,
n.ID, n.OpportunityID, n.NegotiationState,
n.CurrentOfferedPrice, n.RecommendedNextPrice,
n.ConcessionCount, n.RequiresReview, n.CreatedAt, n.UpdatedAt,
)
if err != nil {
return NegotiationRecord{}, fmt.Errorf("upsert negotiation record: %w", err)
}
return n, nil
}

// GetByOpportunity retrieves the negotiation record for an opportunity.
func (s *NegotiationStore) GetByOpportunity(ctx context.Context, opportunityID string) (NegotiationRecord, error) {
const q = `
SELECT id, opportunity_id, negotiation_state, current_offered_price,
   recommended_next_price, concession_count, requires_review, created_at, updated_at
FROM agent_negotiation_records
WHERE opportunity_id = $1`

var n NegotiationRecord
err := s.pool.QueryRow(ctx, q, opportunityID).Scan(
&n.ID, &n.OpportunityID, &n.NegotiationState,
&n.CurrentOfferedPrice, &n.RecommendedNextPrice,
&n.ConcessionCount, &n.RequiresReview, &n.CreatedAt, &n.UpdatedAt,
)
if err == pgx.ErrNoRows {
return NegotiationRecord{}, fmt.Errorf("negotiation record not found for opportunity: %s", opportunityID)
}
if err != nil {
return NegotiationRecord{}, fmt.Errorf("get negotiation record: %w", err)
}
return n, nil
}

// ListAll returns all negotiation records ordered by updated_at DESC.
func (s *NegotiationStore) ListAll(ctx context.Context) ([]NegotiationRecord, error) {
const q = `
SELECT id, opportunity_id, negotiation_state, current_offered_price,
   recommended_next_price, concession_count, requires_review, created_at, updated_at
FROM agent_negotiation_records
ORDER BY updated_at DESC`

rows, err := s.pool.Query(ctx, q)
if err != nil {
return nil, fmt.Errorf("list negotiation records: %w", err)
}
defer rows.Close()

var result []NegotiationRecord
for rows.Next() {
var n NegotiationRecord
if err := rows.Scan(
&n.ID, &n.OpportunityID, &n.NegotiationState,
&n.CurrentOfferedPrice, &n.RecommendedNextPrice,
&n.ConcessionCount, &n.RequiresReview, &n.CreatedAt, &n.UpdatedAt,
); err != nil {
return nil, fmt.Errorf("scan negotiation record: %w", err)
}
result = append(result, n)
}
return result, rows.Err()
}

// OutcomeStore manages pricing outcome persistence in PostgreSQL.
type OutcomeStore struct {
pool *pgxpool.Pool
}

// NewOutcomeStore creates a new OutcomeStore.
func NewOutcomeStore(pool *pgxpool.Pool) *OutcomeStore {
return &OutcomeStore{pool: pool}
}

// Create inserts a new pricing outcome.
func (s *OutcomeStore) Create(ctx context.Context, o PricingOutcome) (PricingOutcome, error) {
if o.ID == "" {
o.ID = uuid.New().String()
}
if o.CreatedAt.IsZero() {
o.CreatedAt = time.Now().UTC()
}

const q = `
INSERT INTO agent_pricing_outcomes
(id, opportunity_id, quoted_price, accepted_price, won, notes, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7)`

_, err := s.pool.Exec(ctx, q,
o.ID, o.OpportunityID, o.QuotedPrice, o.AcceptedPrice,
o.Won, o.Notes, o.CreatedAt,
)
if err != nil {
return PricingOutcome{}, fmt.Errorf("insert pricing outcome: %w", err)
}
return o, nil
}

// ListByOpportunity returns all pricing outcomes for an opportunity.
func (s *OutcomeStore) ListByOpportunity(ctx context.Context, opportunityID string) ([]PricingOutcome, error) {
const q = `
SELECT id, opportunity_id, quoted_price, accepted_price, won, notes, created_at
FROM agent_pricing_outcomes
WHERE opportunity_id = $1
ORDER BY created_at DESC`

rows, err := s.pool.Query(ctx, q, opportunityID)
if err != nil {
return nil, fmt.Errorf("list pricing outcomes: %w", err)
}
defer rows.Close()

var result []PricingOutcome
for rows.Next() {
var o PricingOutcome
if err := rows.Scan(
&o.ID, &o.OpportunityID, &o.QuotedPrice, &o.AcceptedPrice,
&o.Won, &o.Notes, &o.CreatedAt,
); err != nil {
return nil, fmt.Errorf("scan pricing outcome: %w", err)
}
result = append(result, o)
}
return result, rows.Err()
}

// ListByStrategy returns all pricing outcomes for opportunities linked to a strategy.
func (s *OutcomeStore) ListByStrategy(ctx context.Context, strategyID string) ([]PricingOutcome, error) {
const q = `
SELECT o.id, o.opportunity_id, o.quoted_price, o.accepted_price, o.won, o.notes, o.created_at
FROM agent_pricing_outcomes o
JOIN agent_pricing_profiles p ON o.opportunity_id = p.opportunity_id
WHERE p.strategy_id = $1
ORDER BY o.created_at DESC`

rows, err := s.pool.Query(ctx, q, strategyID)
if err != nil {
return nil, fmt.Errorf("list pricing outcomes by strategy: %w", err)
}
defer rows.Close()

var result []PricingOutcome
for rows.Next() {
var o PricingOutcome
if err := rows.Scan(
&o.ID, &o.OpportunityID, &o.QuotedPrice, &o.AcceptedPrice,
&o.Won, &o.Notes, &o.CreatedAt,
); err != nil {
return nil, fmt.Errorf("scan pricing outcome: %w", err)
}
result = append(result, o)
}
return result, rows.Err()
}

// PerformanceStore manages pricing performance aggregation in PostgreSQL.
type PerformanceStore struct {
pool *pgxpool.Pool
}

// NewPerformanceStore creates a new PerformanceStore.
func NewPerformanceStore(pool *pgxpool.Pool) *PerformanceStore {
return &PerformanceStore{pool: pool}
}

// Upsert creates or updates pricing performance (keyed by strategy_id).
func (s *PerformanceStore) Upsert(ctx context.Context, p PricingPerformance) error {
if p.UpdatedAt.IsZero() {
p.UpdatedAt = time.Now().UTC()
}

const q = `
INSERT INTO agent_pricing_performance
(strategy_id, avg_quoted_price, avg_accepted_price, avg_discount_rate,
 win_rate, total_outcomes, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (strategy_id) DO UPDATE SET
avg_quoted_price = EXCLUDED.avg_quoted_price,
avg_accepted_price = EXCLUDED.avg_accepted_price,
avg_discount_rate = EXCLUDED.avg_discount_rate,
win_rate = EXCLUDED.win_rate,
total_outcomes = EXCLUDED.total_outcomes,
updated_at = EXCLUDED.updated_at`

_, err := s.pool.Exec(ctx, q,
p.StrategyID, p.AvgQuotedPrice, p.AvgAcceptedPrice,
p.AvgDiscountRate, p.WinRate, p.TotalOutcomes, p.UpdatedAt,
)
if err != nil {
return fmt.Errorf("upsert pricing performance: %w", err)
}
return nil
}

// Get retrieves pricing performance for a strategy.
func (s *PerformanceStore) Get(ctx context.Context, strategyID string) (PricingPerformance, error) {
const q = `
SELECT strategy_id, avg_quoted_price, avg_accepted_price, avg_discount_rate,
   win_rate, total_outcomes, updated_at
FROM agent_pricing_performance
WHERE strategy_id = $1`

var p PricingPerformance
err := s.pool.QueryRow(ctx, q, strategyID).Scan(
&p.StrategyID, &p.AvgQuotedPrice, &p.AvgAcceptedPrice,
&p.AvgDiscountRate, &p.WinRate, &p.TotalOutcomes, &p.UpdatedAt,
)
if err == pgx.ErrNoRows {
return PricingPerformance{}, nil
}
if err != nil {
return PricingPerformance{}, fmt.Errorf("get pricing performance: %w", err)
}
return p, nil
}

// ListAll returns all pricing performance records.
func (s *PerformanceStore) ListAll(ctx context.Context) ([]PricingPerformance, error) {
const q = `
SELECT strategy_id, avg_quoted_price, avg_accepted_price, avg_discount_rate,
   win_rate, total_outcomes, updated_at
FROM agent_pricing_performance
ORDER BY updated_at DESC`

rows, err := s.pool.Query(ctx, q)
if err != nil {
return nil, fmt.Errorf("list pricing performance: %w", err)
}
defer rows.Close()

var result []PricingPerformance
for rows.Next() {
var p PricingPerformance
if err := rows.Scan(
&p.StrategyID, &p.AvgQuotedPrice, &p.AvgAcceptedPrice,
&p.AvgDiscountRate, &p.WinRate, &p.TotalOutcomes, &p.UpdatedAt,
); err != nil {
return nil, fmt.Errorf("scan pricing performance: %w", err)
}
result = append(result, p)
}
return result, rows.Err()
}
