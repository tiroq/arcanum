package contracts_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tiroq/arcanum/internal/contracts/subjects"
)

// TestNoHardcodedSubjectsOutsidePackage scans all Go source files for hardcoded
// NATS subject strings (containing "runeforge.") outside the subjects package.
func TestNoHardcodedSubjectsOutsidePackage(t *testing.T) {
	repoRoot := findRepoRoot(t)

	subjectsDir := filepath.Join(repoRoot, "internal", "contracts", "subjects")
	// Skip the entire contracts directory (subjects package + this enforcement test itself)
	contractsDir := filepath.Join(repoRoot, "internal", "contracts")

	var violations []string

	err := filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip non-Go files
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip the subjects package, the enforcement test itself, and vendor directory
		absPath, _ := filepath.Abs(path)
		absSubjectsDir, _ := filepath.Abs(subjectsDir)
		absContractsDir, _ := filepath.Abs(contractsDir)
		if strings.HasPrefix(absPath, absSubjectsDir) {
			return nil
		}
		// Skip the enforcement_test.go (and any other files directly in contracts)
		if strings.HasPrefix(absPath, absContractsDir+string(filepath.Separator)) &&
			!strings.Contains(absPath[len(absContractsDir):], string(filepath.Separator)+string(filepath.Separator)) {
			// Check if file is directly in contracts dir (not in a subdir)
			rel := absPath[len(absContractsDir):]
			if strings.Count(rel, string(filepath.Separator)) == 1 {
				return nil
			}
		}
		if strings.Contains(path, "/vendor/") {
			return nil
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			// Look for string literals containing any known subject prefix
			if strings.Contains(line, `"runeforge.`) {
				violations = append(violations, fmt.Sprintf("%s:%d: %s", path, i+1, strings.TrimSpace(line)))
			}
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Empty(t, violations,
		"hardcoded NATS subject strings found outside the subjects package. "+
			"Import subjects.SubjectXxx constants instead:\n%s",
		strings.Join(violations, "\n"))

	// Ensure AllSubjects is non-empty (sanity check)
	assert.NotEmpty(t, subjects.AllSubjects, "AllSubjects must not be empty")
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	assert.NoError(t, err)

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
