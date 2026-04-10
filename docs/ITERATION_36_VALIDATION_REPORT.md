# Iteration 36 — Validation Report: Income Engine

**Date**: 2026-04-10
**Status**: PASS
**Regressions**: 0

---

## Summary

Iteration 36 delivers a minimal but real income-oriented operational layer.
The system can now detect, prioritise, track, and learn from income-related
opportunities through a deterministic pipeline: **opportunity → score →
proposal → outcome → decision graph signal**.

---

## Components Delivered

### 1. Income Package (`internal/agent/income/`)

| File | Purpose |
|------|---------|
| `types.go` | Core types: `IncomeOpportunity`, `IncomeActionProposal`, `IncomeOutcome`, `IncomeSignal`; constants and status enums |
| `store.go` | `OpportunityStore` — PostgreSQL CRUD with `BestOpenScore()` and `CountOpen()` |
| `proposal_store.go` | `ProposalStore` — PostgreSQL CRUD for proposals |
| `outcome_store.go` | `OutcomeStore` — PostgreSQL CRUD for outcomes |
| `scorer.go` | `ScoreOpportunity()` — deterministic formula: `value*0.40 + confidence*0.30 - effort*0.20`, clamped [0,1] |
| `mapper.go` | `MapOpportunityToActions()` — maps opportunity types to agent action types; `IsIncomeAction()` guard |
| `generator.go` | `GenerateProposals()` — governance-aware proposal generation with risk derivation and review gates |
| `engine.go` | `Engine` — full pipeline orchestrator with audit events |
| `graph_adapter.go` | `GraphAdapter` — implements `IncomeSignalProvider` for the decision graph |
| `income_test.go` | 35 unit tests covering all pure functions |

### 2. Decision Graph Integration (`internal/agent/decision_graph/planner_adapter.go`)

- **New interface**: `IncomeSignalProvider` with `GetIncomeSignal()` and `IsIncomeRelated()`
- **New field**: `incomeSignals` on `GraphPlannerAdapter`
- **New method**: `WithIncomeSignals()` for builder chain
- **Pipeline position**: After goal alignment, before `SelectBestPath`
- **Mechanism**: Additive boost = `bestOpenScore * 0.15` applied only to income-related first actions
- **Fail-open**: No signal when provider is nil or no open opportunities

### 3. Database Migrations

| Migration | Table |
|-----------|-------|
| `000038` | `agent_income_opportunities` — opportunities with status, type, value, effort, confidence |
| `000039` | `agent_income_proposals` — proposals with FK to opportunities, review gates, risk level |
| `000040` | `agent_income_outcomes` — outcomes with FK to opportunities, actual value, status |

All tables have proper indexes on status, type, and `created_at DESC`.

### 4. API Endpoints

| Method | Path | Handler |
|--------|------|---------|
| GET/POST | `/api/v1/agent/income/opportunities` | List/create opportunities |
| POST | `/api/v1/agent/income/evaluate` | Score + generate proposals |
| GET | `/api/v1/agent/income/proposals` | List proposals |
| GET/POST | `/api/v1/agent/income/outcomes` | List/record outcomes |
| GET | `/api/v1/agent/income/signal` | Current income signal snapshot |

### 5. Configuration

- `configs/income_opportunity_types.yaml` — Taxonomy of 5 opportunity types with defaults, signals, proposal actions, and governance rules

---

## Scoring Pipeline Order

```
path_learning → comparative → counterfactual → arbitration →
resource_penalty → goal_alignment → income_signal → select
```

Income signal is the final additive boost before path selection.

---

## Governance Integration

- Proposals generated during frozen/safe_hold/rollback_only governance modes
  are automatically flagged `requires_review = true`
- Big-ticket proposals (expected_value > $5,000) always require review
- High-risk proposals (derived from low confidence + high effort) always require review

---

## Audit Events

| Event | When |
|-------|------|
| `income.opportunity_created` | New opportunity persisted |
| `income.opportunity_evaluated` | Opportunity scored |
| `income.proposal_created` | Each proposal generated |
| `income.outcome_recorded` | Outcome persisted + opportunity closed |
| `income.signal_applied` | Income boost applied in decision graph |

---

## Test Results

