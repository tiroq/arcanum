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

// ScoreCandidate computes the score for a single action candidate given
// the goal context and planning context. The function is pure and deterministic.
// It returns the updated candidate with Score, Confidence, and Reasoning populated.
func ScoreCandidate(candidate PlannedActionCandidate, goalPriority, goalConfidence float64, pctx PlanningContext) PlannedActionCandidate {
	c := candidate
	c.Reasoning = nil

	// Start from base score scaled by goal priority.
	c.Score = baseScoreDefault + (goalPriority * 0.3)
	c.Confidence = goalConfidence
	c.Reasoning = append(c.Reasoning, fmt.Sprintf("base=%.2f (default %.2f + goal_priority %.2f * 0.3)", c.Score, baseScoreDefault, goalPriority))

	// noop always gets a penalty so it only wins when everything else is worse.
	if c.ActionType == "noop" {
		c.Score -= noopBasePenalty
		c.Reasoning = append(c.Reasoning, fmt.Sprintf("noop penalty: -%.2f", noopBasePenalty))
		return c
	}

	// --- Historical feedback adjustments ---
	c = applyFeedbackRules(c, pctx)

	// --- System context adjustments ---
	c = applyContextRules(c, pctx)

	// --- Safety preference ---
	if c.ActionType == "log_recommendation" {
		c.Score += safetyPreferenceBoost
		c.Reasoning = append(c.Reasoning, fmt.Sprintf("safety preference (advisory): +%.2f", safetyPreferenceBoost))
	}

	return c
}

// applyFeedbackRules adjusts score based on historical action feedback.
func applyFeedbackRules(c PlannedActionCandidate, pctx PlanningContext) PlannedActionCandidate {
	fb, hasFeedback := pctx.RecentActionFeedback[c.ActionType]
	if !hasFeedback {
		c.Reasoning = append(c.Reasoning, "no historical feedback available")
		return c
	}

	switch fb.Recommendation {
	case actionmemory.RecommendAvoidAction:
		c.Score -= feedbackAvoidPenalty
		c.Reasoning = append(c.Reasoning, fmt.Sprintf(
			"feedback avoid_action: -%.2f (success=%.2f, failure=%.2f, n=%d)",
			feedbackAvoidPenalty, fb.SuccessRate, fb.FailureRate, fb.SampleSize,
		))
	case actionmemory.RecommendPreferAction:
		c.Score += feedbackPreferBoost
		c.Reasoning = append(c.Reasoning, fmt.Sprintf(
			"feedback prefer_action: +%.2f (success=%.2f, failure=%.2f, n=%d)",
			feedbackPreferBoost, fb.SuccessRate, fb.FailureRate, fb.SampleSize,
		))
	case actionmemory.RecommendInsufficientData:
		c.Reasoning = append(c.Reasoning, fmt.Sprintf(
			"feedback insufficient_data (n=%d): no bias applied",
			fb.SampleSize,
		))
	case actionmemory.RecommendNeutral:
		c.Reasoning = append(c.Reasoning, fmt.Sprintf(
			"feedback neutral (success=%.2f, failure=%.2f, n=%d): no bias applied",
			fb.SuccessRate, fb.FailureRate, fb.SampleSize,
		))
	}

	return c
}

// applyContextRules adjusts score based on current system conditions.
func applyContextRules(c PlannedActionCandidate, pctx PlanningContext) PlannedActionCandidate {
	// High queue backlog penalties.
	if pctx.QueueBacklog > queueBacklogHighThreshold {
		if c.ActionType == "trigger_resync" {
			c.Score -= highBacklogResyncPenalty
			c.Reasoning = append(c.Reasoning, fmt.Sprintf(
				"high backlog (%d > %d): trigger_resync penalty -%.2f",
				pctx.QueueBacklog, queueBacklogHighThreshold, highBacklogResyncPenalty,
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
			c.Score += highRetryBoost
			c.Reasoning = append(c.Reasoning, fmt.Sprintf(
				"high retry_scheduled (%d > %d) with healthy feedback: retry_job boost +%.2f",
				pctx.RetryScheduledCount, retryScheduledHighThreshold, highRetryBoost,
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
