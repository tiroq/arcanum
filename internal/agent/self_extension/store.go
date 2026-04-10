package selfextension

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store persists self-extension entities in PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a new self-extension store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// --- Proposals ---

// CreateProposal inserts a new component proposal.
func (s *Store) CreateProposal(ctx context.Context, p ComponentProposal) (ComponentProposal, error) {
	const q = `
		INSERT INTO agent_self_proposals (
			id, title, description, source, goal_alignment_score,
			expected_value, estimated_effort, status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, title, description, source, goal_alignment_score,
		          expected_value, estimated_effort, status, created_at, updated_at`

	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	err := s.pool.QueryRow(ctx, q,
		p.ID, p.Title, p.Description, p.Source, p.GoalAlignmentScore,
		p.ExpectedValue, p.EstimatedEffort, p.Status, p.CreatedAt, p.UpdatedAt,
	).Scan(
		&p.ID, &p.Title, &p.Description, &p.Source, &p.GoalAlignmentScore,
		&p.ExpectedValue, &p.EstimatedEffort, &p.Status, &p.CreatedAt, &p.UpdatedAt,
	)
	return p, err
}

// GetProposal retrieves a single proposal by ID.
func (s *Store) GetProposal(ctx context.Context, id string) (ComponentProposal, error) {
	const q = `
		SELECT id, title, description, source, goal_alignment_score,
		       expected_value, estimated_effort, status, created_at, updated_at
		FROM agent_self_proposals
		WHERE id = $1`

	var p ComponentProposal
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&p.ID, &p.Title, &p.Description, &p.Source, &p.GoalAlignmentScore,
		&p.ExpectedValue, &p.EstimatedEffort, &p.Status, &p.CreatedAt, &p.UpdatedAt,
	)
	return p, err
}

// ListProposals returns recent proposals ordered by created_at DESC.
func (s *Store) ListProposals(ctx context.Context, limit int) ([]ComponentProposal, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `
		SELECT id, title, description, source, goal_alignment_score,
		       expected_value, estimated_effort, status, created_at, updated_at
		FROM agent_self_proposals
		ORDER BY created_at DESC
		LIMIT $1`

	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var proposals []ComponentProposal
	for rows.Next() {
		var p ComponentProposal
		if err := rows.Scan(
			&p.ID, &p.Title, &p.Description, &p.Source, &p.GoalAlignmentScore,
			&p.ExpectedValue, &p.EstimatedEffort, &p.Status, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			continue
		}
		proposals = append(proposals, p)
	}
	return proposals, nil
}

// UpdateProposalStatus updates the status of a proposal with transition validation.
func (s *Store) UpdateProposalStatus(ctx context.Context, id, newStatus string) error {
	const q = `
		UPDATE agent_self_proposals
		SET status = $2, updated_at = $3
		WHERE id = $1`

	_, err := s.pool.Exec(ctx, q, id, newStatus, time.Now().UTC())
	return err
}

// --- Specs ---

// CreateSpec inserts a new component spec.
func (s *Store) CreateSpec(ctx context.Context, spec ComponentSpec) (ComponentSpec, error) {
	deps, _ := json.Marshal(spec.Dependencies)
	constraints, _ := json.Marshal(spec.Constraints)
	testReqs, _ := json.Marshal(spec.TestRequirements)

	const q = `
		INSERT INTO agent_self_specs (
			id, proposal_id, input_contract, output_contract,
			dependencies, constraints, test_requirements, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, proposal_id, input_contract, output_contract,
		          dependencies, constraints, test_requirements, created_at`

	now := time.Now().UTC()
	if spec.CreatedAt.IsZero() {
		spec.CreatedAt = now
	}

	var depsRaw, constraintsRaw, testReqsRaw []byte
	err := s.pool.QueryRow(ctx, q,
		spec.ID, spec.ProposalID, spec.InputContract, spec.OutputContract,
		deps, constraints, testReqs, spec.CreatedAt,
	).Scan(
		&spec.ID, &spec.ProposalID, &spec.InputContract, &spec.OutputContract,
		&depsRaw, &constraintsRaw, &testReqsRaw, &spec.CreatedAt,
	)
	if err != nil {
		return spec, err
	}
	_ = json.Unmarshal(depsRaw, &spec.Dependencies)
	_ = json.Unmarshal(constraintsRaw, &spec.Constraints)
	_ = json.Unmarshal(testReqsRaw, &spec.TestRequirements)
	return spec, nil
}

