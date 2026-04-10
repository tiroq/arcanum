# Iteration 43 — Controlled Self-Extension Sandbox Validation Report

## 1. Summary

Implemented a sandboxed self-extension loop allowing the agent to propose, build, test, and
safely deploy new internal components with strict safety, determinism, and rollback guarantees.
All extensions require human approval before deployment. Code is executed in restricted
sandboxes with static analysis gates, checksum verification, and resource monitoring.

### New Files
- `internal/agent/self_extension/types.go` — Constants, entities (ComponentProposal, ComponentSpec, SandboxRun, TestResult, SandboxMetrics, DeploymentRecord, RollbackPoint, SandboxConfig, ApprovalDecision, CodeArtifact), state machine (proposed→rejected→approved→in_progress→validated→deployed)
- `internal/agent/self_extension/store.go` — PostgreSQL persistence for 7 tables (proposals, specs, sandbox_runs, deployments, rollback_points, code_artifacts, approvals)
- `internal/agent/self_extension/spec_generator.go` — Deterministic spec generation from proposals + code generation with SHA-256 checksums
- `internal/agent/self_extension/sandbox.go` — Isolated execution with restricted env, unsafe code detection (`os.Exit`, `syscall`, `unsafe.Pointer`, `net/http`), checksum verification, output contract validation
- `internal/agent/self_extension/validator.go` — Multi-criteria validation of sandbox runs against spec requirements
- `internal/agent/self_extension/deployer.go` — Versioned deployment with human approval gate, automatic rollback point creation
- `internal/agent/self_extension/engine.go` — Full lifecycle orchestrator with capacity gating and 8 audit events
- `internal/agent/self_extension/adapter.go` — GraphAdapter — nil-safe, fail-open adapter for API
- `internal/agent/self_extension/self_extension_test.go` — 30 tests
- `internal/db/migrations/000047_create_agent_self_extension.up.sql` — Migration (7 tables + indexes)
- `internal/db/migrations/000047_create_agent_self_extension.down.sql` — Rollback migration

### Modified Files
- `internal/api/handlers.go` — Added `selfExtension` field, `WithSelfExtension()`, 8 handler methods
- `internal/api/router.go` — Added 7 self-extension routes
- `cmd/api-gateway/main.go` — Wired self-extension engine, store, adapter, capacity integration

---

## 2. Lifecycle State Machine

```
proposed → rejected        (human rejects)
proposed → approved        (human approves)
approved → in_progress     (sandbox execution begins)
in_progress → validated    (sandbox + validation pass)
in_progress → rejected     (sandbox or validation fails)
validated → deployed       (deployer activates)
deployed → rolled_back     (rollback triggered)
```

All transitions are validated by `IsValidTransition()` using an explicit `ValidTransitions` map.
Invalid transitions are rejected with error. No implicit or silent state changes.

---

## 3. Sandbox Model

### Safety Checks (Pre-Execution)

| Check | Enforcement |
|---|---|
| SHA-256 checksum | Code artifact checksum verified before execution |
| Unsafe pattern detection | Rejects: `os.Exit`, `syscall`, `unsafe.Pointer`, `net/http` |
| Restricted PATH | Only `/usr/bin:/bin` in sandbox environment |
| No secrets | `HOME`, `USER`, `SHELL`, custom env vars stripped |
| Network isolation | `SANDBOX_NETWORK=none` by default |
| `go vet` | Static analysis gate before execution |

### Resource Limits (Configurable via SandboxConfig)

```
MaxExecTime    = 30s        (default)
MaxMemoryMB    = 256        (default)
AllowNetwork   = false      (default)
AllowDiskWrite = false      (default)
AllowExec      = false      (default)
```

### Execution Model

Current implementation uses simulated execution with `go vet` and `go run` subprocess calls
in a restricted environment. Future iterations can upgrade to container-based isolation
(the interface is designed for this).

### Output Contract

Sandbox runs produce structured `TestResult` entries verified against the spec's
`TestRequirements`. The output contract validates:
- All test results have non-empty names and either pass/fail status
- Result count matches expectation
- Execution metrics (latency, memory) are non-negative

---

## 4. Deployment Safety

### Approval Gate (Fail-Safe, NOT Fail-Open)

Deployment **requires** an explicit `ApprovalDecision` with `approved=true`.
This is the only fail-safe (not fail-open) gate in the system.

| Condition | Result |
|---|---|
| No approval record found | **BLOCKED** — deployment refused |
| Approval exists with `approved=false` | **BLOCKED** — deployment refused |
| Approval exists with `approved=true` | Allowed to proceed |

### Versioned Deployment

```
version = "v{N}" where N = max(existing_versions) + 1
```

Deployer automatically:
1. Checks proposal status is `validated`
2. Verifies human approval
3. Deactivates any previous active deployment for the same proposal
4. Creates deployment record with `active=true`
5. Creates rollback point capturing previous version state
6. Emits `self.deployed` audit event

### Rollback

```
Rollback(proposalID) →
  1. Find active deployment
  2. Find rollback point
  3. Restore previous version (or version 0 if first deploy)
  4. Mark deployment as rolled_back + inactive
  5. Emit self.rollback_triggered audit event
```

---

## 5. Capacity Integration

The self-extension engine integrates with Iteration 41's capacity system via `CapacityChecker`:

```go
type CapacityChecker interface {
    GetCapacityPenalty(ctx context.Context) float64
}
```

- If `penalty > CapacityGateThreshold (0.12)` → sandbox execution is **deferred**
- If no capacity data available → fail-open (proceed with execution)
- Prevents builds from consuming scarce owner time during high-load periods

---

## 6. API

