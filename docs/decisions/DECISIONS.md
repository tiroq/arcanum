# Arcanum Architecture Decisions

This document captures key architectural decisions and their rationale.

---

## DECISION-001: Bus-Centric Architecture

### Decision
All inter-service communication MUST go through NATS (JetStream).

### Rationale
- Enables loose coupling
- Supports horizontal scaling
- Allows independent evolution of services
- Makes system observable via event stream

### Consequences
- No direct service-to-service calls
- API layer must also publish events
- Debugging requires event tracing

---

## DECISION-002: Job-Based Processing Model

### Decision
All work is represented as jobs with explicit lifecycle states.

### States
- queued
- leased
- running
- retry_scheduled
- succeeded
- failed
- dead_letter

### Rationale
- Deterministic processing
- Easy retries
- Full auditability

### Consequences
- Requires lease management
- Requires retry scheduling
- Requires cleanup logic

---

## DECISION-003: Lease Mechanism for Workers

### Decision
Workers acquire jobs using a lease (with expiry).

### Rationale
- Prevents duplicate processing
- Enables recovery after crashes

### Required Behavior
- Expired leases MUST be reclaimed
- Lease expiry MUST be enforced

### Consequences
- Requires background reaper
- Requires careful race handling

---

## DECISION-004: Retry Scheduling

### Decision
Failed jobs are moved to `retry_scheduled` with `scheduled_at`.

### Rationale
- Supports delayed retries
- Prevents immediate retry storms

### Required Behavior
- Jobs must be requeued when due
- No manual intervention required

---

## DECISION-005: LLM as Structured Component

### Decision
LLM output is treated as structured API output, not free text.

### Rationale
- Enables deterministic processing
- Allows validation and fallback
- Prevents silent corruption

### Rules
- Prompt schema == Go struct
- JSON must be valid
- Output must be validated

---

## DECISION-006: Execution Profiles (Model Routing)

### Decision
Model selection uses profiles with fallback chains.

Example:

```
modelA?think=on|modelB?think=off
```

### Rationale
- Supports performance vs quality trade-offs
- Enables automatic fallback
- Allows experimentation

### Consequences
- Requires consistent scoring
- Requires validation layer

---

## DECISION-007: Observability as First-Class Concern

### Decision
System must be fully observable.

### Includes
- audit_events (append-only)
- Prometheus metrics
- structured logs

### Rationale
Self-improving system must:
- understand itself
- debug itself
- evolve based on data

---

## DECISION-008: Telegram as Primary Human Interface

### Decision
Human interaction happens via Telegram bot.

### Rationale
- Low friction
- async communication
- easy control channel

### Responsibilities
- notify about proposals
- accept commands
- provide system visibility

---

## DECISION-009: No Silent States

### Decision
Every state must be:
- visible
- queryable
- explainable

### Forbidden
- hidden queues
- implicit retries
- invisible failures

---

## DECISION-010: Separation of Concerns (Future Direction)

### Planned Service Categories

1. Interface (Telegram, API)
2. Command Execution
3. Cognitive (LLM processing)
4. Memory (DB, knowledge store)
5. Monitoring
6. Scheduling

### Rationale
- clearer system boundaries
- independent scaling
- easier evolution

---

## Known Gaps (Current State)

- Lease reclamation initially missing (must be implemented)
- Retry requeue initially missing (must be implemented)
- Prompt/struct mismatch caused empty outputs
- Audit events not fully wired
- Some flows bypass event bus

---

## Guiding Principle

> If a behavior is not observable, it does not exist.

> If a state cannot be recovered, it is a bug.