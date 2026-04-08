package provider_catalog

import "fmt"

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
