package decision_graph

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/arbitration"
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

// ComparativeLearningProvider retrieves comparative feedback for graph scoring.
// Defined here to avoid import cycles — implemented in path_comparison package.
type ComparativeLearningProvider interface {
	GetAllComparativeFeedbackMap(ctx context.Context, goalType string) map[string]string
}

// SnapshotCapturer captures decision snapshots at path selection time.
// Defined here to avoid import cycles — implemented in path_comparison package.
type SnapshotCapturer interface {
	CaptureAndSave(ctx context.Context, decisionID, goalType string, scoredPaths []ScoredPathExport, selectedSignature string, selectedScore float64) error
}

// CalibrationProvider adjusts raw confidence values based on calibration data.
// Defined here to avoid import cycles — implemented in calibration package.
type CalibrationProvider interface {
	CalibrateConfidence(ctx context.Context, rawConfidence float64) float64
}

// ContextualCalibrationProvider adjusts confidence values based on per-context
// historical prediction accuracy. Defined here to avoid import cycles.
// Uses primitive parameters to avoid type coupling across packages.
type ContextualCalibrationProvider interface {
	CalibrateConfidenceForContext(ctx context.Context, rawConfidence float64, goalType, providerName, strategyType string) float64
}

// ModeCalibrationProvider adjusts confidence values based on per-mode
// historical prediction accuracy (Iteration 28). Defined here to avoid import cycles.
type ModeCalibrationProvider interface {
	CalibrateConfidenceForMode(ctx context.Context, rawConfidence float64, mode string) float64
}

// ContextualCalibrationContext carries the context dimensions for calibration lookups.
// Used internally within the planner adapter.
type ContextualCalibrationContext struct {
	GoalType     string
	ProviderName string
	StrategyType string
}

// CounterfactualSimulator runs counterfactual simulation before path selection.
// Defined here to avoid import cycles — implemented in counterfactual package.
type CounterfactualSimulator interface {
	// SimulateAndSave runs simulation for top paths and returns predictions.
	// Returns map[pathSignature] → predicted expected value + confidence.
	// Empty map on failure (fail-open).
	SimulateAndSave(ctx context.Context, decisionID, goalType string, pathScores map[string]float64, pathLengths map[string]int) CounterfactualPredictionExport
}

// CounterfactualPredictionExport carries prediction results from the simulation.
type CounterfactualPredictionExport struct {
	Predictions map[string]float64 // map[pathSignature] → predicted value
	Confidences map[string]float64 // map[pathSignature] → confidence
}

// ScoredPathExport carries a path signature and score for snapshot capture.
type ScoredPathExport struct {
	PathSignature string
	Score         float64
}

// MetaReasoningProvider selects a reasoning mode before graph evaluation.
// Defined here to avoid import cycles — implemented in meta_reasoning package.
type MetaReasoningProvider interface {
	// SelectMode chooses a reasoning mode based on current signals.
	// Returns: mode string, confidence float64, reason string.
	SelectMode(ctx context.Context, goalType string, failureRate, confidence, risk float64, stabilityMode string, missedWinCount int, noopRate, lowValueRate float64) (mode string, conf float64, reason string)
	// RecordOutcome updates mode memory after outcome evaluation.
	RecordOutcome(ctx context.Context, mode string, goalType string, success bool)
}

// GovernanceProvider reads governance state for runtime enforcement.
// Defined here to avoid import cycles — implemented in governance package.
type GovernanceProvider interface {
	IsLearningBlocked(ctx context.Context) bool
	IsPolicyBlocked(ctx context.Context) bool
	IsExplorationBlocked(ctx context.Context) bool
	EffectiveReasoningMode(ctx context.Context) string
	RequiresHumanReview(ctx context.Context) bool
	GetMode(ctx context.Context) string
}

// ReplayPackRecorder records decision replay packs.
// Defined here to avoid import cycles — implemented in governance package.
// Uses primitive parameters to avoid type coupling.
type ReplayPackRecorder interface {
	RecordReplayPack(ctx context.Context,
		decisionID, goalType, selectedMode, selectedPath string,
		confidence float64,
		signals, arbTrace, calInfo, compInfo, cfInfo map[string]any,
	)
}

