# Iteration 7 — Autonomous Scheduling (Safe Version): Validation Report

## 1. Architecture

```
                   ┌─────────────────────┐
                   │   Scheduler (new)    │
                   │                      │
  Config           │  ticker (interval)   │
  ─────────►       │      │               │
  Enabled?         │      ▼               │
  Interval/Timeout │  tryRunCycle()       │
                   │      │               │
                   │  ┌───┴───┐           │
                   │  │running?│           │
                   │  └───┬───┘           │
                   │   no │  yes          │
                   │      │   └──► audit: scheduler.cycle_skipped
                   │      ▼               │
                   │   go executeCycle()  │
                   │      │               │
                   │   ctx = WithTimeout  │
                   │      │               │
                   │   engine.RunCycle()  │  ◄── Same pipeline as manual trigger
                   │      │               │
                   │   audit result       │
                   └──────┼───────────────┘
                          ▼
             Goal Engine → Adaptive Planner → Guardrails →
             Executor → Outcome Verification → Action Memory
```

### Key design decisions

1. **`CycleRunner` interface** — Scheduler accepts `CycleRunner` (satisfied by `*actions.Engine`) rather than a concrete type, enabling full unit testing without DB/NATS.
2. **Cycle goroutine per tick** — Each cycle runs in its own tracked goroutine (`wg.Add(1)`) so the loop remains responsive to detect and audit skipped ticks.
3. **Single integration point** — Wired in `cmd/api-gateway/main.go` alongside the existing action engine. No changes to the pipeline itself.

---

## 2. Scheduler Model

### Config

```go
type SchedulerConfig struct {
    Enabled         bool `envconfig:"AGENT_SCHEDULER_ENABLED" default:"false"`
    IntervalSeconds int  `envconfig:"AGENT_SCHEDULER_INTERVAL_SECONDS" default:"60"`
    TimeoutSeconds  int  `envconfig:"AGENT_SCHEDULER_TIMEOUT_SECONDS" default:"45"`
}
```

Validation (enforced at startup when Enabled=true):
- `IntervalSeconds > 0`
- `TimeoutSeconds > 0`
- `TimeoutSeconds < IntervalSeconds`

### Core type

```go
type Scheduler struct {
    runner   CycleRunner           // *actions.Engine
    interval time.Duration
    timeout  time.Duration
    logger   *zap.Logger
    auditor  audit.AuditRecorder

    mu       sync.Mutex
    running  bool                  // true while a cycle is executing
    started  bool                  // true while the ticker loop is active
    stopCh   chan struct{}
    wg       sync.WaitGroup        // tracks loop + cycle goroutines

    lastRunAt    time.Time
    lastDuration time.Duration
}
```

### Lifecycle

| Method | Behavior |
|--------|----------|
| `Start()` | Starts ticker loop; no-op if already started |
| `Stop()` | Closes stopCh, waits for loop + running cycle to finish; no-op if already stopped |
| `GetStatus(enabled)` | Returns observable state under lock |

---

## 3. Safety Invariants

| # | Invariant | Enforcement |
|---|-----------|-------------|
| 1 | At most ONE active cycle | `s.running` flag checked under `s.mu` lock in `tryRunCycle()` |
| 2 | Bounded timeout per cycle | `context.WithTimeout(background, s.timeout)` in `executeCycle()` |
| 3 | Timeout < interval | Validated in `config.validate()` at startup |
| 4 | Skipped cycles visible | `scheduler.cycle_skipped` audit event with reason |
| 5 | Clean stop | `close(stopCh)` + `s.wg.Wait()` ensures loop exits and running cycle completes |
| 6 | No recursive triggering | Ticker-only dispatch; no retry, no queue, no re-entry |
| 7 | Manual trigger unchanged | `POST /api/v1/agent/run-actions` untouched — calls `engine.RunCycle()` directly |

### Panic safety

`executeCycle()` wraps `runner.RunCycle()` in a closure with `recover()`. A panic:
- Is caught and converted to an error
- Emits `scheduler.cycle_failed` audit event
- Does NOT kill the scheduler loop
- The `running` flag is always released via `defer`

---

## 4. API Control Surface

### `POST /api/v1/agent/scheduler/start`

Starts the scheduler. Idempotent — calling on an already-started scheduler is a no-op.

Response: `{"status": "started"}`

### `POST /api/v1/agent/scheduler/stop`

Stops the scheduler. Blocks until any running cycle completes. Idempotent.

Response: `{"status": "stopped"}`

### `GET /api/v1/agent/scheduler/status`

Returns current scheduler state.

```json
{
  "enabled": true,
  "started": true,
  "running": false,
  "interval_seconds": 60,
  "timeout_seconds": 45,
  "last_run_at": "2026-04-07T12:00:00Z",
  "last_duration_ms": 1234
}
```

All endpoints are admin-authenticated (same middleware chain as existing agent endpoints).

---

## 5. Audit Events Added

| Event | When | Payload |
|-------|------|---------|
| `scheduler.started` | Start() called | interval_seconds, timeout_seconds |
| `scheduler.stopped` | Stop() completes | interval_seconds |
| `scheduler.cycle_started` | Each cycle begins | cycle_id, timeout_seconds |
| `scheduler.cycle_completed` | Cycle succeeds | cycle_id, duration_ms, actions_planned/executed/rejected/failed |
| `scheduler.cycle_failed` | Cycle errors or panics | cycle_id, duration_ms, reason |
| `scheduler.cycle_skipped` | Tick while cycle running | cycle_id, reason |

