package decision_graph

// DecisionNode represents a single action in a decision graph.
// Each node captures the expected outcome of executing an action type.
type DecisionNode struct {
	ID            string  `json:"id"`
	ActionType    string  `json:"action_type"`
	ExpectedValue float64 `json:"expected_value"`
	Risk          float64 `json:"risk"`
	Confidence    float64 `json:"confidence"`
}

// DecisionEdge represents a valid transition between two nodes.
type DecisionEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// DecisionGraph holds the full graph for a goal: nodes and edges.
type DecisionGraph struct {
	GoalType string         `json:"goal_type"`
	Nodes    []DecisionNode `json:"nodes"`
	Edges    []DecisionEdge `json:"edges"`
	MaxDepth int            `json:"max_depth"`
}

// DecisionPath is a sequence of nodes forming a proposed execution plan.
// Only the first node is executed; remaining nodes are stored for learning.
type DecisionPath struct {
	Nodes []DecisionNode `json:"nodes"`

	TotalValue      float64 `json:"total_value"`
	TotalRisk       float64 `json:"total_risk"`
	TotalConfidence float64 `json:"total_confidence"`

	FinalScore float64 `json:"final_score"`
}

// PathSelection captures the result of path evaluation and selection.
type PathSelection struct {
	Paths           []DecisionPath `json:"paths"`
	Selected        *DecisionPath  `json:"selected,omitempty"`
	ExplorationPick *DecisionPath  `json:"exploration_pick,omitempty"`
	Reason          string         `json:"reason"`
	ExplorationUsed bool           `json:"exploration_used"`
}

// GraphConfig controls graph construction and path selection.
type GraphConfig struct {
	// MaxDepth limits path length. safe_mode forces 1, throttled penalizes long paths.
	MaxDepth int `json:"max_depth"`

	// StabilityMode: normal, throttled, safe_mode.
	StabilityMode string `json:"stability_mode"`

	// ShouldExplore: deterministic toggle for exploration override.
	ShouldExplore bool `json:"should_explore"`

	// LongPathPenalty: risk penalty for paths > 1 node in throttled mode.
	LongPathPenalty float64 `json:"long_path_penalty"`
}

// DefaultGraphConfig returns the default configuration.
func DefaultGraphConfig() GraphConfig {
	return GraphConfig{
		MaxDepth:        3,
		StabilityMode:   "normal",
		ShouldExplore:   false,
		LongPathPenalty: 0.15,
	}
}

// EffectiveMaxDepth returns the max depth adjusted for stability mode.
func (gc GraphConfig) EffectiveMaxDepth() int {
	if gc.StabilityMode == "safe_mode" {
		return 1
	}
	if gc.MaxDepth < 1 {
		return 3
	}
	return gc.MaxDepth
}

// --- Scoring Constants ---

const (
	// WeightValue is the scoring weight for average expected value.
	WeightValue = 0.5

	// WeightConfidence is the scoring weight for minimum confidence.
	WeightConfidence = 0.3

	// WeightRisk is the scoring weight for aggregated risk.
	WeightRisk = 0.2

	// FallbackScore is the score assigned to the fallback (noop) path.
	FallbackScore = 0.01

	// MaxNodeCount limits total nodes in a graph to prevent combinatorial explosion.
	MaxNodeCount = 20
)
