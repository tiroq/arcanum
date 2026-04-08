package decision_graph

// SelectBestPath selects the optimal path from scored paths.
//
// Rules:
//  1. Highest FinalScore wins.
//  2. Tie: shorter path wins.
//  3. If all paths have FinalScore <= 0: fallback path (noop single node).
//  4. Exploration: deterministic toggle selects second-best path.
//
// Deterministic: same inputs always produce the same selection.
func SelectBestPath(paths []DecisionPath, config GraphConfig) PathSelection {
	result := PathSelection{
		Paths: paths,
	}

	if len(paths) == 0 {
		result.Reason = "no_paths"
		return result
	}

	// Sort by FinalScore DESC, length ASC, first action type ASC.
	sortPaths(paths)

	// Filter: collect viable paths (FinalScore > 0 or single-node noop).
	var viable []DecisionPath
	for _, p := range paths {
		if p.FinalScore > 0 || isNoopPath(p) {
			viable = append(viable, p)
		}
	}

	if len(viable) == 0 {
		// Fallback: find any noop path.
		for i := range paths {
			if isNoopPath(paths[i]) {
				result.Selected = &paths[i]
				result.Reason = "all_rejected_fallback_noop"
				return result
			}
		}
		result.Reason = "all_rejected_no_fallback"
		return result
	}

	best := viable[0]
	result.Reason = "highest_final_score"

	// Tie-breaker: if top two are very close, prefer shorter.
	if len(viable) > 1 {
		second := viable[1]
		diff := best.FinalScore - second.FinalScore
		if diff < 0.001 && len(second.Nodes) < len(best.Nodes) {
			best = second
			result.Reason = "shorter_path_tiebreak"
		}
	}

	result.Selected = &best

	// Exploration override: deterministic toggle selects second-best.
	if config.ShouldExplore && len(viable) > 1 {
		secondBest := findSecondBestPath(viable, best)
		if secondBest != nil && secondBest.FinalScore > 0 {
			result.ExplorationPick = secondBest
			result.Selected = secondBest
			result.ExplorationUsed = true
			result.Reason = "exploration_override_second_best"
		}
	}

	return result
}

// findSecondBestPath returns the best path that differs from the selected one.
func findSecondBestPath(viable []DecisionPath, selected DecisionPath) *DecisionPath {
	for i := range viable {
		if !samePath(viable[i], selected) {
			return &viable[i]
		}
	}
	return nil
}

// samePath checks if two paths have identical node sequences.
func samePath(a, b DecisionPath) bool {
	if len(a.Nodes) != len(b.Nodes) {
		return false
	}
	for i := range a.Nodes {
		if a.Nodes[i].ID != b.Nodes[i].ID {
			return false
		}
	}
	return true
}

// isNoopPath checks if a path is a single-node noop.
func isNoopPath(p DecisionPath) bool {
	return len(p.Nodes) == 1 && p.Nodes[0].ActionType == "noop"
}

// sortPaths sorts by FinalScore DESC, path length ASC, first action type ASC.
// Uses insertion sort for deterministic, stable ordering.
func sortPaths(paths []DecisionPath) {
	for i := 1; i < len(paths); i++ {
		for j := i; j > 0; j-- {
			if shouldSwapPath(paths[j], paths[j-1]) {
				paths[j], paths[j-1] = paths[j-1], paths[j]
			} else {
				break
			}
		}
	}
}

// shouldSwapPath returns true if a should rank above b.
func shouldSwapPath(a, b DecisionPath) bool {
	if a.FinalScore != b.FinalScore {
		return a.FinalScore > b.FinalScore
	}
	if len(a.Nodes) != len(b.Nodes) {
		return len(a.Nodes) < len(b.Nodes)
	}
	if len(a.Nodes) > 0 && len(b.Nodes) > 0 {
		return a.Nodes[0].ActionType < b.Nodes[0].ActionType
	}
	return false
}
