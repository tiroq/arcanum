package governance

import (
	"context"

	"go.uber.org/zap"
)

// GovernanceProvider is the interface consumed by the decision graph planner adapter
// and other runtime components for governance enforcement.
// Defined with primitive returns to avoid import cycles.
type GovernanceProvider interface {
	// IsLearningBlocked returns true if learning writes should be suppressed.
	IsLearningBlocked(ctx context.Context) bool
	// IsPolicyBlocked returns true if policy updates should be suppressed.
	IsPolicyBlocked(ctx context.Context) bool
	// IsExplorationBlocked returns true if exploration should be disabled.
	IsExplorationBlocked(ctx context.Context) bool
	// EffectiveReasoningMode returns a forced reasoning mode, or "" for no override.
	EffectiveReasoningMode(ctx context.Context) string
	// RequiresHumanReview returns true if autonomous application should be suppressed.
	RequiresHumanReview(ctx context.Context) bool
	// GetMode returns the current governance mode string.
	GetMode(ctx context.Context) string
}

// ControllerAdapter adapts the governance Controller to the GovernanceProvider interface.
// Fail-safe: on read failure, degrades to safer behavior via Controller.GetState.
type ControllerAdapter struct {
	controller *Controller
	logger     *zap.Logger
}

// NewControllerAdapter creates a ControllerAdapter.
func NewControllerAdapter(controller *Controller, logger *zap.Logger) *ControllerAdapter {
	return &ControllerAdapter{
		controller: controller,
		logger:     logger,
	}
}

func (a *ControllerAdapter) IsLearningBlocked(ctx context.Context) bool {
	return a.controller.GetState(ctx).IsLearningBlocked()
}

func (a *ControllerAdapter) IsPolicyBlocked(ctx context.Context) bool {
	return a.controller.GetState(ctx).IsPolicyBlocked()
}

func (a *ControllerAdapter) IsExplorationBlocked(ctx context.Context) bool {
	return a.controller.GetState(ctx).IsExplorationBlocked()
}

func (a *ControllerAdapter) EffectiveReasoningMode(ctx context.Context) string {
	return a.controller.GetState(ctx).EffectiveReasoningMode()
}

func (a *ControllerAdapter) RequiresHumanReview(ctx context.Context) bool {
	return a.controller.GetState(ctx).RequireHumanReview
}

func (a *ControllerAdapter) GetMode(ctx context.Context) string {
	return a.controller.GetState(ctx).Mode
}

// ReplayPackRecorder is the interface consumed by the decision graph adapter
// to record replay packs after each decision.
type ReplayPackRecorder interface {
	RecordReplayPack(ctx context.Context, rp ReplayPack)
}

// ReplayPackBuilderAdapter adapts ReplayPackBuilder to the ReplayPackRecorder interface.
type ReplayPackBuilderAdapter struct {
	builder *ReplayPackBuilder
}

// NewReplayPackBuilderAdapter creates a ReplayPackBuilderAdapter.
func NewReplayPackBuilderAdapter(builder *ReplayPackBuilder) *ReplayPackBuilderAdapter {
	return &ReplayPackBuilderAdapter{builder: builder}
}

func (a *ReplayPackBuilderAdapter) RecordReplayPack(ctx context.Context, rp ReplayPack) {
	if a.builder != nil {
		a.builder.RecordReplayPack(ctx, rp)
	}
}

// GraphReplayAdapter bridges the governance ReplayPackBuilder to the decision graph's
// ReplayPackRecorder interface. Uses primitive parameters to avoid import cycles.
type GraphReplayAdapter struct {
	builder *ReplayPackBuilder
}

// NewGraphReplayAdapter creates a GraphReplayAdapter.
func NewGraphReplayAdapter(builder *ReplayPackBuilder) *GraphReplayAdapter {
	return &GraphReplayAdapter{builder: builder}
}

// RecordReplayPack implements the decision_graph.ReplayPackRecorder interface.
func (a *GraphReplayAdapter) RecordReplayPack(ctx context.Context,
	decisionID, goalType, selectedMode, selectedPath string,
	confidence float64,
	signals, arbTrace, calInfo, compInfo, cfInfo map[string]any,
) {
	if a.builder == nil {
		return
	}
	rp := ReplayPack{
		DecisionID:         decisionID,
		GoalType:           goalType,
		SelectedMode:       selectedMode,
		SelectedPath:       selectedPath,
		Confidence:         confidence,
		Signals:            signals,
		ArbitrationTrace:   arbTrace,
		CalibrationInfo:    calInfo,
		ComparativeInfo:    compInfo,
		CounterfactualInfo: cfInfo,
	}
	a.builder.RecordReplayPack(ctx, rp)
}
