package executionloop

import (
	"context"
	"encoding/json"
	"fmt"

	"go.uber.org/zap"
)

// Executor runs a single execution step through external actions.
// It respects governance mode, review requirements, and dry-run semantics.
type Executor struct {
	extActions ExternalActionsProvider
	governance GovernanceProvider
	logger     *zap.Logger
}

// NewExecutor creates an Executor with the given providers.
func NewExecutor(extActions ExternalActionsProvider, logger *zap.Logger) *Executor {
	return &Executor{
		extActions: extActions,
		logger:     logger,
	}
}

// WithGovernance sets the governance provider.
func (e *Executor) WithGovernance(g GovernanceProvider) *Executor {
	e.governance = g
	return e
}

// Execute runs a single step. Returns a structured result.
// Governance checks:
//   - frozen / rollback_only → reject
//   - safe_hold → only safe actions auto-executed (review_required otherwise)
func (e *Executor) Execute(ctx context.Context, step ExecutionStep, opportunityID string) ExecutorResult {
	// Check governance mode.
	if e.governance != nil {
		mode := e.governance.GetMode(ctx)
		switch mode {
		case "frozen", "rollback_only":
			return ExecutorResult{
				Success: false,
				Error:   fmt.Sprintf("execution blocked: governance mode is %s", mode),
			}
		case "safe_hold":
			// In safe_hold, mark as requiring review.
			return ExecutorResult{
				Success:        false,
				RequiresReview: true,
				Error:          "execution paused: governance mode is safe_hold, requires review",
			}
		}
	}

	// Validate step has a tool.
	if step.Tool == "" {
		return ExecutorResult{
			Success: false,
			Error:   "step has no tool specified",
		}
	}

	// Execute via external actions provider.
	if e.extActions == nil {
		return ExecutorResult{
			Success: false,
			Error:   "external actions provider not available",
		}
	}

	payload := step.Payload
	if payload == nil {
		payload = json.RawMessage(`{}`)
	}

	result, err := e.extActions.CreateAndExecute(ctx, step.Tool, payload, opportunityID)
	if err != nil {
		e.logger.Warn("step execution failed",
			zap.String("step_id", step.ID),
			zap.String("tool", step.Tool),
			zap.Error(err),
		)
		return ExecutorResult{
			Success: false,
			Error:   err.Error(),
		}
	}

	return result
}
