# Iteration 55 — Goal Decomposition & Long-Horizon Planning

## Summary

Implements a goal decomposition and long-horizon planning subsystem that breaks system-level goals (from `configs/system_goals.yaml`) into actionable subgoals, tracks progress, and emits tasks to the task orchestrator. Evolved with GoalPlan entities, dependency graphs, adaptive replanning, strategy selection, and a dedicated `goal_replanning` autonomy cycle.

## Package: `internal/agent/goal_planning/`

### Files (10)

| File | Purpose |
|------|---------|
| `types.go` | Entities (Subgoal, GoalPlan, GoalDependency, GoalProgress, TaskEmission), constants, state machines, strategy/replan enums, provider interfaces |
| `store.go` | PostgreSQL stores (SubgoalStore, ProgressStore, PlanStore, DependencyStore) + in-memory variants for testing |
| `decomposer.go` | Rule-based decomposition of SystemGoals into Subgoals with deterministic UUIDs |
| `progress.go` | Progress measurement, staleness detection, auto-complete, dependency checking |
| `planner.go` | Task emission planning — filters, scores, sorts subgoals into TaskEmission candidates |
| `engine.go` | Orchestration engine with RunCycle, plan management, strategy application, provider wiring, audit events |
| `adapter.go` | Nil-safe GraphAdapter for API and external integration |
| `dependency.go` | DependencyGraph with DFS cycle detection, topological sort, depth validation |
| `strategy.go` | Strategy selection (4 strategies) and emission adjustment |
| `replanner.go` | Adaptive replanning based on reflection, execution feedback, and objective signals |

### Key Design

- **Decomposition rules**: 6 goal types × 1–3 templates = 12 subgoal templates
- **Horizons**: short (1d), medium (7d), long (30d) + daily (1d), weekly (7d), monthly (30d), continuous (90d)
- **Subgoal state machine**: pending → active → completed/failed/blocked
- **Plan state machine**: draft → active → completed/abandoned, active → replanning → active/abandoned
- **Progress**: MeasureProgress computes [0,1] score; auto-complete at ≥0.90; block after 24h stale + <0.10 progress
- **Task emission**: Cooldown 30min, dependency gating, strategy-adjusted priority/risk
- **Strategy selection**: 4 rule-based strategies: exploit_success_path, reduce_failure_path, diversify_attempts, defer_high_risk
- **Replanning**: Feedback-driven cycle evaluates all active/blocked subgoals against reflection signals, execution feedback, and objective delta
- **Dependency graph**: DAG with cycle detection, topological sort, max depth (3) validation
- **Deterministic IDs**: SHA1-based UUIDs from goal_id + template key
- **Safety constants**: MaxSubgoalsPerGoal=5, MaxTasksPerPlan=10, MaxDepth=3, MaxReplanCount=5, MaxActiveSubgoals=12

### Strategies

| Strategy | When Selected | Effect on Emission |
|----------|--------------|-------------------|
| `exploit_success_path` | Default (no failures, positive history) | Boost priority +10%, reduce risk -10% |
| `reduce_failure_path` | ≥3 consecutive failures | Reduce priority -20%, increase risk +15% |
| `diversify_attempts` | Mixed success/failure history | No priority change, reduce risk -5% |
| `defer_high_risk` | Objective delta < -5% | Reduce priority -40%, increase risk +20% |

### Replan Triggers

| Trigger | Condition | Action |
|---------|-----------|--------|
| `execution_failure` | Any failure, <3 consecutive | Change strategy |
| `repeated_failure` | ≥3 consecutive failures | Block subgoal + change strategy |
| `positive_reinforcement` | ≥2 successes, no failures | Boost priority +10% |
| `objective_penalty` | Net utility delta < -5% | Defer with defer_high_risk strategy |

### Provider Interfaces

| Interface | Purpose |
|-----------|---------|
| `ObjectiveProvider` | Net utility/risk for urgency scaling and replan evaluation |
| `CapacityProvider` | Available hours for effort filtering |
| `TaskEmitter` | Emits tasks to task orchestrator |
| `ReflectionProvider` | Reflection signals for replan cycle |
| `ExecutionFeedbackProvider` | Success/failure counts per goal for replan cycle |

## Migrations

- **000059_create_agent_goal_planning.up.sql**: `agent_subgoals` (21 columns, 2 indexes) + `agent_goal_progress` (7 columns, 2 indexes)
- **000060_evolve_goal_planning.up.sql**: `agent_goal_plans` (12 columns, 2 indexes) + `agent_goal_dependencies` (5 columns, 1 index) + ALTER `agent_subgoals` +4 columns (plan_id, strategy, failure_count, success_count)

