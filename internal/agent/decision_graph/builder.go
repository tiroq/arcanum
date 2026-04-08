package decision_graph

import (
	"fmt"
)

// ActionSignals carries per-action scoring data from the planner/strategy layers.
type ActionSignals struct {
	ExpectedValue float64 `json:"expected_value"`
	Risk          float64 `json:"risk"`
	Confidence    float64 `json:"confidence"`
}

// BuildInput holds all inputs needed to construct a decision graph.
type BuildInput struct {
	GoalType string

	// CandidateActions: action types available for this goal.
	CandidateActions []string

	// Signals: per-action scoring signals (from planner/strategy/memory).
	Signals map[string]ActionSignals

	// Transitions: valid from→to action type pairs.
	// When nil, default transitions are used.
	Transitions map[string][]string

	// Config controls depth and stability constraints.
	Config GraphConfig
}

// BuildGraph constructs a decision graph for the given goal.
// Nodes = possible actions, Edges = valid transitions.
// Graph depth is bounded by Config.EffectiveMaxDepth().
// Deterministic: same inputs always produce the same graph.
func BuildGraph(input BuildInput) DecisionGraph {
	maxDepth := input.Config.EffectiveMaxDepth()

	graph := DecisionGraph{
		GoalType: input.GoalType,
		MaxDepth: maxDepth,
	}

	if len(input.CandidateActions) == 0 {
		return graph
	}

	// Build nodes for each candidate action.
	for _, actionType := range input.CandidateActions {
		sig := input.Signals[actionType]
		node := DecisionNode{
			ID:            fmt.Sprintf("%s:%s:1", input.GoalType, actionType),
			ActionType:    actionType,
			ExpectedValue: sig.ExpectedValue,
			Risk:          sig.Risk,
			Confidence:    sig.Confidence,
		}
		graph.Nodes = append(graph.Nodes, node)
	}

	// Build deeper nodes and edges only if maxDepth > 1.
	if maxDepth > 1 {
		transitions := input.Transitions
		if transitions == nil {
			transitions = defaultTransitions(input.GoalType)
		}

		graph = buildDeepNodes(graph, transitions, input.Signals, maxDepth)
	}

	// Enforce node count limit.
	if len(graph.Nodes) > MaxNodeCount {
		graph.Nodes = graph.Nodes[:MaxNodeCount]
	}

	return graph
}

// buildDeepNodes adds depth-2 and depth-3 nodes and edges.
func buildDeepNodes(graph DecisionGraph, transitions map[string][]string, signals map[string]ActionSignals, maxDepth int) DecisionGraph {
	depth1Nodes := make([]DecisionNode, len(graph.Nodes))
	copy(depth1Nodes, graph.Nodes)

	for depth := 2; depth <= maxDepth; depth++ {
		var parentNodes []DecisionNode
		if depth == 2 {
			parentNodes = depth1Nodes
		} else {
			// For depth 3, find depth-2 nodes.
			parentNodes = nodesAtDepth(graph.Nodes, depth-1, graph.GoalType)
		}

		for _, parent := range parentNodes {
			successors, ok := transitions[parent.ActionType]
			if !ok {
				continue
			}
			for _, nextAction := range successors {
				if len(graph.Nodes) >= MaxNodeCount {
					return graph
				}

				sig := signals[nextAction]
				childID := fmt.Sprintf("%s:%s:%d", graph.GoalType, nextAction, depth)

				// Avoid duplicate nodes at the same depth with same action.
				if nodeExists(graph.Nodes, childID) {
					// Still add edge from parent.
					graph.Edges = append(graph.Edges, DecisionEdge{
						From: parent.ID,
						To:   childID,
					})
					continue
				}

				child := DecisionNode{
					ID:            childID,
					ActionType:    nextAction,
					ExpectedValue: sig.ExpectedValue,
					Risk:          sig.Risk,
					Confidence:    sig.Confidence,
				}
				graph.Nodes = append(graph.Nodes, child)
				graph.Edges = append(graph.Edges, DecisionEdge{
					From: parent.ID,
					To:   childID,
				})
			}
		}
	}

	return graph
}

