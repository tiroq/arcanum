package autonomy

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// --- PostgreSQL ExecutionFeedbackStore ---

// PgExecutionFeedbackStore persists execution feedback to PostgreSQL.
type PgExecutionFeedbackStore struct {
	pool *pgxpool.Pool
}

// NewPgExecutionFeedbackStore creates a new PostgreSQL-based feedback store.
func NewPgExecutionFeedbackStore(pool *pgxpool.Pool) *PgExecutionFeedbackStore {
	return &PgExecutionFeedbackStore{pool: pool}
}

func (s *PgExecutionFeedbackStore) Insert(ctx context.Context, f ExecutionFeedback) error {
	const q = `
		INSERT INTO agent_execution_feedback
			(id, task_id, execution_task_id, outcome_type, success,
			 steps_executed, steps_failed, error_summary, semantic_signal,
			 source_decision_type, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	_, err := s.pool.Exec(ctx, q,
		f.ID, f.TaskID, f.ExecutionTaskID, f.OutcomeType, f.Success,
		f.StepsExecuted, f.StepsFailed, f.ErrorSummary, f.SemanticSignal,
		f.SourceDecisionType, f.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert execution feedback: %w", err)
	}
	return nil
}

func (s *PgExecutionFeedbackStore) ListRecent(ctx context.Context, limit int) ([]ExecutionFeedback, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	const q = `
		SELECT id, task_id, execution_task_id, outcome_type, success,
		       steps_executed, steps_failed, error_summary, semantic_signal,
		       source_decision_type, created_at
		FROM agent_execution_feedback
		ORDER BY created_at DESC LIMIT $1`
	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("list execution feedback: %w", err)
	}
	defer rows.Close()
	return collectFeedbackRows(rows)
}

func (s *PgExecutionFeedbackStore) CountByOutcome(ctx context.Context, outcomeType string, since time.Time) (int, error) {
	const q = `SELECT COUNT(*) FROM agent_execution_feedback WHERE outcome_type = $1 AND created_at >= $2`
	var count int
	err := s.pool.QueryRow(ctx, q, outcomeType, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count feedback by outcome: %w", err)
	}
	return count, nil
}

func (s *PgExecutionFeedbackStore) CountBySignal(ctx context.Context, signal string, since time.Time) (int, error) {
	const q = `SELECT COUNT(*) FROM agent_execution_feedback WHERE semantic_signal = $1 AND created_at >= $2`
	var count int
	err := s.pool.QueryRow(ctx, q, signal, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count feedback by signal: %w", err)
	}
	return count, nil
}

func collectFeedbackRows(rows pgx.Rows) ([]ExecutionFeedback, error) {
	var result []ExecutionFeedback
	for rows.Next() {
		var f ExecutionFeedback
		if err := rows.Scan(
			&f.ID, &f.TaskID, &f.ExecutionTaskID, &f.OutcomeType, &f.Success,
			&f.StepsExecuted, &f.StepsFailed, &f.ErrorSummary, &f.SemanticSignal,
			&f.SourceDecisionType, &f.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan execution feedback: %w", err)
		}
		result = append(result, f)
	}
	return result, nil
}

// --- In-memory ExecutionFeedbackStore for testing ---

// InMemoryFeedbackStore is an in-memory implementation of ExecutionFeedbackStore.
type InMemoryFeedbackStore struct {
	mu       sync.Mutex
	feedback []ExecutionFeedback
}

// NewInMemoryFeedbackStore creates a new in-memory feedback store.
func NewInMemoryFeedbackStore() *InMemoryFeedbackStore {
	return &InMemoryFeedbackStore{}
}

func (s *InMemoryFeedbackStore) Insert(_ context.Context, f ExecutionFeedback) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.feedback = append(s.feedback, f)
	return nil
}

func (s *InMemoryFeedbackStore) ListRecent(_ context.Context, limit int) ([]ExecutionFeedback, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Sort by created_at descending.
	sorted := make([]ExecutionFeedback, len(s.feedback))
	copy(sorted, s.feedback)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].CreatedAt.After(sorted[j].CreatedAt)
	})

	if limit > 0 && len(sorted) > limit {
		sorted = sorted[:limit]
	}
	return sorted, nil
}

func (s *InMemoryFeedbackStore) CountByOutcome(_ context.Context, outcomeType string, since time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, f := range s.feedback {
		if f.OutcomeType == outcomeType && !f.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

func (s *InMemoryFeedbackStore) CountBySignal(_ context.Context, signal string, since time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, f := range s.feedback {
		if f.SemanticSignal == signal && !f.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

// AllFeedback returns all stored feedback (test helper).
func (s *InMemoryFeedbackStore) AllFeedback() []ExecutionFeedback {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]ExecutionFeedback, len(s.feedback))
	copy(cp, s.feedback)
	return cp
}