// GetSpecByProposal retrieves the spec for a given proposal.
func (s *Store) GetSpecByProposal(ctx context.Context, proposalID string) (ComponentSpec, error) {
	const q = `
		SELECT id, proposal_id, input_contract, output_contract,
		       dependencies, constraints, test_requirements, created_at
		FROM agent_self_specs
		WHERE proposal_id = $1
		ORDER BY created_at DESC
		LIMIT 1`

	var spec ComponentSpec
	var depsRaw, constraintsRaw, testReqsRaw []byte
	err := s.pool.QueryRow(ctx, q, proposalID).Scan(
		&spec.ID, &spec.ProposalID, &spec.InputContract, &spec.OutputContract,
		&depsRaw, &constraintsRaw, &testReqsRaw, &spec.CreatedAt,
	)
	if err != nil {
		return spec, err
	}
	_ = json.Unmarshal(depsRaw, &spec.Dependencies)
	_ = json.Unmarshal(constraintsRaw, &spec.Constraints)
	_ = json.Unmarshal(testReqsRaw, &spec.TestRequirements)
	return spec, nil
}

// --- Sandbox Runs ---

// CreateSandboxRun inserts a sandbox execution record.
func (s *Store) CreateSandboxRun(ctx context.Context, run SandboxRun) (SandboxRun, error) {
	testResultsJSON, _ := json.Marshal(run.TestResults)
	metricsJSON, _ := json.Marshal(run.Metrics)

	const q = `
		INSERT INTO agent_self_sandbox_runs (
			id, proposal_id, version, execution_result,
			test_results, logs, metrics, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, proposal_id, version, execution_result,
		          test_results, logs, metrics, created_at`

	now := time.Now().UTC()
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}

	var testResultsRaw, metricsRaw []byte
	err := s.pool.QueryRow(ctx, q,
		run.ID, run.ProposalID, run.Version, run.ExecutionResult,
		testResultsJSON, run.Logs, metricsJSON, run.CreatedAt,
	).Scan(
		&run.ID, &run.ProposalID, &run.Version, &run.ExecutionResult,
		&testResultsRaw, &run.Logs, &metricsRaw, &run.CreatedAt,
	)
	if err != nil {
		return run, err
	}
	_ = json.Unmarshal(testResultsRaw, &run.TestResults)
	_ = json.Unmarshal(metricsRaw, &run.Metrics)
	return run, nil
}

