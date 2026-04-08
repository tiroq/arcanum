package governance

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// StateStore persists and retrieves governance state from PostgreSQL.
type StateStore struct {
	pool *pgxpool.Pool
}

// NewStateStore creates a StateStore.
func NewStateStore(pool *pgxpool.Pool) *StateStore {
	return &StateStore{pool: pool}
}

// GetState retrieves the current governance state.
// Returns the default state if no row exists or on read failure (fail-safe).
func (s *StateStore) GetState(ctx context.Context) (GovernanceState, error) {
	const q = `
		SELECT mode, freeze_learning, freeze_policy, freeze_exploration,
		       force_reasoning_mode, force_safe_mode, require_human_review,
		       reason, last_updated
		FROM agent_governance_state WHERE id = 1`

	var st GovernanceState
	err := s.pool.QueryRow(ctx, q).Scan(
		&st.Mode, &st.FreezeLearning, &st.FreezePolicyUpdates,
		&st.FreezeExploration, &st.ForceReasoningMode, &st.ForceSafeMode,
		&st.RequireHumanReview, &st.Reason, &st.LastUpdated,
	)
	if err != nil {
		return DefaultState(), fmt.Errorf("get governance state: %w", err)
	}
	return st, nil
}

// SaveState persists the governance state using single-row UPSERT.
func (s *StateStore) SaveState(ctx context.Context, st GovernanceState) error {
	const q = `
		INSERT INTO agent_governance_state
			(id, mode, freeze_learning, freeze_policy, freeze_exploration,
			 force_reasoning_mode, force_safe_mode, require_human_review,
			 reason, last_updated)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			mode = EXCLUDED.mode,
			freeze_learning = EXCLUDED.freeze_learning,
			freeze_policy = EXCLUDED.freeze_policy,
			freeze_exploration = EXCLUDED.freeze_exploration,
			force_reasoning_mode = EXCLUDED.force_reasoning_mode,
			force_safe_mode = EXCLUDED.force_safe_mode,
			require_human_review = EXCLUDED.require_human_review,
			reason = EXCLUDED.reason,
			last_updated = EXCLUDED.last_updated`

	_, err := s.pool.Exec(ctx, q,
		st.Mode, st.FreezeLearning, st.FreezePolicyUpdates,
		st.FreezeExploration, st.ForceReasoningMode, st.ForceSafeMode,
		st.RequireHumanReview, st.Reason, st.LastUpdated,
	)
	if err != nil {
		return fmt.Errorf("save governance state: %w", err)
	}
	return nil
}

// ActionStore persists governance actions to the append-only log.
type ActionStore struct {
	pool *pgxpool.Pool
}

// NewActionStore creates an ActionStore.
func NewActionStore(pool *pgxpool.Pool) *ActionStore {
	return &ActionStore{pool: pool}
}

// RecordAction appends an operator action to the governance action log.
func (s *ActionStore) RecordAction(ctx context.Context, action GovernanceAction) error {
	payloadJSON, err := json.Marshal(action.Payload)
	if err != nil {
		return fmt.Errorf("marshal action payload: %w", err)
	}

	const q = `
		INSERT INTO agent_governance_actions (action_type, requested_by, reason, payload, created_at)
		VALUES ($1, $2, $3, $4, $5)`

	_, err = s.pool.Exec(ctx, q,
		action.ActionType, action.RequestedBy, action.Reason,
		payloadJSON, action.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("record governance action: %w", err)
	}
	return nil
}

