# Runeforge

Runeforge is a self-hosted autonomous agent platform that processes tasks from upstream systems (starting with Google Tasks), applies LLM-driven transformations, and proposes improvements with full auditability.

### Multi-Model Support

The platform supports role-based model selection for Ollama. Processors request logical model roles (`default`, `fast`, `planner`, `review`) rather than hardcoded model names. Each role can be configured to use a different model and timeout via environment variables, enabling operators to balance latency and reasoning capability per workload. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for details.

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