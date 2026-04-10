# Iteration 45 — External Action Connectors Validation Report

## 1. Summary

Implemented a safe, auditable, pluggable external action execution layer allowing the agent
to interact with external systems through structured connectors with policy-enforced review
gates, dry-run support, retry with idempotency, and full audit trail.

All high-risk actions (send_message, schedule_meeting, publish_post) require human approval.
Connectors are sandboxed via interface — core logic never calls external APIs directly.
Secrets are never logged. All actions are observable and recoverable.

### New Files
- `internal/agent/external_actions/types.go` — Constants, entities (ExternalAction, ExecutionResult, ActionPolicy), state machine (created→review_required→ready→executed/failed), Connector interface, validation, sentinel errors
- `internal/agent/external_actions/store.go` — PostgreSQL persistence for actions + results (CRUD, idempotency key lookup, retry increment)
- `internal/agent/external_actions/connectors.go` — 4 connector implementations: NoopConnector, LogConnector, HTTPConnector (with injectable transport), EmailDraftConnector (draft-only, never sends)
- `internal/agent/external_actions/router.go` — ConnectorRouter (thread-safe connector registry + routing by action type or name), PolicyEngine (risk assessment + review gate logic)
- `internal/agent/external_actions/engine.go` — Full lifecycle orchestrator: create → policy → review → dry-run → execute → result capture, with 5 audit events
- `internal/agent/external_actions/adapter.go` — GraphAdapter — nil-safe, fail-open adapter for API
- `internal/agent/external_actions/external_actions_test.go` — 26 tests (46 including subtests)
- `internal/db/migrations/000048_create_agent_external_actions.up.sql` — Migration (2 tables + indexes)
- `internal/db/migrations/000048_create_agent_external_actions.down.sql` — Rollback migration

### Modified Files
- `internal/api/handlers.go` — Added `externalActions` field, `WithExternalActions()`, 5 handler methods (ExternalActions, ExternalActionRouter, ExternalActionExecute, ExternalActionDryRun, ExternalActionApprove)
- `internal/api/router.go` — Added 2 external action routes
- `cmd/api-gateway/main.go` — Wired external actions engine, store, connectors, policy, adapter

---

## 2. Lifecycle State Machine

```
created → review_required   (policy flags as risky)
created → ready             (policy allows direct execution)
review_required → ready     (human approves)
review_required → failed    (rejected or timeout)
ready → executed            (connector succeeds)
ready → failed              (connector fails after retries)
```

All transitions validated by `IsValidTransition()` using explicit `ValidTransitions` map.
Invalid transitions rejected with error. No implicit or silent state changes.

---

## 3. Connector Model

### Connector Interface

```go
type Connector interface {
    Name() string
    Supports(actionType string) bool
    Execute(payload json.RawMessage) (ExecutionResult, error)
    DryRun(payload json.RawMessage) (ExecutionResult, error)
    Enabled() bool
}
```

### Implemented Connectors

| Connector | Purpose | Sends External? | Supports |
|---|---|---|---|
| `noop` | Testing | No | All types |
| `log` | Audit/observability | No | All types |
| `http` | Generic API calls | Via injectable transport | trigger_api, create_task |
| `email_draft` | Email drafts (NEVER sends) | No | draft_message, send_message |

### Safety Model

- **HTTP connector**: requires injected `HTTPTransport` — nil transport = no execution possible
- **Email draft**: always creates draft, NEVER sends email
- **All connectors**: sandboxed via interface, no direct API calls from core logic
- **Kill-switch**: each connector has `Enabled()` check; disabled connectors are skipped

---

## 4. Review Gate (Fail-Safe)

### Policy Rules

| Condition | Risk Level | Requires Review |
|---|---|---|
| send_message, schedule_meeting, publish_post | high | YES |
| Action linked to income opportunity | medium | YES |
| Standard action (draft, trigger_api) | low | NO |

### Enforcement

- Engine blocks execution of `review_required` actions with `ErrReviewRequired`
- Already-executed actions cannot be re-executed (`ErrAlreadyExecuted`)
- Invalid transitions are rejected
- **No auto-execution of risky actions**

---

## 5. Execution Modes

| Mode | Effect |
|---|---|
| `dry_run` | Simulates action through connector, stores result, marks dry_run_completed |
| `execute` | Real execution, stores result, updates action status |

---

## 6. Retry & Idempotency

