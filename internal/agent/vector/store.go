package vector

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store persists the system vector as a single-row table.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a new vector store backed by PostgreSQL.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Get retrieves the current system vector.
// Returns defaults + nil on no-row (first boot). Returns defaults + error on
// real DB failures so the caller (Engine) can log a visible warning.
func (s *Store) Get(ctx context.Context) (SystemVector, error) {
	v := DefaultVector()
	err := s.pool.QueryRow(ctx, `
		SELECT income_priority, family_safety_priority, infra_priority,
		       automation_priority, exploration_level, risk_tolerance,
		       human_review_strictness, updated_at
		FROM agent_system_vector WHERE id = 'current'
	`).Scan(
		&v.IncomePriority, &v.FamilySafetyPriority, &v.InfraPriority,
		&v.AutomationPriority, &v.ExplorationLevel, &v.RiskTolerance,
		&v.HumanReviewStrictness, &v.UpdatedAt,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return DefaultVector(), nil
		}
		// Real DB error — return defaults but surface the error so
		// Engine.GetVector can log a visible warning.
		return DefaultVector(), err
	}
	return v, nil
}

// Set upserts the system vector.
func (s *Store) Set(ctx context.Context, v SystemVector) error {
	v.Clamp()
	v.UpdatedAt = time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_system_vector (id, income_priority, family_safety_priority,
			infra_priority, automation_priority, exploration_level,
			risk_tolerance, human_review_strictness, updated_at)
		VALUES ('current', $1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET
			income_priority = EXCLUDED.income_priority,
			family_safety_priority = EXCLUDED.family_safety_priority,
			infra_priority = EXCLUDED.infra_priority,
			automation_priority = EXCLUDED.automation_priority,
			exploration_level = EXCLUDED.exploration_level,
			risk_tolerance = EXCLUDED.risk_tolerance,
			human_review_strictness = EXCLUDED.human_review_strictness,
			updated_at = EXCLUDED.updated_at
	`, v.IncomePriority, v.FamilySafetyPriority, v.InfraPriority,
		v.AutomationPriority, v.ExplorationLevel, v.RiskTolerance,
		v.HumanReviewStrictness, v.UpdatedAt)
	return err
}

// InMemoryStore is a test-friendly in-memory implementation.
type InMemoryStore struct {
	mu sync.RWMutex
	v  SystemVector
}

// NewInMemoryStore returns a store initialized with default vector.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{v: DefaultVector()}
}

func (s *InMemoryStore) Get(_ context.Context) (SystemVector, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.v, nil
}

func (s *InMemoryStore) Set(_ context.Context, v SystemVector) error {
	v.Clamp()
	v.UpdatedAt = time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.v = v
	return nil
}

// StoreInterface abstracts over Store and InMemoryStore.
type StoreInterface interface {
	Get(ctx context.Context) (SystemVector, error)
	Set(ctx context.Context, v SystemVector) error
}