// ResourceOptimizationProvider provides resource-aware signals for mode selection
// and path scoring. Defined here to avoid import cycles — implemented in
// resource_optimization package.
type ResourceOptimizationProvider interface {
	// GetModeAdjustment returns a bounded adjustment for mode scoring.
	// Positive = prefer, negative = penalize. Returns 0 if no data (fail-open).
	GetModeAdjustment(ctx context.Context, mode, goalType string, confidence, successRate float64, stabilityMode string) float64
	// GetPathPenalty returns a bounded resource penalty for path scoring.
	// Returns 0 if no data or single-step path (fail-open).
	GetPathPenalty(ctx context.Context, mode, goalType string, pathLength int, stabilityMode string) float64
	// RecordOutcome records resource metrics after a decision. Fail-open.
	RecordOutcome(ctx context.Context, mode, goalType string, latencyMs, reasoningDepth, pathLength, tokenCost, executionCost float64)
}

// ProviderRoutingProvider selects the best provider+model for a task (Iteration 32).
// Defined here to avoid import cycles — implemented in provider_routing package.
type ProviderRoutingProvider interface {
	RouteForTask(ctx context.Context, goalType, taskType, preferredRole string,
		estimatedTokens, latencyBudgetMs int, confidenceRequired float64,
		allowExternal bool) (selected string, selectedModel string, fallbackChain []string, reason string)
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

	// comparativeLearning provides comparative feedback for scoring adjustments.
	comparativeLearning ComparativeLearningProvider

	// snapshotCapturer captures decision snapshots at selection time.
	snapshotCapturer SnapshotCapturer

	// counterfactual runs counterfactual simulation before path selection.
	counterfactual CounterfactualSimulator

	// metaReasoning selects a reasoning mode before graph evaluation (Iteration 24).
	metaReasoning MetaReasoningProvider

	// calibration adjusts confidence values based on historical accuracy (Iteration 25).
	calibration CalibrationProvider

	// contextCalibration adjusts confidence based on per-context accuracy (Iteration 26).
	contextCalibration ContextualCalibrationProvider

	// modeCalibration adjusts confidence based on per-mode accuracy (Iteration 28).
	modeCalibration ModeCalibrationProvider

	// resourceOptimization provides cost/latency-aware signals (Iteration 29).
	resourceOptimization ResourceOptimizationProvider

	// governance provides runtime enforcement of human overrides (Iteration 30).
	governance GovernanceProvider

	// replayRecorder records decision replay packs for post-hoc review (Iteration 30).
	replayRecorder ReplayPackRecorder

	// providerRouting selects provider+model for task execution (Iteration 32).
	providerRouting ProviderRoutingProvider

	// lastSelection stores the most recent path selection for API visibility.
	lastSelection *PathSelection

	// lastArbTraces stores the most recent arbitration traces for API visibility (Iteration 27).
	lastArbTraces []arbitration.ArbitrationTrace
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

// WithComparativeLearning sets the comparative learning provider for scoring adjustments.
func (a *GraphPlannerAdapter) WithComparativeLearning(cl ComparativeLearningProvider) *GraphPlannerAdapter {
	a.comparativeLearning = cl
	return a
}

// WithSnapshotCapturer sets the snapshot capturer for decision snapshot capture.
func (a *GraphPlannerAdapter) WithSnapshotCapturer(sc SnapshotCapturer) *GraphPlannerAdapter {
	a.snapshotCapturer = sc
	return a
}

// WithCounterfactual sets the counterfactual simulator for prediction-guided selection.
func (a *GraphPlannerAdapter) WithCounterfactual(cf CounterfactualSimulator) *GraphPlannerAdapter {
	a.counterfactual = cf
	return a
}

// WithMetaReasoning sets the meta-reasoning provider for mode selection (Iteration 24).
func (a *GraphPlannerAdapter) WithMetaReasoning(mr MetaReasoningProvider) *GraphPlannerAdapter {
	a.metaReasoning = mr
	return a
}

// WithCalibration sets the calibration provider for confidence adjustment (Iteration 25).
func (a *GraphPlannerAdapter) WithCalibration(cp CalibrationProvider) *GraphPlannerAdapter {
	a.calibration = cp
	return a
}

// WithContextualCalibration sets the contextual calibration provider for
// per-context confidence adjustment (Iteration 26).
func (a *GraphPlannerAdapter) WithContextualCalibration(cp ContextualCalibrationProvider) *GraphPlannerAdapter {
	a.contextCalibration = cp
	return a
}

// WithResourceOptimization sets the resource optimization provider for
// cost/latency-aware mode selection and path scoring (Iteration 29).
func (a *GraphPlannerAdapter) WithResourceOptimization(ro ResourceOptimizationProvider) *GraphPlannerAdapter {
	a.resourceOptimization = ro
	return a
}

// WithModeCalibration sets the mode-specific calibration provider for
// per-mode confidence adjustment (Iteration 28).
func (a *GraphPlannerAdapter) WithModeCalibration(mp ModeCalibrationProvider) *GraphPlannerAdapter {
	a.modeCalibration = mp
	return a
}

// WithGovernance sets the governance provider for runtime enforcement (Iteration 30).
func (a *GraphPlannerAdapter) WithGovernance(gp GovernanceProvider) *GraphPlannerAdapter {
	a.governance = gp
	return a
}

// WithReplayRecorder sets the replay pack recorder for decision explanation support (Iteration 30).
func (a *GraphPlannerAdapter) WithReplayRecorder(rr ReplayPackRecorder) *GraphPlannerAdapter {
	a.replayRecorder = rr
	return a
}

// WithProviderRouting sets the provider routing provider for task-level
// provider+model selection (Iteration 32).
func (a *GraphPlannerAdapter) WithProviderRouting(pr ProviderRoutingProvider) *GraphPlannerAdapter {
	a.providerRouting = pr
	return a
}

// LastSelection returns the most recent path selection for API visibility.
func (a *GraphPlannerAdapter) LastSelection() *PathSelection {
	return a.lastSelection
}

// LastArbTraces returns the most recent arbitration traces for API visibility (Iteration 27).
func (a *GraphPlannerAdapter) LastArbTraces() []arbitration.ArbitrationTrace {
	return a.lastArbTraces
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

	// Start timing for resource tracking (Iteration 29).
	decisionStartTime := time.Now()

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

	// --- Governance enforcement: exploration (Iteration 30) ---
	// If governance blocks exploration, override the trigger result.
	if a.governance != nil && a.governance.IsExplorationBlocked(ctx) {
		shouldExplore = false
		a.logger.Info("governance_exploration_blocked",
			zap.String("goal_type", decision.GoalType),
			zap.String("governance_mode", a.governance.GetMode(ctx)),
		)
	}

	// --- Meta-reasoning mode selection (Iteration 24) ---
	// Selects HOW to reason before choosing WHAT to do.
	// Fail-open: defaults to graph mode if meta-reasoning is nil or fails.
	metaMode := "graph"
	if a.metaReasoning != nil {
		// Build meta-reasoning input from current signals.
		avgConfidence, avgRisk, failureRate := computeSignalAverages(decision.Candidates, globalFeedback)
		missedWinCount := 0
		if a.comparativeLearning != nil {
			fbMap := a.comparativeLearning.GetAllComparativeFeedbackMap(ctx, decision.GoalType)
			for _, rec := range fbMap {
				if rec == "underexplored_path" {
					missedWinCount++
				}
			}
		}
		noopRate, lowValueRate := computeStagnationSignals(decision.Candidates)

		// Apply calibration to meta-reasoning confidence input (Iteration 25).
		// If system is overconfident, reduces confidence → less likely to use direct mode.
		// If underconfident, increases confidence → allows more aggressive strategies.
		// Fail-open: if calibration is nil, avgConfidence is unchanged.
		if a.calibration != nil {
			avgConfidence = a.calibration.CalibrateConfidence(ctx, avgConfidence)
		}

		metaMode, _, _ = a.metaReasoning.SelectMode(ctx,
			decision.GoalType, failureRate, avgConfidence, avgRisk,
			stabilityMode, missedWinCount, noopRate, lowValueRate)
	}

	// Emit mode-specific calibration audit event (Iteration 28).
	// Applied after mode selection — uses mode calibration to adjust node confidence,
	// and logs the mode calibration applied event for observability.
	if a.modeCalibration != nil && a.auditor != nil {
		// Log calibration.mode_applied for a representative confidence (0.5 midpoint)
		// to audit the mode-specific correction being applied.
		testConfidence := 0.5
		adjusted := a.modeCalibration.CalibrateConfidenceForMode(ctx, testConfidence, metaMode)
		if adjusted != testConfidence {
			a.auditEvent(ctx, "calibration.mode_applied", map[string]any{
				"mode":                metaMode,
				"original_confidence": testConfidence,
				"adjusted_confidence": adjusted,
				"goal_type":           decision.GoalType,
			})
		}
	}

	// --- Governance enforcement: reasoning mode (Iteration 30) ---
	// Human-forced reasoning mode dominates autonomous meta-reasoning.
	if a.governance != nil {
		forcedMode := a.governance.EffectiveReasoningMode(ctx)
		if forcedMode != "" && forcedMode != metaMode {
			a.logger.Info("governance_mode_override",
				zap.String("original_mode", metaMode),
				zap.String("forced_mode", forcedMode),
				zap.String("governance_mode", a.governance.GetMode(ctx)),
			)
			a.auditEvent(ctx, "governance.override_applied", map[string]any{
				"goal_type":       decision.GoalType,
				"original_mode":   metaMode,
				"forced_mode":     forcedMode,
				"override_type":   "reasoning_mode",
				"governance_mode": a.governance.GetMode(ctx),
			})
			metaMode = forcedMode
		}
	}

	// Apply mode-specific configuration.
	config := GraphConfig{
		MaxDepth:        3,
		StabilityMode:   stabilityMode,
		ShouldExplore:   shouldExplore,
		LongPathPenalty: 0.15,
	}

	switch metaMode {
	case "direct":
		// Fast path: depth-1 only, skip graph expansion.
		config.MaxDepth = 1
	case "conservative":
		// Restrict to safe actions only (noop, log_recommendation).
		config.MaxDepth = 1
		config.StabilityMode = "safe_mode"
	case "exploratory":
		// Force exploration: always select second-best path.
		config.ShouldExplore = true
	default:
		// "graph" — full decision graph evaluation (default).
	}

	// Build action signals from the tactical decision + feedback.
	candidateActions := make([]string, 0, len(decision.Candidates))
	signals := make(map[string]ActionSignals, len(decision.Candidates))

	for _, c := range decision.Candidates {
		// Conservative mode: filter to safe actions only.
		if metaMode == "conservative" && !isSafeAction(c.ActionType) {
			continue
		}
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

	// Apply calibration to node confidence values (Iteration 25).
	// Adjusts raw confidence based on historical accuracy tracking.
	// Fail-open: if calibration is nil, confidence values are unchanged.
	if a.calibration != nil {
		for action, sig := range signals {
			sig.Confidence = a.calibration.CalibrateConfidence(ctx, sig.Confidence)
			signals[action] = sig
		}
	}

	// Apply contextual calibration to node confidence values (Iteration 26).
	// Adjusts confidence based on per-context historical prediction accuracy.
	// Fail-open: if contextCalibration is nil, confidence values are unchanged.
	if a.contextCalibration != nil {
		for action, sig := range signals {
			sig.Confidence = a.contextCalibration.CalibrateConfidenceForContext(ctx, sig.Confidence, decision.GoalType, "", "")
			signals[action] = sig
		}
	}

	// Apply mode-specific calibration to node confidence values (Iteration 28).
	// Adjusts confidence based on per-mode historical prediction accuracy.
	// Applied after contextual calibration in the confidence pipeline:
	//   raw → global → contextual → mode-specific → final
	// Fail-open: if modeCalibration is nil, confidence values are unchanged.
	if a.modeCalibration != nil {
		for action, sig := range signals {
			sig.Confidence = a.modeCalibration.CalibrateConfidenceForMode(ctx, sig.Confidence, metaMode)
			signals[action] = sig
		}
	}

	// Ensure at least noop if all candidates were filtered.
	if len(candidateActions) == 0 {
		candidateActions = []string{"noop"}
		signals["noop"] = ActionSignals{
			ExpectedValue: 0.01,
			Risk:          0.0,
			Confidence:    1.0,
		}
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

	// --- Signal Arbitration (Iteration 27) ---
	// Collect all learning signals and resolve via unified arbitration layer.
	// Replaces sequential Apply{Path,Comparative,Counterfactual}Adjustments.
	// Fail-open: if all providers are nil, scored paths are unchanged.

	var learningSignals *PathLearningSignals
	if a.pathLearning != nil {
		learningSignals = &PathLearningSignals{
			PathFeedback:       a.pathLearning.GetAllPathFeedbackMap(ctx, decision.GoalType),
			TransitionFeedback: a.pathLearning.GetAllTransitionFeedbackMap(ctx, decision.GoalType),
		}
	}

	var comparativeSignals *ComparativeLearningSignals
	if a.comparativeLearning != nil {
		comparativeSignals = &ComparativeLearningSignals{
			ComparativeFeedback: a.comparativeLearning.GetAllComparativeFeedbackMap(ctx, decision.GoalType),
		}
	}

	var cfPredictions *CounterfactualPredictions
	if a.counterfactual != nil {
		pathScores := make(map[string]float64, len(scored))
		pathLengths := make(map[string]int, len(scored))
		for _, sp := range scored {
			sig := pathSignatureFromNodes(sp.Nodes)
			pathScores[sig] = sp.FinalScore
			pathLengths[sig] = len(sp.Nodes)
		}
		provDecisionID := uuid.New().String()
		cfExport := a.counterfactual.SimulateAndSave(ctx, provDecisionID, decision.GoalType, pathScores, pathLengths)
		if len(cfExport.Predictions) > 0 {
			cfPredictions = &CounterfactualPredictions{
				Predictions: cfExport.Predictions,
				Confidences: cfExport.Confidences,
			}
			if a.calibration != nil {
				scaledConf := make(map[string]float64, len(cfPredictions.Confidences))
				for sig, conf := range cfPredictions.Confidences {
					scaledConf[sig] = a.calibration.CalibrateConfidence(ctx, conf)
				}
				cfPredictions.Confidences = scaledConf
			}
		}
	}

	// Compute calibrated confidence for arbitration suppression threshold.
	calibratedConfidence := 0.8 // default: high confidence (no suppression)
	if a.calibration != nil {
		calibratedConfidence = a.calibration.CalibrateConfidence(ctx, 0.8)
	}

	arbSignals := &ArbitratedSignals{
		PathLearning:         learningSignals,
		ComparativeLearning:  comparativeSignals,
		Counterfactual:       cfPredictions,
		StabilityMode:        stabilityMode,
		CalibratedConfidence: calibratedConfidence,
		ExplorationActive:    shouldExplore,
	}

	var arbTraces []arbitration.ArbitrationTrace
	scored, arbTraces = ApplyArbitratedAdjustments(scored, arbSignals)
	a.lastArbTraces = arbTraces

	// Emit arbitration audit events.
	a.emitArbitrationAudit(ctx, decision.GoalID, decision.GoalType, arbTraces)

	// --- Resource-aware path penalty (Iteration 29) ---
	// Apply a bounded penalty to longer/more expensive paths based on
	// historical resource profiles. Fail-open: if provider is nil, paths unchanged.
	if a.resourceOptimization != nil {
		for i, p := range scored {
			penalty := a.resourceOptimization.GetPathPenalty(ctx, metaMode, decision.GoalType, len(p.Nodes), stabilityMode)
			if penalty > 0 {
				scored[i].FinalScore = clamp01(p.FinalScore - penalty)
			}
		}
	}

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
		"meta_mode":        metaMode,
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
	override.DecisionID = override.StrategyID
	override.MetaMode = metaMode
	override.PredictedConfidence = selection.Selected.TotalConfidence

	// --- Provider+model routing (Iteration 32) ---
	// Select the best provider+model for this task. Fail-open: if routing
	// is unavailable, provider/model fields remain empty.
	if a.providerRouting != nil {
		selectedProvider, selectedModel, _, routeReason := a.providerRouting.RouteForTask(
			ctx, decision.GoalType, decision.GoalType, "", 0, 0, 0, true)
		override.SelectedProvider = selectedProvider
		override.SelectedModel = selectedModel
		if selectedProvider != "" {
			a.auditEvent(ctx, "provider.target_selected", map[string]any{
				"goal_type":         decision.GoalType,
				"selected_provider": selectedProvider,
				"selected_model":    selectedModel,
				"reason":            routeReason,
			})
		}
	}

	// Populate path metadata (Iteration 21) for path learning.
	pathActions := make([]string, len(selection.Selected.Nodes))
	for i, n := range selection.Selected.Nodes {
		pathActions[i] = n.ActionType
	}
	override.PathSignature = pathSignatureFromNodes(selection.Selected.Nodes)
	override.PathActionTypes = pathActions
	override.PathLength = len(selection.Selected.Nodes)

	// Capture decision snapshot (Iteration 22).
	// Best-effort: failures are logged but do not block selection.
	if a.snapshotCapturer != nil {
		exportPaths := make([]ScoredPathExport, len(scored))
		for i, sp := range scored {
			exportPaths[i] = ScoredPathExport{
				PathSignature: pathSignatureFromNodes(sp.Nodes),
				Score:         sp.FinalScore,
			}
		}
		if err := a.snapshotCapturer.CaptureAndSave(ctx, override.DecisionID, decision.GoalType, exportPaths, override.PathSignature, selection.Selected.FinalScore); err != nil {
			a.logger.Warn("snapshot_capture_failed",
				zap.String("decision_id", override.DecisionID),
				zap.Error(err),
			)
		}
	}

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

	// --- Record resource metrics (Iteration 29) ---
	// Best-effort: failures are logged but do not block the decision.
	if a.resourceOptimization != nil {
		latencyMs := float64(time.Since(decisionStartTime).Milliseconds())
		reasoningDepth := float64(config.MaxDepth)
		pathLength := float64(len(selection.Selected.Nodes))
		a.resourceOptimization.RecordOutcome(ctx, metaMode, decision.GoalType,
			latencyMs, reasoningDepth, pathLength, 0, 0)
	}

	// --- Record replay pack (Iteration 30) ---
	// Persists a decision explanation pack for post-hoc review.
	// Best-effort: failures are logged but do not block the decision.
	if a.replayRecorder != nil {
		arbTraceMap := map[string]any{}
		if len(arbTraces) > 0 {
			arbTraceEntries := make([]map[string]any, len(arbTraces))
			for i, tr := range arbTraces {
				arbTraceEntries[i] = map[string]any{
					"path_signature":   tr.PathSignature,
					"final_adjustment": tr.FinalAdjustment,
					"rules_applied":    tr.RulesApplied,
					"reason":           tr.Reason,
				}
			}
			arbTraceMap["traces"] = arbTraceEntries
		}

		a.replayRecorder.RecordReplayPack(ctx,
			override.DecisionID, decision.GoalType, metaMode,
			override.PathSignature, selection.Selected.TotalConfidence,
			map[string]any{"stability_mode": stabilityMode, "should_explore": shouldExplore},
			arbTraceMap, nil, nil, nil,
		)
	}

	// --- Governance: human review enforcement (Iteration 30) ---
	// If human review is required, annotate the override and emit audit event.
	// The override is still returned, but marked as requiring review.
	if a.governance != nil && a.governance.RequiresHumanReview(ctx) {
		override.Reason = "governance_review_required: " + override.Reason
		a.auditEvent(ctx, "governance.review_required", map[string]any{
			"goal_id":        decision.GoalID,
			"goal_type":      decision.GoalType,
			"decision_id":    override.DecisionID,
			"graph_action":   firstAction,
			"path_signature": override.PathSignature,
			"meta_mode":      metaMode,
		})
	}

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

// emitArbitrationAudit emits audit events based on arbitration trace results (Iteration 27).
func (a *GraphPlannerAdapter) emitArbitrationAudit(ctx context.Context, goalID, goalType string, traces []arbitration.ArbitrationTrace) {
	if a.auditor == nil || len(traces) == 0 {
		return
	}

	for _, tr := range traces {
		// Emit override events for suppressed signals.
		if len(tr.SuppressedSignals) > 0 {
			suppressedNames := make([]string, len(tr.SuppressedSignals))
			for i, s := range tr.SuppressedSignals {
				suppressedNames[i] = s.Signal.Type.String()
			}
			a.auditEvent(ctx, "arbitration.override_applied", map[string]any{
				"goal_id":        goalID,
				"goal_type":      goalType,
				"path_signature": tr.PathSignature,
				"suppressed":     suppressedNames,
				"reason":         tr.Reason,
			})
		}

		// Emit conflict detection events.
		for _, rule := range tr.RulesApplied {
			if rule == "conflict_neutralization" {
				a.auditEvent(ctx, "arbitration.conflict_detected", map[string]any{
					"goal_id":          goalID,
					"goal_type":        goalType,
					"path_signature":   tr.PathSignature,
					"final_adjustment": tr.FinalAdjustment,
					"reason":           tr.Reason,
				})
			}
		}

		// Emit resolved event.
		a.auditEvent(ctx, "arbitration.resolved", map[string]any{
			"goal_id":          goalID,
			"goal_type":        goalType,
			"path_signature":   tr.PathSignature,
			"final_adjustment": tr.FinalAdjustment,
			"rules_applied":    tr.RulesApplied,
			"reason":           tr.Reason,
		})
	}
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

// --- Meta-reasoning helpers (Iteration 24) ---

// isSafeAction returns true for actions allowed in conservative mode.
func isSafeAction(actionType string) bool {
	return actionType == "noop" || actionType == "log_recommendation"
}

// computeSignalAverages computes weighted averages of confidence/risk and failure rate
// from the candidates and their feedback.
func computeSignalAverages(candidates []planning.PlannedActionCandidate, feedback map[string]actionmemory.ActionFeedback) (avgConfidence, avgRisk, failureRate float64) {
	if len(candidates) == 0 {
		return 0.5, 0.1, 0
	}
	totalConf := 0.0
	totalRisk := 0.0
	totalFailureRate := 0.0
	failureCount := 0
	for _, c := range candidates {
		totalConf += c.Confidence
		totalRisk += 0.1 // default base risk
		if fb, ok := feedback[c.ActionType]; ok {
			totalFailureRate += fb.FailureRate
			failureCount++
		}
	}
	n := float64(len(candidates))
	avgConfidence = totalConf / n
	avgRisk = totalRisk / n
	if failureCount > 0 {
		failureRate = totalFailureRate / float64(failureCount)
	}
	return
}

// computeStagnationSignals extracts noop and low-value rates from candidates.
func computeStagnationSignals(candidates []planning.PlannedActionCandidate) (noopRate, lowValueRate float64) {
	if len(candidates) == 0 {
		return 0, 0
	}
	noopCount := 0
	lowValueCount := 0
	for _, c := range candidates {
		if c.ActionType == "noop" {
			noopCount++
		}
		if c.Score < 0.2 {
			lowValueCount++
		}
	}
	n := float64(len(candidates))
	noopRate = float64(noopCount) / n
	lowValueRate = float64(lowValueCount) / n
	return
}
