package goal_planning

// DependencyGraph validates and queries dependency relationships between subgoals.
type DependencyGraph struct {
	edges map[string][]string // from → [to, ...]
}

// NewDependencyGraph builds a graph from a set of dependencies.
func NewDependencyGraph(deps []GoalDependency) *DependencyGraph {
	g := &DependencyGraph{edges: make(map[string][]string)}
	for _, d := range deps {
		g.edges[d.FromSubgoalID] = append(g.edges[d.FromSubgoalID], d.ToSubgoalID)
	}
	return g
}

// HasCycle returns true if the dependency graph contains a cycle.
func (g *DependencyGraph) HasCycle() bool {
	visited := make(map[string]int) // 0=unvisited, 1=in-stack, 2=done
	for node := range g.edges {
		if visited[node] == 0 && g.dfs(node, visited) {
			return true
		}
	}
	return false
}

func (g *DependencyGraph) dfs(node string, visited map[string]int) bool {
	visited[node] = 1 // in-stack
	for _, neighbor := range g.edges[node] {
		if visited[neighbor] == 1 {
			return true // back edge = cycle
		}
		if visited[neighbor] == 0 && g.dfs(neighbor, visited) {
			return true
		}
	}
	visited[node] = 2 // done
	return false
}

// TopologicalSort returns subgoal IDs in dependency order (prerequisites first).
// Returns nil if the graph has a cycle.
func (g *DependencyGraph) TopologicalSort(subgoalIDs []string) []string {
	if g.HasCycle() {
		return nil
	}

	// Collect all nodes.
	nodeSet := make(map[string]bool, len(subgoalIDs))
	for _, id := range subgoalIDs {
		nodeSet[id] = true
	}

	visited := make(map[string]bool)
	var order []string

	var visit func(string)
	visit = func(node string) {
		if visited[node] || !nodeSet[node] {
			return
		}
		visited[node] = true
		for _, dep := range g.edges[node] {
			visit(dep)
		}
		order = append(order, node)
	}

	for _, id := range subgoalIDs {
		visit(id)
	}

	// Reverse for topological order (dependencies first).
	for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
		order[i], order[j] = order[j], order[i]
	}
	return order
}

// Depth returns the maximum dependency chain depth for a given subgoal.
func (g *DependencyGraph) Depth(subgoalID string) int {
	return g.depthDFS(subgoalID, make(map[string]bool))
}

func (g *DependencyGraph) depthDFS(node string, visited map[string]bool) int {
	if visited[node] {
		return 0 // cycle guard
	}
	visited[node] = true
	maxChild := 0
	for _, child := range g.edges[node] {
		d := g.depthDFS(child, visited)
		if d > maxChild {
			maxChild = d
		}
	}
	visited[node] = false
	return maxChild + 1
}

// Dependents returns the direct dependents of a subgoal.
func (g *DependencyGraph) Dependents(subgoalID string) []string {
	return g.edges[subgoalID]
}

// Prerequisites returns all subgoal IDs that the given subgoal depends on.
func (g *DependencyGraph) Prerequisites(subgoalID string) []string {
	var prereqs []string
	for from, tos := range g.edges {
		for _, to := range tos {
			if to == subgoalID {
				prereqs = append(prereqs, from)
			}
		}
	}
	return prereqs
}

// ValidateMaxDepth checks that no dependency chain exceeds MaxDepth.
func (g *DependencyGraph) ValidateMaxDepth(subgoalIDs []string) bool {
	for _, id := range subgoalIDs {
		if g.Depth(id) > MaxDepth {
			return false
		}
	}
	return true
}
