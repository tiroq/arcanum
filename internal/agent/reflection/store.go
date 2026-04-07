package reflection

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store persists and retrieves reflection findings.
type Store struct {
	db *pgxpool.Pool
}

// NewStore creates a Store backed by PostgreSQL.
func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// SaveFindings persists a batch of findings from a single reflection cycle.
func (s *Store) SaveFindings(ctx context.Context, findings []Finding) error {
	if len(findings) == 0 {
		return nil
	}

	const q = `
INSERT INTO agent_reflection_findings
(id, cycle_id, rule, severity, action_type, summary, detail)
VALUES ($1, $2, $3, $4, $5, $6, $7)`

	for _, f := range findings {
		detailJSON, err := json.Marshal(f.Detail)
		if err != nil {
			return fmt.Errorf("marshal detail: %w", err)
		}
		_, err = s.db.Exec(ctx, q,
			f.ID, f.CycleID, string(f.Rule), string(f.Severity),
			f.ActionType, f.Summary, detailJSON,
		)
		if err != nil {
			return fmt.Errorf("insert finding: %w", err)
		}
	}
	return nil
}

// ListRecent retrieves the most recent findings, ordered by created_at DESC.
func (s *Store) ListRecent(ctx context.Context, limit int) ([]Finding, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	const q = `
SELECT id, cycle_id, rule, severity, action_type, summary, detail, created_at
FROM agent_reflection_findings
ORDER BY created_at DESC
LIMIT $1`

	rows, err := s.db.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("query findings: %w", err)
	}
	defer rows.Close()

	var results []Finding
	for rows.Next() {
		var f Finding
		var detailJSON []byte
		var rule, severity string
		if err := rows.Scan(
			&f.ID, &f.CycleID, &rule, &severity,
			&f.ActionType, &f.Summary, &detailJSON, &f.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan finding: %w", err)
		}
		f.Rule = Rule(rule)
		f.Severity = Severity(severity)
		if err := json.Unmarshal(detailJSON, &f.Detail); err != nil {
			f.Detail = map[string]any{}
		}
		results = append(results, f)
	}
	return results, rows.Err()
}

// ListByCycle retrieves all findings for a specific reflection cycle.
func (s *Store) ListByCycle(ctx context.Context, cycleID string) ([]Finding, error) {
	const q = `
SELECT id, cycle_id, rule, severity, action_type, summary, detail, created_at
FROM agent_reflection_findings
WHERE cycle_id = $1
ORDER BY created_at ASC`

	rows, err := s.db.Query(ctx, q, cycleID)
	if err != nil {
		return nil, fmt.Errorf("query findings by cycle: %w", err)
	}
	defer rows.Close()

	var results []Finding
	for rows.Next() {
		var f Finding
		var detailJSON []byte
		var rule, severity string
		if err := rows.Scan(
			&f.ID, &f.CycleID, &rule, &severity,
			&f.ActionType, &f.Summary, &detailJSON, &f.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan finding: %w", err)
		}
		f.Rule = Rule(rule)
		f.Severity = Severity(severity)
		if err := json.Unmarshal(detailJSON, &f.Detail); err != nil {
			f.Detail = map[string]any{}
		}
		results = append(results, f)
	}
	return results, rows.Err()
}