## Integration

### Autonomy Orchestrator
- **goal_planning** cycle: Decomposes goals, activates subgoals, measures progress, emits tasks
- **goal_replanning** cycle (NEW): Runs adaptive replanning based on feedback signals
- `GoalPlanningRunner` interface in `chain_closure.go`
- `GoalReplanningRunner` interface in `chain_closure.go` (NEW)
- `autonomyGoalPlanBridge` in main.go bridges to `Engine.RunCycle(ctx, goals)`
- `autonomyGoalReplanBridge` in main.go bridges to `Replanner.RunReplanCycle(ctx)` (NEW)

### API Endpoints
| Method | Path | Handler |
|--------|------|---------|
| GET | `/api/v1/agent/goals/subgoals` | `GoalPlanningSubgoals` |
| GET | `/api/v1/agent/goals/subgoals/{goal_id}` | `GoalPlanningSubgoalsByGoal` |
| GET | `/api/v1/agent/goals/progress/{goal_id}` | `GoalPlanningProgress` |
| GET | `/api/v1/agent/goals/plans` | `GoalPlanningListPlans` (NEW) |
| POST | `/api/v1/agent/goals/plan` | `GoalPlanningCreatePlan` (NEW) |
| POST | `/api/v1/agent/goals/replan` | `GoalPlanningReplan` (NEW) |

### Bridge Adapters (main.go)
| Struct | Implements | Delegates To |
|--------|-----------|-------------|
| `goalPlanEmitterBridge` | `goal_planning.TaskEmitter` | `task_orchestrator.GraphAdapter.CreateTask` |
| `autonomyGoalPlanBridge` | `autonomy.GoalPlanningRunner` | `goal_planning.Engine.RunCycle` |
| `autonomyGoalReplanBridge` | `autonomy.GoalReplanningRunner` | `goal_planning.Replanner.RunReplanCycle` (NEW) |

## Test Results

- **83 tests** in `goal_planning_test.go` — ALL PASS
- **55 packages** across full suite — ALL PASS
- **Zero regressions**

### Test Coverage

| Area | Tests |
|------|-------|
| Decomposer | 8 (income type, unknown type, invalid horizon, metric override, invalid target, deterministic IDs, all types, priority clamp) |
| Progress | 11 (standard/completed/exceeds/zero target/violations, stale, auto-complete, block, dependency, overall) |
| Planner | 5 (active subgoals, skips completed, cooldown, dependency, not-active) |
| State machine | 2 (subgoal transitions, plan transitions) |
| Engine | 14 (decompose, activate, max active, dependency blocking, auto-complete, block, dry run, emitter, RunCycle, transitions, summary, create plan, list plans, decompose creates plan, replan cycle, nil replanner) |
| Strategy | 8 (select default, repeated failure, objective penalty, diversify, apply defer, apply exploit, should replan triggers×4, no action) |
| Dependency graph | 7 (no cycle, with cycle, topological sort, sort cycle nil, depth, max depth validation, prerequisites) |
| Plan store | 2 (in-memory plan store, in-memory dependency store) |
| Replanner | 7 (repeated failure, objective penalty, reinforcement, no action, reflection signals, skips completed, with engine) |
| Adapter | 5 (nil-safe×2 existing, list plans nil, replan nil, run replan cycle nil) |
| Constants/horizons | 4 (MaxSubgoalsPerGoal, MaxTasksPerPlan, MaxDepth, horizon days) |
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
| `goal_planning.plan_created` | GoalPlan created (NEW) |
| `goal_planning.replan_completed` | Replan cycle finished (NEW) |
| `goal_planning.subgoal_replanned` | Individual subgoal replanned (NEW) |

## Architecture Compliance

- ✅ Bus-first: Tasks emitted via orchestrator, not direct calls
- ✅ Explicit state machine: Subgoals (5 states) + Plans (5 states) with ValidTransitions maps
- ✅ No silent failure: All errors logged, nil-safe adapters, fail-open providers
- ✅ Observability: 9 audit event types
- ✅ Deterministic: SHA1 UUIDs, rule-based decomposition/strategy, no randomness
- ✅ Bounded: MaxDepth=3, MaxSubgoalsPerGoal=5, MaxTasksPerPlan=10, MaxReplanCount=5
- ✅ Recoverable: Replanning cycle detects stuck/failed subgoals and adjusts strategies
- ✅ Dependency safety: DAG cycle detection prevents circular dependencies
- ✅ Fail-open: All providers optional, return zero defaults
