# Iteration 21 — Path Memory + Transition Learning Layer: Validation Report

## 1. Overview

Iteration 21 introduces path-level and transition-level learning to the Arcanum decision graph. The system now records which multi-step paths (sequences of action types) succeed or fail, and which transitions between adjacent actions contribute positively or negatively. This historical data feeds back into the decision graph scoring pipeline to influence future path selection.

**Core concept**: When the graph evaluates candidate paths, it adjusts scores based on historical path success rates (path memory) and individual transition helpfulness (transition memory).

---

## 2. Architecture

### Data Flow

```
Path Executed → Outcome Evaluated → Path Memory Updated + Transition Memory Updated
                                              ↓                        ↓
                                   PathFeedback generated   TransitionFeedback generated
                                              ↓                        ↓
                                   GraphPlannerAdapter.EvaluateForPlanner()
                                              ↓
                                   ApplyPathLearningAdjustments(paths, signals)
                                              ↓
                                   SelectBestPath(adjusted paths)
```

### Integration Points

1. **Decision Graph** — `ApplyPathLearningAdjustments` applied between `EvaluateAllPaths` and `SelectBestPath`
2. **Outcome Handler** — `evaluatePathOutcome` called after action outcome evaluation
3. **Planner** — `StrategyOverride` carries path metadata (`PathSignature`, `PathActionTypes`, `PathLength`)
4. **API** — 3 new endpoints for observability

---

## 3. Files Created

| File | Purpose |
|------|---------|
| `internal/db/migrations/000026_create_agent_path_learning.up.sql` | Creates `agent_path_memory`, `agent_transition_memory`, `agent_path_outcomes` tables |
| `internal/db/migrations/000026_create_agent_path_learning.down.sql` | Rollback migration |
| `internal/agent/path_learning/types.go` | Core types, constants, pure functions (BuildPathSignature, GeneratePathFeedback, etc.) |
| `internal/agent/path_learning/memory.go` | MemoryStore — path memory + path outcome persistence with UPSERT |
| `internal/agent/path_learning/transition.go` | TransitionStore — transition memory persistence with UPSERT |
| `internal/agent/path_learning/evaluator.go` | Evaluator — evaluates path outcomes, updates both stores, emits audit events |
| `internal/agent/path_learning/adapter.go` | GraphAdapter — implements `PathLearningProvider` interface for decision graph |
| `internal/agent/path_learning/path_learning_test.go` | 29 unit tests covering all pure functions, feedback generation, thresholds |

---

## 4. Files Modified

| File | Changes |
|------|---------|
| `internal/agent/decision_graph/evaluator.go` | Added `PathLearningSignals`, `ApplyPathLearningAdjustments`, `pathSignatureFromNodes`, score adjustment constants |
| `internal/agent/decision_graph/planner_adapter.go` | Added `PathLearningProvider` interface, `WithPathLearning`, path learning wiring in `EvaluateForPlanner`, path metadata on `StrategyOverride` |
| `internal/agent/decision_graph/decision_graph_test.go` | Added 13 integration tests for `ApplyPathLearningAdjustments` |
| `internal/agent/planning/planner.go` | Extended `StrategyOverride` with `PathSignature`, `PathActionTypes`, `PathLength`; planner tags these on `Action.Params` |
| `internal/agent/outcome/handler.go` | Added `PathOutcomeEvaluator` interface, `WithPathOutcomeEvaluator`, `evaluatePathOutcome` |
| `internal/api/handlers.go` | Added `PathMemoryList`, `TransitionMemoryList`, `PathOutcomesList` handlers |
| `internal/api/router.go` | Added 3 routes under `/api/v1/agent/` |
| `cmd/api-gateway/main.go` | Wired path learning stores, adapter, evaluator |

---

## 5. Data Model

### agent_path_memory
| Column | Type | Description |
|--------|------|-------------|
| id | SERIAL PK | Auto-increment |
| path_signature | TEXT | Canonical joined action types (e.g., `retry_job>log_recommendation`) |
| goal_type | TEXT | Goal family context |
| total_attempts | INT | Total times path was selected |
| success_count | INT | Successful outcome count |
| failure_count | INT | Failed outcome count |
| neutral_count | INT | Neutral outcome count |
| avg_value | DOUBLE | Running mean of outcome values |
| last_outcome | TEXT | Most recent outcome status |
| created_at / updated_at | TIMESTAMPTZ | Timestamps |