// ListActions returns recent governance actions, ordered by created_at DESC.
func (s *ActionStore) ListActions(ctx context.Context, limit, offset int) ([]GovernanceAction, error) {
	const q = `
		SELECT id, action_type, requested_by, reason, payload, created_at
		FROM agent_governance_actions
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := s.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list governance actions: %w", err)
	}
	defer rows.Close()

	var actions []GovernanceAction
	for rows.Next() {
		var a GovernanceAction
		var payloadJSON []byte
		if err := rows.Scan(&a.ID, &a.ActionType, &a.RequestedBy, &a.Reason, &payloadJSON, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan governance action: %w", err)
		}
		if len(payloadJSON) > 0 {
			_ = json.Unmarshal(payloadJSON, &a.Payload)
		}
		if a.Payload == nil {
			a.Payload = map[string]any{}
		}
		actions = append(actions, a)
	}
	if actions == nil {
		actions = []GovernanceAction{}
	}
	return actions, nil
}

// ReplayStore persists decision replay packs.
type ReplayStore struct {
	pool *pgxpool.Pool
}

// NewReplayStore creates a ReplayStore.
func NewReplayStore(pool *pgxpool.Pool) *ReplayStore {
	return &ReplayStore{pool: pool}
}

// Save persists a replay pack using UPSERT on decision_id.
func (s *ReplayStore) Save(ctx context.Context, rp ReplayPack) error {
	signalsJSON, _ := json.Marshal(rp.Signals)
	arbJSON, _ := json.Marshal(rp.ArbitrationTrace)
	calJSON, _ := json.Marshal(rp.CalibrationInfo)
	compJSON, _ := json.Marshal(rp.ComparativeInfo)
	cfJSON, _ := json.Marshal(rp.CounterfactualInfo)

	const q = `
		INSERT INTO agent_replay_packs
			(decision_id, goal_type, selected_mode, selected_path, confidence,
			 signals, arbitration_trace, calibration_info, comparative_info,
			 counterfactual_info, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (decision_id) DO UPDATE SET
			goal_type = EXCLUDED.goal_type,
			selected_mode = EXCLUDED.selected_mode,
			selected_path = EXCLUDED.selected_path,
			confidence = EXCLUDED.confidence,
			signals = EXCLUDED.signals,
			arbitration_trace = EXCLUDED.arbitration_trace,
			calibration_info = EXCLUDED.calibration_info,
			comparative_info = EXCLUDED.comparative_info,
			counterfactual_info = EXCLUDED.counterfactual_info,
			created_at = EXCLUDED.created_at`

	_, err := s.pool.Exec(ctx, q,
		rp.DecisionID, rp.GoalType, rp.SelectedMode, rp.SelectedPath,
		rp.Confidence, signalsJSON, arbJSON, calJSON, compJSON, cfJSON,
		rp.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("save replay pack: %w", err)
	}
	return nil
}

// GetByDecisionID retrieves a replay pack by decision ID.
func (s *ReplayStore) GetByDecisionID(ctx context.Context, decisionID string) (*ReplayPack, error) {
	const q = `
		SELECT id, decision_id, goal_type, selected_mode, selected_path, confidence,
		       signals, arbitration_trace, calibration_info, comparative_info,
		       counterfactual_info, created_at
		FROM agent_replay_packs WHERE decision_id = $1`

	var rp ReplayPack
	var signalsJSON, arbJSON, calJSON, compJSON, cfJSON []byte
	err := s.pool.QueryRow(ctx, q, decisionID).Scan(
		&rp.ID, &rp.DecisionID, &rp.GoalType, &rp.SelectedMode, &rp.SelectedPath,
		&rp.Confidence, &signalsJSON, &arbJSON, &calJSON, &compJSON, &cfJSON,
		&rp.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get replay pack: %w", err)
	}

	rp.Signals = unmarshalMap(signalsJSON)
	rp.ArbitrationTrace = unmarshalMap(arbJSON)
	rp.CalibrationInfo = unmarshalMap(calJSON)
	rp.ComparativeInfo = unmarshalMap(compJSON)
	rp.CounterfactualInfo = unmarshalMap(cfJSON)

	return &rp, nil
}

// unmarshalMap is a helper that unmarshals JSON bytes into a map.
func unmarshalMap(data []byte) map[string]any {
	if len(data) == 0 {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]any{}
	}
	return m
}

// Now returns the current time in UTC. Exported for testing.
func Now() time.Time {
	return time.Now().UTC()
}
