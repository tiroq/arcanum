package causal

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store provides PostgreSQL persistence for causal attributions.
type Store struct {
	db *pgxpool.Pool
}

// NewStore creates a Store.
func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// Save persists a causal attribution.
func (s *Store) Save(ctx context.Context, a *CausalAttribution) error {
	evidenceJSON, err := json.Marshal(a.Evidence)
	if err != nil {
		return fmt.Errorf("marshal evidence: %w", err)
	}
	compJSON, err := json.Marshal(a.CompetingExplanations)
	if err != nil {
		return fmt.Errorf("marshal competing explanations: %w", err)
	}

	err = s.db.QueryRow(ctx,
		`INSERT INTO agent_causal_attributions
         (subject_type, subject_id, hypothesis, attribution, confidence, evidence, competing_explanations)
         VALUES ($1, $2, $3, $4, $5, $6, $7)
         RETURNING id, created_at`,
		string(a.SubjectType), a.SubjectID, a.Hypothesis,
		string(a.Attribution), a.Confidence, evidenceJSON, compJSON,
	).Scan(&a.ID, &a.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert causal attribution: %w", err)
	}
	return nil
}

// ListRecent returns recent causal attributions ordered by created_at desc.
func (s *Store) ListRecent(ctx context.Context, limit int) ([]CausalAttribution, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, subject_type, subject_id, hypothesis, attribution,
                confidence, evidence, competing_explanations, created_at
         FROM agent_causal_attributions
         ORDER BY created_at DESC
         LIMIT $1`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list causal attributions: %w", err)
	}
	defer rows.Close()

	var out []CausalAttribution
	for rows.Next() {
		var a CausalAttribution
		var evidenceJSON, compJSON []byte
		if err := rows.Scan(
			&a.ID, &a.SubjectType, &a.SubjectID, &a.Hypothesis,
			&a.Attribution, &a.Confidence, &evidenceJSON, &compJSON, &a.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan causal attribution: %w", err)
		}
		if err := json.Unmarshal(evidenceJSON, &a.Evidence); err != nil {
			a.Evidence = map[string]any{}
		}
		if err := json.Unmarshal(compJSON, &a.CompetingExplanations); err != nil {
			a.CompetingExplanations = nil
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListBySubject returns attributions for a specific subject.
func (s *Store) ListBySubject(ctx context.Context, subjectID uuid.UUID) ([]CausalAttribution, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, subject_type, subject_id, hypothesis, attribution,
                confidence, evidence, competing_explanations, created_at
         FROM agent_causal_attributions
         WHERE subject_id = $1
         ORDER BY created_at DESC`, subjectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list by subject: %w", err)
	}
	defer rows.Close()

	var out []CausalAttribution
	for rows.Next() {
		var a CausalAttribution
		var evidenceJSON, compJSON []byte
		if err := rows.Scan(
			&a.ID, &a.SubjectType, &a.SubjectID, &a.Hypothesis,
			&a.Attribution, &a.Confidence, &evidenceJSON, &compJSON, &a.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan causal attribution: %w", err)
		}
		if err := json.Unmarshal(evidenceJSON, &a.Evidence); err != nil {
			a.Evidence = map[string]any{}
		}
		if err := json.Unmarshal(compJSON, &a.CompetingExplanations); err != nil {
			a.CompetingExplanations = nil
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
