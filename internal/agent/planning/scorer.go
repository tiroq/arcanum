package planning

import (
	"fmt"

	"github.com/tiroq/arcanum/internal/agent/actionmemory"
)

// --- Scoring constants (all explicit, no magic numbers) ---

const (
	// baseScoreDefault is the starting score for every candidate.
	baseScoreDefault = 0.5

	// feedbackAvoidPenalty is subtracted when historical recommendation is avoid_action.
	feedbackAvoidPenalty = 0.40

	// feedbackPreferBoost is added when historical recommendation is prefer_action.
	feedbackPreferBoost = 0.25

	// highBacklogResyncPenalty penalizes trigger_resync when queue backlog is high.
	highBacklogResyncPenalty = 0.30

	// highBacklogRetryPenalty penalizes retry_job slightly when backlog is high.
	highBacklogRetryPenalty = 0.10

	// highRetryBoost boosts retry_job when retry_scheduled is high and feedback is healthy.
	highRetryBoost = 0.15

	// highFailureRateBadHistoryPenalty penalizes actions with bad feedback when system failure rate is high.
	highFailureRateBadHistoryPenalty = 0.20

	// highFailureRateRecommendBoost boosts log_recommendation when failure rate is high.
	highFailureRateRecommendBoost = 0.15

	// lowAcceptanceAdvisoryBoost boosts advisory/non-destructive actions when acceptance is very low.
	lowAcceptanceAdvisoryBoost = 0.10

	// lowAcceptanceDestructivePenalty penalizes destructive actions when acceptance is very low.
	lowAcceptanceDestructivePenalty = 0.10

	// safetyPreferenceBoost gives a small advantage to lower-risk actions (noop, log_recommendation).
	safetyPreferenceBoost = 0.05

	// noopBasePenalty ensures noop scores below other healthy candidates.
	noopBasePenalty = 0.20

	// queueBacklogHighThreshold is the backlog count above which context penalties apply.
	queueBacklogHighThreshold = 50

	// retryScheduledHighThreshold is the retry count above which context boosts apply.
	retryScheduledHighThreshold = 10

	// failureRateHighThreshold is the system failure rate above which context rules apply.
	failureRateHighThreshold = 0.20

	// acceptanceRateLowThreshold is the acceptance rate below which context rules apply.
	acceptanceRateLowThreshold = 0.40
)

// ScoringParams holds all tunable scoring parameters.
// When populated from the policy store, the planner uses dynamic values.
type ScoringParams struct {
	FeedbackAvoidPenalty     float64
	FeedbackPreferBoost      float64
	HighBacklogResyncPenalty float64
	HighRetryBoost           float64
	SafetyPreferenceBoost    float64
	NoopBasePenalty          float64
}

// DefaultScoringParams returns ScoringParams from the hardcoded defaults.
func DefaultScoringParams() ScoringParams {
	return ScoringParams{
		FeedbackAvoidPenalty:     feedbackAvoidPenalty,
		FeedbackPreferBoost:      feedbackPreferBoost,
		HighBacklogResyncPenalty: highBacklogResyncPenalty,
		HighRetryBoost:           highRetryBoost,
		SafetyPreferenceBoost:    safetyPreferenceBoost,
		NoopBasePenalty:          noopBasePenalty,
	}
}

// ScoreCandidate computes the score for a single action candidate given
// the goal context and planning context. Uses default constant values.
// It returns the updated candidate with Score, Confidence, and Reasoning populated.
func ScoreCandidate(candidate PlannedActionCandidate, goalPriority, goalConfidence float64, pctx PlanningContext) PlannedActionCandidate {
	return ScoreCandidateWithParams(candidate, goalPriority, goalConfidence, pctx, DefaultScoringParams())
}

// ScoreCandidateWithParams computes score using the provided ScoringParams.
// This is the primary scoring function; ScoreCandidate is a convenience wrapper.
func ScoreCandidateWithParams(candidate PlannedActionCandidate, goalPriority, goalConfidence float64, pctx PlanningContext, params ScoringParams) PlannedActionCandidate {
	c := candidate
	c.Reasoning = nil

	// Start from base score scaled by goal priority.
	c.Score = baseScoreDefault + (goalPriority * 0.3)
	c.Confidence = goalConfidence
	c.Reasoning = append(c.Reasoning, fmt.Sprintf("base=%.2f (default %.2f + goal_priority %.2f * 0.3)", c.Score, baseScoreDefault, goalPriority))

	// noop always gets a penalty so it only wins when everything else is worse.
	if c.ActionType == "noop" {
		c.Score -= params.NoopBasePenalty
		c.Reasoning = append(c.Reasoning, fmt.Sprintf("noop penalty: -%.2f", params.NoopBasePenalty))
		return c
	}

	// --- Historical feedback adjustments ---
	c = applyFeedbackRulesP(c, pctx, params)

	// --- System context adjustments ---
	c = applyContextRulesP(c, pctx, params)

	// --- Safety preference ---
	if c.ActionType == "log_recommendation" {
		c.Score += params.SafetyPreferenceBoost
		c.Reasoning = append(c.Reasoning, fmt.Sprintf("safety preference (advisory): +%.2f", params.SafetyPreferenceBoost))
	}

	return c
}

