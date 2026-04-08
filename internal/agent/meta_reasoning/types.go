package meta_reasoning

import "time"

// DecisionMode represents the reasoning strategy the system uses.
type DecisionMode string

const (
	// ModeGraph is the default: full decision graph evaluation (Iteration 20).
	ModeGraph DecisionMode = "graph"
	// ModeDirect is the fast path: single-step best action, skip graph expansion.
	ModeDirect DecisionMode = "direct"
	// ModeConservative restricts to safe actions (noop, recommendation).
	ModeConservative DecisionMode = "conservative"
	// ModeExploratory forces a non-top choice (exploration bias).
	ModeExploratory DecisionMode = "exploratory"
)

// AllModes enumerates all valid decision modes.
var AllModes = []DecisionMode{ModeGraph, ModeDirect, ModeConservative, ModeExploratory}

// IsValid returns true if the mode is one of the defined constants.
func (m DecisionMode) IsValid() bool {
	switch m {
	case ModeGraph, ModeDirect, ModeConservative, ModeExploratory:
		return true
	}
	return false
}

// ModeDecision captures the result of meta-reasoning mode selection.
type ModeDecision struct {
	Mode       DecisionMode `json:"mode"`
	Confidence float64      `json:"confidence"`
	Reason     string       `json:"reason"`
}

// ModeMemoryRecord tracks per-mode, per-goal selection statistics.
type ModeMemoryRecord struct {
	ID             int          `json:"id"`
	Mode           DecisionMode `json:"mode"`
	GoalType       string       `json:"goal_type"`
	SelectionCount int          `json:"selection_count"`
	SuccessCount   int          `json:"success_count"`
	FailureCount   int          `json:"failure_count"`
	SuccessRate    float64      `json:"success_rate"`
	LastSelectedAt *time.Time   `json:"last_selected_at,omitempty"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
}

// ModeHistoryRecord is a single mode selection event for observability.
type ModeHistoryRecord struct {
	ID           int          `json:"id"`
	GoalType     string       `json:"goal_type"`
	SelectedMode DecisionMode `json:"selected_mode"`
	Confidence   float64      `json:"confidence"`
	Reason       string       `json:"reason"`
	Outcome      string       `json:"outcome"`
	CreatedAt    time.Time    `json:"created_at"`
}

// MetaInput collects all signals needed for mode selection.
type MetaInput struct {
	GoalType string `json:"goal_type"`
	// Current decision signals.
	FailureRate float64 `json:"failure_rate"`
	Confidence  float64 `json:"confidence"`
	Risk        float64 `json:"risk"`
	// Stability layer.
	StabilityMode string `json:"stability_mode"`
	// Comparative learning signals.
	MissedWinCount int `json:"missed_win_count"`
	// Path learning signals.
	AvgPathSuccessRate float64 `json:"avg_path_success_rate"`
	PathSampleSize     int     `json:"path_sample_size"`
	// Stagnation signals.
	RecentNoopRate     float64 `json:"recent_noop_rate"`
	RecentLowValueRate float64 `json:"recent_low_value_rate"`
	// Previous mode for inertia.
	LastMode *DecisionMode `json:"last_mode,omitempty"`
}

// ModeScore holds a mode's computed score with breakdown.
type ModeScore struct {
	Mode       DecisionMode `json:"mode"`
	Score      float64      `json:"score"`
	MemoryRate float64      `json:"memory_rate"`
	Confidence float64      `json:"confidence"`
	Risk       float64      `json:"risk"`
}

// --- Thresholds ---
const (
	HighFailureRateThreshold    = 0.5
	StrongConfidenceThreshold   = 0.8
	LowRiskThreshold            = 0.2
	MissedWinThreshold          = 3
	StagnationNoopThreshold     = 0.6
	StagnationLowValueThreshold = 0.5
	InertiaBoost                = 0.07
	InertiaThreshold            = 0.15
	MinPathSamplesForDirect     = 5
)
