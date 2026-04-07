package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store provides PostgreSQL persistence for policy state and change records.
type Store struct {
	db *pgxpool.Pool
}

// NewStore creates a Store.
func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// GetAll returns the current values of all tunable parameters.
func (s *Store) GetAll(ctx context.Context) (map[PolicyParam]float64, error) {
	rows, err := s.db.Query(ctx, "SELECT key, value FROM agent_policy_state")
	if err != nil {
		return nil, fmt.Errorf("query policy state: %w", err)
	}
	defer rows.Close()

	vals := make(map[PolicyParam]float64)
	for rows.Next() {
		var k string
		var v float64
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("scan policy state: %w", err)
		}
		vals[PolicyParam(k)] = v
	}
	return vals, rows.Err()
}

// Get returns a single parameter value, falling back to default.
func (s *Store) Get(ctx context.Context, param PolicyParam) (float64, error) {
	var v float64
	err := s.db.QueryRow(ctx,
		"SELECT value FROM agent_policy_state WHERE key = $1", string(param),
	).Scan(&v)
	if err == pgx.ErrNoRows {
		if def, ok := DefaultValues[param]; ok {
			return def, nil
		}
		return 0, fmt.Errorf("unknown policy parameter: %s", param)
	}
	if err != nil {
		return 0, fmt.Errorf("get policy param %s: %w", param, err)
	}
	return v, nil
}

// Set updates (or inserts) a parameter value.
func (s *Store) Set(ctx context.Context, param PolicyParam, value float64) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO agent_policy_state (key, value, updated_at)
 VALUES ($1, $2, now())
 ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = now()`,
		string(param), value,
	)
	if err != nil {
		return fmt.Errorf("set policy param %s: %w", param, err)
	}
	return nil
}

// RecordChange persists a policy change record.
func (s *Store) RecordChange(ctx context.Context, c PolicyChange, applied bool) (*ChangeRecord, error) {
	evidenceJSON, err := json.Marshal(c.Evidence)
	if err != nil {
		return nil, fmt.Errorf("marshal evidence: %w", err)
	}

	var rec ChangeRecord
	err = s.db.QueryRow(ctx,
		`INSERT INTO agent_policy_changes (parameter, old_value, new_value, reason, evidence, applied)
 VALUES ($1, $2, $3, $4, $5, $6)
 RETURNING id, parameter, old_value, new_value, reason, evidence, applied, created_at`,
		string(c.Parameter), c.OldValue, c.NewValue, c.Reason, evidenceJSON, applied,
	).Scan(&rec.ID, &rec.Parameter, &rec.OldValue, &rec.NewValue, &rec.Reason,
		&evidenceJSON, &rec.Applied, &rec.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("record change: %w", err)
	}
	if err := json.Unmarshal(evidenceJSON, &rec.Evidence); err != nil {
		rec.Evidence = map[string]any{}
	}
	return &rec, nil
}

// ListChanges returns recent policy changes ordered by created_at desc.
func (s *Store) ListChanges(ctx context.Context, limit int) ([]ChangeRecord, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, parameter, old_value, new_value, reason, evidence, applied,
        created_at, evaluated_at, improvement_detected
 FROM agent_policy_changes
 ORDER BY created_at DESC
 LIMIT $1`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list changes: %w", err)
	}
	defer rows.Close()

	var out []ChangeRecord
	for rows.Next() {
		var r ChangeRecord
		var evidenceJSON []byte
		if err := rows.Scan(
			&r.ID, &r.Parameter, &r.OldValue, &r.NewValue, &r.Reason,
			&evidenceJSON, &r.Applied, &r.CreatedAt, &r.EvaluatedAt, &r.ImprovementDetected,
		); err != nil {
			return nil, fmt.Errorf("scan change: %w", err)
		}
		if err := json.Unmarshal(evidenceJSON, &r.Evidence); err != nil {
			r.Evidence = map[string]any{}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListUnevaluatedChanges returns applied changes that haven't been evaluated yet.
func (s *Store) ListUnevaluatedChanges(ctx context.Context, olderThan time.Time) ([]ChangeRecord, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, parameter, old_value, new_value, reason, evidence, applied,
        created_at, evaluated_at, improvement_detected
 FROM agent_policy_changes
 WHERE applied = true AND evaluated_at IS NULL AND created_at < $1
 ORDER BY created_at ASC`, olderThan,
	)
	if err != nil {
		return nil, fmt.Errorf("list unevaluated: %w", err)
	}
	defer rows.Close()

	var out []ChangeRecord
	for rows.Next() {
		var r ChangeRecord
		var evidenceJSON []byte
		if err := rows.Scan(
			&r.ID, &r.Parameter, &r.OldValue, &r.NewValue, &r.Reason,
			&evidenceJSON, &r.Applied, &r.CreatedAt, &r.EvaluatedAt, &r.ImprovementDetected,
		); err != nil {
			return nil, fmt.Errorf("scan unevaluated: %w", err)
		}
		if err := json.Unmarshal(evidenceJSON, &r.Evidence); err != nil {
			r.Evidence = map[string]any{}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkEvaluated sets the evaluation result for a change.
func (s *Store) MarkEvaluated(ctx context.Context, id uuid.UUID, improved bool) error {
	_, err := s.db.Exec(ctx,
		`UPDATE agent_policy_changes
 SET evaluated_at = now(), improvement_detected = $2
 WHERE id = $1`,
		id, improved,
	)
	if err != nil {
		return fmt.Errorf("mark evaluated: %w", err)
	}
	return nil
}
