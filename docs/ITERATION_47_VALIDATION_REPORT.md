# Iteration 47 — Negotiation / Pricing Intelligence — Validation Report

## 1 Summary

Iteration 47 adds a **Negotiation / Pricing Intelligence** layer to the Arcanum agent.
The system computes deterministic price bands (minimum / target / stretch) for every
income opportunity, manages a 6-state negotiation state machine, applies bounded
concession logic, and learns from verified outcomes — all without autonomous
negotiation.  Every pricing recommendation is explainable, auditable, and subject
to governance review gates.

| Metric | Value |
|---|---|
| New files | 8 (6 source + 2 migration) |
| Modified files | 3 (handlers, router, main) |
| New tests | 28 |
| Regression tests | 70 packages, 0 failures |
| Migration | `000050_create_agent_pricing` (4 tables) |
| API endpoints | 6 |
| Audit event types | 7 |

---

## 2 Pricing Model

### 2.1 Price Band Computation

```
cost_basis = base_hourly_rate × effort_hours × type_multiplier
minimum    = cost_basis × MinMultiplier (1.2)
target     = cost_basis × TargetMultiplier (1.8) [± performance adjustment]
stretch    = cost_basis × StretchMultiplier (2.4)
```

**Adjustments applied to minimum (floor):**

| Factor | Formula | Cap |
|---|---|---|
| Financial pressure | `min += cost_basis × pressure × MaxPressureFloorBoost` | +20% of cost_basis |
| Capacity overload | `min += cost_basis × (utilisation − 0.7) × MaxCapacityFloorBoost` | +25% of cost_basis |

**Adjustment applied to target:**

| Factor | Formula | Guard |
|---|---|---|
| Historical performance | `target *= (1 + (winRate − 0.5) × 0.20)` | Only when `total_outcomes ≥ MinOutcomesForPerformance (5)` |

**Invariant enforced:** `minimum ≤ target ≤ stretch` — clamped after all adjustments.

### 2.2 Type Multipliers

| Opportunity Type | Multiplier |
|---|---|
| consulting | 1.0 |
| automation | 1.3 |
| service | 1.1 |
| content | 0.8 |
| other | 1.0 |

### 2.3 Constants

| Name | Value | Purpose |
|---|---|---|
| `BaseHourlyRate` | 100.0 | Default hourly rate |
| `MinMultiplier` | 1.2 | Floor multiplier |
| `TargetMultiplier` | 1.8 | Target multiplier |
| `StretchMultiplier` | 2.4 | Stretch multiplier |
| `MaxConcessionCount` | 5 | Hard limit on concessions per negotiation |
| `ConcessionStepFraction` | 0.10 | Each concession reduces remaining by 10% |
| `MaxPressureFloorBoost` | 0.20 | Pressure cap on floor boost |
| `MaxCapacityFloorBoost` | 0.25 | Capacity cap on floor boost |
| `MinOutcomesForPerformance` | 5 | Cold-start guard for learning |
| `DefaultConfidence` | 0.50 | Confidence when insufficient data |
| `MaxConfidence` | 0.95 | Confidence cap |
| `MinConfidence` | 0.30 | Confidence floor |

---

## 3 Negotiation State Machine

```
                ┌──────────────────────────────────────┐
                │                                      ▼
draft ──► quoted ──► negotiating ──► accepted ──► won
                         │                        ▲
                         │                        │
                         ├── counter_offered ──┘ (→ negotiating)
                         │
                         └── rejected ──► lost
                         
quoted ──────────────────────────────────► lost
negotiating ──────────────────────────────► lost
```

### 3.1 Valid Transitions

| From | To |
|---|---|
| `draft` | `quoted` |
| `quoted` | `negotiating`, `accepted`, `lost` |
| `negotiating` | `counter_offered`, `accepted`, `rejected`, `lost` |
| `counter_offered` | `negotiating` |
| `accepted` | `won`, `lost` |
| `rejected` | `lost` |

Invalid transitions produce `InvalidTransitionError` (non-silent failure).

---

## 4 Concession Logic

```go
remaining   = currentOffer − minimumPrice
concession  = remaining × ConcessionStepFraction  // 10%
newOffer    = currentOffer − concession
```

