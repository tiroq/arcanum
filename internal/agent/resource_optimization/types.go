package resource_optimization

import "time"

// ResourceProfile tracks historical cost/latency/depth statistics for a
// mode+goal_type combination. Updated via rolling averages after each decision.
type ResourceProfile struct {
	ID                int       `json:"id"`
	Mode              string    `json:"mode"`
	GoalType          string    `json:"goal_type"`
	AvgLatencyMs      float64   `json:"avg_latency_ms"`
	AvgReasoningDepth float64   `json:"avg_reasoning_depth"`
	AvgPathLength     float64   `json:"avg_path_length"`
	AvgTokenCost      float64   `json:"avg_token_cost"`
	AvgExecutionCost  float64   `json:"avg_execution_cost"`
	SampleCount       int       `json:"sample_count"`
	LastUpdated       time.Time `json:"last_updated"`
}

// ResourceDecisionSignals holds the computed penalty signals for a resource profile.
// All values are in [0, 1]. Higher = worse (more expensive / slower).
type ResourceDecisionSignals struct {
	LatencyPenalty  float64 `json:"latency_penalty"`
	CostPenalty     float64 `json:"cost_penalty"`
	DepthPenalty    float64 `json:"depth_penalty"`
	EfficiencyScore float64 `json:"efficiency_score"`
}

// ResourceOutcomeInput carries the resource metrics collected after a decision
// is executed and its outcome observed.
type ResourceOutcomeInput struct {
	Mode           string  `json:"mode"`
	GoalType       string  `json:"goal_type"`
	LatencyMs      float64 `json:"latency_ms"`
	ReasoningDepth float64 `json:"reasoning_depth"` // graph depth or 1 for direct
	PathLength     float64 `json:"path_length"`
	TokenCost      float64 `json:"token_cost"`     // proxy: could be path_length * mode_weight
	ExecutionCost  float64 `json:"execution_cost"` // normalized internal cost
}

// ResourceSummary aggregates profiles for observability.
type ResourceSummary struct {
	TotalProfiles  int                          `json:"total_profiles"`
	AvgEfficiency  float64                      `json:"avg_efficiency"`
	PressureState  string                       `json:"pressure_state"` // "none", "high_latency", "high_cost"
	ProfilesByMode map[string][]ResourceProfile `json:"profiles_by_mode"`
}

// ResourceDecisionRecord captures a single decision's resource penalties for API visibility.
type ResourceDecisionRecord struct {
	Mode           string                  `json:"mode"`
	GoalType       string                  `json:"goal_type"`
	Signals        ResourceDecisionSignals `json:"signals"`
	AppliedPenalty float64                 `json:"applied_penalty"`
	Timestamp      time.Time               `json:"timestamp"`
}

// --- Thresholds and Constants ---

const (
	// LatencyThresholdMs is the latency above which penalties begin to increase.
	LatencyThresholdMs = 500.0

	// HighLatencyMs is the latency at which LatencyPenalty reaches 1.0.
	HighLatencyMs = 5000.0

	// CostThreshold is the normalized execution cost above which penalties start.
	CostThreshold = 1.0

	// HighCost is the cost at which CostPenalty reaches 1.0.
	HighCost = 10.0

	// DepthThreshold is the reasoning depth above which DepthPenalty starts.
	DepthThreshold = 1.0

	// HighDepth is the depth at which DepthPenalty reaches 1.0.
	HighDepth = 5.0

	// LatencyWeight is the weight of the latency penalty in EfficiencyScore.
	LatencyWeight = 0.4

	// CostWeight is the weight of the cost penalty in EfficiencyScore.
	CostWeight = 0.4

	// DepthWeight is the weight of the depth penalty in EfficiencyScore.
	DepthWeight = 0.2

	// ResourcePenaltyWeight is the maximum influence of resource signals on path scoring.
	// Bounded to prevent domination.
	ResourcePenaltyWeight = 0.10

	// ModeDirectBoost is the efficiency boost for direct mode when confidence is high.
	ModeDirectBoost = 0.05

	// ModeGraphPenalty is the maximum penalty applied to graph mode when cost is high
	// and marginal benefit is low.
	ModeGraphPenalty = 0.08

	// HighLatencyPressureThreshold defines when the system enters high-latency pressure.
	HighLatencyPressureThreshold = 2000.0

	// HighCostPressureThreshold defines when the system enters high-cost pressure.
	HighCostPressureThreshold = 5.0

	// MinSamplesForSignals is the minimum sample count before resource signals are used.
	MinSamplesForSignals = 3

	// MaxRecentDecisions is the maximum number of recent resource decisions stored for API.
	MaxRecentDecisions = 50
)

// ModeComplexityWeight returns a deterministic complexity weight per mode.
// Used to compute normalized execution cost proxy when real cost is unavailable.
func ModeComplexityWeight(mode string) float64 {
	switch mode {
	case "direct":
		return 1.0
	case "conservative":
		return 0.5
	case "exploratory":
		return 2.0
	case "graph":
		return 3.0
	default:
		return 1.0
	}
}

// clamp01 constrains a value to [0, 1].
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