**Unique constraint**: `(path_signature, goal_type)`

### agent_transition_memory
| Column | Type | Description |
|--------|------|-------------|
| id | SERIAL PK | Auto-increment |
| transition_key | TEXT | Canonical `from->to` key (e.g., `retry_job->log_recommendation`) |
| goal_type | TEXT | Goal family context |
| total_seen | INT | Total transition observations |
| helpful_count | INT | Times classified as helpful |
| unhelpful_count | INT | Times classified as unhelpful |
| neutral_count | INT | Times classified as neutral |
| avg_value_delta | DOUBLE | Mean value change across transition |
| created_at / updated_at | TIMESTAMPTZ | Timestamps |

**Unique constraint**: `(transition_key, goal_type)`

### agent_path_outcomes
| Column | Type | Description |
|--------|------|-------------|
| id | SERIAL PK | Auto-increment |
| path_signature | TEXT | Path that was evaluated |
| goal_type | TEXT | Goal family |
| outcome_status | TEXT | success/failure/neutral |
| outcome_value | DOUBLE | Numeric value |
| executed_transitions | INT | Number of transitions actually executed |
| continuation_used | BOOLEAN | Whether continuation logic was used |
| step1_action | TEXT | First action type in path |
| step2_action | TEXT | Second action type (nullable) |
| metadata | JSONB | Extra context |
| created_at | TIMESTAMPTZ | Timestamp |

---

## 6. Score Adjustment Model

### Path-Level Adjustments

| Feedback | Condition | Score Δ |
|----------|-----------|---------|
| `prefer_path` | success_rate ≥ 0.7, min 5 samples | +0.10 |
| `avoid_path` | failure_rate ≥ 0.5, min 5 samples | -0.20 |
| `neutral` | Neither condition met | +0.00 |

### Transition-Level Adjustments (per edge)

| Feedback | Condition | Score Δ |
|----------|-----------|---------|
| `prefer_transition` | helpful_rate ≥ 0.6, min 5 samples | +0.05 |
| `avoid_transition` | unhelpful_rate ≥ 0.5, min 5 samples | -0.10 |
| `neutral` | Neither condition met | +0.00 |

### Application Order

1. `EvaluateAllPaths` computes base FinalScore per path
2. `ApplyPathLearningAdjustments` adds path-level delta + per-edge transition deltas
3. Result clamped to [0, 1]
4. `SelectBestPath` picks the highest adjusted score

---

## 7. Transition Helpfulness Classification

Transitions are classified when evaluated:
- **helpful**: `step2_status == "success"` OR (`step1_status == "neutral"` AND `step2_status == "neutral"` AND `outcome_value > 0`)
- **unhelpful**: `step2_status` is `"failure"` or `"error"`, OR (`step1_status == "success"` AND `step2_status != "success"`)
- **neutral**: everything else (including when step2 is absent)

---

## 8. API Endpoints

### GET /api/v1/agent/path-memory
Query parameters: `goal_type`, `path_signature`
Returns: list of path memory records with computed feedback recommendation for each.

### GET /api/v1/agent/transition-memory
Query parameters: `goal_type`, `transition_key`
Returns: list of transition memory records.

### GET /api/v1/agent/path-outcomes
Query parameters: `goal_type`, `limit` (default 100, max 500)
Returns: recent path outcomes sorted by newest first.

---

## 9. Audit Events

| Event | Entity Type | When |
|-------|-------------|------|
| `path.outcome_evaluated` | `path_learning` | After path outcome saved + path memory updated |
| `path.feedback_generated` | `path_learning` | When feedback is computed for a path |
| `transition.feedback_generated` | `path_learning` | When feedback is computed for a transition |

---

## 10. Test Results