// nodesAtDepth returns nodes at a specific depth (extracted from ID suffix).
func nodesAtDepth(nodes []DecisionNode, depth int, goalType string) []DecisionNode {
	suffix := fmt.Sprintf(":%d", depth)
	var result []DecisionNode
	for _, n := range nodes {
		if len(n.ID) > len(suffix) && n.ID[len(n.ID)-len(suffix):] == suffix {
			result = append(result, n)
		}
	}
	return result
}

// nodeExists checks if a node with the given ID already exists.
func nodeExists(nodes []DecisionNode, id string) bool {
	for _, n := range nodes {
		if n.ID == id {
			return true
		}
	}
	return false
}

// EnumeratePaths extracts all valid paths from the graph.
// A path starts at a depth-1 node and follows edges to deeper nodes.
// Also includes single-node paths (depth=1 only).
func EnumeratePaths(graph DecisionGraph) []DecisionPath {
	// Build adjacency list from edges.
	adj := make(map[string][]string)
	for _, e := range graph.Edges {
		adj[e.From] = append(adj[e.From], e.To)
	}

	// Index nodes by ID.
	nodeIndex := make(map[string]DecisionNode, len(graph.Nodes))
	for _, n := range graph.Nodes {
		nodeIndex[n.ID] = n
	}

	// Find all depth-1 nodes (roots).
	var roots []DecisionNode
	for _, n := range graph.Nodes {
		if isDepth1(n.ID) {
			roots = append(roots, n)
		}
	}

	// DFS from each root to enumerate all paths.
	var paths []DecisionPath
	for _, root := range roots {
		var currentPath []DecisionNode
		currentPath = append(currentPath, root)
		paths = enumerateFrom(currentPath, adj, nodeIndex, paths)
	}

	return paths
}

// enumerateFrom recursively builds paths from the current node.
func enumerateFrom(current []DecisionNode, adj map[string][]string, nodeIndex map[string]DecisionNode, paths []DecisionPath) []DecisionPath {
	last := current[len(current)-1]
	children := adj[last.ID]

	if len(children) == 0 {
		// Leaf: emit path.
		pathCopy := make([]DecisionNode, len(current))
		copy(pathCopy, current)
		paths = append(paths, DecisionPath{Nodes: pathCopy})
		return paths
	}

	// Always include the current path as a valid option (stop early).
	earlyStop := make([]DecisionNode, len(current))
	copy(earlyStop, current)
	paths = append(paths, DecisionPath{Nodes: earlyStop})

	for _, childID := range children {
		child, ok := nodeIndex[childID]
		if !ok {
			continue
		}
		extended := make([]DecisionNode, len(current), len(current)+1)
		copy(extended, current)
		extended = append(extended, child)
		paths = enumerateFrom(extended, adj, nodeIndex, paths)
	}

	return paths
}

// isDepth1 checks if a node ID ends with ":1".
func isDepth1(id string) bool {
	return len(id) >= 2 && id[len(id)-2:] == ":1"
}

// defaultTransitions returns valid action-to-action transitions per goal type.
// These define the edges in the decision graph.
func defaultTransitions(goalType string) map[string][]string {
	switch goalType {
	case "reduce_retry_rate", "investigate_failed_jobs":
		return map[string][]string{
			"retry_job":          {"log_recommendation"},
			"log_recommendation": {"retry_job"},
			"noop":               {},
		}
	case "resolve_queue_backlog":
		return map[string][]string{
			"trigger_resync":     {"log_recommendation"},
			"log_recommendation": {"trigger_resync"},
			"noop":               {},
		}
	default:
		return map[string][]string{
			"log_recommendation": {},
			"noop":               {},
		}
	}
}
