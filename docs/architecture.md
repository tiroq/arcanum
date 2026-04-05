Arcanum Architecture

Overview

Arcanum is a self-hosted autonomous agent platform built as a bus-centric microservice system.

Its purpose is to:
	•	ingest work from external interfaces,
	•	transform and evaluate that work with LLM-driven services,
	•	persist memory and audit history,
	•	expose state through observable technical signals,
	•	and improve over time through explicit feedback loops.

Arcanum is not a monolith with helper services.
It is a distributed system whose components communicate through the data bus and whose state transitions must remain explicit, observable, and recoverable.

⸻

Architectural Principles

1. Bus-first communication

All inter-service communication must go through the message bus.

Services must communicate using:
	•	events,
	•	commands,
	•	results,
	•	alerts.

Direct service-to-service orchestration is forbidden by default.

2. Explicit state transitions

Every durable workflow must move through clearly defined states.

No hidden retries.
No invisible side effects.
No silent terminal states.

3. Recoverability

If a worker crashes or a model call fails, the system must be able to:
	•	detect the condition,
	•	surface it,
	•	and recover or escalate deterministically.

4. Observability as a core requirement

System health is not inferred from a single green health endpoint.

Arcanum must expose:
	•	audit events,
	•	metrics,
	•	logs,
	•	queue state,
	•	processing outcomes,
	•	and failure reasons.

5. Separation of concerns

Interface logic, orchestration, cognition, memory, execution, and monitoring must remain separated so the system can evolve without collapsing into tight coupling.

⸻

High-Level System Model

Arcanum is composed of six logical layers:
	1.	Interface Layer
	2.	Bus Layer
	3.	Orchestration Layer
	4.	Cognitive / Processing Layer
	5.	Memory and Persistence Layer
	6.	Monitoring and Control Layer

These are logical groupings, not necessarily one binary each.

⸻

1. Interface Layer

The Interface Layer connects Arcanum to the outside world.

Initial interfaces include:
	•	Telegram
	•	Admin/API endpoints
	•	future source connectors such as Google Tasks or other upstream systems

Responsibilities
	•	accept inbound messages or triggers
	•	authenticate and normalize them
	•	publish them into the bus as system messages
	•	present outbound notifications and summaries

Rules
	•	interface services must not contain deep business logic
	•	interface services must not directly mutate internal system state except through sanctioned application services
	•	external requests should become commands or events on the bus when they enter the system

Expected services
	•	telegram-ingress
	•	telegram-egress
	•	api-gateway
	•	future source-sync

⸻

2. Bus Layer

The Bus Layer is the communication backbone of Arcanum.

Initial technology:
	•	NATS
	•	JetStream for durable streams and consumers

Responsibilities
	•	decouple producers and consumers
	•	preserve important messages durably where needed
	•	allow independent services to react to system activity
	•	support replay, tracing, and future self-analysis

Message classes

Arcanum should distinguish at least four classes of bus traffic:

Events
Facts that happened.

Examples:
	•	arcanum.system.started
	•	arcanum.job.created
	•	arcanum.job.succeeded
	•	arcanum.proposal.created
	•	arcanum.alert.raised

Commands
Requests to perform work.

Examples:
	•	arcanum.command.process_task
	•	arcanum.command.write_memory
	•	arcanum.command.send_telegram
	•	arcanum.command.retry_job

Results
Structured outputs of commands.

Examples:
	•	arcanum.result.process_task.completed
	•	arcanum.result.memory.write.completed
	•	arcanum.result.telegram.sent

Alerts
Operational or diagnostic signals.

Examples:
	•	arcanum.alert.lease_expired
	•	arcanum.alert.invalid_json_spike
	•	arcanum.alert.queue_backlog

Bus rules
	•	subject naming must be explicit and stable
	•	important workflows must emit lifecycle events
	•	direct DB write without corresponding bus publication is an architectural smell

⸻

3. Orchestration Layer

The Orchestration Layer decides what happens next.

It should not do all work itself.
It should coordinate work by interpreting events and publishing commands.

Responsibilities
	•	react to creation of new work
	•	choose or assign processing paths
	•	schedule retries or follow-ups
	•	route work to the correct processing service
	•	maintain workflow-level policy

