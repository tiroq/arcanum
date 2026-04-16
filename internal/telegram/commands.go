package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// SetAPIClient attaches an API client for talking to the api-gateway.
func (b *Bot) SetAPIClient(client *APIClient) {
	b.apiClient = client
}

// handleGoals shows active goals and subgoals.
func (b *Bot) handleGoals() string {
	if b.apiClient == nil {
		return "API client not configured."
	}
	ctx := context.Background()
	goals, err := b.apiClient.GetGoals(ctx)
	if err != nil {
		return fmt.Sprintf("Error fetching goals: %v", err)
	}
	if len(goals) == 0 {
		return "No active goals."
	}

	var lines []string
	lines = append(lines, "<b>🎯 Active Goals</b>\n")
	for _, g := range goals {
		name, _ := g["name"].(string)
		priority, _ := g["priority"].(float64)
		gType, _ := g["type"].(string)
		lines = append(lines, fmt.Sprintf("• <b>%s</b> [%s] priority=%.2f", name, gType, priority))
	}

	// Also fetch subgoals
	sgs, err := b.apiClient.GetSubgoals(ctx)
	if err == nil && len(sgs) > 0 {
		lines = append(lines, fmt.Sprintf("\n<b>Subgoals:</b> %d total", len(sgs)))
		for i, sg := range sgs {
			if i >= 5 {
				lines = append(lines, fmt.Sprintf("  ... and %d more", len(sgs)-5))
				break
			}
			label, _ := sg["label"].(string)
			status, _ := sg["status"].(string)
			progress, _ := sg["progress"].(float64)
			lines = append(lines, fmt.Sprintf("  - %s [%s] %.0f%%", label, status, progress*100))
		}
	}
	return strings.Join(lines, "\n")
}

// handleQueueTasks shows the current task priority queue.
func (b *Bot) handleQueueTasks() string {
	if b.apiClient == nil {
		return "API client not configured."
	}
	ctx := context.Background()
	queue, err := b.apiClient.GetTaskQueue(ctx)
	if err != nil {
		return fmt.Sprintf("Error fetching queue: %v", err)
	}
	if len(queue) == 0 {
		return "Task queue is empty."
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("<b>📋 Task Queue</b> (%d items)\n", len(queue)))
	for i, t := range queue {
		if i >= 10 {
			lines = append(lines, fmt.Sprintf("  ... and %d more", len(queue)-10))
			break
		}
		goal, _ := t["goal"].(string)
		priority, _ := t["priority"].(float64)
		status, _ := t["status"].(string)
		lines = append(lines, fmt.Sprintf("  %d. <b>%s</b> [%s] priority=%.2f",
			i+1, truncate(goal, 50), status, priority))
	}
	return strings.Join(lines, "\n")
}

// handleFocus shows what the system is currently working on.
func (b *Bot) handleFocus() string {
	if b.apiClient == nil {
		return "API client not configured."
	}
	ctx := context.Background()

	state, err := b.apiClient.GetAutonomyState(ctx)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	mode, _ := state["mode"].(string)
	running, _ := state["running"].(bool)
	tasksCreated, _ := state["tasks_created_from_actuation"].(float64)
	execCompleted, _ := state["execution_completed"].(float64)
	execFailed, _ := state["execution_failed"].(float64)
	feedbackRecorded, _ := state["feedback_recorded"].(float64)

	var lines []string
	lines = append(lines, "<b>🔍 Current Focus</b>\n")
	lines = append(lines, fmt.Sprintf("Mode: <b>%s</b>", mode))
	lines = append(lines, fmt.Sprintf("Running: %v", running))
	lines = append(lines, fmt.Sprintf("Tasks created: %.0f", tasksCreated))
	lines = append(lines, fmt.Sprintf("Exec completed: %.0f | failed: %.0f", execCompleted, execFailed))
	lines = append(lines, fmt.Sprintf("Feedback recorded: %.0f", feedbackRecorded))

	// Show running tasks
	tasks, err := b.apiClient.GetTasks(ctx)
	if err == nil {
		var running []map[string]interface{}
		for _, t := range tasks {
			if s, _ := t["status"].(string); s == "running" {
				running = append(running, t)
			}
		}
		if len(running) > 0 {
			lines = append(lines, fmt.Sprintf("\n<b>Running tasks:</b> %d", len(running)))
			for _, t := range running {
				goal, _ := t["goal"].(string)
				lines = append(lines, fmt.Sprintf("  → %s", truncate(goal, 60)))
			}
		} else {
			lines = append(lines, "\nNo tasks currently running.")
		}
	}

	return strings.Join(lines, "\n")
}

