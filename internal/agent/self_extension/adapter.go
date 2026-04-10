package selfextension

import (
	"context"

	"go.uber.org/zap"
)

// GraphAdapter exposes self-extension data for the decision graph and API.
// Nil-safe and fail-open: returns empty values if engine is not available.
type GraphAdapter struct {
	engine *Engine
	logger *zap.Logger
}

// NewGraphAdapter creates a new self-extension graph adapter.
func NewGraphAdapter(engine *Engine, logger *zap.Logger) *GraphAdapter {
	return &GraphAdapter{engine: engine, logger: logger}
}

// --- Proposal API ---

// CreateProposal creates a new component proposal.
func (a *GraphAdapter) CreateProposal(ctx context.Context, p ComponentProposal) (ComponentProposal, error) {
	if a == nil || a.engine == nil {
		return ComponentProposal{}, nil
	}
	return a.engine.CreateProposal(ctx, p)
}

// ListProposals returns recent proposals.
func (a *GraphAdapter) ListProposals(ctx context.Context, limit int) ([]ComponentProposal, error) {
	if a == nil || a.engine == nil {
		return nil, nil
	}
	return a.engine.ListProposals(ctx, limit)
}

// GetProposal retrieves a proposal by ID.
func (a *GraphAdapter) GetProposal(ctx context.Context, id string) (ComponentProposal, error) {
	if a == nil || a.engine == nil {
		return ComponentProposal{}, nil
	}
	return a.engine.GetProposal(ctx, id)
}

// --- Spec API ---

// GenerateSpec converts a proposal into a formal component spec.
func (a *GraphAdapter) GenerateSpec(ctx context.Context, proposalID string) (ComponentSpec, error) {
	if a == nil || a.engine == nil {
		return ComponentSpec{}, nil
	}
	return a.engine.GenerateSpec(ctx, proposalID)
}

// --- Sandbox API ---

// RunSandbox executes a component in the sandbox.
func (a *GraphAdapter) RunSandbox(ctx context.Context, proposalID string) (SandboxRun, error) {
	if a == nil || a.engine == nil {
		return SandboxRun{}, nil
	}
	return a.engine.RunSandbox(ctx, proposalID)
}

// GetSandboxResults returns sandbox runs for a proposal.
func (a *GraphAdapter) GetSandboxResults(ctx context.Context, proposalID string) ([]SandboxRun, error) {
	if a == nil || a.engine == nil {
		return nil, nil
	}
	return a.engine.GetSandboxResults(ctx, proposalID)
}

// --- Approval API ---

// Approve records an approval decision.
func (a *GraphAdapter) Approve(ctx context.Context, proposalID, approvedBy, reason string) (ApprovalDecision, error) {
	if a == nil || a.engine == nil {
		return ApprovalDecision{}, nil
	}
	return a.engine.Approve(ctx, proposalID, approvedBy, reason)
}

// Reject records a rejection decision.
func (a *GraphAdapter) Reject(ctx context.Context, proposalID, rejectedBy, reason string) (ApprovalDecision, error) {
	if a == nil || a.engine == nil {
		return ApprovalDecision{}, nil
	}
	return a.engine.Reject(ctx, proposalID, rejectedBy, reason)
}

// --- Deployment API ---

// Deploy deploys a validated component.
func (a *GraphAdapter) Deploy(ctx context.Context, proposalID string) (DeploymentRecord, error) {
	if a == nil || a.engine == nil {
		return DeploymentRecord{}, nil
	}
	return a.engine.Deploy(ctx, proposalID)
}

// Rollback rolls back a deployment.
func (a *GraphAdapter) Rollback(ctx context.Context, deploymentID string) error {
	if a == nil || a.engine == nil {
		return nil
	}
	return a.engine.Rollback(ctx, deploymentID)
}

// GetDeployment retrieves a deployment record.
func (a *GraphAdapter) GetDeployment(ctx context.Context, id string) (DeploymentRecord, error) {
	if a == nil || a.engine == nil {
		return DeploymentRecord{}, nil
	}
	return a.engine.GetDeployment(ctx, id)
}
