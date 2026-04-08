package decision_graph

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/planning"
	"github.com/tiroq/arcanum/internal/audit"
)

// StabilityProvider reads current stability state without importing stability.
type StabilityProvider interface {
	GetMode(ctx context.Context) string
}

// PathLearningProvider retrieves path and transition feedback for graph scoring.
// Defined here to avoid import cycles — implemented in path_learning package.
type PathLearningProvider interface {
	GetAllPathFeedbackMap(ctx context.Context, goalType string) map[string]string
	GetAllTransitionFeedbackMap(ctx context.Context, goalType string) map[string]string
}

// GraphPlannerAdapter adapts the decision graph layer to the
// planning.StrategyProvider interface, replacing strategy portfolio
// competition with graph-based decision evaluation.
type GraphPlannerAdapter struct {
	stability StabilityProvider
	auditor   audit.AuditRecorder
	logger    *zap.Logger

	// explorationTrigger is a deterministic function that returns true
	// when exploration should override exploitation.
	explorationTrigger func(goalType string) bool

	// pathLearning provides path and transition feedback for scoring adjustments.
	pathLearning PathLearningProvider

	// lastSelection stores the most recent path selection for API visibility.
	lastSelection *PathSelection
}

// NewGraphPlannerAdapter creates a GraphPlannerAdapter.
func NewGraphPlannerAdapter(
	stability StabilityProvider,
	auditor audit.AuditRecorder,
	logger *zap.Logger,
) *GraphPlannerAdapter {
	return &GraphPlannerAdapter{
		stability: stability,
		auditor:   auditor,
		logger:    logger,
	}
}

// WithExplorationTrigger sets a deterministic exploration trigger function.
func (a *GraphPlannerAdapter) WithExplorationTrigger(fn func(goalType string) bool) *GraphPlannerAdapter {
	a.explorationTrigger = fn
	return a
}

// WithPathLearning sets the path learning provider for scoring adjustments.
func (a *GraphPlannerAdapter) WithPathLearning(pl PathLearningProvider) *GraphPlannerAdapter {
	a.pathLearning = pl
	return a
}

// LastSelection returns the most recent path selection for API visibility.
func (a *GraphPlannerAdapter) LastSelection() *PathSelection {
	return a.lastSelection
}

