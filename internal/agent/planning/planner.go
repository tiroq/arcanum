package planning

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
	"github.com/tiroq/arcanum/internal/agent/actions"
	"github.com/tiroq/arcanum/internal/agent/goals"
	"github.com/tiroq/arcanum/internal/audit"
)

// AdaptivePlanner replaces static goal→action mapping with context-aware,
// feedback-informed action selection. It is deterministic: same inputs
// always produce the same outputs.
type AdaptivePlanner struct {
	collector      *ContextCollector
	targetResolver actions.TargetResolver
	auditor        audit.AuditRecorder
	logger         *zap.Logger

	// journal is an optional durable store for planning decisions.
	// When non-nil, decisions are persisted after each planning cycle.
	journal *DecisionJournal

	// lastDecisions holds the most recent planning decisions for API visibility.
	lastDecisions []PlanningDecision
}

// NewAdaptivePlanner creates an AdaptivePlanner.
func NewAdaptivePlanner(collector *ContextCollector, targetResolver actions.TargetResolver, auditor audit.AuditRecorder, logger *zap.Logger) *AdaptivePlanner {
	return &AdaptivePlanner{
		collector:      collector,
		targetResolver: targetResolver,
		auditor:        auditor,
		logger:         logger,
	}
}

// LastDecisions returns the most recent set of planning decisions.
func (ap *AdaptivePlanner) LastDecisions() []PlanningDecision {
	return ap.lastDecisions
}

// WithJournal attaches a DecisionJournal for durable persistence.
func (ap *AdaptivePlanner) WithJournal(j *DecisionJournal) *AdaptivePlanner {
	ap.journal = j
	return ap
}

// PlanActions implements the same signature as actions.Planner.PlanActions
// but uses adaptive scoring instead of a static mapping.
func (ap *AdaptivePlanner) PlanActions(ctx context.Context, goalList []goals.Goal) ([]actions.Action, error) {
	pctx, err := ap.collector.Collect(ctx)
	if err != nil {
		ap.logger.Warn("planning_context_collection_failed", zap.Error(err))
		// Fallback: use empty context so planning can still proceed.
		pctx = PlanningContext{
			RecentActionFeedback: make(map[string]actionmemory.ActionFeedback),
			Timestamp:            time.Now().UTC(),
		}
	}

	var allActions []actions.Action
	var decisions []PlanningDecision

	for _, g := range goalList {
		decision := ap.planForGoal(g, pctx)

		// Audit planning events.
		ap.auditPlanningEvaluated(ctx, decision)
		if decision.SelectedActionType != "noop" {
			ap.auditActionSelected(ctx, decision)
		}

		decisions = append(decisions, decision)

		// Convert the selected candidate into executable Action(s).
		if decision.SelectedActionType == "noop" {
			ap.logger.Info("planning_noop_selected",
				zap.String("goal_id", g.ID),
				zap.String("goal_type", g.Type),
				zap.String("explanation", decision.Explanation),
			)
			continue
		}

		resolved := ap.resolveActions(ctx, g, decision)
		allActions = append(allActions, resolved...)
	}

	ap.lastDecisions = decisions

	// Best-effort persist to durable journal.
	if ap.journal != nil && len(decisions) > 0 {
		cycleID := uuid.New().String()
		if err := ap.journal.Save(ctx, cycleID, decisions); err != nil {
			ap.logger.Warn("planning_journal_persist_failed", zap.Error(err))
		}
	}

	return allActions, nil
}

// planForGoal scores all candidates for a goal and selects the best one.
func (ap *AdaptivePlanner) planForGoal(g goals.Goal, pctx PlanningContext) PlanningDecision {
	candidateTypes := CandidatesForGoal(g.Type)
	now := time.Now().UTC()

	var candidates []PlannedActionCandidate
	for _, at := range candidateTypes {
		raw := PlannedActionCandidate{
			ActionType: at,
			GoalType:   g.Type,
		}
		scored := ScoreCandidate(raw, g.Priority, g.Confidence, pctx)
		candidates = append(candidates, scored)
	}

	// Sort by score descending (deterministic: stable sort).
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	// Select highest non-rejected candidate.
	selectedType := "noop"
	for i := range candidates {
		if candidates[i].Score <= 0 && candidates[i].ActionType != "noop" {
			candidates[i].Rejected = true
			candidates[i].RejectReason = fmt.Sprintf("score %.2f <= 0", candidates[i].Score)
			continue
		}
		if !candidates[i].Rejected {
			selectedType = candidates[i].ActionType
			break
		}
	}

	explanation := buildExplanation(selectedType, candidates, pctx)

	return PlanningDecision{
		GoalID:             g.ID,
		GoalType:           g.Type,
		Candidates:         candidates,
		SelectedActionType: selectedType,
		Explanation:        explanation,
		PlannedAt:          now,
	}
}