**Bounds enforced:**
- `newOffer` is never below `minimumPrice`
- `concessionCount` is capped at `MaxConcessionCount` (5)
- At cap: `exhausted = true`, no further concessions
- At floor: concession amount = 0

This is a *convergent* model: each step is smaller than the last, naturally
converging towards the floor without crossing it.

---

## 5 Learning from Outcomes

### 5.1 Performance Aggregation

For each strategy type, outcomes are aggregated into `PricingPerformance`:

| Field | Computation |
|---|---|
| `win_rate` | won / total |
| `avg_discount` | mean of (1 − accepted/quoted) for won deals |
| `avg_accepted_price` | mean accepted price for won deals |
| `avg_quoted_price` | mean quoted price across all deals |
| `total_outcomes` | count of outcomes |

### 5.2 Confidence Model

```go
if total_outcomes < MinOutcomesForPerformance → DefaultConfidence (0.50)
otherwise: clamp(0.50 + total × 0.05, MinConfidence, MaxConfidence)
```

Confidence increases with data but is capped at 0.95 to prevent overconfidence.

### 5.3 Target Adjustment

When `total_outcomes ≥ 5`:
```go
target *= (1 + (win_rate − 0.5) × 0.20)
```

- Win rate > 50% → target increases (market accepts higher prices)
- Win rate < 50% → target decreases (market pushes back)
- Win rate = 50% → no adjustment

---

## 6 Integration Points

### 6.1 Provider Interfaces (Local, Cycle-Free)

```go
// In engine.go — avoids importing financial_pressure or capacity packages
type FinancialPressureProvider interface {
    GetPressure(ctx) (score float64, urgency string, err error)
}
type CapacityProvider interface {
    GetUtilisation(ctx) (float64, error)
}
type GovernanceProvider interface {
    GetMode(ctx) (string, error)
}
```

### 6.2 Startup Wiring (main.go)

Bridge adapters (`pricingFinancialPressureAdapter`, `pricingCapacityAdapter`)
wrap existing graph adapters to satisfy pricing's local interfaces —
identical pattern used by portfolio (Iteration 46).

### 6.3 Governance Integration

- `ComputeProfile` checks governance mode before proceeding
- `frozen`, `rollback_only`, `safe_hold` → pricing blocked with error
- Audit event records governance state

---

## 7 API Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/agent/pricing/profiles` | List all pricing profiles |
| `POST` | `/api/v1/agent/pricing/compute/{opportunityID}` | Compute price bands for opportunity |
| `GET` | `/api/v1/agent/pricing/negotiations` | List all negotiation records |
| `POST` | `/api/v1/agent/pricing/recommend/{opportunityID}` | Generate pricing recommendation |
| `POST` | `/api/v1/agent/pricing/outcomes` | Record a pricing outcome |
| `GET` | `/api/v1/agent/pricing/performance` | List pricing performance by strategy |

All endpoints follow the standard auth + logging + recovery middleware chain.

---

## 8 Audit Events

| Event Type | Emitted When |
|---|---|
| `pricing.profile_computed` | Price bands calculated and persisted |
| `pricing.recommendation_generated` | Recommendation delivered for opportunity |
| `pricing.negotiation_transitioned` | State machine transition executed |
| `pricing.concession_applied` | Concession computed during counter-offer |
| `pricing.outcome_recorded` | Outcome (won/lost + amounts) recorded |
| `pricing.performance_updated` | Strategy performance recomputed |
| `pricing.governance_blocked` | Pricing blocked by governance mode |

---

## 9 Database Schema (Migration 000050)

### Tables

| Table | Key | Purpose |
|---|---|---|
| `agent_pricing_profiles` | `id` (UUID), UNIQUE `opportunity_id` | Stores computed price bands per opportunity |
| `agent_negotiation_records` | `id` (UUID), UNIQUE `opportunity_id` | Tracks negotiation state machine per opportunity |
| `agent_pricing_outcomes` | `id` (UUID) | Records win/loss outcomes with amounts |
| `agent_pricing_performance` | `strategy_id` (TEXT, PK) | Aggregated performance metrics per strategy |

### Indexes