Important note

The orchestrator should not become a god service.
It coordinates. It does not absorb all execution.

Expected responsibilities over time
	•	job lifecycle control
	•	priority routing
	•	retry policy enforcement
	•	deadline-aware scheduling
	•	escalation decisions

Current risk to avoid

If the API creates jobs directly in the database without emitting corresponding events, the orchestrator becomes bypassed and effectively ornamental.

⸻

4. Cognitive / Processing Layer

The Cognitive Layer performs model-driven reasoning and transformation.

This includes:
	•	routing tasks
	•	rewriting or normalizing content
	•	evaluating outputs
	•	proposal generation
	•	future review, planning, or self-improvement reasoning

Responsibilities
	•	invoke LLM providers
	•	validate outputs
	•	apply processor-specific business rules
	•	emit structured results rather than hidden side effects

Design rules
	•	prompt schema must exactly match code schema
	•	invalid structured output must not silently succeed
	•	provider behavior must be observable
	•	role-based model routing must remain explicit

Model execution policy

Arcanum uses role-based execution profiles.

Example roles:
	•	fast
	•	default
	•	planner
	•	review

Each role may map to a candidate chain with:
	•	model
	•	think mode
	•	timeout
	•	JSON requirement
	•	fallback order

Initial observation from current system state

The system already contains LLM-driven processors, but processor correctness depends on strict schema alignment. Prompt/struct mismatch is a critical failure mode and must be treated as an architectural concern, not a one-off bug.

⸻

5. Memory and Persistence Layer

The Persistence Layer stores durable system state.

Initial technologies include:
	•	PostgreSQL for durable relational state
	•	JetStream for durable event transport

Responsibilities
	•	store source tasks
	•	store processing jobs and state transitions
	•	store proposals
	•	store processing runs
	•	store snapshots
	•	store audit trail
	•	support future persistent memory and retrieval

Core persistent entities

At minimum, Arcanum should have durable records for:
	•	source tasks
	•	processing jobs
	•	processing runs
	•	proposals
	•	snapshots
	•	audit events
	•	memory entries (future)

Persistence rules
	•	state transitions must be queryable
	•	FK constraints should protect integrity
	•	terminal records should remain inspectable
	•	audit history should be append-only

Job lifecycle model

The processing job lifecycle should include explicit states such as:
	•	queued
	•	leased
	•	running
	•	retry_scheduled
	•	succeeded
	•	failed
	•	dead_letter

Mandatory recovery flows

The architecture requires:
	•	lease reclamation for expired leased jobs
	•	retry requeue for due retry_scheduled jobs

Without these, the state machine is incomplete and the system is not operationally trustworthy.

⸻

6. Monitoring and Control Layer

The Monitoring Layer observes the system from a technical perspective.

This must exist as separate services connected to the bus.
It should not be hidden inside the main worker logic.

Responsibilities
	•	monitor health and readiness
	•	observe queue state
	•	detect expired leases
	•	detect overdue retries
	•	detect repeated invalid model outputs
	•	detect notification delivery failures
	•	expose metrics and alert conditions

Two subtypes

Passive monitoring
	•	metrics
	•	audit summaries
	•	queue visibility
	•	state dashboards

Active control
	•	raise alerts
	•	publish recovery commands
	•	trigger safe corrective actions
	•	enforce cooldowns or suppression later if needed

Examples of future services
	•	health-monitor
	•	queue-monitor
	•	lease-reaper
	•	retry-requeuer
	•	alert-router
	•	resource-monitor

Architectural requirement

If a state can become stuck, some monitoring or control service must either:
	•	recover it,
	•	or escalate it.

⸻

External Interfaces

Telegram

Telegram is the primary owner communication channel in the first phase.

Telegram should support:
	•	receiving owner commands
	•	sending proposal notifications
	•	sending summaries
	•	surfacing blockers and alerts
	•	providing lightweight status visibility

Telegram is an interface layer concern and should remain separated from core processing logic.

HTTP Admin/API

The API layer provides:
	•	task inspection
	•	job inspection
	•	proposal inspection
	•	manual triggers
	•	metrics summaries

The API must not become a bypass around the event-driven architecture.

