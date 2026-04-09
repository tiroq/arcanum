package provider_catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// LoadCatalog reads all YAML files from the given directory and returns
// the parsed provider catalog entries in deterministic (filename-sorted) order.
// Returns nil, nil if the directory does not exist (fail-open).
// Returns an error only for I/O failures reading valid files.
func LoadCatalog(dir string, logger *zap.Logger) ([]ProviderCatalogFile, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			if logger != nil {
				logger.Info("provider catalog directory not found, skipping",
					zap.String("dir", dir))
			}
			return nil, nil
		}
		return nil, fmt.Errorf("stat catalog directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("catalog path is not a directory: %s", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read catalog directory: %w", err)
	}

	// Collect YAML files in deterministic order.
	// Underscore-prefixed files (e.g. _global.yaml) are meta/config files
	// and must NOT be parsed as ProviderCatalogFile entries — they use a
	// different schema and are loaded via dedicated functions (LoadGlobalPolicy).
	var yamlFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), "_") {
			continue // skip meta files
		}
		ext := filepath.Ext(e.Name())
		if ext == ".yaml" || ext == ".yml" {
			yamlFiles = append(yamlFiles, e.Name())
		}
	}
	sort.Strings(yamlFiles)

	var catalogs []ProviderCatalogFile
	for _, name := range yamlFiles {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read catalog file %s: %w", name, err)
		}

		var entry ProviderCatalogFile
		if err := yaml.Unmarshal(data, &entry); err != nil {
			if logger != nil {
				logger.Warn("skipping invalid catalog file",
					zap.String("file", name),
					zap.Error(err))
			}
			continue
		}

		// Validate and skip invalid entries.
		if errs := ValidateCatalogEntry(entry, name); len(errs) > 0 {
			if logger != nil {
				for _, ve := range errs {
					logger.Warn("catalog validation error",
						zap.String("file", name),
						zap.String("error", ve))
				}
			}
			continue
		}

		catalogs = append(catalogs, entry)
	}

	return catalogs, nil
}
