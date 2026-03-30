# Architecture Decision Records

This document records significant architectural decisions made during the design of Runeforge. Each ADR explains the context, the decision, and the trade-offs accepted.

---

## ADR-001: Go for Backend Services

**Status:** Accepted

**Context:**
Runeforge is a pipeline of stateless microservices with heavy concurrent I/O: polling external APIs, publishing to NATS, querying PostgreSQL, and calling LLM providers — often simultaneously.

**Decision:**
Use Go 1.22+ for all backend services.

**Reasons:**
- **Concurrency model:** Goroutines and channels are a natural fit for concurrent I/O pipelines without callback hell.
- **Performance:** Compiled binaries with low memory overhead. Each service binary is ~10-15 MB.
- **Deployment simplicity:** Single static binary per service. No runtime dependency (JVM, Python interpreter) to manage.
- **Standard library richness:** `net/http`, `text/template`, `encoding/json` cover the majority of needs without external dependencies.
- **Type safety:** Catches contract violations at compile time (e.g., missing fields in event structs).

**Trade-offs:**
- Less expressive type system than Rust or Haskell.
- Generics support (since 1.18) is adequate but less mature than in some languages.
- Slower iteration for rapid prototyping than Python or TypeScript.

---

## ADR-002: PostgreSQL as the Primary Database

**Status:** Accepted

**Context:**
The platform needs a reliable store for task state, job queues, proposals, and an append-only audit log.

**Decision:**
Use PostgreSQL 16 as the single database for all services.

**Reasons:**
- **JSONB:** Task payloads and proposal diffs are semi-structured. JSONB provides schemaless flexibility with indexable JSON paths.
- **ACID transactions:** Critical for job queue operations (lease acquisition must be atomic to prevent double-processing).
- **Append-only tables:** PostgreSQL handles high-insert-rate append-only tables (audit_events, snapshots) efficiently with table partitioning.
- **Operational familiarity:** Widely understood; excellent tooling (psql, pgAdmin, Prometheus exporters).
- **Mature migration tooling:** `golang-migrate` supports PostgreSQL out of the box.

**Trade-offs:**
- Single point of failure without HA setup (Patroni, RDS Multi-AZ, etc.).
- Not ideal for extremely high write throughput (millions of events/sec); acceptable for current scale.
- Requires careful index management as tables grow.

**Alternatives considered:**
- **SQLite:** Too limited for concurrent multi-service access and JSONB queries.
- **MongoDB:** JSONB in PostgreSQL provides similar flexibility without giving up relational integrity.
- **CockroachDB:** Overkill for initial scale; adds operational complexity.

---

## ADR-003: NATS JetStream over Kafka / RabbitMQ

**Status:** Accepted

**Context:**
Services need reliable async messaging with at-least-once delivery, consumer groups, and dead-letter capability.

**Decision:**
Use NATS JetStream for all inter-service messaging.

**Reasons:**
- **Operational simplicity:** Single binary, embedded JetStream persistence. No ZooKeeper, no broker cluster required for development.
- **At-least-once delivery:** JetStream provides durable streams with acknowledgment semantics.
- **Low memory footprint:** NATS server uses ~50 MB at rest vs. Kafka's multi-GB JVM heap.
- **Subject hierarchy:** `runeforge.source.task.detected` — human-readable, wildcard-subscriptions (`runeforge.>`), natural namespacing.
- **Go-native client:** `nats.go` is the canonical client, first-class support.

**Trade-offs:**
- Smaller ecosystem than Kafka for stream processing (Kafka Streams, ksqlDB have no NATS equivalent).
- JetStream is newer than Kafka; fewer battle-tested patterns in literature.
- Limited cross-language admin tooling compared to Kafka.

**Alternatives considered:**
- **Kafka:** Excellent for high-throughput log processing but operationally heavy for a self-hosted tool.
- **RabbitMQ:** Strong AMQP support but NATS is simpler for Go services and has better JetStream semantics.
- **Redis Streams:** Good option but adds another dependency when PostgreSQL already exists.

---

## ADR-004: Standard `net/http` Without a Router Framework

**Status:** Accepted

