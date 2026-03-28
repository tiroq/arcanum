# Security

## Authentication

### Admin-Token

All API endpoints (except `/health` and `/metrics`) require an `Admin-Token` header. This is a shared secret configured via the `ADMIN_TOKEN` environment variable.

```
Admin-Token: <your-secret-token>
```

The middleware in `internal/api/middleware.go` validates the token on every request. Missing or incorrect tokens receive `401 Unauthorized`.

**Token requirements:**
- Minimum 16 characters recommended in production
- Rotate by updating the environment variable and restarting the api-gateway
- Never use the default `change-me` value in production

### Future: Per-user tokens / OAuth

The current single-token model is intentionally simple for a self-hosted tool. Future iterations may add per-user JWT tokens with role-based access control (RBAC) — particularly to distinguish "read" access from "approve proposal" actions.

---

## Secret Handling

### Principles
- **No secrets in source code.** API keys, database passwords, and admin tokens are environment variables only.
- **No secrets in logs.** Structured logging is configured to omit sensitive fields.
- **No secrets in JSONB without encryption.** The `source_connections.config` field stores provider credentials. In production, encrypt this field at the application layer or use Postgres column-level encryption before storing.

### Environment Variables

| Variable | Sensitivity | Notes |
|---|---|---|
| `ADMIN_TOKEN` | High | Shared secret for API access |
| `DATABASE_DSN` | High | Contains database password |
| `OPENAI_API_KEY` | High | LLM provider key |
| `OPENROUTER_API_KEY` | High | LLM provider key |
| `GOOGLE_TASKS_CREDENTIALS_PATH` | Medium | Path to OAuth credentials file |

### Recommended Secret Storage

- **Development:** `.env` file (gitignored)
- **Production:** Use your platform's secret manager (AWS Secrets Manager, GCP Secret Manager, HashiCorp Vault, Kubernetes Secrets with encryption at rest)

---

## Audit Trail

Every significant platform action is recorded in the `audit_events` table:

- Job created, retried, dead-lettered
- Proposal created, approved, rejected
- Writeback requested, completed, failed
- Source connection created, updated, deleted

The `audit_events` table is **append-only**: no rows are updated or deleted. Retention policy is configurable at the database level. This provides:
- Full traceability for compliance
- Debugging capability for production incidents
- A basis for future user-facing audit logs

---

## Rate Limiting

Rate limiting hooks exist in the HTTP middleware layer but are not enforced by default in the current version. The `middleware.go` file is the correct place to add:
- Per-token rate limits
- Per-IP rate limits
- Burst allowances

Recommended: add a sliding-window rate limiter before production exposure to the internet.

---

## Network Security

### Docker Compose (development)
- The PostgreSQL port (5432) and NATS port (4222) are bound to `0.0.0.0` for convenience. In production, bind only to internal network interfaces.
- The `ADMIN_TOKEN` is passed via environment variable, never hardcoded in Compose files.

### Production Recommendations
- Run all services in a private network; only api-gateway and admin-web should be internet-facing.
- Use TLS termination at the load balancer for api-gateway.
- Enable NATS TLS and authentication in production NATS deployments.
- Enable Postgres `sslmode=require` (or `verify-full`) in production DSNs.

---

## LLM Data Handling

Task titles and descriptions are sent to the configured LLM provider (OpenAI, OpenRouter, or Ollama). Consider:

- **Self-hosted (Ollama):** Data never leaves your infrastructure.
- **OpenAI / OpenRouter:** Task content is sent to third-party APIs. Review their data processing agreements.
- **No PII in prompts:** The prompt templates do not include user identity information — only task content.

---

## Known Limitations

1. **Single shared token:** No per-user authentication. Any holder of `ADMIN_TOKEN` has full access.
2. **No TLS in default Docker Compose:** Add a reverse proxy (e.g., Caddy, nginx) with TLS for production.
3. **Source connection config not encrypted at rest:** Encrypt `config` column for production.
4. **No CSRF protection:** Admin-web should only be served over HTTPS in production.
