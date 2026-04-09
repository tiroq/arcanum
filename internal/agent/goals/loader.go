package goals

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// SystemGoal represents a strategic directive loaded from system_goals.yaml.
// Distinct from Goal (runtime advisory) — these are persistent high-level intentions.
type SystemGoal struct {
	ID               string                 `yaml:"id"              json:"id"`
	Type             string                 `yaml:"type"            json:"type"`
	Priority         float64                `yaml:"priority"        json:"priority"`
	Horizon          string                 `yaml:"horizon"         json:"horizon,omitempty"`
	Description      string                 `yaml:"description"     json:"description"`
	SuccessMetrics   []Metric               `yaml:"success_metrics" json:"success_metrics,omitempty"`
	Signals          []string               `yaml:"signals"         json:"signals,omitempty"`
	PreferredActions []string               `yaml:"preferred_actions" json:"preferred_actions,omitempty"`
	Constraints      map[string]interface{} `yaml:"constraints"     json:"constraints,omitempty"`
}

// Metric defines a measurable success criterion for a SystemGoal.
type Metric struct {
	Name   string `yaml:"name"   json:"name"`
	Target string `yaml:"target" json:"target"`
}

// SystemGoals is the top-level container loaded from system_goals.yaml.
type SystemGoals struct {
	Goals []SystemGoal `yaml:"goals" json:"goals"`
}

// LoadSystemGoals reads and validates the system goals YAML file.
//
// The file may be a multi-document YAML (documents separated by ---).
// Document 1 must be the top-level mapping with a `goals:` key.
// Subsequent documents are treated as additional goal list items.
//
// Rules:
//   - Fails if the file is missing or unreadable.
//   - Fails if any goal has an empty ID.
//   - Fails if any goal has priority outside [0, 1].
//   - Fails if any two goals share the same ID.
//   - Returns goals sorted deterministically: priority DESC, id ASC.
func LoadSystemGoals(path string) (*SystemGoals, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read system goals %q: %w", path, err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))

	// Document 1: top-level mapping with goals: [...].
	var firstDoc struct {
		Goals []SystemGoal `yaml:"goals"`
	}
	if err := dec.Decode(&firstDoc); err != nil {
		return nil, fmt.Errorf("parse system goals (doc 1) %q: %w", path, err)
	}

	sg := &SystemGoals{Goals: firstDoc.Goals}

	// Subsequent documents: each is a YAML sequence (list) of goal items.
	for {
		var additional []SystemGoal
		err := dec.Decode(&additional)
		if err == io.EOF {
			break
		}
		if err != nil {
			// Best-effort: skip unparseable continuation documents.
			// This preserves backward compatibility if the YAML format changes.
			break
		}
		sg.Goals = append(sg.Goals, additional...)
	}

	if err := validateSystemGoals(sg); err != nil {
		return nil, err
	}

	// Deterministic ordering: priority DESC, then id ASC (tie-break).
	sort.SliceStable(sg.Goals, func(i, j int) bool {
		a, b := sg.Goals[i], sg.Goals[j]
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		return a.ID < b.ID
	})

	return sg, nil
}

// validateSystemGoals checks all goals for required fields and uniqueness.
func validateSystemGoals(sg *SystemGoals) error {
	seen := make(map[string]struct{}, len(sg.Goals))
	for i, g := range sg.Goals {
		if g.ID == "" {
			return fmt.Errorf("system goal at index %d: id must not be empty", i)
		}
		if g.Priority < 0 || g.Priority > 1 {
			return fmt.Errorf("system goal %q: priority %.4f is outside [0, 1]", g.ID, g.Priority)
		}
		if _, dup := seen[g.ID]; dup {
			return fmt.Errorf("system goal %q: duplicate id", g.ID)
		}
		seen[g.ID] = struct{}{}
	}
	return nil
}