### path_learning package — 29 tests
```
PASS — TestBuildPathSignature_Canonical
PASS — TestBuildPathSignature_OrderMatters
PASS — TestBuildTransitionKey_Canonical
PASS — TestBuildTransitionKey_OrderMatters
PASS — TestExtractTransitions
PASS — TestClassifyTransitionHelpfulness_HelpfulWithImprovement
PASS — TestClassifyTransitionHelpfulness_HelpfulStep2Success
PASS — TestClassifyTransitionHelpfulness_Unhelpful
PASS — TestClassifyTransitionHelpfulness_UnhelpfulNeutral
PASS — TestClassifyTransitionHelpfulness_NeutralNoStep2
PASS — TestGeneratePathFeedback_PreferPath
PASS — TestGeneratePathFeedback_AvoidPath
PASS — TestGeneratePathFeedback_Neutral
PASS — TestGeneratePathFeedback_InsufficientData
PASS — TestGenerateTransitionFeedback_PreferTransition
PASS — TestGenerateTransitionFeedback_AvoidTransition
PASS — TestGenerateTransitionFeedback_Neutral
PASS — TestGenerateTransitionFeedback_InsufficientData
PASS — TestOutcomeIncrements
PASS — TestHelpfulnessIncrements
PASS — TestPathFeedback_Deterministic
PASS — TestTransitionFeedback_Deterministic
PASS — TestPathFeedback_AtThreshold
PASS — TestPathFeedback_BelowThreshold
PASS — TestTransitionFeedback_AtThreshold
PASS — TestPathOutcome_Fields
PASS — TestNeutralPathFeedback / TestNeutralTransitionFeedback
PASS — TestFirstStepAction
PASS — TestClassifyTransitionHelpfulness_NoStep2Status / _Failure
PASS — TestScoreAdjustmentConstants
```

### decision_graph package — 13 new tests (35 total)
```
PASS — TestApplyPathLearningAdjustments_PreferPathIncreasesScore
PASS — TestApplyPathLearningAdjustments_AvoidPathDecreasesScore
PASS — TestApplyPathLearningAdjustments_PreferTransitionIncreasesScore
PASS — TestApplyPathLearningAdjustments_AvoidTransitionDecreasesScore
PASS — TestApplyPathLearningAdjustments_CombinedAdjustments
PASS — TestApplyPathLearningAdjustments_NilSignalsNoChange
PASS — TestApplyPathLearningAdjustments_NoMatchingSignalsNoChange
PASS — TestApplyPathLearningAdjustments_SingleNodeNoTransitionEffect
PASS — TestApplyPathLearningAdjustments_ScoreClampedToRange
PASS — TestApplyPathLearningAdjustments_MultipleTransitions
PASS — TestPathSignatureFromNodes / _Empty / _Single
```

### Full suite — 0 failures, 0 regressions
All existing packages continue to pass.

---

## 11. Validation Checklist

| # | Requirement | Status |
|---|-------------|--------|
| 1 | Path signature canonically joins action types with `>` | ✅ |
| 2 | Transition key uses `from->to` format | ✅ |
| 3 | Path memory UPSERT atomically increments counters | ✅ |
| 4 | Transition memory UPSERT atomically increments counters | ✅ |
| 5 | Feedback thresholds: prefer_path ≥0.7/5, avoid_path ≥0.5/5 | ✅ |
| 6 | Feedback thresholds: prefer_transition ≥0.6/5, avoid_transition ≥0.5/5 | ✅ |
| 7 | Score adjustments: path +0.10/-0.20, transition +0.05/-0.10 | ✅ |
| 8 | Applied between EvaluateAllPaths and SelectBestPath | ✅ |
| 9 | Clamped to [0, 1] | ✅ |
| 10 | Nil/missing signals → no change (fail-open) | ✅ |
| 11 | Only executed transitions are updated | ✅ |
| 12 | 3 API endpoints with proper filtering | ✅ |
| 13 | 3 audit event types emitted | ✅ |
| 14 | Path metadata tagged on Action.Params | ✅ |
| 15 | 42 total tests (29 + 13), all passing | ✅ |
| 16 | Zero regressions across full test suite | ✅ |
| 17 | No import cycles | ✅ |
| 18 | Migration 000026 with proper up/down | ✅ |