- Each action has `idempotency_key` (auto-generated UUID if not provided)
- Unique index on `idempotency_key` prevents duplicate execution
- `max_retries` (default: 3) caps retry attempts
- `retry_count` incremented on each failure
- `ErrMaxRetriesExceeded` when limit reached → action marked failed

---

## 7. API

| Method | Endpoint | Description |
|---|---|---|
| GET | `/api/v1/agent/external/actions` | List all external actions |
| POST | `/api/v1/agent/external/actions` | Create a new external action |
| POST | `/api/v1/agent/external/actions/{id}/execute` | Execute an action |
| POST | `/api/v1/agent/external/actions/{id}/dry-run` | Dry-run an action |
| POST | `/api/v1/agent/external/actions/{id}/approve` | Approve a review-required action |

---

## 8. Audit Events

| Event | When |
|---|---|
| `external.action_created` | New action created (includes type, connector, risk, review status) |
| `external.action_dry_run` | Dry-run completed |
| `external.action_executed` | Real execution completed (includes external_id, duration) |
| `external.action_failed` | Execution or dry-run failed (includes error, retry count) |
| `external.action_approved` | Action approved for execution |

---

## 9. Example: Draft Email Flow

```
1. POST /api/v1/agent/external/actions
   {
     "action_type": "draft_message",
     "payload": {"to": "client@example.com", "subject": "Proposal", "body": "..."},
     "connector_name": "email_draft"
   }
   → Status: "ready" (draft_message is low risk)
   → Audit: external.action_created

2. POST /api/v1/agent/external/actions/{id}/dry-run
   → Returns preview of what would be drafted
   → Audit: external.action_dry_run

3. POST /api/v1/agent/external/actions/{id}/execute
   → Creates email draft (NOT sent)
   → Status: "executed"
   → Audit: external.action_executed
   → Response includes draft_id
```

### Send Message Flow (High Risk)

```
1. POST /api/v1/agent/external/actions
   {"action_type": "send_message", "payload": {...}}
   → Status: "review_required" (high risk)
   → Audit: external.action_created

2. POST /api/v1/agent/external/actions/{id}/execute
   → BLOCKED: "action requires review before execution"

3. POST /api/v1/agent/external/actions/{id}/approve
   {"approved_by": "admin"}
   → Status: "ready"
   → Audit: external.action_approved

4. POST /api/v1/agent/external/actions/{id}/execute
   → Executes through email_draft connector (creates draft, not send)
   → Status: "executed"
```

---

## 10. Tests — 26 passing (46 including subtests)

### Connector Tests (1-9)
1. ✅ noop connector executes successfully
2. ✅ noop connector dry-run works
3. ✅ log connector executes and records entry
4. ✅ http connector dry-run works (validates payload without transport)
5. ✅ http connector dry-run rejects invalid payload
6. ✅ http connector execute fails without transport (safe)
7. ✅ email draft connector creates draft (never sends)
8. ✅ email draft connector dry-run produces preview
9. ✅ email draft connector rejects missing fields

### Policy Tests (10)
10. ✅ action requires review when risky (5 sub-tests: send_message, schedule_meeting, publish_post, opportunity-linked, low-risk)

### Routing Tests (11-12)
11. ✅ connector routing works (by type and by name)
12. ✅ empty router returns no connector

### State Machine Tests (13)
13. ✅ all valid transitions accepted, invalid rejected (11 sub-tests)

### Validation Tests (14-15)
14. ✅ payload validation (valid JSON, array, empty, invalid — 4 sub-tests)
15. ✅ action type validation

### Nil Safety Tests (16)
16. ✅ nil adapter returns zero values (fail-open) — 6 operations tested

### Integration Tests (17-18)
17. ✅ engine policy + review gate logic verified
18. ✅ idempotency key generation verified

### Connector Support Tests (19-20)
19. ✅ HTTP connector supports correct action types
20. ✅ email draft connector supports correct action types

### Helper Tests (21)
21. ✅ truncate helper works correctly

### Transport Tests (22-23)
22. ✅ HTTP connector execute with mock transport (200 OK)
23. ✅ HTTP connector execute with failed transport (500)

### Registry Tests (24)
24. ✅ connector router lists all registered connectors

### Audit Tests (25)
25. ✅ audit events emitted with correct structure

### Error Tests (26)
26. ✅ all sentinel error messages match expected text (9 errors verified)

---

## 11. Regression Summary

