package provider_catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// ValidKinds are the accepted provider kinds.
var ValidKinds = map[string]bool{
	"local":  true,
	"cloud":  true,
	"router": true,
}

// ValidCostClasses are the accepted cost classifications.
var ValidCostClasses = map[string]bool{
	"free":    true,
	"local":   true,
	"cheap":   true,
	"unknown": true,
}

// ValidateCatalogEntry checks a parsed catalog file for required fields
// and semantic correctness. Returns a slice of human-readable errors.
// An empty slice means the entry is valid.
// Deprecated: Use ValidateCatalogEntryStructured for structured output.
func ValidateCatalogEntry(entry ProviderCatalogFile, filename string) []string {
	var errs []string

	// Provider section.
	if entry.Provider.Name == "" {
		errs = append(errs, fmt.Sprintf("%s: provider.name is required", filename))
	}
	if entry.Provider.Kind == "" {
		errs = append(errs, fmt.Sprintf("%s: provider.kind is required", filename))
	} else if !ValidKinds[entry.Provider.Kind] {
		errs = append(errs, fmt.Sprintf("%s: provider.kind %q is not valid (local|cloud|router)", filename, entry.Provider.Kind))
	}

	// Models section — at least one model required when the provider is enabled.
	if entry.Provider.Enabled && len(entry.Models) == 0 {
		errs = append(errs, fmt.Sprintf("%s: at least one model is required when provider is enabled", filename))
	}

	// Check for duplicate model names.
	modelNames := make(map[string]bool)
	for i, m := range entry.Models {
		if m.Name == "" {
			errs = append(errs, fmt.Sprintf("%s: models[%d].name is required", filename, i))
			continue
		}
		if modelNames[m.Name] {
			errs = append(errs, fmt.Sprintf("%s: duplicate model name %q", filename, m.Name))
		}
		modelNames[m.Name] = true

		// Validate cost class.
		if m.CostClass != "" && !ValidCostClasses[m.CostClass] {
			errs = append(errs, fmt.Sprintf("%s: models[%d].cost_class %q is not valid (free|local|cheap|unknown)", filename, i, m.CostClass))
		}

		// Validate relative cost range.
		if m.RelativeCost < 0 || m.RelativeCost > 1 {
			errs = append(errs, fmt.Sprintf("%s: models[%d].relative_cost must be 0.0–1.0, got %.2f", filename, i, m.RelativeCost))
		}
	}

	// Check model count bound.
	if len(entry.Models) > MaxModelsPerProvider {
		errs = append(errs, fmt.Sprintf("%s: too many models (%d > max %d)", filename, len(entry.Models), MaxModelsPerProvider))
	}

	return errs
}

// --- Structured Validation API ---

// ValidateCatalogDir validates all YAML files in the given directory.
// Returns a ValidationResult and an error only for I/O failures.
func ValidateCatalogDir(dir string) (ValidationResult, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return ValidationResult{Valid: true}, nil
		}
		return ValidationResult{}, fmt.Errorf("stat catalog directory: %w", err)
	}
	if !info.IsDir() {
		return ValidationResult{}, fmt.Errorf("catalog path is not a directory: %s", dir)
	}

	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return ValidationResult{}, fmt.Errorf("read catalog directory: %w", err)
	}

	// Collect YAML file paths in deterministic order.
	var files []string
	for _, e := range dirEntries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext == ".yaml" || ext == ".yml" {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)

	return ValidateCatalogFiles(files)
}

// ValidateCatalogFiles validates a list of YAML file paths.
// Returns a ValidationResult and an error only for I/O failures.
func ValidateCatalogFiles(files []string) (ValidationResult, error) {
	var allIssues []ValidationIssue
	var validEntries []ProviderCatalogFile
	var validFilenames []string

	for _, path := range files {
		filename := filepath.Base(path)

		data, err := os.ReadFile(path)
		if err != nil {
			return ValidationResult{}, fmt.Errorf("read file %s: %w", filename, err)
		}

		var entry ProviderCatalogFile
		if err := yaml.Unmarshal(data, &entry); err != nil {
			allIssues = append(allIssues, ValidationIssue{
				File:     filename,
				Code:     "yaml_parse_error",
				Message:  fmt.Sprintf("YAML parse error: %v", err),
				Severity: SeverityError,
			})
			continue
		}

		// Skip non-provider files (e.g. defaults.yaml with catalog: root).
		if entry.Provider.Name == "" && entry.Provider.Kind == "" && len(entry.Models) == 0 {
			continue
		}

		// Per-entry validation.
		entryIssues := validateSingleEntry(entry, filename)
		allIssues = append(allIssues, entryIssues...)

		validEntries = append(validEntries, entry)
		validFilenames = append(validFilenames, filename)
	}

	// Cross-file validation.
	crossIssues := validateCrossFile(validEntries, validFilenames)
	allIssues = append(allIssues, crossIssues...)

	return buildResult(allIssues), nil
}

// ValidateCatalogEntryStructured validates a single catalog entry
// and returns structured validation issues.
func ValidateCatalogEntryStructured(entry ProviderCatalogFile, filename string) ValidationResult {
	issues := validateSingleEntry(entry, filename)
	return buildResult(issues)
}

// ValidateCatalog validates entries already loaded in a CatalogRegistry
// plus the raw catalog files for cross-file checks.
func ValidateCatalog(entries []ProviderCatalogFile, filenames []string) ValidationResult {
	var allIssues []ValidationIssue

	for i, entry := range entries {
		filename := ""
		if i < len(filenames) {
			filename = filenames[i]
		} else {
			filename = fmt.Sprintf("entry[%d]", i)
		}
		allIssues = append(allIssues, validateSingleEntry(entry, filename)...)
	}

	allIssues = append(allIssues, validateCrossFile(entries, filenames)...)
	return buildResult(allIssues)
}

// buildResult constructs a ValidationResult from issues.
func buildResult(issues []ValidationIssue) ValidationResult {
	sortIssues(issues)

	errorCount := 0
	warningCount := 0
	for _, issue := range issues {
		switch issue.Severity {
		case SeverityError:
			errorCount++
		case SeverityWarning:
			warningCount++
		}
	}

	return ValidationResult{
		Valid:        errorCount == 0,
		ErrorCount:   errorCount,
		WarningCount: warningCount,
		Issues:       issues,
	}
}