// ListSandboxRuns returns sandbox runs for a proposal, ordered by version DESC.
func (s *Store) ListSandboxRuns(ctx context.Context, proposalID string) ([]SandboxRun, error) {
	const q = `
		SELECT id, proposal_id, version, execution_result,
		       test_results, logs, metrics, created_at
		FROM agent_self_sandbox_runs
		WHERE proposal_id = $1
		ORDER BY version DESC`

	rows, err := s.pool.Query(ctx, q, proposalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []SandboxRun
	for rows.Next() {
		var r SandboxRun
		var testResultsRaw, metricsRaw []byte
		if err := rows.Scan(
			&r.ID, &r.ProposalID, &r.Version, &r.ExecutionResult,
			&testResultsRaw, &r.Logs, &metricsRaw, &r.CreatedAt,
		); err != nil {
			continue
		}
		_ = json.Unmarshal(testResultsRaw, &r.TestResults)
		_ = json.Unmarshal(metricsRaw, &r.Metrics)
		runs = append(runs, r)
	}
	return runs, nil
}

// GetLatestSandboxRun returns the most recent sandbox run for a proposal.
func (s *Store) GetLatestSandboxRun(ctx context.Context, proposalID string) (SandboxRun, error) {
	const q = `
		SELECT id, proposal_id, version, execution_result,
		       test_results, logs, metrics, created_at
		FROM agent_self_sandbox_runs
		WHERE proposal_id = $1
		ORDER BY version DESC
		LIMIT 1`

	var r SandboxRun
	var testResultsRaw, metricsRaw []byte
	err := s.pool.QueryRow(ctx, q, proposalID).Scan(
		&r.ID, &r.ProposalID, &r.Version, &r.ExecutionResult,
		&testResultsRaw, &r.Logs, &metricsRaw, &r.CreatedAt,
	)
	if err != nil {
		return r, err
	}
	_ = json.Unmarshal(testResultsRaw, &r.TestResults)
	_ = json.Unmarshal(metricsRaw, &r.Metrics)
	return r, nil
}

// --- Deployments ---

// CreateDeployment inserts a new deployment record.
func (s *Store) CreateDeployment(ctx context.Context, d DeploymentRecord) (DeploymentRecord, error) {
	const q = `
		INSERT INTO agent_self_deployments (
			id, proposal_id, version, status, deployed_at, rollback_available
		) VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, proposal_id, version, status, deployed_at, rollback_available`

	now := time.Now().UTC()
	if d.DeployedAt.IsZero() {
		d.DeployedAt = now
	}

	err := s.pool.QueryRow(ctx, q,
		d.ID, d.ProposalID, d.Version, d.Status, d.DeployedAt, d.RollbackAvailable,
	).Scan(
		&d.ID, &d.ProposalID, &d.Version, &d.Status, &d.DeployedAt, &d.RollbackAvailable,
	)
	return d, err
}

// GetDeployment retrieves a deployment by ID.
func (s *Store) GetDeployment(ctx context.Context, id string) (DeploymentRecord, error) {
	const q = `
		SELECT id, proposal_id, version, status, deployed_at, rollback_available
		FROM agent_self_deployments
		WHERE id = $1`

	var d DeploymentRecord
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&d.ID, &d.ProposalID, &d.Version, &d.Status, &d.DeployedAt, &d.RollbackAvailable,
	)
	return d, err
}

// GetActiveDeployment returns the currently active deployment for a proposal (if any).
func (s *Store) GetActiveDeployment(ctx context.Context, proposalID string) (DeploymentRecord, error) {
	const q = `
		SELECT id, proposal_id, version, status, deployed_at, rollback_available
		FROM agent_self_deployments
		WHERE proposal_id = $1 AND status = 'active'
		ORDER BY deployed_at DESC
		LIMIT 1`

	var d DeploymentRecord
	err := s.pool.QueryRow(ctx, q, proposalID).Scan(
		&d.ID, &d.ProposalID, &d.Version, &d.Status, &d.DeployedAt, &d.RollbackAvailable,
	)
	return d, err
}

