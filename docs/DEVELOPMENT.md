# Development Guide

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| Go | 1.22+ | Backend services |
| Docker | 24+ | Infrastructure and containerization |
| Docker Compose | v2.20+ | Local orchestration |
| Node.js | 20+ | Admin web (optional) |
| `golangci-lint` | latest | Linting (optional) |
| `jq` | any | Health check output formatting |

---

## Local Setup

### 1. Clone and configure

```bash
git clone https://github.com/tiroq/arcanum
cd arcanum
cp .env.example .env
```

Edit `.env` with your values. At minimum:
```env
ADMIN_TOKEN=your-local-dev-token
DATABASE_DSN=postgres://runeforge:runeforge@localhost:5432/runeforge?sslmode=disable
NATS_URL=nats://localhost:4222
# Optional: set one of these for real LLM calls
OPENAI_API_KEY=sk-...
OLLAMA_BASE_URL=http://localhost:11434
```

### 2. Start infrastructure

```bash
make docker-infra
```

This starts PostgreSQL and NATS only. Waits for health checks.

### 3. Run database migrations

```bash
make migrate-up
```

### 4. Run services

In separate terminals:

```bash
make dev-api          # api-gateway on :8080
make dev-sync         # source-sync on :8081
make dev-orchestrator # orchestrator on :8082
make dev-worker       # worker on :8083 (uses ./prompts/)
```

### 5. Verify

```bash
make health
# or
curl http://localhost:8080/health
```

---

## Running Tests

```bash
make test              # All tests
make test-verbose      # With output
make test-coverage     # With HTML coverage report
```

Tests use `t.TempDir()` and in-process fakes — no Docker required for unit tests.

See [TESTING.md](TESTING.md) for details on each test category.

---

## Configuration

All configuration is via environment variables. See `internal/config/config.go` for the full list.

Key variables by service:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DATABASE_DSN` | Yes | — | Postgres connection string |
| `ADMIN_TOKEN` | Yes | — | API authentication token |
| `NATS_URL` | No | `nats://localhost:4222` | NATS server URL |
| `HTTP_PORT` | No | `8080` | HTTP listen port |
| `LOG_LEVEL` | No | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | No | `json` | `json` or `console` |
| `OPENAI_API_KEY` | No | — | Required if using OpenAI provider |
| `OPENAI_DEFAULT_MODEL` | No | `gpt-4o-mini` | Model name |
| `OLLAMA_BASE_URL` | No | `http://localhost:11434` | Ollama base URL |
| `OLLAMA_DEFAULT_MODEL` | No | `llama3.2` | Default (general-purpose) model name |
| `OLLAMA_FAST_MODEL` | No | — | Fast model for low-latency tasks (falls back to default) |
| `OLLAMA_PLANNER_MODEL` | No | — | Planner model for heavy reasoning (falls back to default) |
| `OLLAMA_REVIEW_MODEL` | No | — | Review model for critique/validation (falls back to default) |
| `OLLAMA_TIMEOUT_SECONDS` | No | `120` | Default timeout for Ollama calls |
| `OLLAMA_FAST_TIMEOUT_SECONDS` | No | — | Timeout override for fast role (falls back to default) |
| `OLLAMA_PLANNER_TIMEOUT_SECONDS` | No | — | Timeout override for planner role (falls back to default) |
| `PROMPTS_PATH` | No | — | Path to prompt template directory (worker) |
| `FEATURE_AUTO_APPROVE` | No | `false` | Auto-approve all proposals |
| `FEATURE_WRITEBACK_ENABLED` | No | `false` | Enable writeback to source |
| `RETRY_MAX_ATTEMPTS` | No | `3` | Max job retry attempts |

---

## Adding a New Processor

A processor transforms a task and produces a `SuggestionProposal`. To add one:

### 1. Define the prompt template

Create `prompts/{your_processor}/v1.yaml` following the existing templates. Use `user_prompt_tpl` (not `user_prompt_template`) with `{{index . "FieldName"}}` syntax.

### 2. Implement the `Processor` interface

```go
// internal/processors/processor.go
type Processor interface {
    Process(ctx context.Context, task *models.SourceTask) (*ProcessorResult, error)
    Name() string
}
```

Create a new file in `internal/processors/`. See `llm_rewrite.go` for a reference implementation.

### 3. Register in the composite processor

In `internal/processors/composite.go`, add your processor to the pipeline.

### 4. Add a job type constant

Add the new job type string as a constant. Update `orchestrator.go` to create jobs of this type.

### 5. Write tests

Add unit tests in `internal/processors/processors_test.go`.

---

## Adding a New Source Connector

A source connector fetches tasks from an external system. To add one:

### 1. Implement the `Connector` interface

```go
// internal/source/connector.go
type Connector interface {
    // Name returns the provider identifier (e.g., matches source_connections.provider_type).
    Name() string

    // FetchTasks fetches raw tasks from the upstream system using the given connection.
    FetchTasks(ctx context.Context, conn models.SourceConnection) ([]RawTask, error)

    // NormalizeTask converts a raw upstream task into the normalized internal representation.
    NormalizeTask(raw RawTask) (NormalizedTask, error)
}
```

Create a directory under `internal/source/yourprovider/`.

### 2. Register in the factory

Update `internal/source/syncer.go` to instantiate your connector based on the `provider` field in `source_connections`.

### 3. Add config struct

Add configuration fields to `internal/config/config.go` with `envconfig` tags.

### 4. Write tests

Add unit tests using interface mocks or a test server.

---

## Code Style

- Standard Go formatting: run `gofmt -w .` before committing.
- Error handling: always wrap errors with `fmt.Errorf("context: %w", err)`.
- Logging: use structured `zap.Logger` fields, never `fmt.Println`.
- No `init()` functions in business logic.
- Comments only where the code is not self-explanatory.

---

## Useful Commands

```bash
make vet              # go vet
make lint             # golangci-lint (must be installed)
make generate         # go generate
make deps             # go mod download && go mod tidy
make db-shell         # open psql shell against dev DB
make docker-logs      # tail all Docker service logs
```
