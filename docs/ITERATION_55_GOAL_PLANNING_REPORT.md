# Iteration 55 — Goal Decomposition & Long-Horizon Planning

## Summary

Implements a goal decomposition and long-horizon planning subsystem that breaks system-level goals (from `configs/system_goals.yaml`) into actionable subgoals, tracks progress, and emits tasks to the task orchestrator.

## Package: `internal/agent/goal_planning/`

### Files Created (7)

| File | Purpose |
|------|---------|
| `types.go` | Entities (Subgoal, GoalProgress, TaskEmission), constants, state machine, provider interfaces |
| `store.go` | PostgreSQL stores (SubgoalStore, ProgressStore) + in-memory variants for testing |
| `decomposer.go` | Rule-based decomposition of SystemGoals into Subgoals with deterministic UUIDs |
| `progress.go` | Progress measurement, staleness detection, auto-complete, dependency checking |
| `planner.go` | Task emission planning — filters, scores, sorts subgoals into TaskEmission candidates |
| `engine.go` | Orchestration engine with RunCycle, provider wiring, audit events |
| `adapter.go` | Nil-safe GraphAdapter for API and external integration |

### Key Design

- **Decomposition rules**: 6 goal types × 1–3 templates = 12 subgoal templates
- **Horizons**: immediate (7d), short (30d), medium (90d), long (180d)
- **State machine**: pending → active → completed/failed/blocked
- **Progress**: MeasureProgress computes [0,1] score; auto-complete at ≥0.90; block after 24h stale + <0.10 progress
- **Task emission**: Cooldown 30min, dependency gating, priority = urgency×0.4 + goalPriority×0.35 + (1-progress)×0.25
- **Deterministic IDs**: SHA1-based UUIDs from goal_id + template key
- **Constants**: MaxSubgoalsPerGoal=8, MaxActiveSubgoals=12, MinProgressToComplete=0.90

### Provider Interfaces

| Interface | Purpose |
|-----------|---------|
| `ObjectiveProvider` | Net utility for urgency scaling |
| `CapacityProvider` | Available hours for effort filtering |
| `TaskEmitter` | Emits tasks to task orchestrator |

## Migration

- **000059_create_agent_goal_planning.up.sql**: `agent_subgoals` (17 columns, 2 indexes) + `agent_goal_progress` (7 columns, 2 indexes)
- **000059_create_agent_goal_planning.down.sql**: DROP both tables

## Integration

### Autonomy Orchestrator
- New cycle: `goal_planning` (registered in `allCycles`, `runCycle` switch)
- `GoalPlanningRunner` interface in `chain_closure.go`
- `autonomyGoalPlanBridge` in main.go bridges to `Engine.RunCycle(ctx, goals)`
- Nil-safe: skips if engine or goals are nil

### API Endpoints
| Method | Path | Handler |
|--------|------|---------|
| GET | `/api/v1/agent/goals/subgoals` | `GoalPlanningSubgoals` |
| GET | `/api/v1/agent/goals/subgoals/{goal_id}` | `GoalPlanningSubgoalsByGoal` |
| GET | `/api/v1/agent/goals/progress/{goal_id}` | `GoalPlanningProgress` |

### Bridge Adapters (main.go)
| Struct | Implements | Delegates To |
|--------|-----------|-------------|
| `goalPlanEmitterBridge` | `goal_planning.TaskEmitter` | `task_orchestrator.GraphAdapter.CreateTask` |
| `autonomyGoalPlanBridge` | `autonomy.GoalPlanningRunner` | `goal_planning.Engine.RunCycle` |

## Test Results

- **39 tests** in `goal_planning_test.go` — ALL PASS
- **55 packages** across full suite — ALL PASS
- **Zero regressions**

### Test Coverage

| Area | Tests |
|------|-------|
| Decomposer | 8 (income type, unknown type, invalid horizon, metric override, invalid target, deterministic IDs, all types, priority clamp) |
| Progress | 11 (standard/completed/exceeds/zero target/violations, stale, auto-complete, block, dependency, overall) |
| Planner | 5 (active subgoals, skips completed, cooldown, dependency, not-active) |
| State machine | 1 (10 transitions) |
| Engine | 9 (decompose, activate, max active, dependency blocking, auto-complete, block, dry run, emitter, RunCycle, transitions, summary) |
| Adapter | 2 (nil-safe, with engine) |
| Rules/clamp | 2 |

## Audit Events

| Event | Trigger |
|-------|---------|
| `goal_planning.decomposed` | Goals decomposed into subgoals |
| `goal_planning.activated` | Subgoals transitioned to active |
| `goal_planning.progress_updated` | Progress measured and persisted |
| `goal_planning.task_emitted` | Task sent to orchestrator |
| `goal_planning.cycle_completed` | Full RunCycle finished |
| `goal_planning.transition` | Subgoal state transition |

## Architecture Compliance

- ✅ Bus-first: Tasks emitted via orchestrator, not direct calls
- ✅ Explicit state machine: 5 states with ValidSubgoalTransitions map
- ✅ No silent failure: All errors logged, nil-safe adapters
- ✅ Observability: 6 audit event types
- ✅ Deterministic: SHA1 UUIDs, rule-based decomposition, no randomness
- ✅ Fail-open: All providers optional, return zero defaults