| Package | Status |
|---|---|
| `go build ./internal/agent/external_actions/...` | ✅ Clean |
| `internal/agent/external_actions/...` | ✅ 26/26 pass |
| `internal/agent/self_extension/...` | ✅ Pass |
| `internal/agent/capacity/...` | ✅ Pass |
| `internal/agent/income/...` | ✅ Pass |
| `internal/agent/financial_pressure/...` | ✅ Pass |
| `internal/agent/financial_truth/...` | ✅ Pass |
| `internal/agent/discovery/...` | ✅ Pass |
| `internal/agent/arbitration/...` | ✅ Pass |
| `internal/agent/calibration/...` | ✅ Pass |
| `internal/agent/goals/...` | ✅ Pass |
| `internal/agent/governance/...` | ✅ Pass |
| `internal/agent/provider_routing/...` | ✅ Pass |
| `internal/agent/provider_catalog/...` | ✅ Pass |
| `internal/agent/resource_optimization/...` | ✅ Pass |
| `internal/agent/path_learning/...` | ✅ Pass |
| `internal/agent/meta_reasoning/...` | ✅ Pass |
| `internal/agent/stability/...` | ✅ Pass |
| `internal/agent/strategy/...` | ✅ Pass |
| `internal/agent/planning/...` | ✅ Pass |
| `internal/agent/policy/...` | ✅ Pass |
| `internal/agent/outcome/...` | ✅ Pass |
| `internal/agent/actions/...` | ✅ Pass |
| `internal/agent/actionmemory/...` | ✅ Pass |
| `internal/agent/exploration/...` | ✅ Pass |
| `internal/agent/causal/...` | ✅ Pass |
| `internal/agent/scheduler/...` | ✅ Pass |
| `internal/agent/strategy_learning/...` | ✅ Pass |
| `internal/agent/reflection/...` | ✅ Pass |
| Pre-existing failures (portfolio, decision_graph*) | ⚠️ Pre-existing, not caused by this iteration |

*decision_graph/counterfactual/path_comparison/signals failures are from a separate in-progress change to planner_adapter.go, verified by git stash comparison.

---

## 12. Remaining Risks

| Risk | Mitigation |
|---|---|
| HTTP connector with real transport could leak secrets in error messages | Secrets stripped from payloads before audit; connector logs sanitized |
| Email draft connector is in-memory only | Persistent draft storage can be added via store extension |
| No rate limiting on external action creation | Idempotency key prevents duplicates; quota limits are future work |
| ConnectorRouter is not ordered/weighted | First-match routing is deterministic; weighted routing can be added |
| No webhook/callback for async external results | Result polling via API; event-driven callbacks are future work |
| HTTP transport is nil by default | Prevents accidental external calls; must be explicitly configured |

---

## 13. Architecture Notes

### Design Principles
- **Safety > Automation**: High-risk actions always require human approval
- **Sandboxed execution**: All connectors implement the Connector interface; no direct API calls
- **Fail-open for routing**: If no connector found, action fails gracefully
- **Fail-safe for review**: Unapproved risky actions are blocked, not executed
- **Observable**: All actions, results, and state transitions are audited
- **Idempotent**: Unique idempotency keys prevent duplicate execution
- **Secrets safe**: No secret material in payloads or audit logs

### Integration Points
- Connects to income opportunities via `opportunity_id` field
- Wired into API gateway alongside existing subsystems
- Uses same audit infrastructure as all other agent components
- Follows existing adapter pattern (nil-safe, fail-open)

---

## 14. Rollout Recommendation

### **READY_WITH_GUARDS**

Rationale:
- All state transitions are explicit and validated by state machine
- Human-in-the-loop review is fail-safe (blocks unapproved risky actions)
- Connectors are sandboxed via interface — no direct external API calls
- HTTP connector requires explicit transport injection (nil = safe)
- Email draft connector NEVER sends — draft only
- Idempotency keys prevent duplicate execution
- Retry with configurable limits prevents infinite loops
- All adapters are nil-safe and fail-open
- No regressions across all agent packages
- 26 tests cover all connectors, policies, state machine, safety, and edge cases

Guards:
- Monitor `external.action_failed` audit events for unexpected patterns
- Review `external.action_executed` to verify connector selection
- Validate that review_required actions are not bypassed
- Track retry_count distribution to tune max_retries
- Before enabling HTTP transport in production, audit allowed URLs
