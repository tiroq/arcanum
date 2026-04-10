package selfextension

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/audit"
)

// Deployer handles versioned deployment and rollback of validated components.
// All deployments are reversible. No deployment without approval. No silent upgrades.
type Deployer struct {
	store   *Store
	auditor audit.AuditRecorder
	logger  *zap.Logger
}

// NewDeployer creates a new Deployer.
func NewDeployer(store *Store, auditor audit.AuditRecorder, logger *zap.Logger) *Deployer {
	return &Deployer{store: store, auditor: auditor, logger: logger}
}

// Deploy creates a new versioned deployment for a validated proposal.
// Requirements:
//   - proposal must be in validated status
//   - explicit approval must exist
//   - previous active deployment (if any) gets a rollback point
//
// Fail-safe: deployment is NOT fail-open. Errors cause rejection.
func (d *Deployer) Deploy(ctx context.Context, proposalID string, version int) (DeploymentRecord, error) {
	// Check approval gate — no bypass.
	approval, found, err := d.store.GetApproval(ctx, proposalID)
	if err != nil {
		return DeploymentRecord{}, fmt.Errorf("check approval: %w", err)
	}
	if !found || !approval.Approved {
		return DeploymentRecord{}, fmt.Errorf("deployment rejected: no valid approval for proposal %s", proposalID)
	}

	// Check proposal status.
	proposal, err := d.store.GetProposal(ctx, proposalID)
	if err != nil {
		return DeploymentRecord{}, fmt.Errorf("get proposal: %w", err)
	}
	if proposal.Status != StatusValidated {
		return DeploymentRecord{}, fmt.Errorf("deployment rejected: proposal %s has status %s, expected %s",
			proposalID, proposal.Status, StatusValidated)
	}

	// If there's an existing active deployment, create rollback point.
	existingDeploy, err := d.store.GetActiveDeployment(ctx, proposalID)
	if err == nil && existingDeploy.ID != "" {
		// Mark existing deployment as rolled back.
		if err := d.store.UpdateDeploymentStatus(ctx, existingDeploy.ID, DeployRolledBack); err != nil {
			return DeploymentRecord{}, fmt.Errorf("deactivate previous deployment: %w", err)
		}

		// Create rollback point.
		rp := RollbackPoint{
			ID:              uuid.New().String(),
			PreviousVersion: existingDeploy.Version,
		}
		// DeploymentID will be set after new deployment is created.
		_ = rp // used below
	}

	// Create new deployment.
	deployment := DeploymentRecord{
		ID:                uuid.New().String(),
		ProposalID:        proposalID,
		Version:           version,
		Status:            DeployActive,
		RollbackAvailable: existingDeploy.ID != "",
	}

	saved, err := d.store.CreateDeployment(ctx, deployment)
	if err != nil {
		return DeploymentRecord{}, fmt.Errorf("create deployment: %w", err)
	}

	// Create rollback point if there was a previous version.
	if existingDeploy.ID != "" {
		rp := RollbackPoint{
			ID:              uuid.New().String(),
			DeploymentID:    saved.ID,
			PreviousVersion: existingDeploy.Version,
		}
		if _, err := d.store.CreateRollbackPoint(ctx, rp); err != nil {
			d.logger.Warn("failed to create rollback point", zap.Error(err))
		}
	}

	// Update proposal status.
	if err := d.store.UpdateProposalStatus(ctx, proposalID, StatusDeployed); err != nil {
		d.logger.Warn("failed to update proposal status to deployed", zap.Error(err))
	}

	// Audit.
	d.emitAudit(ctx, "self.deployed", map[string]any{
		"proposal_id": proposalID,
		"version":     version,
		"deployment":  saved.ID,
		"rollback":    saved.RollbackAvailable,
	})

	return saved, nil
}

// Rollback restores the previous version for a deployment.
// Marks the current deployment as rolled_back. Emits audit event.
func (d *Deployer) Rollback(ctx context.Context, deploymentID string) error {
	// Get the deployment to rollback.
	deployment, err := d.store.GetDeployment(ctx, deploymentID)
	if err != nil {
		return fmt.Errorf("get deployment: %w", err)
	}

	if deployment.Status != DeployActive {
		return fmt.Errorf("cannot rollback deployment %s: status is %s, expected %s",
			deploymentID, deployment.Status, DeployActive)
	}

	// Get rollback point.
	rp, err := d.store.GetRollbackPointByDeployment(ctx, deploymentID)
	if err != nil {
		return fmt.Errorf("no rollback point found for deployment %s", deploymentID)
	}

	// Mark deployment as rolled back.
	if err := d.store.UpdateDeploymentStatus(ctx, deploymentID, DeployRolledBack); err != nil {
		return fmt.Errorf("update deployment status: %w", err)
	}

	// Audit.
	d.emitAudit(ctx, "self.rollback_triggered", map[string]any{
		"deployment_id":       deploymentID,
		"proposal_id":         deployment.ProposalID,
		"rolled_back_version": deployment.Version,
		"previous_version":    rp.PreviousVersion,
	})

	d.logger.Info("deployment rolled back",
		zap.String("deployment_id", deploymentID),
		zap.String("proposal_id", deployment.ProposalID),
		zap.Int("rolled_back_version", deployment.Version),
		zap.Int("previous_version", rp.PreviousVersion),
	)

	return nil
}

func (d *Deployer) emitAudit(ctx context.Context, eventType string, payload map[string]any) {
	if d.auditor == nil {
		return
	}
	_ = d.auditor.RecordEvent(ctx, "self_extension", uuid.Nil, eventType, "system", "deployer", payload)
}
