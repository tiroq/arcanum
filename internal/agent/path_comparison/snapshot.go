package pathcomparison

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SnapshotStore persists and retrieves decision snapshots.
type SnapshotStore struct {
	db *pgxpool.Pool
}

// NewSnapshotStore creates a SnapshotStore backed by PostgreSQL.
func NewSnapshotStore(db *pgxpool.Pool) *SnapshotStore {
	return &SnapshotStore{db: db}
}

// SaveSnapshot persists a decision snapshot.
func (s *SnapshotStore) SaveSnapshot(ctx context.Context, snap DecisionSnapshot) error {
	candidatesJSON, err := json.Marshal(snap.Candidates)
	if err != nil {
		return err
	}

	const q = `
		INSERT INTO agent_path_decision_snapshots
			(id, decision_id, goal_type, selected_path, selected_score, candidates, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (decision_id) DO NOTHING`

	_, err = s.db.Exec(ctx, q,
		uuid.New(), snap.DecisionID, snap.GoalType,
		snap.SelectedPathSignature, snap.SelectedScore,
		candidatesJSON, snap.CreatedAt,
	)
	return err
}

// GetSnapshot retrieves a decision snapshot by decision ID.
// Returns nil if not found.
func (s *SnapshotStore) GetSnapshot(ctx context.Context, decisionID string) (*DecisionSnapshot, error) {
	const q = `
		SELECT decision_id, goal_type, selected_path, selected_score, candidates, created_at
		FROM agent_path_decision_snapshots
		WHERE decision_id = $1`

	var snap DecisionSnapshot
	var candidatesJSON []byte
	err := s.db.QueryRow(ctx, q, decisionID).Scan(
		&snap.DecisionID, &snap.GoalType,
		&snap.SelectedPathSignature, &snap.SelectedScore,
		&candidatesJSON, &snap.CreatedAt,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(candidatesJSON, &snap.Candidates); err != nil {
		return nil, err
	}
	return &snap, nil
}

// ListSnapshots returns recent decision snapshots ordered by created_at DESC.
func (s *SnapshotStore) ListSnapshots(ctx context.Context, limit, offset int) ([]DecisionSnapshot, error) {
	const q = `
		SELECT decision_id, goal_type, selected_path, selected_score, candidates, created_at
		FROM agent_path_decision_snapshots
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := s.db.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []DecisionSnapshot
	for rows.Next() {
		var snap DecisionSnapshot
		var candidatesJSON []byte
		if err := rows.Scan(
			&snap.DecisionID, &snap.GoalType,
			&snap.SelectedPathSignature, &snap.SelectedScore,
			&candidatesJSON, &snap.CreatedAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(candidatesJSON, &snap.Candidates); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snap)
	}
	return snapshots, nil
}

// ListSnapshotsByGoalType returns snapshots filtered by goal type.
func (s *SnapshotStore) ListSnapshotsByGoalType(ctx context.Context, goalType string, limit, offset int) ([]DecisionSnapshot, error) {
	const q = `
		SELECT decision_id, goal_type, selected_path, selected_score, candidates, created_at
		FROM agent_path_decision_snapshots
		WHERE goal_type = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`

	rows, err := s.db.Query(ctx, q, goalType, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []DecisionSnapshot
	for rows.Next() {
		var snap DecisionSnapshot
		var candidatesJSON []byte
		if err := rows.Scan(
			&snap.DecisionID, &snap.GoalType,
			&snap.SelectedPathSignature, &snap.SelectedScore,
			&candidatesJSON, &snap.CreatedAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(candidatesJSON, &snap.Candidates); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snap)
	}
	return snapshots, nil
}

// CaptureSnapshot creates a DecisionSnapshot from scored paths and selection.
// Pure function — does not persist.
func CaptureSnapshot(decisionID, goalType string, scoredPaths []ScoredPathInfo, selectedSignature string, selectedScore float64) DecisionSnapshot {
	candidates := make([]PathCandidateSnapshot, len(scoredPaths))

	// Sort by score descending to assign ranks.
	sorted := make([]ScoredPathInfo, len(scoredPaths))
	copy(sorted, scoredPaths)
	sortScoredPaths(sorted)

	for i, sp := range sorted {
		candidates[i] = PathCandidateSnapshot{
			PathSignature: sp.PathSignature,
			Score:         sp.Score,
			Rank:          i + 1,
		}
	}

	return DecisionSnapshot{
		DecisionID:            decisionID,
		GoalType:              goalType,
		SelectedPathSignature: selectedSignature,
		SelectedScore:         selectedScore,
		Candidates:            candidates,
		CreatedAt:             time.Now().UTC(),
	}
}

// ScoredPathInfo carries a path signature and its score for snapshot capture.
type ScoredPathInfo struct {
	PathSignature string
	Score         float64
}

// sortScoredPaths sorts by Score DESC, PathSignature ASC for deterministic ordering.
// Uses insertion sort for stable, deterministic results.
func sortScoredPaths(paths []ScoredPathInfo) {
	for i := 1; i < len(paths); i++ {
		for j := i; j > 0; j-- {
			if shouldSwapScored(paths[j], paths[j-1]) {
				paths[j], paths[j-1] = paths[j-1], paths[j]
			} else {
				break
			}
		}
	}
}

// shouldSwapScored returns true if a should rank above b.
func shouldSwapScored(a, b ScoredPathInfo) bool {
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	return a.PathSignature < b.PathSignature
}
