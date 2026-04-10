# Iteration 42 — Financial Truth Layer Validation Report

## 1. Summary

Implemented a financial truth layer that distinguishes estimated value, attributed value,
and verified financial truth. The system now ingests raw financial events (bank transactions,
invoices, payments), normalises them into verified facts, links them to income opportunities
and outcomes, and computes monthly truth summaries. Pressure computation and income
attribution are upgraded to prefer verified values when available.

### New Files
- `internal/agent/financial_truth/types.go` — Constants, entities (FinancialEvent, FinancialFact, FinancialSummary, AttributionMatch)
- `internal/agent/financial_truth/store.go` — PostgreSQL persistence (EventStore, FactStore, MatchStore)
- `internal/agent/financial_truth/normalizer.go` — Deterministic event → fact normalisation
- `internal/agent/financial_truth/linker.go` — Heuristic + exact + manual fact-to-outcome linking
- `internal/agent/financial_truth/summary.go` — Monthly verified/pending summary computation
- `internal/agent/financial_truth/engine.go` — Orchestrator (IngestEvent, LinkFact, GetSummary, Recompute, audit events)
- `internal/agent/financial_truth/adapter.go` — GraphAdapter implementing decision graph provider (GetVerifiedIncome, GetTruthSignal) — nil-safe, fail-open
- `internal/agent/financial_truth/pressure_adapter.go` — Bridge to financial_pressure (FinancialTruthProvider)
- `internal/agent/financial_truth/learning_adapter.go` — Bridge to income engine (EngineFinancialTruthProvider)
- `internal/agent/financial_truth/financial_truth_test.go` — 32 tests
- `internal/db/migrations/000046_create_agent_financial_truth.up.sql` — Migration (3 tables)
- `internal/db/migrations/000046_create_agent_financial_truth.down.sql` — Rollback migration

### Modified Files
- `internal/agent/financial_pressure/adapter.go` — Added `FinancialTruthProvider` interface, `WithTruth()`, truth-aware pressure override
- `internal/agent/income/engine.go` — Added `EngineFinancialTruthProvider` interface, `WithTruthProvider()`, truth-aware attribution
- `internal/api/handlers.go` — Added `financialTruth` field, `WithFinancialTruth()`, 5 handler methods
- `internal/api/router.go` — Added 6 financial truth routes
- `cmd/api-gateway/main.go` — Wired financial truth engine, stores, adapters

---

## 2. Truth Model

### Three Tiers of Financial Data

| Tier | Source | Confidence | Example |
|---|---|---|---|
| **Estimated** | Income opportunity `expected_value` | Variable (scorer output) | "Consulting gig might pay $2,000" |
| **Attributed** | Income outcome `actual_value` | Self-reported | "I earned $1,800 from the gig" |
| **Verified** | Financial fact from bank/processor | 1.0 | "$1,800 payment received from Acme Corp" |

### Core Entities

**FinancialEvent** — Raw money event from external source:
```
id, source, event_type, direction, amount, currency,
description, external_ref, occurred_at, metadata
```

**FinancialFact** — Normalised verified record:
```
id, event_id, fact_type, direction, amount, currency,
source, verified, confidence, linked_opportunity_id,
linked_outcome_id, linked_proposal_id, occurred_at
```

**AttributionMatch** — Link between fact and outcome/opportunity:
```
id, fact_id, outcome_id, opportunity_id, match_type,
match_confidence, matched_at
```

**FinancialSummary** — Monthly truth:
```
month, verified_income, verified_expenses, verified_net,
pending_unverified_inflow, pending_unverified_outflow, computed_at
```

---

## 3. Normalisation Pipeline

```
FinancialEvent → ValidateEvent() → NormalizeFact() → FinancialFact
```

### Rules

| Event Type | Direction | → Fact Type |
|---|---|---|
| payment_received | inflow | income |
| expense_recorded | outflow | expense |
| invoice_paid | outflow | expense |
| subscription_charge | outflow | expense |
| transfer_in | inflow | transfer |
| transfer_out | outflow | transfer |

### Confidence Assignment

