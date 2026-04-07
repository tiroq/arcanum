package exploration

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BudgetStore persists exploration budget state across process restarts.
// Uses a single-row table (agent_exploration_budget) with rolling counters.
type BudgetStore struct {
	db *pgxpool.Pool
}

// NewBudgetStore creates a BudgetStore.
func NewBudgetStore(db *pgxpool.Pool) *BudgetStore {
	return &BudgetStore{db: db}
}

// Load reads the current budget state from the database.
// If no row exists, returns default budget (never explored).
func (s *BudgetStore) Load(ctx context.Context) (*ExplorationBudget, error) {
	b := DefaultBudget()

	row := s.db.QueryRow(ctx, `
		SELECT max_per_cycle, max_per_hour, used_this_window, window_start
		FROM agent_exploration_budget
		ORDER BY id LIMIT 1
	`)

	var windowStart time.Time
	err := row.Scan(&b.MaxPerCycle, &b.MaxPerHour, &b.UsedThisWindow, &windowStart)
	if err != nil {
		// No row yet — return defaults. Will be created on first save.
		return &b, nil
	}

	b.WindowStart = windowStart
	// UsedThisCycle is always 0 when loaded — it resets per cycle.
	b.UsedThisCycle = 0
	return &b, nil
}

// Save persists the current budget state. Uses upsert to handle first-time creation.
func (s *BudgetStore) Save(ctx context.Context, b *ExplorationBudget) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO agent_exploration_budget (id, max_per_cycle, max_per_hour, used_this_window, window_start, updated_at)
		VALUES (1, $1, $2, $3, $4, NOW())
		ON CONFLICT (id) DO UPDATE SET
			max_per_cycle = $1,
			max_per_hour = $2,
			used_this_window = $3,
			window_start = $4,
			updated_at = NOW()
	`, b.MaxPerCycle, b.MaxPerHour, b.UsedThisWindow, b.WindowStart)
	if err != nil {
		return fmt.Errorf("save exploration budget: %w", err)
	}
	return nil
}

// CountRecentExplorations counts exploration events in the audit trail
// within the given time window. Used as a secondary check against budget.
func (s *BudgetStore) CountRecentExplorations(ctx context.Context, since time.Time) (int, error) {
	var count int
	err := s.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM audit_events
		WHERE event_type = 'exploration.chosen'
		  AND occurred_at >= $1
	`, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count recent explorations: %w", err)
	}
	return count, nil
}