⸻

Current Service Topology

The current system appears to center on services such as:
	•	api-gateway
	•	orchestrator
	•	worker
	•	notification

This is a reasonable starting topology, but it is still transitional.

Likely current mapping
	•	api-gateway → interface layer
	•	orchestrator → orchestration layer
	•	worker → cognitive + command execution hybrid
	•	notification → interface egress layer

Architectural concern

The current worker likely combines multiple concerns:
	•	job acquisition
	•	LLM invocation
	•	output transformation
	•	persistence side effects

This is acceptable early on, but long term the system should move toward clearer separation between:
	•	command execution
	•	cognitive reasoning
	•	monitoring/control

⸻

Data Flow Example

Example: task resync to proposal
	1.	A task is submitted or resynced through the API.
	2.	The API validates input and creates or requests creation of a processing job.
	3.	A job.created event is published to the bus.
	4.	The orchestrator observes the event and may apply routing or policy.
	5.	A worker leases the job.
	6.	The cognitive processor invokes the configured model.
	7.	Output is validated.
	8.	A proposal is persisted.
	9.	proposal.created is published.
	10.	Notification service sends Telegram message.
	11.	Audit and metrics are updated.

Failure path example
	1.	Worker leases a job.
	2.	Worker crashes or model call hangs.
	3.	Lease expires.
	4.	Lease reclamation service detects expiry.
	5.	Job is requeued and alert/event emitted.
	6.	Worker or another worker picks it up again.

This recovery path is not optional. It is part of the architecture.

⸻

Required Invariants

The system should maintain the following invariants:
	1.	No work item may remain permanently in leased after expiry.
	2.	No due retry may remain permanently in retry_scheduled.
	3.	Every important durable state transition should be visible in at least one of:
	•	database records
	•	audit events
	•	metrics
	•	logs
	4.	Structured LLM output must either validate or fail explicitly.
	5.	Services should not depend on hidden direct calls to preserve core workflows.
	6.	Operator-facing summaries must not hide meaningful job states.

⸻

Known Architectural Risks

1. Bus bypass through direct database writes

If job creation or workflow advancement happens directly in DB without corresponding events, orchestration and monitoring become incomplete.

2. Multi-concern worker service

If too much logic lives in the worker, the architecture drifts back toward a distributed monolith.

3. Weak observability contracts

If audit events, metrics, and summaries do not reflect real system state, the platform cannot self-improve reliably.

4. Contract mismatch between prompt and code

Prompt/code schema mismatch causes silent corruption of cognitive output and must be treated as a first-class architectural risk.

5. Unbounded growth without retention policy

Jobs, runs, proposals, and snapshots may accumulate without cleanup or archival strategy.

⸻

Evolution Path

Phase 1 — Stabilize the existing loop

Focus on:
	•	schema correctness
	•	lease reclamation
	•	retry requeue
	•	audit wiring
	•	accurate metrics
	•	bus publication consistency

Phase 2 — Strengthen observability and control

Focus on:
	•	complete state visibility
	•	monitoring services
	•	alert routing
	•	token accounting
	•	proposal review/approval path

Phase 3 — Refine service boundaries

Focus on:
	•	extracting cognitive services from generic workers
	•	formalizing memory services
	•	adding scheduling and monitoring microservices
	•	reducing direct DB-centric flow coupling

Phase 4 — Self-improvement and economic usefulness

Focus on:
	•	persistent memory retrieval
	•	improvement research loops
	•	operator summaries
	•	opportunity discovery
	•	tooling that increases owner productivity and leverage

⸻

What This Architecture Is Optimizing For

Arcanum is optimized for:
	•	operational truth
	•	recoverability
	•	long-term maintainability
	•	self-improvement readiness
	•	low-friction human oversight
	•	local-first execution with explicit model policy

It is not optimized for:
	•	minimal code count
	•	magical hidden automation
	•	tightly coupled convenience flows

⸻

Guiding Summary

Arcanum must behave like a real distributed system, not like a single application split into binaries.

That means:
	•	messages are first-class,
	•	state transitions are explicit,
	•	failures are recoverable,
	•	outputs are validated,
	•	and technical truth is always preferred over apparent success.