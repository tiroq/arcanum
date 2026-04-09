package goals

import (
	"os"
	"path/filepath"
	"testing"
)

// -----------------------------------------------------------------------------
// Test helpers
// -----------------------------------------------------------------------------

func writeGoalsFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "system_goals.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// -----------------------------------------------------------------------------
// 4.1 Goal Loader tests
// -----------------------------------------------------------------------------

// Valid single-document YAML with two goals.
const validYAML = `
goals:
  - id: income_goal
    type: income
    priority: 0.95
    description: Grow income
    preferred_actions:
      - propose_income_action
    signals:
      - new_opportunities

  - id: family_goal
    type: safety
    priority: 1.0
    description: Keep family safe
    constraints:
      forbid_actions:
        - unsafe_external_calls
`

func TestLoadSystemGoals_Valid(t *testing.T) {
	path := writeGoalsFile(t, validYAML)
	sg, err := LoadSystemGoals(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sg.Goals) != 2 {
		t.Fatalf("expected 2 goals, got %d", len(sg.Goals))
	}
	// Deterministic order: priority DESC → family (1.0) first, income (0.95) second.
	if sg.Goals[0].ID != "family_goal" {
		t.Errorf("expected family_goal first, got %q", sg.Goals[0].ID)
	}
	if sg.Goals[1].ID != "income_goal" {
		t.Errorf("expected income_goal second, got %q", sg.Goals[1].ID)
	}
}

func TestLoadSystemGoals_MultiDocument(t *testing.T) {
	// Simulates the real system_goals.yaml format where --- separates documents.
	content := `goals:
  - id: goal_one
    type: safety
    priority: 1.0
    description: First goal
---
  - id: goal_two
    type: income
    priority: 0.9
    description: Second goal
`
	path := writeGoalsFile(t, content)
	sg, err := LoadSystemGoals(path)
	if err != nil {
		t.Fatalf("unexpected error loading multi-document YAML: %v", err)
	}
	if len(sg.Goals) != 2 {
		t.Fatalf("expected 2 goals from multi-doc YAML, got %d", len(sg.Goals))
	}
}

func TestLoadSystemGoals_RealFile(t *testing.T) {
	// Integration test against the real configs/system_goals.yaml.
	repoRoot := findRepoRoot(t)
	path := filepath.Join(repoRoot, "configs", "system_goals.yaml")
	sg, err := LoadSystemGoals(path)
	if err != nil {
		t.Fatalf("failed to load real system_goals.yaml: %v", err)
	}
	if len(sg.Goals) == 0 {
		t.Fatal("expected at least one goal from real file")
	}
	// Highest-priority goal should be family_stability (priority=1.0).
	if sg.Goals[0].ID != "family_stability" {
		t.Errorf("expected family_stability as top goal, got %q", sg.Goals[0].ID)
	}
}

func TestLoadSystemGoals_MissingFile(t *testing.T) {
	_, err := LoadSystemGoals("/nonexistent/system_goals.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadSystemGoals_DuplicateIDs(t *testing.T) {
	content := `
goals:
  - id: dup
    type: income
    priority: 0.5
    description: First
  - id: dup
    type: safety
    priority: 0.6
    description: Second
`
	path := writeGoalsFile(t, content)
	_, err := LoadSystemGoals(path)
	if err == nil {
		t.Fatal("expected error for duplicate IDs, got nil")
	}
}

func TestLoadSystemGoals_InvalidPriority(t *testing.T) {
	content := `
goals:
  - id: bad_priority
    type: income
    priority: 1.5
    description: Priority > 1
`
	path := writeGoalsFile(t, content)
	_, err := LoadSystemGoals(path)
	if err == nil {
		t.Fatal("expected error for priority > 1, got nil")
	}
}

func TestLoadSystemGoals_NegativePriority(t *testing.T) {
	content := `
goals:
  - id: neg_priority
    type: income
    priority: -0.1
    description: Negative priority
`
	path := writeGoalsFile(t, content)
	_, err := LoadSystemGoals(path)
	if err == nil {
		t.Fatal("expected error for negative priority, got nil")
	}
}

func TestLoadSystemGoals_EmptyID(t *testing.T) {
	content := `
goals:
  - id: ""
    type: income
    priority: 0.5
    description: No ID
`
	path := writeGoalsFile(t, content)
	_, err := LoadSystemGoals(path)
	if err == nil {
		t.Fatal("expected error for empty ID, got nil")
	}
}

func TestLoadSystemGoals_EmptyGoals(t *testing.T) {
	content := `goals: []`
	path := writeGoalsFile(t, content)
	sg, err := LoadSystemGoals(path)
	if err != nil {
		t.Fatalf("unexpected error for empty goals: %v", err)
	}
	if len(sg.Goals) != 0 {
		t.Errorf("expected 0 goals, got %d", len(sg.Goals))
	}
}

func TestLoadSystemGoals_DeterministicOrdering(t *testing.T) {
	// Same priority → alphabetical ID ordering.
	content := `
goals:
  - id: zebra
    type: income
    priority: 0.5
    description: Last alphabetically
  - id: apple
    type: safety
    priority: 0.5
    description: First alphabetically
`
	path := writeGoalsFile(t, content)
	sg, err := LoadSystemGoals(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sg.Goals[0].ID != "apple" {
		t.Errorf("expected apple first (same priority, alpha), got %q", sg.Goals[0].ID)
	}
}

// findRepoRoot walks up from the test file to find the module root (go.mod).
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod not found)")
		}
		dir = parent
	}
}