// EvaluateForPlanner implements planning.StrategyProvider.
// Builds a decision graph, evaluates all paths, and selects the best one.
// If the selected path's first action differs from tactical, overrides.
func (a *GraphPlannerAdapter) EvaluateForPlanner(
	ctx context.Context,
	decision planning.PlanningDecision,
	globalFeedback map[string]actionmemory.ActionFeedback,
	strategyLearning map[string]planning.StrategyLearningFeedback,
) planning.StrategyOverride {
	override := planning.StrategyOverride{Applied: false}

	// Determine stability mode.
	stabilityMode := "normal"
	if a.stability != nil {
		stabilityMode = a.stability.GetMode(ctx)
	}

	// Determine exploration toggle.
	shouldExplore := false
	if a.explorationTrigger != nil {
		shouldExplore = a.explorationTrigger(decision.GoalType)
	}

	config := GraphConfig{
		MaxDepth:        3,
		StabilityMode:   stabilityMode,
		ShouldExplore:   shouldExplore,
		LongPathPenalty: 0.15,
	}

	// Build action signals from the tactical decision + feedback.
	candidateActions := make([]string, 0, len(decision.Candidates))
	signals := make(map[string]ActionSignals, len(decision.Candidates))

	for _, c := range decision.Candidates {
		candidateActions = append(candidateActions, c.ActionType)
		sig := ActionSignals{
			ExpectedValue: c.Score,
			Risk:          0.1, // default base risk
			Confidence:    c.Confidence,
		}
		// Enrich from action memory feedback.
		if fb, ok := globalFeedback[c.ActionType]; ok {
			if fb.Recommendation == "avoid_action" {
				sig.Risk = clamp01(sig.Risk + 0.3)
				sig.ExpectedValue = clamp01(sig.ExpectedValue - 0.2)
			} else if fb.Recommendation == "prefer_action" {
				sig.Risk = clamp01(sig.Risk - 0.05)
				sig.ExpectedValue = clamp01(sig.ExpectedValue + 0.1)
			}
		}
		// Enrich from strategy learning.
		if sl, ok := strategyLearning[c.ActionType]; ok {
			if sl.Recommendation == "avoid_strategy" {
				sig.Risk = clamp01(sig.Risk + 0.2)
			} else if sl.Recommendation == "prefer_strategy" {
				sig.ExpectedValue = clamp01(sig.ExpectedValue + 0.05)
			}
		}
		signals[c.ActionType] = sig
	}

	// Build graph.
	input := BuildInput{
		GoalType:         decision.GoalType,
		CandidateActions: candidateActions,
		Signals:          signals,
		Config:           config,
	}
	graph := BuildGraph(input)

	// Enumerate and evaluate paths.
	paths := EnumeratePaths(graph)
	scored := EvaluateAllPaths(paths, config)

	// Apply path/transition learning adjustments (Iteration 21).
	// Fail-open: if pathLearning is nil, scored paths are unchanged.
	var learningSignals *PathLearningSignals
	if a.pathLearning != nil {
		learningSignals = &PathLearningSignals{
			PathFeedback:       a.pathLearning.GetAllPathFeedbackMap(ctx, decision.GoalType),
			TransitionFeedback: a.pathLearning.GetAllTransitionFeedbackMap(ctx, decision.GoalType),
		}
	}
	scored = ApplyPathLearningAdjustments(scored, learningSignals)

	// Select best path.
	selection := SelectBestPath(scored, config)
	a.lastSelection = &selection

	// Audit the graph evaluation.
	a.auditEvent(ctx, "decision_graph.evaluated", map[string]any{
		"goal_id":          decision.GoalID,
		"goal_type":        decision.GoalType,
		"node_count":       len(graph.Nodes),
		"edge_count":       len(graph.Edges),
		"path_count":       len(scored),
		"stability_mode":   stabilityMode,
		"should_explore":   shouldExplore,
		"exploration_used": selection.ExplorationUsed,
		"reason":           selection.Reason,
	})

	if selection.Selected == nil || len(selection.Selected.Nodes) == 0 {
		override.Reason = "no path selected"
		return override
	}

	// Execute only the first node of the selected path.
	firstAction := selection.Selected.Nodes[0].ActionType
	if firstAction == "noop" {
		override.Reason = "graph selected noop"
		return override
	}

	// Only override if the graph's first action differs from tactical.
	if firstAction == decision.SelectedActionType {
		override.Reason = "graph agrees with tactical selection"
		return override
	}

	override.Applied = true
	override.ActionType = firstAction
	override.StrategyID = uuid.New().String()
	override.StrategyType = "decision_graph"
	override.Reason = "graph_path: " + selection.Reason

	// Populate path metadata (Iteration 21) for path learning.
	pathActions := make([]string, len(selection.Selected.Nodes))
	for i, n := range selection.Selected.Nodes {
		pathActions[i] = n.ActionType
	}
	override.PathSignature = pathSignatureFromNodes(selection.Selected.Nodes)
	override.PathActionTypes = pathActions
	override.PathLength = len(selection.Selected.Nodes)

	// Audit the override.
	a.auditEvent(ctx, "decision_graph.override", map[string]any{
		"goal_id":         decision.GoalID,
		"goal_type":       decision.GoalType,
		"tactical_action": decision.SelectedActionType,
		"graph_action":    firstAction,
		"path_length":     len(selection.Selected.Nodes),
		"final_score":     selection.Selected.FinalScore,
		"reason":          override.Reason,
	})

	return override
}

// auditEvent records a decision graph audit event.
func (a *GraphPlannerAdapter) auditEvent(ctx context.Context, eventType string, payload map[string]any) {
	if a.auditor == nil {
		return
	}
	_ = a.auditor.RecordEvent(ctx, "decision_graph", uuid.New(), eventType,
		"system", "decision_graph_engine", payload)
}

// --- Top-level orchestration function ---

// Evaluate is a convenience function that runs the full decision graph pipeline:
// build → enumerate → evaluate → select.
// Returns the PathSelection and the first action type to execute.
func Evaluate(input BuildInput) (PathSelection, string) {
	graph := BuildGraph(input)
	paths := EnumeratePaths(graph)
	scored := EvaluateAllPaths(paths, input.Config)
	selection := SelectBestPath(scored, input.Config)

	actionType := ""
	if selection.Selected != nil && len(selection.Selected.Nodes) > 0 {
		actionType = selection.Selected.Nodes[0].ActionType
	}

	return selection, actionType
}

// Timestamp returns the current time for audit events. Exported for testing.
func Timestamp() time.Time {
	return time.Now().UTC()
}