// handleReport shows the latest autonomy report.
func (b *Bot) handleReport() string {
	if b.apiClient == nil {
		return "API client not configured."
	}
	ctx := context.Background()

	reports, err := b.apiClient.GetAutonomyReports(ctx)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	if len(reports) == 0 {
		return "No autonomy reports available yet."
	}

	r := reports[0] // most recent
	rType, _ := r["type"].(string)
	mode, _ := r["mode"].(string)
	safeRouted, _ := r["safe_actions_routed"].(float64)
	reviewQueued, _ := r["review_queued"].(float64)
	failures, _ := r["failure_count"].(float64)
	downgraded, _ := r["downgraded"].(bool)
	createdAt, _ := r["created_at"].(string)

	var lines []string
	lines = append(lines, "<b>📊 Latest Autonomy Report</b>\n")
	lines = append(lines, fmt.Sprintf("Type: %s", rType))
	lines = append(lines, fmt.Sprintf("Mode: %s", mode))
	lines = append(lines, fmt.Sprintf("Safe actions routed: %.0f", safeRouted))
	lines = append(lines, fmt.Sprintf("Review queued: %.0f", reviewQueued))
	lines = append(lines, fmt.Sprintf("Failures: %.0f", failures))
	lines = append(lines, fmt.Sprintf("Downgraded: %v", downgraded))
	lines = append(lines, fmt.Sprintf("Created: %s", createdAt))

	if warnings, ok := r["warnings"].([]interface{}); ok && len(warnings) > 0 {
		lines = append(lines, "\n<b>Warnings:</b>")
		for _, w := range warnings {
			if s, ok := w.(string); ok {
				lines = append(lines, "  ⚠️ "+s)
			}
		}
	}

	// Add objective summary
	obj, err := b.apiClient.GetObjectiveSummary(ctx)
	if err == nil && obj != nil {
		netUtility, _ := obj["net_utility"].(float64)
		lines = append(lines, fmt.Sprintf("\n<b>Objective:</b> net_utility=%.3f", netUtility))
	}

	return strings.Join(lines, "\n")
}

// handlePause pauses the autonomy orchestrator.
func (b *Bot) handlePause() string {
	if b.apiClient == nil {
		return "API client not configured."
	}
	if err := b.apiClient.SetAutonomyMode(context.Background(), "frozen"); err != nil {
		return fmt.Sprintf("Error pausing: %v", err)
	}
	return "⏸ System paused (mode: frozen). Use /resume to restart."
}

// handleResume resumes the autonomy orchestrator.
func (b *Bot) handleResume() string {
	if b.apiClient == nil {
		return "API client not configured."
	}
	if err := b.apiClient.SetAutonomyMode(context.Background(), "supervised_autonomy"); err != nil {
		return fmt.Sprintf("Error resuming: %v", err)
	}
	return "▶️ System resumed (mode: supervised_autonomy)."
}

// handleVector shows or updates the system vector.
// Usage: /vector (show), /vector income_priority=0.8 risk_tolerance=0.5
func (b *Bot) handleVector(args string) string {
	if b.apiClient == nil {
		return "API client not configured."
	}
	ctx := context.Background()

	if args == "" {
		// Show current vector
		v, err := b.apiClient.GetVector(ctx)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		var lines []string
		lines = append(lines, "<b>🧭 System Vector</b>\n")
		fields := []string{
			"income_priority", "family_safety_priority", "infra_priority",
			"automation_priority", "exploration_level", "risk_tolerance",
			"human_review_strictness",
		}
		for _, f := range fields {
			val, _ := v[f].(float64)
			lines = append(lines, fmt.Sprintf("  %s: <b>%.2f</b>", f, val))
		}
		lines = append(lines, "\n<i>Set: /vector income_priority=0.8 risk_tolerance=0.5</i>")
		return strings.Join(lines, "\n")
	}

	// Parse key=value pairs
	updates := make(map[string]interface{})
	// First get current vector to use as base
	current, err := b.apiClient.GetVector(ctx)
	if err != nil {
		return fmt.Sprintf("Error fetching current vector: %v", err)
	}
	for k, v := range current {
		updates[k] = v
	}

	parts := strings.Fields(args)
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		val, err := strconv.ParseFloat(kv[1], 64)
		if err != nil {
			return fmt.Sprintf("Invalid value for %s: %s", kv[0], kv[1])
		}
		updates[kv[0]] = val
	}

	result, err := b.apiClient.SetVector(ctx, updates)
	if err != nil {
		return fmt.Sprintf("Error setting vector: %v", err)
	}

	var lines []string
	lines = append(lines, "<b>🧭 Vector Updated</b>\n")
	fields := []string{
		"income_priority", "family_safety_priority", "infra_priority",
		"automation_priority", "exploration_level", "risk_tolerance",
		"human_review_strictness",
	}
	for _, f := range fields {
		val, _ := result[f].(float64)
		lines = append(lines, fmt.Sprintf("  %s: <b>%.2f</b>", f, val))
	}
	return strings.Join(lines, "\n")
}

