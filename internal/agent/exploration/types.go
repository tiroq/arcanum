package exploration

import "time"

// --- Budget ---

// ExplorationBudget tracks the bounded exploration allowance.
// Per-cycle and per-hour limits ensure exploration remains tightly controlled.
type ExplorationBudget struct {
	MaxPerCycle    int       `json:"max_per_cycle"`
	MaxPerHour     int       `json:"max_per_hour"`
	UsedThisCycle  int       `json:"used_this_cycle"`
	UsedThisWindow int       `json:"used_this_window"`
	WindowStart    time.Time `json:"window_start"`
}

// DefaultBudget returns safe defaults: 1 per cycle, 3 per hour.
func DefaultBudget() ExplorationBudget {
	return ExplorationBudget{
		MaxPerCycle:  1,
		MaxPerHour:   3,
		WindowStart:  time.Now().UTC(),
	}
}

// HasCycleBudget returns true if exploration is allowed in the current cycle.
func (b *ExplorationBudget) HasCycleBudget() bool {
	return b.UsedThisCycle < b.MaxPerCycle
}

// HasWindowBudget returns true if hourly budget is not exhausted.
// Automatically rolls the window if the current window has expired.
func (b *ExplorationBudget) HasWindowBudget(now time.Time) bool {
	b.rollWindow(now)
	return b.UsedThisWindow < b.MaxPerHour
}

// Consume records one exploration action against both budgets.
func (b *ExplorationBudget) Consume(now time.Time) {
	b.rollWindow(now)
	b.UsedThisCycle++
	b.UsedThisWindow++
}

// ResetCycle resets the per-cycle counter. Called at the start of each planning cycle.
func (b *ExplorationBudget) ResetCycle() {
	b.UsedThisCycle = 0
}

// rollWindow resets the hourly window counter when the window has elapsed.
func (b *ExplorationBudget) rollWindow(now time.Time) {
	if now.Sub(b.WindowStart) >= time.Hour {
		b.UsedThisWindow = 0
		b.WindowStart = now
	}
}

// BudgetReason returns a human-readable reason why budget is available or exhausted.
func (b *ExplorationBudget) BudgetReason(now time.Time) string {
	if !b.HasCycleBudget() {
		return "cycle_budget_exhausted"
	}
	if !b.HasWindowBudget(now) {
		return "hourly_budget_exhausted"
	}
	return "budget_available"
}

// --- Candidate ---

// ExplorationCandidate represents a potential exploratory action with
// computed scores across multiple dimensions.
type ExplorationCandidate struct {
	ActionType        string  `json:"action_type"`
	GoalType          string  `json:"goal_type"`
	BaseDecisionScore float64 `json:"base_decision_score"`
	UncertaintyScore  float64 `json:"uncertainty_score"`
	NoveltyScore      float64 `json:"novelty_score"`
	SafetyScore       float64 `json:"safety_score"`
	ExplorationScore  float64 `json:"exploration_score"`
	Reason            string  `json:"reason"`
}

// --- Decision ---

// ExplorationDecision captures the full exploration deliberation for a
// planning cycle, including why exploration was or was not chosen.
type ExplorationDecision struct {
	Enabled          bool                   `json:"enabled"`
	Chosen           bool                   `json:"chosen"`
	ChosenActionType string                 `json:"chosen_action_type,omitempty"`
	BudgetReason     string                 `json:"budget_reason"`
	DecisionReason   string                 `json:"decision_reason"`
	Candidates       []ExplorationCandidate `json:"candidates,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
}

// --- Trigger Thresholds ---

const (
	// TriggerConfidenceThreshold: exploration considered when top candidate
	// confidence is below this.
	TriggerConfidenceThreshold = 0.60

	// TriggerScoreGapThreshold: exploration considered when the score gap
	// between top two candidates is below this.
	TriggerScoreGapThreshold = 0.08

	// ExplorationScoreThreshold: minimum exploration score for an exploratory
	// candidate to override the exploitation choice.
	ExplorationScoreThreshold = 0.30

	// MaxSampleSizeForNovelty: candidates with sample size at or above this
	// are NOT considered novel/underexplored.
	MaxSampleSizeForNovelty = 5

	// SafetyAvoidConfidenceThreshold: if avoid_action confidence is above
	// this, the candidate is unsafe for exploration.
	SafetyAvoidConfidenceThreshold = 0.60
)
