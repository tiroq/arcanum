package goals

import (
	"fmt"
	"time"
)

// Evaluation thresholds — deterministic, no LLM.
const (
	// thresholdFailureRate: if failure_rate > 0.2, emit increase_reliability goal.
	thresholdFailureRate = 0.20

	// thresholdRetryBacklog: if retry_scheduled count > 10, emit reduce_retry_rate goal.
	thresholdRetryBacklog int64 = 10

	// thresholdLowAcceptance: if acceptance_rate < 0.4, emit increase_model_quality goal.
	thresholdLowAcceptance = 0.40

	// thresholdQueueBacklog: if queued jobs > 50, emit resolve_queue_backlog goal.
	thresholdQueueBacklog int64 = 50

	// thresholdHighLatencyMS: if avg latency > 30s, emit reduce_latency goal.
	thresholdHighLatencyMS = 30_000.0

	// thresholdDeadLetterRate: if dead_letter_rate > 0.1, emit investigate_failed_jobs goal.
	thresholdDeadLetterRate = 0.10
)

// EvaluateSystem inspects a SystemSnapshot and returns zero or more advisory
// goals. The function is pure: same input always produces same output.
// No database access, no side effects.
func EvaluateSystem(snap SystemSnapshot) []Goal {
	now := time.Now().UTC()
	var goals []Goal

	goals = evalFailureRate(snap, now, goals)
	goals = evalRetryBacklog(snap, now, goals)
	goals = evalAcceptanceRate(snap, now, goals)
	goals = evalQueueBacklog(snap, now, goals)
	goals = evalLatency(snap, now, goals)
	goals = evalDeadLetters(snap, now, goals)

	return goals
}

func evalFailureRate(snap SystemSnapshot, now time.Time, goals []Goal) []Goal {
	if snap.TotalJobsRecent == 0 {
		return goals
	}
	rate := float64(snap.FailedJobsRecent) / float64(snap.TotalJobsRecent)
	if rate <= thresholdFailureRate {
		return goals
	}
	return append(goals, Goal{
		ID:         fmt.Sprintf("goal-%s-%d", GoalIncreaseReliability, now.Unix()),
		Type:       string(GoalIncreaseReliability),
		Priority:   clamp(rate * 2), // higher failure → higher priority
		Confidence: confidence(snap.TotalJobsRecent),
		Description: fmt.Sprintf(
			"Failure rate %.1f%% exceeds threshold %.0f%%. "+
				"Investigate failing jobs and consider model or provider changes.",
			rate*100, thresholdFailureRate*100,
		),
		Evidence: map[string]any{
			"failure_rate":      rate,
			"threshold":         thresholdFailureRate,
			"failed_recent":     snap.FailedJobsRecent,
			"total_recent":      snap.TotalJobsRecent,
		},
		CreatedAt: now,
	})
}

func evalRetryBacklog(snap SystemSnapshot, now time.Time, goals []Goal) []Goal {
	retryCount := snap.QueueStats["retry_scheduled"]
	if retryCount <= thresholdRetryBacklog {
		return goals
	}
	return append(goals, Goal{
		ID:   fmt.Sprintf("goal-%s-%d", GoalReduceRetryRate, now.Unix()),
		Type: string(GoalReduceRetryRate),
		// Priority scales with how far above threshold we are, capped at 1.0.
		Priority:   clamp(float64(retryCount) / float64(thresholdRetryBacklog) / 5.0),
		Confidence: 0.90, // queue stats are exact counts
		Description: fmt.Sprintf(
			"Retry backlog %d exceeds threshold %d. "+
				"Reduce retry pressure by investigating root cause of failures.",
			retryCount, thresholdRetryBacklog,
		),
		Evidence: map[string]any{
			"retry_scheduled": retryCount,
			"threshold":       thresholdRetryBacklog,
		},
		CreatedAt: now,
	})
}