// applyFeedbackRules adjusts score based on historical action feedback (uses defaults).
func applyFeedbackRules(c PlannedActionCandidate, pctx PlanningContext) PlannedActionCandidate {
	return applyFeedbackRulesP(c, pctx, DefaultScoringParams())
}

// applyFeedbackRulesP adjusts score using provided params.
// Resolution order: provider-context → contextual → global (fallback chain).
// When provider-context data is available, it blends with the next-best signal.
func applyFeedbackRulesP(c PlannedActionCandidate, pctx PlanningContext, params ScoringParams) PlannedActionCandidate {
	// Resolve provider-context feedback if data exists.
	var provFb *actionmemory.ContextualFeedback
	if len(pctx.ProviderContextRecords) > 0 && pctx.ProviderName != "" {
		provFb = actionmemory.ResolveProviderContextFeedback(
			pctx.ProviderContextRecords, c.ActionType, c.GoalType,
			pctx.ProviderName, pctx.ModelRole,
			pctx.FailureBucket, pctx.BacklogBucket,
		)
	}

	// Resolve contextual feedback if data exists.
	var ctxFb *actionmemory.ContextualFeedback
	if len(pctx.ContextRecords) > 0 {
		ctxFb = actionmemory.ResolveContextualFeedback(
			pctx.ContextRecords, c.ActionType, c.GoalType,
			pctx.FailureBucket, pctx.BacklogBucket,
		)
	}

	globalFb, hasGlobal := pctx.RecentActionFeedback[c.ActionType]

	// When provider-context data is available, blend with fallback (contextual or global).
	if provFb != nil {
		// Compute the fallback adjustment (contextual → global).
		fallbackAdj := 0.0
		fallbackReason := ""
		if ctxFb != nil {
			var globalPtr *actionmemory.ActionFeedback
			if hasGlobal {
				globalPtr = &globalFb
			}
			fallbackAdj, fallbackReason = actionmemory.BlendFeedbackAdjustment(
				ctxFb, globalPtr, params.FeedbackAvoidPenalty, params.FeedbackPreferBoost,
			)
		} else if hasGlobal {
			fallbackAdj = actionmemory.FeedbackAdjustmentValue(globalFb.Recommendation, params.FeedbackAvoidPenalty, params.FeedbackPreferBoost)
			fallbackReason = fmt.Sprintf("global %s: %.2f", globalFb.Recommendation, fallbackAdj)
		}

		adj, reason := actionmemory.BlendProviderFeedbackAdjustment(
			provFb, fallbackAdj, fallbackReason,
			params.FeedbackAvoidPenalty, params.FeedbackPreferBoost,
		)
		if adj != 0 {
			c.Score += adj
		}
		if reason != "" {
			c.Reasoning = append(c.Reasoning, reason)
		} else {
			c.Reasoning = append(c.Reasoning, "provider-context + fallback feedback: no bias applied")
		}
		return c
	}

	// No provider-context: fall back to contextual → global (existing behavior).
	// When contextual data is available, use blended adjustment.
	if ctxFb != nil {
		var globalPtr *actionmemory.ActionFeedback
		if hasGlobal {
			globalPtr = &globalFb
		}
		adj, reason := actionmemory.BlendFeedbackAdjustment(
			ctxFb, globalPtr, params.FeedbackAvoidPenalty, params.FeedbackPreferBoost,
		)
		if adj != 0 {
			c.Score += adj
		}
		if reason != "" {
			c.Reasoning = append(c.Reasoning, reason)
		} else {
			c.Reasoning = append(c.Reasoning, "contextual + global feedback: no bias applied")
		}
		return c
	}

	// Fall back to global-only feedback (original behavior, unchanged).
	if !hasGlobal {
		c.Reasoning = append(c.Reasoning, "no historical feedback available")
		return c
	}

	switch globalFb.Recommendation {
	case actionmemory.RecommendAvoidAction:
		c.Score -= params.FeedbackAvoidPenalty
		c.Reasoning = append(c.Reasoning, fmt.Sprintf(
			"feedback avoid_action: -%.2f (success=%.2f, failure=%.2f, n=%d)",
			params.FeedbackAvoidPenalty, globalFb.SuccessRate, globalFb.FailureRate, globalFb.SampleSize,
		))
	case actionmemory.RecommendPreferAction:
		c.Score += params.FeedbackPreferBoost
		c.Reasoning = append(c.Reasoning, fmt.Sprintf(
			"feedback prefer_action: +%.2f (success=%.2f, failure=%.2f, n=%d)",
			params.FeedbackPreferBoost, globalFb.SuccessRate, globalFb.FailureRate, globalFb.SampleSize,
		))
	case actionmemory.RecommendInsufficientData:
		c.Reasoning = append(c.Reasoning, fmt.Sprintf(
			"feedback insufficient_data (n=%d): no bias applied",
			globalFb.SampleSize,
		))
	case actionmemory.RecommendNeutral:
		c.Reasoning = append(c.Reasoning, fmt.Sprintf(
			"feedback neutral (success=%.2f, failure=%.2f, n=%d): no bias applied",
			globalFb.SuccessRate, globalFb.FailureRate, globalFb.SampleSize,
		))
	}

	return c
}