// UpdateDeploymentStatus updates the status of a deployment.
func (s *Store) UpdateDeploymentStatus(ctx context.Context, id, status string) error {
	const q = `
		UPDATE agent_self_deployments
		SET status = $2
		WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, id, status)
	return err
}

// --- Rollback Points ---

// CreateRollbackPoint inserts a new rollback point.
func (s *Store) CreateRollbackPoint(ctx context.Context, rp RollbackPoint) (RollbackPoint, error) {
	const q = `
		INSERT INTO agent_self_rollback_points (
			id, deployment_id, previous_version, created_at
		) VALUES ($1, $2, $3, $4)
		RETURNING id, deployment_id, previous_version, created_at`

	now := time.Now().UTC()
	if rp.CreatedAt.IsZero() {
		rp.CreatedAt = now
	}

	err := s.pool.QueryRow(ctx, q,
		rp.ID, rp.DeploymentID, rp.PreviousVersion, rp.CreatedAt,
	).Scan(
		&rp.ID, &rp.DeploymentID, &rp.PreviousVersion, &rp.CreatedAt,
	)
	return rp, err
}

// GetRollbackPointByDeployment returns the rollback point for a deployment.
func (s *Store) GetRollbackPointByDeployment(ctx context.Context, deploymentID string) (RollbackPoint, error) {
	const q = `
		SELECT id, deployment_id, previous_version, created_at
		FROM agent_self_rollback_points
		WHERE deployment_id = $1`

	var rp RollbackPoint
	err := s.pool.QueryRow(ctx, q, deploymentID).Scan(
		&rp.ID, &rp.DeploymentID, &rp.PreviousVersion, &rp.CreatedAt,
	)
	return rp, err
}

// --- Code Artifacts ---

// CreateCodeArtifact inserts a new code artifact (versioned, append-only).
func (s *Store) CreateCodeArtifact(ctx context.Context, a CodeArtifact) (CodeArtifact, error) {
	const q = `
		INSERT INTO agent_self_code_artifacts (
			id, proposal_id, version, language, source, content, checksum, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, proposal_id, version, language, source, content, checksum, created_at`

	now := time.Now().UTC()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}

	err := s.pool.QueryRow(ctx, q,
		a.ID, a.ProposalID, a.Version, a.Language, a.Source, a.Content, a.Checksum, a.CreatedAt,
	).Scan(
		&a.ID, &a.ProposalID, &a.Version, &a.Language, &a.Source, &a.Content, &a.Checksum, &a.CreatedAt,
	)
	return a, err
}

// GetCodeArtifact retrieves the latest code artifact for a proposal.
func (s *Store) GetCodeArtifact(ctx context.Context, proposalID string) (CodeArtifact, error) {
	const q = `
		SELECT id, proposal_id, version, language, source, content, checksum, created_at
		FROM agent_self_code_artifacts
		WHERE proposal_id = $1
		ORDER BY version DESC
		LIMIT 1`

	var a CodeArtifact
	err := s.pool.QueryRow(ctx, q, proposalID).Scan(
		&a.ID, &a.ProposalID, &a.Version, &a.Language, &a.Source, &a.Content, &a.Checksum, &a.CreatedAt,
	)
	return a, err
}

// GetCodeArtifactByVersion retrieves a specific version of a code artifact.
func (s *Store) GetCodeArtifactByVersion(ctx context.Context, proposalID string, version int) (CodeArtifact, error) {
	const q = `
		SELECT id, proposal_id, version, language, source, content, checksum, created_at
		FROM agent_self_code_artifacts
		WHERE proposal_id = $1 AND version = $2`

	var a CodeArtifact
	err := s.pool.QueryRow(ctx, q, proposalID, version).Scan(
		&a.ID, &a.ProposalID, &a.Version, &a.Language, &a.Source, &a.Content, &a.Checksum, &a.CreatedAt,
	)
	return a, err
}

// --- Approvals ---

// CreateApproval inserts an approval decision.
func (s *Store) CreateApproval(ctx context.Context, a ApprovalDecision) (ApprovalDecision, error) {
	const q = `
		INSERT INTO agent_self_approvals (
			proposal_id, approved_by, approval_type, approved, reason, decided_at
		) VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING proposal_id, approved_by, approval_type, approved, reason, decided_at`

	now := time.Now().UTC()
	if a.DecidedAt.IsZero() {
		a.DecidedAt = now
	}

	err := s.pool.QueryRow(ctx, q,
		a.ProposalID, a.ApprovedBy, a.ApprovalType, a.Approved, a.Reason, a.DecidedAt,
	).Scan(
		&a.ProposalID, &a.ApprovedBy, &a.ApprovalType, &a.Approved, &a.Reason, &a.DecidedAt,
	)
	return a, err
}

// GetApproval retrieves the approval decision for a proposal.
func (s *Store) GetApproval(ctx context.Context, proposalID string) (ApprovalDecision, bool, error) {
	const q = `
		SELECT proposal_id, approved_by, approval_type, approved, reason, decided_at
		FROM agent_self_approvals
		WHERE proposal_id = $1
		ORDER BY decided_at DESC
		LIMIT 1`

	var a ApprovalDecision
	err := s.pool.QueryRow(ctx, q, proposalID).Scan(
		&a.ProposalID, &a.ApprovedBy, &a.ApprovalType, &a.Approved, &a.Reason, &a.DecidedAt,
	)
	if err != nil {
		return a, false, nil // fail-open: no approval found
	}
	return a, true, nil
}

// NextVersion returns the next version number for sandbox runs of a proposal.
func (s *Store) NextVersion(ctx context.Context, proposalID string) (int, error) {
	const q = `
		SELECT COALESCE(MAX(version), 0) + 1
		FROM agent_self_sandbox_runs
		WHERE proposal_id = $1`

	var v int
	err := s.pool.QueryRow(ctx, q, proposalID).Scan(&v)
	return v, err
}