| Source | Confidence |
|---|---|
| bank | 1.0 (VerifiedConfidence) |
| payment_processor | 1.0 (VerifiedConfidence) |
| accounting_system | 1.0 (VerifiedConfidence) |
| any other | 0.50 (UnverifiedConfidence) |

### Verified Flag

A fact is marked `verified = true` when its source is in the verified set
(bank, payment_processor, accounting_system).

### Validation

`ValidateEvent()` rejects events with:
- Missing or invalid `event_type`
- Missing or invalid `direction`
- `amount ≤ 0`
- Empty `source`
- Zero `occurred_at`

---

## 4. Linking Model

Three linking strategies, attempted in order:

### 4.1 Exact ID Match
```
fact.linked_opportunity_id == outcome.opportunity_id
→ match_type = "exact", confidence = 1.0
```

### 4.2 External Reference Match
```
fact.external_ref == outcome.external_ref (non-empty)
→ match_type = "exact", confidence = 1.0
```

### 4.3 Heuristic (Amount + Date)
```
amount_diff = |fact.amount - outcome.actual_value| / max(fact.amount, outcome.actual_value)
date_diff   = |fact.occurred_at - outcome.recorded_at|

if amount_diff ≤ 0.05 (5%) AND date_diff ≤ 7 days:
  match_confidence = HeuristicConfidence × (1 - amount_diff)
  if match_confidence ≥ HeuristicLinkThreshold (0.60):
    → match_type = "heuristic"
```

### 4.4 Manual Link
```
ForceManualLink() → match_type = "manual", confidence = 1.0
```

Always succeeds. Used for human-curated links via API.

### Constants

| Constant | Value | Purpose |
|---|---|---|
| VerifiedConfidence | 1.0 | Bank/processor sourced facts |
| HeuristicConfidence | 0.70 | Base confidence for heuristic matches |
| UnverifiedConfidence | 0.50 | Non-verified source default |
| HeuristicLinkThreshold | 0.60 | Minimum confidence to auto-link |
| AmountTolerancePct | 0.05 | 5% amount tolerance |
| DateToleranceDays | 7 | 7-day date tolerance |

---

## 5. Monthly Summary

`ComputeSummary(month string)` aggregates all facts for a given month:

```
For each fact in month:
  if verified AND direction == inflow AND type != transfer:
    verified_income += amount
  if verified AND direction == outflow AND type != transfer:
    verified_expenses += amount
  if NOT verified AND direction == inflow:
    pending_unverified_inflow += amount
  if NOT verified AND direction == outflow:
    pending_unverified_outflow += amount

verified_net = verified_income - verified_expenses
```

Returns zero summary on empty facts (fail-open).

---

## 6. Integration

### 6.1 Financial Pressure (Truth-Aware)

When the truth provider is wired into the financial pressure adapter:

```
if truth_provider != nil:
  verified_income, err = truth_provider.GetVerifiedMonthlyIncome(ctx)
  if err == nil AND verified_income > 0:
    state.CurrentIncomeMonth = verified_income   // override estimate
    → audit: financial.truth_applied
```

This means pressure computation uses **real bank-verified income** instead of
self-reported estimates, making the pressure score more accurate.

### 6.2 Income Attribution (Truth-Aware)

When the truth provider is wired into the income engine:

```
if truth_provider != nil:
  verified_value, err = truth_provider.GetVerifiedValueForOpportunity(ctx, opp_id)
  if err == nil AND verified_value > 0:
    accuracy = verified_value / estimated_value   // instead of actual / estimated
    → audit includes: verified_value, used_verified=true
```

This replaces self-reported `actual_value` with verified bank value for accuracy
computation, making learning signals more reliable.

### 6.3 Decision Graph Pipeline Position

The financial truth layer operates **outside** the real-time decision pipeline.
It feeds into existing pipeline stages via:

```
financial_truth → pressure_adapter (overrides CurrentIncomeMonth)
financial_truth → income_engine (overrides attribution accuracy)
```

Existing pipeline order is unchanged:
```
arbitration → resource_penalty → goal_alignment → income_signal →
outcome_attribution → signal_ingestion → financial_pressure →
capacity_penalty → select_best_path
```

---

## 7. API