// applyContextRules adjusts score based on current system conditions (uses defaults).
func applyContextRules(c PlannedActionCandidate, pctx PlanningContext) PlannedActionCandidate {
	return applyContextRulesP(c, pctx, DefaultScoringParams())
}

// applyContextRulesP adjusts score using provided params.
func applyContextRulesP(c PlannedActionCandidate, pctx PlanningContext, params ScoringParams) PlannedActionCandidate {
	// High queue backlog penalties.
	if pctx.QueueBacklog > queueBacklogHighThreshold {
		if c.ActionType == "trigger_resync" {
			c.Score -= params.HighBacklogResyncPenalty
			c.Reasoning = append(c.Reasoning, fmt.Sprintf(
				"high backlog (%d > %d): trigger_resync penalty -%.2f",
				pctx.QueueBacklog, queueBacklogHighThreshold, params.HighBacklogResyncPenalty,
			))
		}
		if c.ActionType == "retry_job" {
			c.Score -= highBacklogRetryPenalty
			c.Reasoning = append(c.Reasoning, fmt.Sprintf(
				"high backlog (%d > %d): retry_job minor penalty -%.2f",
				pctx.QueueBacklog, queueBacklogHighThreshold, highBacklogRetryPenalty,
			))
		}
	}

	// High retry_scheduled: boost retry_job if feedback is healthy.
	if pctx.RetryScheduledCount > retryScheduledHighThreshold && c.ActionType == "retry_job" {
		fb, hasFb := pctx.RecentActionFeedback[c.ActionType]
		if !hasFb || fb.Recommendation != actionmemory.RecommendAvoidAction {
			c.Score += params.HighRetryBoost
			c.Reasoning = append(c.Reasoning, fmt.Sprintf(
				"high retry_scheduled (%d > %d) with healthy feedback: retry_job boost +%.2f",
				pctx.RetryScheduledCount, retryScheduledHighThreshold, params.HighRetryBoost,
			))
		}
	}

	// High system failure rate: penalize actions with bad history, boost advisory.
	if pctx.FailureRate > failureRateHighThreshold {
		fb, hasFb := pctx.RecentActionFeedback[c.ActionType]
		if hasFb && fb.Recommendation == actionmemory.RecommendAvoidAction {
			c.Score -= highFailureRateBadHistoryPenalty
			c.Reasoning = append(c.Reasoning, fmt.Sprintf(
				"high failure_rate (%.2f > %.2f) with bad history: penalty -%.2f",
				pctx.FailureRate, failureRateHighThreshold, highFailureRateBadHistoryPenalty,
			))
		}
		if c.ActionType == "log_recommendation" {
			c.Score += highFailureRateRecommendBoost
			c.Reasoning = append(c.Reasoning, fmt.Sprintf(
				"high failure_rate (%.2f > %.2f): log_recommendation boost +%.2f",
				pctx.FailureRate, failureRateHighThreshold, highFailureRateRecommendBoost,
			))
		}
	}

	// Low acceptance rate: prefer non-destructive, penalize destructive.
	if pctx.AcceptanceRate > 0 && pctx.AcceptanceRate < acceptanceRateLowThreshold {
		if c.ActionType == "log_recommendation" || c.ActionType == "noop" {
			c.Score += lowAcceptanceAdvisoryBoost
			c.Reasoning = append(c.Reasoning, fmt.Sprintf(
				"low acceptance (%.2f < %.2f): advisory boost +%.2f",
				pctx.AcceptanceRate, acceptanceRateLowThreshold, lowAcceptanceAdvisoryBoost,
			))
		}
		if c.ActionType == "retry_job" || c.ActionType == "trigger_resync" {
			c.Score -= lowAcceptanceDestructivePenalty
			c.Reasoning = append(c.Reasoning, fmt.Sprintf(
				"low acceptance (%.2f < %.2f): destructive action penalty -%.2f",
				pctx.AcceptanceRate, acceptanceRateLowThreshold, lowAcceptanceDestructivePenalty,
			))
		}
	}

	return c
}