// resolveActions converts a planning decision into concrete executable Action(s).
// For action types that need specific targets (retry_job, trigger_resync), it
// delegates to the TargetResolver to find actual job/task IDs.
func (ap *AdaptivePlanner) resolveActions(ctx context.Context, g goals.Goal, d PlanningDecision) []actions.Action {
	switch d.SelectedActionType {
	case "retry_job":
		if ap.targetResolver != nil {
			targets, err := ap.targetResolver.FindRetryTargets(ctx, g)
			if err != nil {
				ap.logger.Warn("resolve_retry_targets_failed", zap.Error(err))
			}
			if len(targets) > 0 {
				// Annotate description with adaptive planning context.
				for i := range targets {
					targets[i].Description = fmt.Sprintf("[adaptive] %s", targets[i].Description)
				}
				return targets
			}
		}
		// Fallback: emit a log_recommendation if no targets found.
		return []actions.Action{ap.makeLogRecommendation(g, d)}

	case "trigger_resync":
		if ap.targetResolver != nil {
			targets, err := ap.targetResolver.FindResyncTargets(ctx, g)
			if err != nil {
				ap.logger.Warn("resolve_resync_targets_failed", zap.Error(err))
			}
			if len(targets) > 0 {
				for i := range targets {
					targets[i].Description = fmt.Sprintf("[adaptive] %s", targets[i].Description)
				}
				return targets
			}
		}
		return []actions.Action{ap.makeLogRecommendation(g, d)}

	case "log_recommendation":
		return []actions.Action{ap.makeLogRecommendation(g, d)}

	default:
		return []actions.Action{ap.makeLogRecommendation(g, d)}
	}
}

// makeLogRecommendation creates a log_recommendation action with planning context.
func (ap *AdaptivePlanner) makeLogRecommendation(g goals.Goal, d PlanningDecision) actions.Action {
	return actions.Action{
		ID:         uuid.New().String(),
		Type:       string(actions.ActionLogRecommendation),
		Priority:   g.Priority,
		Confidence: g.Confidence,
		GoalID:     g.ID,
		Description: fmt.Sprintf(
			"[adaptive] Recommendation for %s: %s",
			g.Type, d.Explanation,
		),
		Params: map[string]any{
			"goal_type":            g.Type,
			"description":          g.Description,
			"selected_action_type": d.SelectedActionType,
			"explanation":          d.Explanation,
		},
		Safe:      true,
		CreatedAt: time.Now().UTC(),
	}
}

// buildExplanation constructs a concise human-readable explanation.
func buildExplanation(selectedType string, candidates []PlannedActionCandidate, pctx PlanningContext) string {
	var parts []string

	// State why the selected action was chosen.
	for _, c := range candidates {
		if c.ActionType == selectedType {
			parts = append(parts, fmt.Sprintf("selected %s (score=%.2f)", c.ActionType, c.Score))
			// Include final reasoning entries.
			for _, r := range c.Reasoning {
				parts = append(parts, r)
			}
			break
		}
	}

	// State why others were penalized or rejected.
	for _, c := range candidates {
		if c.ActionType == selectedType {
			continue
		}
		if c.Rejected {
			parts = append(parts, fmt.Sprintf("rejected %s: %s", c.ActionType, c.RejectReason))
		} else if c.Score < 0.5 {
			for _, r := range c.Reasoning {
				if strings.Contains(r, "penalty") || strings.Contains(r, "avoid") {
					parts = append(parts, fmt.Sprintf("%s: %s", c.ActionType, r))
				}
			}
		}
	}

	// Summarize key context factors.
	if pctx.QueueBacklog > queueBacklogHighThreshold {
		parts = append(parts, fmt.Sprintf("context: high backlog (%d)", pctx.QueueBacklog))
	}
	if pctx.FailureRate > failureRateHighThreshold {
		parts = append(parts, fmt.Sprintf("context: high failure rate (%.2f)", pctx.FailureRate))
	}
	if pctx.AcceptanceRate > 0 && pctx.AcceptanceRate < acceptanceRateLowThreshold {
		parts = append(parts, fmt.Sprintf("context: low acceptance rate (%.2f)", pctx.AcceptanceRate))
	}

	return strings.Join(parts, "; ")
}

// --- Audit helpers ---

func (ap *AdaptivePlanner) auditPlanningEvaluated(ctx context.Context, d PlanningDecision) {
	if ap.auditor == nil {
		return
	}

	candidateSummaries := make([]map[string]any, 0, len(d.Candidates))
	for _, c := range d.Candidates {
		candidateSummaries = append(candidateSummaries, map[string]any{
			"action_type": c.ActionType,
			"score":       c.Score,
			"rejected":    c.Rejected,
		})
	}

	entityID := uuid.New()
	_ = ap.auditor.RecordEvent(ctx, "planning", entityID, "planning.evaluated", "system", "adaptive_planner", map[string]any{
		"goal_id":              d.GoalID,
		"goal_type":            d.GoalType,
		"candidates":           candidateSummaries,
		"selected_action_type": d.SelectedActionType,
		"explanation":          d.Explanation,
	})
}

func (ap *AdaptivePlanner) auditActionSelected(ctx context.Context, d PlanningDecision) {
	if ap.auditor == nil {
		return
	}

	candidateSummaries := make([]map[string]any, 0, len(d.Candidates))
	for _, c := range d.Candidates {
		candidateSummaries = append(candidateSummaries, map[string]any{
			"action_type": c.ActionType,
			"score":       c.Score,
			"rejected":    c.Rejected,
		})
	}

	entityID := uuid.New()
	_ = ap.auditor.RecordEvent(ctx, "planning", entityID, "planning.action_selected", "system", "adaptive_planner", map[string]any{
		"goal_id":              d.GoalID,
		"goal_type":            d.GoalType,
		"selected_action_type": d.SelectedActionType,
		"candidates":           candidateSummaries,
		"explanation":          d.Explanation,
	})
}
