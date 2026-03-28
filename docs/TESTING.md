# Testing Guide

Runeforge uses Go's standard `testing` package with `testify/assert` and `testify/require` for assertions. No external test runner is needed.

---

## Running Tests

```bash
# All tests
make test

# With verbose output (see individual test names)
make test-verbose

# With coverage report (generates coverage.html)
make test-coverage

# Single package
go test ./internal/prompts/...

# Single test
go test -run TestTemplateLoader_Render ./internal/prompts/...
```

---

## Test Categories

### Unit Tests

Test a single function or type in isolation. Dependencies are replaced with fakes or in-memory implementations. **No Docker, no network, no filesystem side-effects.**

Location: `*_test.go` files alongside the code they test.

Examples:
- `internal/prompts/loader_test.go` — template loading and rendering
- `internal/config/config_test.go` — configuration validation
- `internal/processors/processors_test.go` — processor logic with mock LLM
- `internal/providers/providers_test.go` — provider serialization/deserialization
- `internal/source/source_test.go` — change detection and hashing

**Key patterns:**
- Use `t.TempDir()` for filesystem tests; cleaned up automatically.
- Use `httptest.NewServer` for HTTP-based provider tests.
- Use interface mocks for the `Provider` and `Connector` interfaces.

---

### Integration Tests

Test a service against real dependencies (PostgreSQL, NATS). These require the infrastructure to be running.

```bash
# Start infra first
make docker-infra

# Then run integration tests (tagged)
go test -tags integration ./...
```

Integration tests are gated behind the `//go:build integration` build tag to prevent them from running in CI without infrastructure.

Location: `*_integration_test.go` files, or `*_test.go` with the integration build tag.

---

### Contract / Enforcement Tests

Verify that message contracts are consistently defined across the codebase. These are unit tests that run without any external dependencies.

Location: `internal/contracts/enforcement_test.go`

**What they check:**
- Every subject in `subjects.AllSubjects` matches a subject constant in `subjects.go`
- No subject string literals appear outside of `subjects/subjects.go` (checked via AST walk or grep)
- Event structs have the `Version` field

Run with:
```bash
go test ./internal/contracts/...
```

---

### Provider Tests

Test LLM provider implementations against a local mock HTTP server.

Location: `internal/providers/providers_test.go`

These tests use `httptest.NewServer` to simulate OpenAI-compatible API responses and verify:
- Correct request serialization
- Response deserialization
- Error handling (timeouts, non-200 status codes)

---

### Source Tests

Test the source change detection pipeline.

Location: `internal/source/source_test.go`

These verify:
- `ContentHasher` produces stable, consistent hashes
- `ChangeDetector` correctly identifies new, changed, and unchanged tasks
- Deduplication logic

---

### End-to-End Tests (E2E)

Not yet implemented. When added, E2E tests will:
1. Start all services via Docker Compose
2. Create a source connection via the API
3. Inject a mock task event into NATS
4. Assert that a proposal is created within a timeout
5. Approve the proposal via the API
6. Assert that a writeback operation is created

Location (future): `test/e2e/`

---

## Writing New Tests

### Unit test skeleton

```go
package mypackage_test

import (
    "testing"
    "github.com/stretchr/testify/require"
    "github.com/tiroq/arcanum/internal/mypackage"
)

func TestMyFeature(t *testing.T) {
    t.Run("success case", func(t *testing.T) {
        result, err := mypackage.DoThing("input")
        require.NoError(t, err)
        require.Equal(t, "expected", result)
    })

    t.Run("error case", func(t *testing.T) {
        _, err := mypackage.DoThing("")
        require.Error(t, err)
    })
}
```

### Mocking the LLM provider

```go
type mockProvider struct {
    response string
    err      error
}

func (m *mockProvider) Complete(_ context.Context, _ providers.CompletionRequest) (providers.CompletionResponse, error) {
    return providers.CompletionResponse{Content: m.response}, m.err
}

func (m *mockProvider) Name() string { return "mock" }
```

---

## CI

Tests run automatically on every push via GitHub Actions. The CI matrix runs:
1. `go vet ./...`
2. `go test ./...` (unit + contract tests only, no integration tag)
3. Build all services: `go build ./...`

Integration tests require infrastructure and are run separately.