- `idx_pricing_profiles_opportunity` on `agent_pricing_profiles(opportunity_id)`
- `idx_negotiation_records_opportunity` on `agent_negotiation_records(opportunity_id)`
- `idx_negotiation_records_state` on `agent_negotiation_records(negotiation_state)`
- `idx_pricing_outcomes_opportunity` on `agent_pricing_outcomes(opportunity_id)`
- `idx_pricing_outcomes_strategy` on `agent_pricing_outcomes(strategy_id)`
- `idx_pricing_performance_updated` on `agent_pricing_performance(updated_at)`

---

## 10 Tests — 28 Passing

| # | Test | Validates |
|---|---|---|
| 1 | `TestComputePriceBands_BasicConsulting` | Base case: consulting, 10h, no pressure |
| 2 | `TestComputePriceBands_HigherEffortRaisesFloor` | More hours → higher cost basis |
| 3 | `TestComputePriceBands_CapacityOverloadRaisesFloor` | Utilisation > 0.7 raises minimum |
| 4 | `TestComputePriceBands_PressureCannotPushBelowMinimum` | Pressure only raises, never lowers |
| 5 | `TestComputePriceBands_StrategyPerformanceAdjustsTarget` | High win rate → higher target |
| 6 | `TestComputePriceBands_NoHistoricalData_DeterministicDefault` | Cold start uses defaults |
| 7 | `TestComputePriceBands_ZeroEffort_FallsBackToMinimumHour` | Zero effort safeguard |
| 8 | `TestConcession_DecreasesTargetButNotBelowFloor` | Basic concession mechanics |
| 9 | `TestConcession_CountCapEnforced` | 5-concession hard limit |
| 10 | `TestConcession_ConvergesToFloor` | Repeated concessions converge |
| 11 | `TestConcession_AtFloorNoChange` | At floor → zero movement |
| 12 | `TestValidateTransition_ValidTransitions` | All 10 valid transitions pass |
| 13 | `TestValidateTransition_InvalidTransitions` | Invalid transitions rejected |
| 14 | `TestValidateTransition_InvalidStates` | Unknown states produce error |
| 15 | `TestComputePerformance_AcceptedQuotedRatio` | Discount calculation from outcomes |
| 16 | `TestComputePerformance_WinLossUpdatesWinRate` | Win rate tracks correctly |
| 17 | `TestComputePerformance_UpdatedFromVerifiedOutcomes` | Verified outcomes contribute |
| 18 | `TestComputePerformance_Empty` | Zero outcomes → zero performance |
| 19 | `TestComputePerformance_AllLost` | All-loss scenario |
| 20 | `TestRecommendMessageType` | Recommendation messages differ by band |
| 21 | `TestConfidence_BelowMinOutcomes_ReturnsDefault` | Cold-start confidence = 0.50 |
| 22 | `TestConfidence_IncreasesWithOutcomes` | More data → higher confidence |
| 23 | `TestPriceBands_MinTargetStretchInvariant` | min ≤ target ≤ stretch always holds |
| 24 | `TestGraphAdapter_NilSafe` | Nil adapter returns zero values |
| 25 | `TestInvalidStateError` | Error message formatting |
| 26 | `TestInvalidTransitionError` | Error message formatting |
| 27 | `TestComputePriceBands_PressureFloorBoostBounded` | Pressure boost capped at 20% |
| 28 | `TestComputePriceBands_ColdStartPerformanceIgnored` | < 5 outcomes → no adjustment |

---

## 11 Regression Summary

```
70 packages tested — 0 failures
28 new pricing tests — all pass
All existing subsystem tests cached/pass:
  - arbitration, calibration, capacity, causal, counterfactual
  - decision_graph, discovery, exploration, external_actions
  - financial_pressure, financial_truth, goals, governance
  - income, meta_reasoning, outcome, path_comparison
  - path_learning, planning, policy, portfolio
  - provider_catalog, provider_routing, reflection
  - resource_optimization, scheduler, scheduling
  - self_extension, signals, stability, strategy, strategy_learning
```

---

## 12 Validation Examples

### 12.1 Consulting Quote — Base Case