```
=== Income Package (35 tests) ===
PASS: TestScoreOpportunity_HighValueHighConfidence
PASS: TestScoreOpportunity_ZeroValue
PASS: TestScoreOpportunity_MaxValue
PASS: TestScoreOpportunity_ClampedToZero
PASS: TestScoreOpportunity_ClampedToOne
PASS: TestIsIncomeAction_Known
PASS: TestIsIncomeAction_Unknown
PASS: TestMapOpportunityToActions_Consulting
PASS: TestMapOpportunityToActions_Automation
PASS: TestMapOpportunityToActions_Service
PASS: TestMapOpportunityToActions_Content
PASS: TestMapOpportunityToActions_Unknown
PASS: TestGenerateProposals_BelowThreshold
PASS: TestGenerateProposals_AboveThreshold
PASS: TestGenerateProposals_GovernanceFrozenRequiresReview
PASS: TestGenerateProposals_HighRiskRequiresReview
PASS: TestGenerateProposals_BigTicketRequiresReview
PASS: TestDeriveRiskLevel_Low
PASS: TestDeriveRiskLevel_Medium
PASS: TestDeriveRiskLevel_High
PASS: TestClamp01_InRange
PASS: TestClamp01_Below
PASS: TestClamp01_Above
PASS: TestValidateOpportunity_Valid
PASS: TestValidateOpportunity_EmptyTitle
PASS: TestValidateOpportunity_InvalidType
PASS: TestValidateOpportunity_NegativeValue
PASS: TestValidateOpportunity_EffortOutOfRange
PASS: TestValidateOpportunity_ConfidenceOutOfRange
PASS: TestGraphAdapter_NilEngine
PASS: TestGraphAdapter_NilAdapter
PASS: TestGraphAdapter_IsIncomeRelated
PASS: TestIncomeSignal_ZeroValue
PASS: TestConstants_Bounds
PASS: TestValidOpportunityTypes_AllPresent
PASS: TestValidOpportunityTypes_InvalidNotPresent

=== Full Agent Suite (0 regressions) ===
ok  actionmemory, actions, arbitration, calibration, causal,
    counterfactual, decision_graph, exploration, goals, governance,
    income, meta_reasoning, outcome, path_comparison, path_learning,
    planning, policy, provider_catalog, provider_routing, reflection,
    resource_optimization, scheduler, stability, strategy, strategy_learning
```

---

## Build Verification

```
$ go build ./...    → PASS (no errors)
$ go test ./internal/agent/... -count=1 → ALL PASS, 0 regressions
```

---

## Architecture Compliance

| Principle | Status |
|-----------|--------|
| Bus-first architecture | ✅ Events via audit bus; no direct service calls |
| Explicit state machines | ✅ Opportunity: open → evaluated → proposed → closed/rejected |
| No silent failure | ✅ All errors logged; audit trail for every operation |
| LLM contracts strict | N/A (no LLM in this iteration) |
| Observability first | ✅ 5 audit events, API signal endpoint, full trace |
| Deterministic recovery | ✅ Scoring is pure; proposals are idempotent; outcomes close opportunities |
| Minimal magic | ✅ Simple pipeline, explicit thresholds, no hidden side effects |

---

## Fail-Open Design

- `GraphAdapter.GetIncomeSignal()` returns (0, 0) when engine is nil
- Income signal boost is skipped when no open opportunities exist
- `IncomeSignalMaxBoost = 0.15` caps maximum influence at 15%
- All store operations fail gracefully without crashing the pipeline

---

## Files Changed/Created

### New files (10)
- `internal/agent/income/types.go`
- `internal/agent/income/store.go`
- `internal/agent/income/proposal_store.go`
- `internal/agent/income/outcome_store.go`
- `internal/agent/income/scorer.go`
- `internal/agent/income/mapper.go`
- `internal/agent/income/generator.go`
- `internal/agent/income/engine.go`
- `internal/agent/income/graph_adapter.go`
- `internal/agent/income/income_test.go`

### New migrations (6 files)
- `internal/db/migrations/000038_create_agent_income_opportunities.{up,down}.sql`
- `internal/db/migrations/000039_create_agent_income_proposals.{up,down}.sql`
- `internal/db/migrations/000040_create_agent_income_outcomes.{up,down}.sql`

### Modified files (4)
- `internal/agent/decision_graph/planner_adapter.go` — IncomeSignalProvider interface + pipeline integration
- `internal/api/handlers.go` — 5 income handler methods + WithIncomeEngine
- `internal/api/router.go` — 5 income routes
- `cmd/api-gateway/main.go` — income engine wiring

### Configuration (1)
- `configs/income_opportunity_types.yaml` — opportunity type taxonomy
