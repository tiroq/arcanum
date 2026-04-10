package selfextension

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// CapacityChecker checks whether the owner has capacity for sandbox builds.
// Defined here to avoid import cycles — implemented by capacity.GraphAdapter.
type CapacityChecker interface {
	GetCapacityPenalty(ctx context.Context) float64
}

// Engine orchestrates the full self-extension lifecycle:
// proposal → spec → code → sandbox → validation → approval → deploy → rollback
type Engine struct {
	store     *Store
	specGen   *SpecGenerator
	codeGen   *CodeGenerator
	sandbox   *SandboxExecutor
	validator *Validator
	deployer  *Deployer
	capacity  CapacityChecker
	auditor   audit.AuditRecorder
	logger    *zap.Logger
}

// NewEngine creates a new self-extension engine.
func NewEngine(
	store *Store,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *Engine {
	return &Engine{
		store:     store,
		specGen:   NewSpecGenerator(),
		codeGen:   NewCodeGenerator(nil), // stub generation by default
		sandbox:   NewSandboxExecutor(DefaultSandboxConfig()),
		validator: NewValidator(),
		deployer:  NewDeployer(store, auditor, logger),
		auditor:   auditor,
		logger:    logger,
	}
}

// WithCapacity sets the capacity checker for build gating.
func (e *Engine) WithCapacity(cc CapacityChecker) *Engine {
	e.capacity = cc
	return e
}

// WithCodeGenerationProvider sets an optional LLM-assisted code generation provider.
func (e *Engine) WithCodeGenerationProvider(provider CodeGenerationProvider) *Engine {
	e.codeGen = NewCodeGenerator(provider)
	return e
}

// WithSandboxConfig overrides the default sandbox configuration.
func (e *Engine) WithSandboxConfig(config SandboxConfig) *Engine {
	e.sandbox = NewSandboxExecutor(config)
	return e
}

// --- Proposal Lifecycle ---

// CreateProposal creates a new component proposal.
// Sources: discovery, reflection, manual, failure_pattern.
// Does NOT auto-deploy anything.
func (e *Engine) CreateProposal(ctx context.Context, p ComponentProposal) (ComponentProposal, error) {
	if !IsValidSource(p.Source) {
		return ComponentProposal{}, fmt.Errorf("invalid source: %s", p.Source)
	}

	p.ID = uuid.New().String()
	p.Status = StatusProposed

	saved, err := e.store.CreateProposal(ctx, p)
	if err != nil {
		return ComponentProposal{}, fmt.Errorf("create proposal: %w", err)
	}

	e.emitAudit(ctx, "self.proposal_created", map[string]any{
		"proposal_id": saved.ID,
		"title":       saved.Title,
		"source":      saved.Source,
		"goal_score":  saved.GoalAlignmentScore,
		"value":       saved.ExpectedValue,
		"effort":      saved.EstimatedEffort,
	})

	return saved, nil
}

// ListProposals returns recent proposals.
func (e *Engine) ListProposals(ctx context.Context, limit int) ([]ComponentProposal, error) {
	return e.store.ListProposals(ctx, limit)
}

// GetProposal retrieves a single proposal.
func (e *Engine) GetProposal(ctx context.Context, id string) (ComponentProposal, error) {
	return e.store.GetProposal(ctx, id)
}

// --- Spec Generation ---

// GenerateSpec converts a proposal into a formal component spec.
// Transitions proposal status: proposed → approved (must be approved first externally).
func (e *Engine) GenerateSpec(ctx context.Context, proposalID string) (ComponentSpec, error) {
	proposal, err := e.store.GetProposal(ctx, proposalID)
	if err != nil {
		return ComponentSpec{}, fmt.Errorf("get proposal: %w", err)
	}

	// Allow spec generation for proposed or approved proposals.
	if proposal.Status != StatusProposed && proposal.Status != StatusApproved {
		return ComponentSpec{}, fmt.Errorf("cannot generate spec for proposal in status %s", proposal.Status)
	}

	spec := e.specGen.GenerateSpec(proposal)
	spec.ID = uuid.New().String()
	spec.ProposalID = proposalID

	saved, err := e.store.CreateSpec(ctx, spec)
	if err != nil {
		return ComponentSpec{}, fmt.Errorf("save spec: %w", err)
	}

	e.emitAudit(ctx, "self.spec_generated", map[string]any{
		"proposal_id":  proposalID,
		"spec_id":      saved.ID,
		"dependencies": saved.Dependencies,
		"constraints":  saved.Constraints,
		"test_count":   len(saved.TestRequirements),
	})

	return saved, nil
}

// --- Sandbox Execution ---

// RunSandbox executes a component in the sandbox.
// Checks capacity before execution. Transitions: approved → in_progress.
func (e *Engine) RunSandbox(ctx context.Context, proposalID string) (SandboxRun, error) {
	// Capacity gate: don't execute heavy builds when owner capacity is low.
	if e.capacity != nil {
		penalty := e.capacity.GetCapacityPenalty(ctx)
		if penalty > 0.12 { // >80% capacity used — defer sandbox builds
			e.logger.Info("sandbox deferred due to low capacity",
				zap.String("proposal_id", proposalID),
				zap.Float64("capacity_penalty", penalty),
			)
			return SandboxRun{}, fmt.Errorf("sandbox deferred: owner capacity too low (penalty=%.2f)", penalty)
		}
	}

	proposal, err := e.store.GetProposal(ctx, proposalID)
	if err != nil {
		return SandboxRun{}, fmt.Errorf("get proposal: %w", err)
	}

	// Must be approved or in_progress.
	if proposal.Status != StatusApproved && proposal.Status != StatusInProgress {
		return SandboxRun{}, fmt.Errorf("cannot run sandbox for proposal in status %s", proposal.Status)
	}

	// Get the spec.
	spec, err := e.store.GetSpecByProposal(ctx, proposalID)
	if err != nil {
		return SandboxRun{}, fmt.Errorf("get spec: %w", err)
	}

	// Get or generate code artifact.
	artifact, err := e.store.GetCodeArtifact(ctx, proposalID)
	if err != nil {
		// No code artifact yet — generate stub.
		artifact = e.codeGen.GenerateStub(spec)
		artifact.ID = uuid.New().String()
		artifact.Version = 1
		artifact, err = e.store.CreateCodeArtifact(ctx, artifact)
		if err != nil {
			return SandboxRun{}, fmt.Errorf("save code artifact: %w", err)
		}
	}

	// Transition to in_progress.
	if proposal.Status == StatusApproved {
		if err := e.store.UpdateProposalStatus(ctx, proposalID, StatusInProgress); err != nil {
			e.logger.Warn("failed to update proposal status to in_progress", zap.Error(err))
		}
	}

	// Get next version.
	version, err := e.store.NextVersion(ctx, proposalID)
	if err != nil {
		version = 1
	}

	// Execute in sandbox.
	run := e.sandbox.Execute(ctx, artifact, spec)
	run.ID = uuid.New().String()
	run.Version = version

	// Persist sandbox run.
	saved, err := e.store.CreateSandboxRun(ctx, run)
	if err != nil {
		e.logger.Warn("failed to persist sandbox run", zap.Error(err))
		saved = run
	}

	// Validate.
	validation := e.validator.Validate(saved, spec)

	eventType := "self.sandbox_executed"
	if validation.Valid {
		eventType = "self.validation_passed"
		if err := e.store.UpdateProposalStatus(ctx, proposalID, StatusValidated); err != nil {
			e.logger.Warn("failed to update proposal status to validated", zap.Error(err))
		}
	} else {
		e.emitAudit(ctx, "self.validation_failed", map[string]any{
			"proposal_id": proposalID,
			"version":     version,
			"reasons":     validation.Reasons,
		})
	}

	e.emitAudit(ctx, eventType, map[string]any{
		"proposal_id": proposalID,
		"version":     version,
		"result":      saved.ExecutionResult,
		"tests":       len(saved.TestResults),
		"latency_ms":  saved.Metrics.LatencyMs,
		"errors":      saved.Metrics.Errors,
		"valid":       validation.Valid,
	})

	return saved, nil
}

// GetSandboxResults returns sandbox runs for a proposal.
func (e *Engine) GetSandboxResults(ctx context.Context, proposalID string) ([]SandboxRun, error) {
	return e.store.ListSandboxRuns(ctx, proposalID)
}

// --- Approval ---

// Approve records an approval decision for a proposal.
func (e *Engine) Approve(ctx context.Context, proposalID, approvedBy, reason string) (ApprovalDecision, error) {
	proposal, err := e.store.GetProposal(ctx, proposalID)
	if err != nil {
		return ApprovalDecision{}, fmt.Errorf("get proposal: %w", err)
	}

	if proposal.Status != StatusProposed {
		return ApprovalDecision{}, fmt.Errorf("can only approve proposals in 'proposed' status, got %s", proposal.Status)
	}

	decision := ApprovalDecision{
		ProposalID:   proposalID,
		ApprovedBy:   approvedBy,
		ApprovalType: ApprovalManual,
		Approved:     true,
		Reason:       reason,
	}

	saved, err := e.store.CreateApproval(ctx, decision)
	if err != nil {
		return ApprovalDecision{}, fmt.Errorf("save approval: %w", err)
	}

	// Transition proposal to approved.
	if err := e.store.UpdateProposalStatus(ctx, proposalID, StatusApproved); err != nil {
		return ApprovalDecision{}, fmt.Errorf("update proposal status: %w", err)
	}

	e.emitAudit(ctx, "self.deployment_approved", map[string]any{
		"proposal_id": proposalID,
		"approved_by": approvedBy,
		"reason":      reason,
	})

	return saved, nil
}

// Reject records a rejection decision for a proposal.
func (e *Engine) Reject(ctx context.Context, proposalID, rejectedBy, reason string) (ApprovalDecision, error) {
	proposal, err := e.store.GetProposal(ctx, proposalID)
	if err != nil {
		return ApprovalDecision{}, fmt.Errorf("get proposal: %w", err)
	}

	// Can reject from any non-terminal state.
	if proposal.Status == StatusDeployed || proposal.Status == StatusRejected {
		return ApprovalDecision{}, fmt.Errorf("cannot reject proposal in status %s", proposal.Status)
	}

	decision := ApprovalDecision{
		ProposalID:   proposalID,
		ApprovedBy:   rejectedBy,
		ApprovalType: ApprovalManual,
		Approved:     false,
		Reason:       reason,
	}

	saved, err := e.store.CreateApproval(ctx, decision)
	if err != nil {
		return ApprovalDecision{}, fmt.Errorf("save rejection: %w", err)
	}

	if err := e.store.UpdateProposalStatus(ctx, proposalID, StatusRejected); err != nil {
		return ApprovalDecision{}, fmt.Errorf("update proposal status: %w", err)
	}

	return saved, nil
}

// --- Deployment ---

// Deploy deploys a validated component. Requires prior approval.
func (e *Engine) Deploy(ctx context.Context, proposalID string) (DeploymentRecord, error) {
	// Get latest sandbox run version for the deployment.
	latestRun, err := e.store.GetLatestSandboxRun(ctx, proposalID)
	if err != nil {
		return DeploymentRecord{}, fmt.Errorf("no sandbox run found for proposal %s", proposalID)
	}

	return e.deployer.Deploy(ctx, proposalID, latestRun.Version)
}

// Rollback rolls back a deployment by ID.
func (e *Engine) Rollback(ctx context.Context, deploymentID string) error {
	return e.deployer.Rollback(ctx, deploymentID)
}

// GetDeployment retrieves a deployment record.
func (e *Engine) GetDeployment(ctx context.Context, id string) (DeploymentRecord, error) {
	return e.store.GetDeployment(ctx, id)
}

func (e *Engine) emitAudit(ctx context.Context, eventType string, payload map[string]any) {
	if e.auditor == nil {
		return
	}
	_ = e.auditor.RecordEvent(ctx, "self_extension", uuid.Nil, eventType, "system", "self_extension_engine", payload)
}
