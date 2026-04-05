# Copilot Instructions for Arcanum

## Purpose

Arcanum is a self-hosted autonomous agent platform designed to:
- process tasks from upstream systems,
- apply LLM-driven transformations,
- evolve its own behavior over time,
- operate via a bus-centric microservice architecture,
- remain fully observable and auditable.

The system is NOT a simple CRUD backend.
It is an evolving autonomous system with strict requirements for:
- reliability,
- traceability,
- deterministic state transitions.

---

## Core Principles

### 1. Bus-First Architecture

ALL communication between services must go through the message bus (NATS).

❌ Do NOT:
- call services directly
- bypass the bus with hidden DB coupling

✅ DO:
- publish events
- subscribe to events
- keep services loosely coupled

---

### 2. Explicit State Machines

All state transitions must be:
- explicit
- observable
- recoverable

Example (jobs):
- queued → leased → running → succeeded/failed
- leased → expired → queued (reclaimed)
- retry_scheduled → queued (when due)

❌ No silent transitions  
❌ No implicit logic  
❌ No hidden retries  

---

### 3. No Silent Failure

If something fails:
- it must be visible
- it must be logged
- it must be recoverable

Examples:
- expired leases must be reclaimed
- invalid LLM output must be detected
- retries must actually execute

---

### 4. LLM Contracts Are Strict APIs

LLM prompts define structured contracts.

Rules:
- Prompt schema MUST match Go structs exactly
- No field name mismatch
- No partial parsing
- No silent data loss

If output is invalid:
→ trigger fallback or fail explicitly

---

### 5. Observability First

Every important action must be traceable:

- audit events must be recorded
- metrics must reflect real state
- API summaries must include ALL states

System must answer:
- what happened?
- why?
- when?

---

### 6. Deterministic Recovery

System must self-heal:

- expired leases → reclaimed
- retry_scheduled → requeued
- failed jobs → retried or dead-lettered

❌ No permanent stuck states

---

### 7. Minimal Magic

Prefer:
- simple logic
- explicit flows
- predictable behavior

Avoid:
- hidden side effects
- implicit assumptions
- over-abstraction

---

## Coding Guidelines

### Go

- Always define JSON tags (`json:"snake_case"`)
- Never rely on default struct serialization
- Use explicit types for state and transitions
- Keep business logic separate from transport

---

### LLM Integration

- Always validate JSON output
- Always define schema in prompt
- Always align schema with struct
- Always log raw response (for debugging)

---

### NATS / JetStream

- Prefer idempotent consumers
- Handle reconnection properly
- Avoid durable push consumer pitfalls
- Use clear subject naming

---

### Database

- Enforce integrity with constraints
- Prefer explicit queries over ORM magic
- Keep migrations simple and reversible

---

## What Copilot SHOULD DO

- Suggest small, correct, explicit fixes
- Maintain consistency with existing architecture
- Preserve auditability and observability
- Highlight potential state inconsistencies

---

## What Copilot MUST NOT DO

- Introduce hidden behavior
- Bypass the message bus
- Add implicit retries
- Ignore state transitions
- Create silent failure paths

---

## Mental Model

Think of Arcanum as:

> A deterministic, observable, self-evolving system  
> — not just an application.

Every change must answer:
- What state changes?
- How is it observed?
- How is it recovered if broken?