| Method | Endpoint | Description |
|---|---|---|
| GET | `/api/v1/agent/financial/events` | List financial events (optional `?limit=N`) |
| POST | `/api/v1/agent/financial/events` | Ingest a new financial event |
| GET | `/api/v1/agent/financial/facts` | List financial facts (optional `?month=2025-01`) |
| POST | `/api/v1/agent/financial/link` | Manually link a fact to an opportunity/outcome |
| GET | `/api/v1/agent/financial/summary?month=2025-01` | Get monthly verified summary |
| POST | `/api/v1/agent/financial/recompute` | Re-normalise all events into facts |

### Example: Ingest Event
```json
POST /api/v1/agent/financial/events
{
  "source": "bank",
  "event_type": "payment_received",
  "direction": "inflow",
  "amount": 1800.00,
  "currency": "USD",
  "description": "Acme Corp consulting payment",
  "external_ref": "TXN-12345",
  "occurred_at": "2025-01-15T10:00:00Z"
}
```

### Example: Manual Link
```json
POST /api/v1/agent/financial/link
{
  "fact_id": "fact-uuid",
  "opportunity_id": "opp-uuid",
  "outcome_id": "outcome-uuid"
}
```

---

## 8. Audit Events

| Event | When |
|---|---|
| `financial.event_recorded` | After persisting a raw event |
| `financial.fact_normalized` | After normalising event → fact |
| `financial.fact_linked` | After linking fact to outcome/opportunity |
| `financial.summary_updated` | After computing monthly summary |
| `financial.truth_applied` | When verified income overrides pressure state |

---

## 9. Tests — 32 passing

### Normalisation Tests (1–8)
1. ✅ `TestNormalize_InflowToIncomeFact` — inflow payment → income fact
2. ✅ `TestNormalize_OutflowToExpenseFact` — outflow expense → expense fact
3. ✅ `TestNormalize_TransferDoesNotCountAsIncome` — transfer_in → transfer (not income)
4. ✅ `TestNormalize_VerifiedPreserved` — bank source → verified=true, confidence=1.0
5. ✅ `TestNormalize_ConfidenceDeterministic` — same input → same output 3 times
6. ✅ `TestNormalize_InvoicePaidClassifiedAsExpense` — invoice_paid → expense
7. ✅ `TestNormalize_SubscriptionChargeClassifiedAsExpense` — subscription_charge → expense
8. ✅ `TestNormalize_TransferOutClassifiedAsTransfer` — transfer_out → transfer

### Linking Tests (9–16)
9. ✅ `TestLink_ExactIdentifierMatch` — matching opportunity_id → exact match
10. ✅ `TestLink_ExternalRefMatch` — matching external_ref → exact match
11. ✅ `TestLink_HeuristicAmountAndDateMatch` — same amount, 2 days apart → heuristic
12. ✅ `TestLink_LowConfidenceDoesNotAutoLink` — large amount diff → no link
13. ✅ `TestLink_DatesTooFarApart` — 10 days apart → no link
14. ✅ `TestLink_ManualLinkAlwaysSucceeds` — forced manual → always links
15. ✅ `TestLink_ZeroAmountsDoNotLink` — zero amounts → no heuristic link
16. ✅ `TestLink_NoExactMatchWhenIDsDiffer` — different IDs → no exact match

### Validation Tests (17–24)
17. ✅ `TestValidateEvent_MissingEventType` — empty event_type → error
18. ✅ `TestValidateEvent_InvalidEventType` — unknown type → error
19. ✅ `TestValidateEvent_MissingDirection` — empty direction → error
20. ✅ `TestValidateEvent_InvalidDirection` — unknown direction → error
21. ✅ `TestValidateEvent_ZeroAmount` — amount=0 → error
22. ✅ `TestValidateEvent_NegativeAmount` — amount<0 → error
23. ✅ `TestValidateEvent_MissingOccurredAt` — zero time → error
24. ✅ `TestValidateEvent_ValidEvent` — all fields valid → no error

