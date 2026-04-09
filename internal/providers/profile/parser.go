package profile

import (
	"fmt"
)

// ParseProfile is disabled. The DSL profile format has been removed.
// All execution configuration must be defined in the provider catalog YAML
// under models[].execution and referenced via execution_profiles[role][N].ref.
//
// If this function is called, the system has a configuration error.
// Fail-fast: return an explicit error so the operator can identify and fix the issue.
func ParseProfile(_ string) ([]ModelCandidate, error) {
	return nil, fmt.Errorf(
		"DSL profile parsing is disabled: define execution settings in " +
			"providers/<name>.yaml under models[].execution and reference models " +
			"via execution_profiles[role][N].ref — see RUNBOOK.md for migration guide")
}

// ParseProfileOrSingle is disabled. See ParseProfile for details.
// Backward-compatible entry point kept to produce a clear error on migration.
func ParseProfileOrSingle(s string) ([]ModelCandidate, error) {
	return nil, fmt.Errorf(
		"DSL profile parsing is disabled (attempted to parse %q): define execution "+
			"settings in providers/<name>.yaml under models[].execution — "+
			"see RUNBOOK.md for migration guide", s)
}