```
Input:  type=consulting, effort=10h, pressure=0.0, utilisation=0.5
        base_hourly_rate=100, no historical performance

cost_basis = 100 × 10 × 1.0 = 1000
minimum    = 1000 × 1.2      = 1200
target     = 1000 × 1.8      = 1800
stretch    = 1000 × 2.4      = 2400

Result: {min: 1200, target: 1800, stretch: 2400, confidence: 0.50}
```

### 12.2 Overloaded Owner — Higher Floor

```
Input:  type=consulting, effort=10h, pressure=0.0, utilisation=0.95
        capacity overload = 0.95 − 0.7 = 0.25

cost_basis   = 1000
capacity_add = 1000 × 0.25 × 0.25 = 62.5
minimum      = 1200 + 62.5         = 1262.5
target       = 1800   (unclamped, already > minimum)
stretch      = 2400

Result: {min: 1262.50, target: 1800, stretch: 2400}
```

### 12.3 Concession Example

```
State:  currentOffer=1800, minimumPrice=1200, concessionCount=0

Step 1: remaining = 600, concession = 60, new = 1740, count = 1
Step 2: remaining = 540, concession = 54, new = 1686, count = 2
Step 3: remaining = 486, concession = 48.6, new = 1637.4, count = 3
...
Step 5: count = 5 → exhausted = true, no further concessions
```

### 12.4 Verified Pricing Outcome

```
Outcomes for strategy "consulting":
  1. quoted=1800, accepted=1700, won=true   → discount=5.56%
  2. quoted=2000, accepted=1900, won=true   → discount=5.00%
  3. quoted=1500, accepted=0,    won=false
  4. quoted=1800, accepted=1600, won=true   → discount=11.11%
  5. quoted=1700, accepted=0,    won=false

Performance:
  win_rate        = 3/5 = 0.60
  avg_discount    = (5.56 + 5.00 + 11.11) / 3 = 7.22%
  avg_accepted    = (1700 + 1900 + 1600) / 3 = 1733.33
  avg_quoted      = (1800 + 2000 + 1500 + 1800 + 1700) / 5 = 1760
  total_outcomes  = 5
  confidence      = clamp(0.50 + 5 × 0.05, 0.30, 0.95) = 0.75

Target adjustment: target *= (1 + (0.60 − 0.50) × 0.20) = target × 1.02
  → Win rate above 50% slightly raises target (market accepts)
```

---

## 13 Remaining Risks

| Risk | Severity | Mitigation |
|---|---|---|
| No integration test with real DB | Medium | PostgreSQL stores follow UPSERT patterns validated in 10+ prior iterations; schema tested via migration linter |
| Governance provider optional | Low | Fail-open: nil governance → pricing proceeds (logged) |
| No decision-graph pipeline integration | Low | Pricing is advisory-only by design; does not modify path scores |
| Type multiplier values are initial guesses | Low | Constants are easily tunable; learning from outcomes will self-correct over time |
| Cold-start with no outcomes | Low | DefaultConfidence=0.50 and no target adjustments until 5 outcomes observed |

---

## 14 Files Changed

### New Files
- `internal/agent/pricing/types.go` — entities, constants, state machine
- `internal/agent/pricing/store.go` — 4 PostgreSQL stores
- `internal/agent/pricing/scorer.go` — pure deterministic computation
- `internal/agent/pricing/engine.go` — orchestration with audit
- `internal/agent/pricing/adapter.go` — nil-safe API adapter
- `internal/agent/pricing/pricing_test.go` — 28 unit tests
- `internal/db/migrations/000051_create_agent_pricing.up.sql` — 4 tables
- `internal/db/migrations/000051_create_agent_pricing.down.sql` — rollback

### Modified Files
- `internal/api/handlers.go` — +1 import, +1 field, +1 builder method, +6 handlers
- `internal/api/router.go` — +6 routes
- `cmd/api-gateway/main.go` — +1 import, engine init, bridge adapters, WithPricing

---

## 15 Rollout Recommendation

**READY_WITH_GUARDS**

Guards:
1. Run migration `000050` on staging before production
2. Monitor `pricing.*` audit events for first 48 hours
3. Confirm governance blocking works in frozen/safe_hold modes
4. Seed at least 5 outcomes per strategy type before trusting target adjustments
5. Review type multipliers after 30 days of real pricing data
