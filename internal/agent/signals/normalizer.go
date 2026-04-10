package signals

import (
	"math"
	"time"

	"github.com/google/uuid"
)

// Normalize converts a RawEvent into zero or one Signal.
// Returns (signal, true) when the event maps to a known signal type.
// Returns (Signal{}, false) when the event type is unrecognised.
// Deterministic: same input always produces same output (except UUID).
func Normalize(e RawEvent) (Signal, bool) {
	sig := Signal{
		ID:          uuid.New().String(),
		Source:      e.Source,
		ContextTags: []string{},
		ObservedAt:  e.ObservedAt,
		RawEventID:  e.ID,
		CreatedAt:   time.Now().UTC(),
	}

	switch e.EventType {
	case "job_failed", "failed_jobs":
		sig.SignalType = SignalFailedJobs
		sig.Value = floatFromPayload(e.Payload, "count", 1)
		sig.Confidence = 0.9
		sig.Severity = severityFromValue(sig.Value, 3, 10)
		sig.ContextTags = tagsFromPayload(e.Payload, "job_type")

	case "dead_letter", "dead_letter_spike":
		sig.SignalType = SignalDeadLetterSpike
		sig.Value = floatFromPayload(e.Payload, "count", 1)
		sig.Confidence = 0.85
		sig.Severity = severityFromValue(sig.Value, 5, 20)
		sig.ContextTags = tagsFromPayload(e.Payload, "queue")

	case "pending_tasks":
		sig.SignalType = SignalPendingTasks
		sig.Value = floatFromPayload(e.Payload, "count", 1)
		sig.Confidence = 0.8
		sig.Severity = severityFromValue(sig.Value, 10, 50)

	case "overdue_tasks":
		sig.SignalType = SignalOverdueTasks
		sig.Value = floatFromPayload(e.Payload, "count", 1)
		sig.Confidence = 0.85
		sig.Severity = severityFromValue(sig.Value, 3, 10)

	case "cost_spike":
		sig.SignalType = SignalCostSpike
		sig.Value = floatFromPayload(e.Payload, "amount", 0)
		sig.Confidence = 0.7
		sig.Severity = severityFromValue(sig.Value, 50, 200)

	case "income_gap":
		sig.SignalType = SignalIncomeGap
		sig.Value = floatFromPayload(e.Payload, "gap", 0)
		sig.Confidence = 0.75
		sig.Severity = severityFromValue(sig.Value, 500, 2000)

	case "new_opportunity":
		sig.SignalType = SignalNewOpportunity
		sig.Value = floatFromPayload(e.Payload, "estimated_value", 0)
		sig.Confidence = 0.6
		sig.Severity = severityFromValue(sig.Value, 1000, 5000)
		sig.ContextTags = tagsFromPayload(e.Payload, "opportunity_type")

	case "high_cognitive_load", "cognitive_load":
		sig.SignalType = SignalHighCognitiveLoad
		sig.Value = floatFromPayload(e.Payload, "score", 0)
		sig.Confidence = 0.65
		sig.Severity = severityFromValue(sig.Value, 0.5, 0.8)

	default:
		return Signal{}, false
	}

	return sig, true
}

// severityFromValue maps a numeric value to low/medium/high using two thresholds.
func severityFromValue(v, medThreshold, highThreshold float64) string {
	if v >= highThreshold {
		return SeverityHigh
	}
	if v >= medThreshold {
		return SeverityMedium
	}
	return SeverityLow
}

// floatFromPayload extracts a float64 from a map, returning defaultVal if missing.
func floatFromPayload(payload map[string]any, key string, defaultVal float64) float64 {
	v, ok := payload[key]
	if !ok {
		return defaultVal
	}
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return defaultVal
		}
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return defaultVal
	}
}

// tagsFromPayload extracts a string value as a single-element tag slice.
func tagsFromPayload(payload map[string]any, key string) []string {
	v, ok := payload[key]
	if !ok {
		return []string{}
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return []string{}
	}
	return []string{s}
}
