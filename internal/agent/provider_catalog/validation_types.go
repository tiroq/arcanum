package provider_catalog

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ValidationSeverity indicates whether a validation finding is an error or a warning.
type ValidationSeverity string

const (
	SeverityError   ValidationSeverity = "error"
	SeverityWarning ValidationSeverity = "warning"
)

// ValidationIssue represents a single validation finding.
type ValidationIssue struct {
	File     string             `json:"file"`
	Provider string             `json:"provider,omitempty"`
	Model    string             `json:"model,omitempty"`
	Field    string             `json:"field,omitempty"`
	Code     string             `json:"code"`
	Message  string             `json:"message"`
	Severity ValidationSeverity `json:"severity"`
}

// ValidationResult contains the full outcome of catalog validation.
type ValidationResult struct {
	Valid        bool              `json:"valid"`
	ErrorCount   int               `json:"error_count"`
	WarningCount int               `json:"warning_count"`
	Issues       []ValidationIssue `json:"issues"`
}

// JSON returns the validation result as a JSON string.
func (r ValidationResult) JSON() string {
	data, _ := json.MarshalIndent(r, "", "  ")
	return string(data)
}

// Text returns a human-readable summary of the validation result.
func (r ValidationResult) Text() string {
	var sb strings.Builder

	if r.Valid {
		sb.WriteString("Provider catalog validation: VALID\n")
	} else {
		sb.WriteString("Provider catalog validation: INVALID\n")
	}
	sb.WriteString(fmt.Sprintf("  Errors:   %d\n", r.ErrorCount))
	sb.WriteString(fmt.Sprintf("  Warnings: %d\n", r.WarningCount))

	if len(r.Issues) == 0 {
		return sb.String()
	}

	sb.WriteString("\nIssues:\n")
	for _, issue := range r.Issues {
		severity := "ERROR"
		if issue.Severity == SeverityWarning {
			severity = "WARN "
		}
		location := issue.File
		if issue.Provider != "" {
			location += " > " + issue.Provider
		}
		if issue.Model != "" {
			location += " > " + issue.Model
		}
		if issue.Field != "" {
			location += " > " + issue.Field
		}
		sb.WriteString(fmt.Sprintf("  [%s] %s: %s (%s)\n", severity, location, issue.Message, issue.Code))
	}

	return sb.String()
}

// sortIssues sorts issues deterministically by file, provider, model, field, code.
func sortIssues(issues []ValidationIssue) {
	sort.SliceStable(issues, func(i, j int) bool {
		a, b := issues[i], issues[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		return a.Code < b.Code
	})
}

// ValidRoles are the recognized provider/model role values.
var ValidRoles = map[string]bool{
	"fast":     true,
	"planner":  true,
	"reviewer": true,
	"batch":    true,
	"fallback": true,
}

// ValidCapabilities are the recognized model capability values.
var ValidCapabilities = map[string]bool{
	"json_mode":         true,
	"long_context":      true,
	"low_latency":       true,
	"tool_calling":      true,
	"structured_output": true,
}

// CriticalRoles are roles that must be covered by at least one enabled model in the catalog.
var CriticalRoles = []string{"fast", "planner", "fallback"}

// envVarNamePattern matches valid environment variable name patterns.
// Letters, digits, and underscores, starting with a letter or underscore.
var envVarNamePattern = "^[A-Za-z_][A-Za-z0-9_]*$"