// handleWhy explains why a decision or task was made.
func (b *Bot) handleWhy(id string) string {
	if b.apiClient == nil {
		return "API client not configured."
	}
	if id == "" {
		return "Usage: /why &lt;task_id or decision_id&gt;"
	}
	ctx := context.Background()

	// Try actuation decisions
	decisions, err := b.apiClient.GetActuationDecisions(ctx)
	if err == nil {
		for _, d := range decisions {
			did, _ := d["id"].(string)
			if did == id || strings.HasPrefix(did, id) {
				aType, _ := d["action_type"].(string)
				status, _ := d["status"].(string)
				priority, _ := d["priority"].(float64)
				reason, _ := d["reason"].(string)
				return fmt.Sprintf("<b>Decision %s</b>\nType: %s\nStatus: %s\nPriority: %.2f\nReason: %s",
					shortID(did), aType, status, priority, reason)
			}
		}
	}

	// Try tasks
	tasks, err := b.apiClient.GetTasks(ctx)
	if err == nil {
		for _, t := range tasks {
			tid, _ := t["id"].(string)
			if tid == id || strings.HasPrefix(tid, id) {
				goal, _ := t["goal"].(string)
				status, _ := t["status"].(string)
				priority, _ := t["priority"].(float64)
				source, _ := t["source"].(string)
				return fmt.Sprintf("<b>Task %s</b>\nGoal: %s\nStatus: %s\nPriority: %.2f\nSource: %s",
					shortID(tid), goal, status, priority, source)
			}
		}
	}

	return fmt.Sprintf("No decision or task found with ID starting with '%s'.", id)
}

// handleApproveActuation approves an actuation decision.
func (b *Bot) handleApproveActuation(id string) string {
	if b.apiClient == nil {
		return "API client not configured."
	}
	if id == "" {
		return "Usage: /approve &lt;id&gt;"
	}
	if err := b.apiClient.ApproveActuationDecision(context.Background(), id); err != nil {
		// Fall back to proposal approval
		return b.handleApprove(id)
	}
	return fmt.Sprintf("✅ Decision %s approved.", shortID(id))
}

// handleRejectActuation rejects an actuation decision.
func (b *Bot) handleRejectActuation(id string) string {
	if b.apiClient == nil {
		return "API client not configured."
	}
	if id == "" {
		return "Usage: /reject &lt;id&gt;"
	}
	if err := b.apiClient.RejectActuationDecision(context.Background(), id); err != nil {
		// Fall back to proposal rejection
		return b.handleReject(id)
	}
	return fmt.Sprintf("❌ Decision %s rejected.", shortID(id))
}

// handleExtendedStatus shows full system status including autonomy state.
func (b *Bot) handleExtendedStatus() string {
	// Start with the DB-based basic status
	basic := b.handleStatus()

	if b.apiClient == nil {
		return basic
	}

	ctx := context.Background()
	state, err := b.apiClient.GetAutonomyState(ctx)
	if err != nil {
		return basic + "\n\n<i>Autonomy state unavailable.</i>"
	}

	mode, _ := state["mode"].(string)
	running, _ := state["running"].(bool)
	consecutiveFailures, _ := state["consecutive_failures"].(float64)
	downgraded, _ := state["downgraded"].(bool)
	safeRouted, _ := state["safe_actions_routed"].(float64)
	reviewQueued, _ := state["review_actions_queued"].(float64)
	suppressed, _ := state["suppressed_decisions"].(float64)

	return basic + fmt.Sprintf(`

<b>Autonomy Runtime</b>
Mode: <b>%s</b> | Running: %v
Failures: %.0f | Downgraded: %v
Actions: %.0f safe, %.0f review, %.0f suppressed`,
		mode, running, consecutiveFailures, downgraded,
		safeRouted, reviewQueued, suppressed)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
