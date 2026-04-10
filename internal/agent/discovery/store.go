package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CandidateStore persists DiscoveryCandidate records in PostgreSQL.
type CandidateStore struct {
	pool *pgxpool.Pool
}

// NewCandidateStore creates a CandidateStore backed by the given pool.
func NewCandidateStore(pool *pgxpool.Pool) *CandidateStore {
	return &CandidateStore{pool: pool}
}

// Create inserts a new candidate.
func (s *CandidateStore) Create(ctx context.Context, c DiscoveryCandidate) (DiscoveryCandidate, error) {
	now := time.Now().UTC()
	c.CreatedAt = now
	if c.Status == "" {
		c.Status = CandidateStatusNew
	}

	sourceRefsJSON, err := json.Marshal(c.SourceRefs)
	if err != nil {
		return DiscoveryCandidate{}, fmt.Errorf("marshal source_refs: %w", err)
	}

	const q = `
INSERT INTO agent_discovery_candidates
  (id, candidate_type, source_type, source_refs, title, description,
   confidence, estimated_value, estimated_effort, dedupe_key, status, evidence_count, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
RETURNING id, candidate_type, source_type, source_refs, title, description,
          confidence, estimated_value, estimated_effort, dedupe_key, status, evidence_count, created_at`

	row := s.pool.QueryRow(ctx, q,
		c.ID, c.CandidateType, c.SourceType, sourceRefsJSON, c.Title, c.Description,
		c.Confidence, c.EstimatedValue, c.EstimatedEffort, c.DedupeKey, c.Status, c.EvidenceCount, c.CreatedAt,
	)
	return scanCandidate(row)
}

// GetByID retrieves a single candidate by primary key.
func (s *CandidateStore) GetByID(ctx context.Context, id string) (DiscoveryCandidate, error) {
	const q = `
SELECT id, candidate_type, source_type, source_refs, title, description,
       confidence, estimated_value, estimated_effort, dedupe_key, status, evidence_count, created_at
FROM agent_discovery_candidates WHERE id = $1`

	row := s.pool.QueryRow(ctx, q, id)
	return scanCandidate(row)
}

// UpdateStatus sets the status of a candidate.
func (s *CandidateStore) UpdateStatus(ctx context.Context, id, status string) error {
	const q = `UPDATE agent_discovery_candidates SET status=$1 WHERE id=$2`
	tag, err := s.pool.Exec(ctx, q, status, id)
	if err != nil {
		return fmt.Errorf("update candidate status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("candidate not found: %s", id)
	}
	return nil
}

// IncrementEvidence increments the evidence count for a candidate.
func (s *CandidateStore) IncrementEvidence(ctx context.Context, id string) error {
	const q = `UPDATE agent_discovery_candidates SET evidence_count = evidence_count + 1 WHERE id = $1`
	tag, err := s.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("increment evidence: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("candidate not found: %s", id)
	}
	return nil
}

// FindByDedupeKey looks for an existing candidate with the same dedupe key and candidate type
// created within the given window. Returns nil if none found.
func (s *CandidateStore) FindByDedupeKey(ctx context.Context, dedupeKey, candidateType string, windowHours int) (*DiscoveryCandidate, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(windowHours) * time.Hour)
	const q = `
SELECT id, candidate_type, source_type, source_refs, title, description,
       confidence, estimated_value, estimated_effort, dedupe_key, status, evidence_count, created_at
FROM agent_discovery_candidates
WHERE dedupe_key = $1
  AND candidate_type = $2
  AND created_at >= $3
  AND status != $4
ORDER BY created_at DESC
LIMIT 1`

	row := s.pool.QueryRow(ctx, q, dedupeKey, candidateType, cutoff, CandidateStatusSkipped)
	c, err := scanCandidate(row)
	if err != nil {
		return nil, nil // not found or error → treat as not found
	}
	return &c, nil
}

// List returns candidates ordered by created_at DESC with pagination.
func (s *CandidateStore) List(ctx context.Context, limit, offset int) ([]DiscoveryCandidate, error) {
	const q = `
SELECT id, candidate_type, source_type, source_refs, title, description,
       confidence, estimated_value, estimated_effort, dedupe_key, status, evidence_count, created_at
FROM agent_discovery_candidates
ORDER BY created_at DESC
LIMIT $1 OFFSET $2`

	rows, err := s.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list candidates: %w", err)
	}
	defer rows.Close()

	var out []DiscoveryCandidate
	for rows.Next() {
		c, err := scanCandidateRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Stats returns aggregate discovery statistics.
func (s *CandidateStore) Stats(ctx context.Context) (DiscoveryStats, error) {
	const q = `
SELECT
  COUNT(*) AS total_generated,
  COUNT(*) FILTER (WHERE status = 'promoted') AS total_promoted,
  COALESCE(MAX(created_at), NOW()) AS updated_at
FROM agent_discovery_candidates`

	var stats DiscoveryStats
	err := s.pool.QueryRow(ctx, q).Scan(
		&stats.TotalCandidatesGenerated,
		&stats.TotalCandidatesPromoted,
		&stats.UpdatedAt,
	)
	if err != nil {
		return DiscoveryStats{}, fmt.Errorf("stats: %w", err)
	}
	return stats, nil
}

// CountDeduped returns the count of candidates that were deduped (evidence_count > 1).
func (s *CandidateStore) CountDeduped(ctx context.Context) int {
	const q = `SELECT COUNT(*) FROM agent_discovery_candidates WHERE evidence_count > 1`
	var n int
	s.pool.QueryRow(ctx, q).Scan(&n) //nolint:errcheck
	return n
}

// --- row scanner helpers ---

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCandidate(row rowScanner) (DiscoveryCandidate, error) {
	var c DiscoveryCandidate
	var sourceRefsJSON []byte
	err := row.Scan(
		&c.ID, &c.CandidateType, &c.SourceType, &sourceRefsJSON, &c.Title, &c.Description,
		&c.Confidence, &c.EstimatedValue, &c.EstimatedEffort, &c.DedupeKey, &c.Status, &c.EvidenceCount, &c.CreatedAt,
	)
	if err != nil {
		return DiscoveryCandidate{}, fmt.Errorf("scan candidate: %w", err)
	}
	if len(sourceRefsJSON) > 0 {
		_ = json.Unmarshal(sourceRefsJSON, &c.SourceRefs)
	}
	if c.SourceRefs == nil {
		c.SourceRefs = []string{}
	}
	return c, nil
}

func scanCandidateRow(row rowScanner) (DiscoveryCandidate, error) {
	return scanCandidate(row)
}