func evalAcceptanceRate(snap SystemSnapshot, now time.Time, goals []Goal) []Goal {
	if snap.TotalProposals == 0 {
		return goals
	}
	rate := float64(snap.AcceptedProposals) / float64(snap.TotalProposals)
	if rate >= thresholdLowAcceptance {
		return goals
	}
	return append(goals, Goal{
		ID:         fmt.Sprintf("goal-%s-%d", GoalImproveModelQuality, now.Unix()),
		Type:       string(GoalImproveModelQuality),
		Priority:   clamp((1.0 - rate) * 1.5), // worse acceptance → higher priority
		Confidence: confidence(snap.TotalProposals),
		Description: fmt.Sprintf(
			"Proposal acceptance rate %.1f%% is below threshold %.0f%%. "+
				"Consider escalating to a stronger model or adjusting prompts.",
			rate*100, thresholdLowAcceptance*100,
		),
		Evidence: map[string]any{
			"acceptance_rate":    rate,
			"threshold":         thresholdLowAcceptance,
			"accepted_proposals": snap.AcceptedProposals,
			"rejected_proposals": snap.RejectedProposals,
			"total_proposals":    snap.TotalProposals,
		},
		CreatedAt: now,
	})
}

func evalQueueBacklog(snap SystemSnapshot, now time.Time, goals []Goal) []Goal {
	queued := snap.QueueStats["queued"]
	if queued <= thresholdQueueBacklog {
		return goals
	}
	return append(goals, Goal{
		ID:         fmt.Sprintf("goal-%s-%d", GoalResolveBacklog, now.Unix()),
		Type:       string(GoalResolveBacklog),
		Priority:   clamp(float64(queued) / float64(thresholdQueueBacklog) / 3.0),
		Confidence: 0.95, // exact count
		Description: fmt.Sprintf(
			"Queue backlog %d exceeds threshold %d. "+
				"System may need more workers or throughput optimization.",
			queued, thresholdQueueBacklog,
		),
		Evidence: map[string]any{
			"queued":    queued,
			"threshold": thresholdQueueBacklog,
		},
		CreatedAt: now,
	})
}

func evalLatency(snap SystemSnapshot, now time.Time, goals []Goal) []Goal {
	if snap.AvgLatencyMS <= 0 || snap.AvgLatencyMS <= thresholdHighLatencyMS {
		return goals
	}
	return append(goals, Goal{
		ID:         fmt.Sprintf("goal-%s-%d", GoalReduceLatency, now.Unix()),
		Type:       string(GoalReduceLatency),
		Priority:   clamp(snap.AvgLatencyMS / thresholdHighLatencyMS / 3.0),
		Confidence: confidence(snap.TotalJobsRecent),
		Description: fmt.Sprintf(
			"Average processing latency %.0fms exceeds threshold %.0fms. "+
				"Consider using faster models or reducing prompt complexity.",
			snap.AvgLatencyMS, thresholdHighLatencyMS,
		),
		Evidence: map[string]any{
			"avg_latency_ms": snap.AvgLatencyMS,
			"threshold_ms":   thresholdHighLatencyMS,
		},
		CreatedAt: now,
	})
}

func evalDeadLetters(snap SystemSnapshot, now time.Time, goals []Goal) []Goal {
	if snap.TotalJobsRecent == 0 {
		return goals
	}
	rate := float64(snap.DeadLetterRecent) / float64(snap.TotalJobsRecent)
	if rate <= thresholdDeadLetterRate {
		return goals
	}
	return append(goals, Goal{
		ID:         fmt.Sprintf("goal-%s-%d", GoalInvestigateFailures, now.Unix()),
		Type:       string(GoalInvestigateFailures),
		Priority:   clamp(rate * 3), // dead letters are serious
		Confidence: confidence(snap.TotalJobsRecent),
		Description: fmt.Sprintf(
			"Dead letter rate %.1f%% exceeds threshold %.0f%%. "+
				"Jobs are exhausting retries. Investigate root cause.",
			rate*100, thresholdDeadLetterRate*100,
		),
		Evidence: map[string]any{
			"dead_letter_rate":   rate,
			"threshold":          thresholdDeadLetterRate,
			"dead_letter_recent": snap.DeadLetterRecent,
			"total_recent":       snap.TotalJobsRecent,
		},
		CreatedAt: now,
	})
}

// clamp constrains v to [0.0, 1.0].
func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// confidence produces a 0-1 confidence score based on sample size.
// More data → higher confidence. Minimum 0.30 for any non-zero sample.
func confidence(sampleSize int64) float64 {
	if sampleSize <= 0 {
		return 0
	}
	if sampleSize >= 100 {
		return 0.95
	}
	if sampleSize >= 50 {
		return 0.85
	}
	if sampleSize >= 20 {
		return 0.70
	}
	if sampleSize >= 5 {
		return 0.50
	}
	return 0.30
}