| Method | Endpoint | Description |
|---|---|---|
| GET | `/api/v1/agent/self/proposals` | List all proposals |
| POST | `/api/v1/agent/self/proposals` | Create a new proposal |
| POST | `/api/v1/agent/self/spec/{id}` | Generate spec for proposal |
| POST | `/api/v1/agent/self/sandbox/run/{id}` | Execute sandbox run |
| GET | `/api/v1/agent/self/sandbox/results/{id}` | Get sandbox results |
| POST | `/api/v1/agent/self/deploy/{id}` | Deploy validated component |
| POST | `/api/v1/agent/self/rollback/{id}` | Rollback deployed component |
| POST | `/api/v1/agent/self/approve/{id}` | Submit approval decision |

---

## 7. Audit Events

| Event | When |
|---|---|
| `self.proposed` | New component proposal created |
| `self.spec_generated` | Spec generated from proposal |
| `self.sandbox_started` | Sandbox execution begins |
| `self.sandbox_completed` | Sandbox execution finishes (pass or fail) |
| `self.validated` | Validation passes all criteria |
| `self.approved` | Human approval submitted |
| `self.deployed` | Component deployed (with version) |
| `self.rollback_triggered` | Rollback executed |

---

## 8. Tests — 30 passing

### Proposal Tests (1-2)
1. ✅ create proposal with valid fields → stored with status=proposed
2. ✅ invalid source rejected → error returned

### Spec Generation Tests (3-6)
3. ✅ deterministic spec generation → same proposal produces identical spec
4. ✅ spec includes dependencies from proposal
5. ✅ spec includes resource constraints from config defaults
6. ✅ spec expected value propagated from proposal

### Sandbox Tests (7-11)
7. ✅ sandbox executes isolated code → produces test results and metrics
8. ✅ sandbox detects failure → marks run as failed
9. ✅ sandbox captures execution logs
10. ✅ unsafe code patterns rejected (4 sub-tests: os.Exit, syscall, unsafe.Pointer, net/http)
11. ✅ checksum mismatch detected → execution blocked

### Validation Tests (12-14)
12. ✅ passing sandbox run validates successfully
13. ✅ failed sandbox run rejected with reasons
14. ✅ insufficient test coverage detected

### Deployment Tests (15-17)
15. ✅ approved + validated proposal deploys successfully
16. ✅ version increments correctly (v1 → v2)
17. ✅ rollback point created on deploy

### Rollback Tests (18-19)
18. ✅ rollback restores previous version
19. ✅ deployment marked as rolled_back after rollback

### Integration Tests (20-22)
20. ✅ discovery → proposal flow works end-to-end
21. ✅ deployment without approval blocked (fail-safe gate)
22. ✅ nil adapter returns zero values (fail-open)

### State Machine Test (23)
23. ✅ all valid transitions accepted, invalid transitions rejected (13 sub-tests)

### Code Generation Tests (24-26)
24. ✅ stub generation produces compilable code
25. ✅ provider fallback to stub on error
26. ✅ checksum computed and verified

### Config & Lifecycle Tests (27-30)
27. ✅ default sandbox config has safe values
28. ✅ sandbox metrics collected (latency, memory, test count)
29. ✅ approval decision stored and retrieved
30. ✅ full lifecycle unit test (propose → spec → sandbox → validate → approve → deploy)

---

## 9. Regression Summary

| Package | Status |
|---|---|
| Full `go build ./...` | ✅ Clean |
| `internal/agent/self_extension/...` | ✅ 30/30 pass |
| `internal/agent/capacity/...` | ✅ Pass |
| `internal/agent/income/...` | ✅ Pass |
| `internal/agent/signals/...` | ✅ Pass |
| `internal/agent/financial_pressure/...` | ✅ Pass |
| `internal/agent/decision_graph/...` | ✅ Pass |
| `internal/agent/arbitration/...` | ✅ Pass |
| `internal/agent/calibration/...` | ✅ Pass |
| `internal/agent/discovery/...` | ✅ Pass |
| All 36 agent packages | ✅ Pass |

---

## 10. Remaining Risks

| Risk | Mitigation |
|---|---|
| Sandbox isolation is process-level, not container-level | Restricted env + unsafe pattern detection + no network; future iteration can add container isolation |
| `go run` subprocess could be exploited by crafted code | Static analysis gate catches known dangerous patterns; review gate blocks unvetted code |
| Code generation stubs are minimal | CodeGenerationProvider interface allows LLM-backed generation in future |
| Single-node deployment only | DeploymentRecord tracks version + rollback state; multi-node deployment is future work |
| No automated re-validation after rollback | Rollback restores version but doesn't re-run sandbox; manual re-test required |
| Approval is binary (no conditional approvals) | Sufficient for current system; structured approval conditions can be added later |

---

## 11. Rollout Recommendation

### **READY_WITH_GUARDS**

Rationale:
- All state transitions are explicit and validated by state machine
- Human-in-the-loop approval is fail-safe (blocks without approval)
- Sandbox isolation prevents dangerous code patterns via static analysis
- SHA-256 checksums ensure code integrity through the pipeline
- Capacity gating prevents builds during high-load periods
- Versioned deployment with automatic rollback points
- All adapters are nil-safe and fail-open (except approval gate)
- No regressions across all 36 agent packages
- 30 tests cover all lifecycle phases, safety checks, and edge cases

Guards:
- Monitor `self.sandbox_completed` audit events for unexpected failures
- Review `self.deployed` events to verify version progression
- Validate that `self.rollback_triggered` events include correct previous version
- Ensure capacity gate threshold (0.12) is appropriate for production load profiles
- Periodically review unsafe pattern list for new dangerous patterns
