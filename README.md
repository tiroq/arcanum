# Runeforge

Runeforge is a self-hosted autonomous agent platform that processes tasks from upstream systems (starting with Google Tasks), applies LLM-driven transformations, and proposes improvements with full auditability.

### Multi-Model Support

The platform supports role-based model selection for Ollama. Processors request logical model roles (`default`, `fast`, `planner`, `review`) rather than hardcoded model names. Each role can be configured to use a different model and timeout via environment variables, enabling operators to balance latency and reasoning capability per workload. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for details.

### Execution Profiles

Execution profiles extend role-based model selection with **candidate chains**, **thinking modes**, and **automatic fallback**. When the primary model fails (timeout, server error, etc.), the engine automatically tries the next candidate in the chain.

#### Configuration Priority

For each role, the resolution order is:

1. `MODEL_<ROLE>_PROFILE` — full DSL with candidate chains and parameters
2. `OLLAMA_<ROLE>_MODEL` — single model name (legacy, still works)
3. `OLLAMA_DEFAULT_MODEL` — base fallback

#### Profile DSL

```
model?param=value&param=value|model2?param=value
```

- `|` separates candidates (tried in order on failure)
- `?` starts parameters
- `&` separates parameters

Supported parameters:

| Param     | Values                                       | Default           |
|-----------|----------------------------------------------|-------------------|
| `think`   | `on`, `off`, `thinking`, `nothinking`        | provider default   |
| `timeout` | seconds (integer)                            | role timeout       |
| `json`    | `true`, `false`                              | `false`            |

#### Examples

**Simple — one model per role (legacy env vars):**

```bash
OLLAMA_DEFAULT_MODEL=qwen2.5:7b
OLLAMA_FAST_MODEL=llama3.2:3b
OLLAMA_PLANNER_MODEL=qwen2.5:14b
```

**Advanced — fallback chains with thinking modes (profile DSL):**

```bash
# Fast role: try small model first, fall back to tiny model with thinking disabled
MODEL_FAST_PROFILE=llama3.2:3b?think=off&timeout=30|llama3.2:1b?think=off&timeout=15

# Planner role: large model with thinking, fall back to medium model
MODEL_PLANNER_PROFILE=qwen2.5:14b?think=on&timeout=300|qwen2.5:7b?think=on&timeout=120

# Review role: require JSON output, two candidates
MODEL_REVIEW_PROFILE=qwen2.5:7b?json=true&timeout=120|llama3.2:3b?json=true&timeout=60
```

When `MODEL_FAST_PROFILE` is set, `OLLAMA_FAST_MODEL` is ignored for that role. Unset roles fall back to `OLLAMA_DEFAULT_MODEL` as a single candidate.

## Quick Start

### Prerequisites
- Docker and Docker Compose
- Go 1.22+
- Node.js 20+ (for admin web)

### Local Development

1. Clone and configure:
```bash
cp .env.example .env
# Edit .env with your API keys
```

2. Start infrastructure:
```bash
make docker-infra
```

3. Run services:
```bash
make dev-api        # API Gateway on :8080
make dev-sync       # Source Sync on :8081
make dev-worker     # Worker on :8083
make dev-admin      # Admin Web on :3000
```

Or start everything with Docker:
```bash
make docker-up
```

4. Check health:
```bash
make health
```

## Architecture

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full system design.

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [Data Model](docs/DATA_MODEL.md)
- [Message Contracts](docs/MESSAGE_CONTRACTS.md)
- [API Reference](docs/API.md)
- [Security](docs/SECURITY.md)
- [Development Guide](docs/DEVELOPMENT.md)
- [Testing Guide](docs/TESTING.md)
- [Runbook](docs/RUNBOOK.md)
- [Architecture Decisions](docs/DECISIONS.md)