### Adapter & Safety Tests (25–32)
25. ✅ `TestAdapter_NilSafety` — nil adapter methods return zero values
26. ✅ `TestAdapter_NilEngine` — nil engine adapter returns zero values
27. ✅ `TestTruthSignal_VerifiedSourcePrefersBank` — bank source → verified=true
28. ✅ `TestLink_HeuristicWithin5PercentAmount` — 4% diff → links (within 5%)
29. ✅ `TestLink_HeuristicWithin7Days` — 6 days apart → links (within 7 days)
30. ✅ `TestNormalize_Deterministic` — repeated normalisation is stable
31. ✅ `TestFailOpen_NilAdapterReturnsZero` — nil GraphAdapter returns zero + nil
32. ✅ `TestConstants_Sanity` — all constants within expected ranges

---

## 10. Regression Summary

| Package | Status |
|---|---|
| `internal/agent/financial_truth/...` | ✅ 32/32 pass |
| `internal/agent/financial_pressure/...` | ✅ Pass |
| `internal/agent/income/...` | ✅ Pass |
| `internal/agent/decision_graph/...` | ✅ Pass |
| `internal/agent/capacity/...` | ✅ Pass |
| `internal/agent/signals/...` | ✅ Pass |
| `internal/agent/governance/...` | ✅ Pass |
| `internal/agent/arbitration/...` | ✅ Pass |
| `internal/agent/calibration/...` | ✅ Pass |
| `internal/agent/resource_optimization/...` | ✅ Pass |
| `internal/agent/provider_catalog/...` | ✅ Pass |
| `internal/agent/provider_routing/...` | ✅ Pass |
| All 29 agent packages (excl. self_extension, discovery) | ✅ Pass |
| Build `go build ./cmd/... ./internal/agent/... ./internal/api/...` | ✅ Clean |

---

## 11. Validation Examples

### Example 1: Income Inflow — Bank Payment
- Event: source=bank, type=payment_received, direction=inflow, amount=$1,800
- Normalisation: fact_type=income, verified=true, confidence=1.0
- Linking: external_ref matches outcome → match_type=exact, confidence=1.0
- Summary: verified_income += $1,800
- Pressure: CurrentIncomeMonth overridden with verified total

### Example 2: Expense — Subscription Charge
- Event: source=payment_processor, type=subscription_charge, direction=outflow, amount=$49.99
- Normalisation: fact_type=expense, verified=true, confidence=1.0
- Summary: verified_expenses += $49.99

### Example 3: Heuristic Link
- Fact: amount=$960, occurred_at=Jan 17
- Outcome: actual_value=$1000, recorded_at=Jan 15
- Amount diff: |960-1000|/1000 = 4% ≤ 5% ✅
- Date diff: 2 days ≤ 7 days ✅
- match_confidence = 0.70 × (1 - 0.04) = 0.672 ≥ 0.60 → linked ✅

### Example 4: Monthly Summary
- January facts: 3 verified income ($1800 + $500 + $200), 2 verified expenses ($49.99 + $120), 1 unverified inflow ($300)
- verified_income = $2,500
- verified_expenses = $169.99
- verified_net = $2,330.01
- pending_unverified_inflow = $300

---

## 12. Remaining Risks

| Risk | Mitigation |
|---|---|
| No real bank API integration | Manual event ingest via API; future iteration can add Plaid/bank sync |
| Heuristic linking may false-positive | Confidence threshold (0.60) + manual override via /link API |
| Currency conversion not supported | All amounts assumed same currency; future iteration can add FX |
| No duplicate event detection | external_ref can be used for dedup; future iteration can add idempotency key |
| Recompute may be slow with many events | Bounded by event count; future iteration can add pagination |

---

## 13. Rollout Recommendation

### **READY_WITH_GUARDS**

Rationale:
- All normalisation is deterministic and bounded
- All components fail-open (nil-safe adapters)
- Truth overrides are additive — only applied when verified_income > 0
- No auto-ingestion — all events enter via explicit API call
- No side effects on decision pipeline — truth feeds into existing stages
- Linking defaults to conservative thresholds (5% amount, 7 days)
- Manual link escape hatch for edge cases
- Pre-existing self_extension/discovery issues are unrelated

Guards:
- Monitor `financial.truth_applied` audit events to confirm override frequency
- Verify heuristic link accuracy via `financial.fact_linked` audit trail
- Validate monthly summaries match expected bank statement totals
- Review unverified pending amounts periodically for missing verification