**Context:**
The API Gateway exposes ~15 REST endpoints. Go 1.22 enhanced the standard `net/http` mux with method-based routing.

**Decision:**
Use `net/http` (`http.ServeMux`) directly. No external router package (Gin, Chi, Echo, Gorilla).

**Reasons:**
- **Dependency reduction:** Fewer dependencies = smaller attack surface, simpler upgrades.
- **Go 1.22 routing patterns:** `http.NewServeMux()` now supports `GET /v1/jobs/{id}` patterns natively.
- **Transparency:** Middleware is explicit `http.Handler` wrappers — easy to read and debug.
- **Sufficient for current scale:** 15-20 endpoints do not require the ergonomics of a full framework.

**Trade-offs:**
- More boilerplate for path parameter extraction vs. Chi/Gorilla (mitigated by `r.PathValue("id")`).
- No built-in parameter validation — must validate manually.

**When to reconsider:** If the API grows to 50+ endpoints with complex nested routing, evaluate Chi or similar.

---

## ADR-005: File-based Prompt Templates (YAML)

**Status:** Accepted

**Context:**
LLM prompts need to be versioned, auditable, and editable without code changes.

**Decision:**
Store prompt templates as YAML files on the filesystem at `prompts/{template_id}/{version}.yaml`. The worker loads them at startup with an in-memory cache.

**Reasons:**
- **Auditability:** Prompt changes are Git commits — full history, diffs, blame.
- **Versioning:** Each template version is a separate file (`v1.yaml`, `v2.yaml`). The orchestrator can specify which version to use per job type.
- **No database dependency for prompts:** Templates can be updated and hot-reloaded without a schema migration.
- **Human-readable:** YAML is easy to review in pull requests.
- **Separation of concerns:** Prompt engineering does not require Go code changes.

**Trade-offs:**
- Templates must be mounted into the container (handled via Docker volume in Compose).
- No UI for editing templates — requires Git access.
- Hot-reload not implemented in v1 (requires service restart).

**Alternatives considered:**
- **Database-stored prompts:** More flexible for runtime editing but loses Git history and requires migration for schema changes.
- **Go string constants:** Zero operational overhead but requires recompilation for every prompt change.

---

## ADR-006: Append-only Snapshots for Change Detection

**Status:** Accepted

**Context:**
The source-sync service needs to detect when a task has changed between polls and trigger re-processing. We also want a full history of task states.

**Decision:**
Maintain `source_task_snapshots` as an append-only table. On every detected change, insert a new snapshot row. Use content hashes (SHA-256 of normalized task fields) for fast change detection.

**Reasons:**
- **Auditability:** Complete history of every state a task has ever been in.
- **Rollback capability:** If a writeback produces an undesired result, the original state is always available.
- **Idempotent sync:** Re-running sync on unchanged data produces no new rows (hash comparison short-circuits).
- **Decoupled from processing:** Snapshots are purely a sync concern; the worker reads from `source_tasks` (current state).

**Trade-offs:**
- Storage grows over time. Mitigation: partition by `snapshot_taken_at` and archive old partitions.
- Slightly more complex sync logic (hash comparison before insert).

---

## ADR-007: Provider Abstraction Before First Integration

**Status:** Accepted

**Context:**
The first LLM provider to integrate was OpenAI. We could have hard-coded OpenAI calls throughout.

**Decision:**
Define the `providers.Provider` interface from day one, even before the second provider (Ollama, OpenRouter) was implemented.

**Reasons:**
- **Avoid lock-in:** LLM providers change pricing and availability frequently. Switching providers should be a config change, not a code change.
- **Testability:** Unit tests can inject a `mockProvider` without real API calls.
- **Self-hosting support:** Ollama support (local LLMs) was a primary requirement. The abstraction makes this a drop-in alternative.
- **Interface cost is low:** The `Provider` interface has two methods. The added abstraction cost is minimal.

**Trade-offs:**
- Lowest-common-denominator API: provider-specific features (function calling, streaming) require interface extension.
- Initial setup requires a factory/selector to pick the active provider.

**The same pattern applies to `source.Connector`:** Abstracting the source integration before building the first connector ensures Google Tasks is not the only option forever.