All events recorded via `audit.AuditRecorder` with `entity_type=scheduler`, `actor_type=system`, `actor_id=scheduler`.

---

## 6. Tests Added

### scheduler package — 9 tests, all PASS

| Test | Validates |
|------|-----------|
| `TestScheduler_PeriodicExecution` | **Validation A** — cycles run automatically on interval; all 4 core audit events emitted |
| `TestScheduler_SingleFlight` | **Validation B** — concurrent cycle execution is prevented; max concurrency = 1 via `concurrencyTrackingRunner` |
| `TestScheduler_SkipVisibility` | **Validation C** — skipped ticks produce `scheduler.cycle_skipped` audit events |
| `TestScheduler_TimeoutEnforcement` | **Validation D** — blocked cycle cancelled via context timeout; emits `cycle_failed` |
| `TestScheduler_PanicRecovery` | Panic inside cycle does not kill scheduler; subsequent cycles still fire |
| `TestScheduler_StartStopLifecycle` | **Validation E** — idempotent start/stop; exactly 1 started + 1 stopped event |
| `TestScheduler_StatusReporting` | Status reflects started/running/stopped state transitions |
| `TestScheduler_StatusDuringRunning` | Status shows `running=true` while cycle is executing |
| `TestScheduler_CleanShutdownDuringCycle` | Stop waits for running cycle to finish before returning |

---

## 7. Validation Results

### Required validations

| Validation | Status | Test |
|-----------|--------|------|
| A — Periodic execution | ✅ | `TestScheduler_PeriodicExecution` |
| B — Single-flight | ✅ | `TestScheduler_SingleFlight` |
| C — Skip visibility | ✅ | `TestScheduler_SkipVisibility` |
| D — Timeout behavior | ✅ | `TestScheduler_TimeoutEnforcement` |
| E — Clean shutdown | ✅ | `TestScheduler_StartStopLifecycle` + `TestScheduler_CleanShutdownDuringCycle` |
| F — Regression safety | ✅ | Full suite: all packages pass |

### Hard invariants

| # | Invariant | Status |
|---|-----------|--------|
| 1 | At most ONE active cycle | ✅ Enforced by mutex-guarded `running` flag |
| 2 | Bounded timeout per cycle | ✅ `context.WithTimeout` in every cycle |
| 3 | Timeout < interval | ✅ Validated in config |
| 4 | Skipped cycles visible | ✅ Audit event emitted |
| 5 | Clean stop | ✅ `wg.Wait()` blocks until loop + cycle done |
| 6 | No recursive triggering | ✅ Ticker-only dispatch |
| 7 | Manual trigger works | ✅ `POST /api/v1/agent/run-actions` unchanged |

### Full test suite

```
ok  github.com/tiroq/arcanum/internal/agent/actionmemory
ok  github.com/tiroq/arcanum/internal/agent/actions
ok  github.com/tiroq/arcanum/internal/agent/goals
ok  github.com/tiroq/arcanum/internal/agent/outcome
ok  github.com/tiroq/arcanum/internal/agent/planning
ok  github.com/tiroq/arcanum/internal/agent/scheduler       (9 tests — NEW)
ok  github.com/tiroq/arcanum/internal/api
ok  github.com/tiroq/arcanum/internal/config
ok  github.com/tiroq/arcanum/internal/contracts
ok  github.com/tiroq/arcanum/internal/control
ok  github.com/tiroq/arcanum/internal/db/models
ok  github.com/tiroq/arcanum/internal/jobs
ok  github.com/tiroq/arcanum/internal/processors
ok  github.com/tiroq/arcanum/internal/prompts
ok  github.com/tiroq/arcanum/internal/providers
ok  github.com/tiroq/arcanum/internal/providers/execution
ok  github.com/tiroq/arcanum/internal/providers/profile
ok  github.com/tiroq/arcanum/internal/providers/routing
ok  github.com/tiroq/arcanum/internal/source
ok  github.com/tiroq/arcanum/internal/worker
```

---

## 8. Remaining Risks

1. **Time-based tests** — Tests use `time.Sleep` with real wall-clock. On heavily loaded CI machines, timing-sensitive tests (SingleFlight, SkipVisibility) could become flaky. Mitigated by using generous margins.
2. **Scheduler lives in api-gateway** — If api-gateway restarts, the scheduler restarts too (if enabled). This is acceptable for a single-node deployment but would need coordination for multi-instance.
3. **No metrics integration** — Scheduler does not currently expose Prometheus counters for cycles/skips/failures. Could be added in a future iteration.
4. **No rate limiting on API** — Start/Stop endpoints have no rate limiting beyond auth. Rapid toggling is harmless (idempotent) but generates audit noise.

---

## 9. Recommended Next Step

**Iteration 8: Self-Reflection + Decision Journal**

Now that the system runs autonomously, the next logical step is:
- Persist planning decisions to a `planning_decisions` table
- After N cycles, reflect on aggregate outcomes vs. plans
- Surface "did my actions actually improve the metrics I was targeting?"
- Feed reflection insights back into the scoring layer

This closes the loop from "I planned X" → "X happened" → "did X help?" → "adjust future plans."
