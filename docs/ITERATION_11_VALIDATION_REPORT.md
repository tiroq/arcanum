# Iteration 11 — Causal Reasoning Layer: Validation Report

## Summary

Iteration 11 implements a **Causal Reasoning Layer** that analyzes recent self-modification events (policy changes, stability interventions, planner conditions) and produces deterministic causal attributions with explicit confidence levels and competing explanations.

**Status: COMPLETE — 13 new tests, 0 regressions**

---

## Components Delivered

### 1. Database Migration (000018)

- `agent_causal_attributions` — stores causal attributions with subject tracking, hypothesis, attribution type, confidence, evidence (JSONB), and competing explanations (JSONB)
- Indexes on `(subject_type, subject_id)` and `(created_at DESC)`

### 2. Causal Package (`internal/agent/causal/`)

| File | Purpose |
|------|---------|
| `types.go` | Attribution constants (internal/external/mixed/ambiguous), SubjectType constants, CausalAttribution struct, AnalysisInput/Result |
| `store.go` | PostgreSQL persistence: Save, ListRecent, ListBySubject |
| `analyzer.go` | 5 deterministic causal rules — pure function, no side effects |
| `engine.go` | Full cycle orchestration: collect signals → analyze → persist → audit |

### 3. API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/agent/causal` | Recent causal attributions |
| POST | `/api/v1/agent/causal/evaluate` | Trigger causal analysis pass |
| GET | `/api/v1/agent/causal/{subject_id}` | Attributions for a specific subject |

### 4. Wiring (`cmd/api-gateway/main.go`)

- CausalStore + CausalEngine created after policy and stability engines
- Handlers receive engine via `WithCausalEngine()`

---

## Attribution Model

### Attribution Types

| Type | Meaning |
|------|---------|
| `internal` | Change likely caused by the system's own actions (policy change, stability intervention) |
| `external` | Change likely caused by external factors (provider degradation, system failure) |
| `mixed` | Both internal and external factors contributed |
| `ambiguous` | Insufficient evidence or too many simultaneous factors |

### Subject Types

| Type | What is analyzed |
|------|-----------------|
| `policy_change` | Applied policy parameter adjustments |
| `stability_event` | Stability mode transitions (escalation/de-escalation) |
| `planner_shift` | Planner behavior degradation from external conditions |

---

## 5 Deterministic Causal Rules

| # | Rule | Condition | Attribution | Confidence |
|---|------|-----------|-------------|------------|
| 1 | Likely internal cause | Policy change + expected improvement + no external instability | internal | 0.75 |
| 2 | Likely external cause | Metric changed + provider/cycle/system instability present | external or mixed | 0.50–0.60 |
| 3 | Ambiguous | Too many simultaneous changes OR insufficient data | ambiguous | 0.20–0.35 |
| 4 | Stability intervention | Mode escalated + harmful patterns decreased | internal | 0.65–0.70 |
| 5 | No causal support | Change occurred but target metric didn't move | ambiguous | 0.35 |

---

## Example Causal Attribution

```json
{
  "id": "a1b2c3d4-...",
  "subject_type": "policy_change",
  "subject_id": "e5f6g7h8-...",
  "hypothesis": "policy change to feedbackAvoidPenalty (0.400 → 0.450) likely caused observed improvement",
  "attribution": "internal",
  "confidence": 0.75,
  "evidence": {
    "parameter": "feedbackAvoidPenalty",
    "old_value": 0.40,
    "new_value": 0.45,
    "applied": true,
    "improvement_detected": true,
    "external_instability": false
  },
  "competing_explanations": [
    "temporary recovery unrelated to policy change",
    "workload pattern shift coinciding with change"
  ],
  "created_at": "2026-04-07T12:00:00Z"
}
```

---

## Competing Explanations

Every attribution records plausible alternatives. Examples per scenario:

- **Internal attribution**: "temporary recovery unrelated to policy change", "workload pattern shift"
- **External attribution**: "provider degradation affecting outcomes", "high system failure rate distorting metrics"
- **Mixed**: combines internal + external alternatives
- **Ambiguous**: "parameter change may be too small", "target metric may respond on longer timescale"
- **Stability**: "harmful patterns may have subsided naturally", "scheduler throttling reduced action volume"

---

## Test Results

### Causal Package — 13 tests

```
TestAnalyze_PolicyImprovement_Internal              PASS
TestAnalyze_PolicyWithExternalInstability_Mixed      PASS
TestAnalyze_PolicyNoImprovement_ExternalCause        PASS
TestAnalyze_InsufficientEvidence_Ambiguous           PASS
TestAnalyze_TooManySimultaneousChanges_Ambiguous     PASS
TestAnalyze_StabilityEscalation_Effective            PASS
TestAnalyze_StabilityEscalation_StillUnstable        PASS
TestAnalyze_StabilityDeescalation                    PASS
TestAnalyze_PolicyNoImprovement_NoCause              PASS
TestAnalyze_CompetingExplanationsAlwaysPopulated     PASS
TestAnalyze_PlannerDegradation_External              PASS
TestAnalyze_NoSignals_Empty                          PASS
TestAnalyze_NonAppliedChangesSkipped                 PASS
```

### Full Suite — 0 failures

All existing packages continue to pass. No regressions introduced.

---

## Ambiguity Handling Validation

| Scenario | Expected | Actual |
|----------|----------|--------|
| Unevaluated policy change | ambiguous, confidence ≤ 0.30 | ✅ ambiguous, confidence = 0.20 |
| 4+ simultaneous changes | ambiguous regardless of improvement | ✅ ambiguous, confidence = 0.30 |
| No improvement, no external cause | ambiguous, confidence ≤ 0.40 | ✅ ambiguous, confidence = 0.35 |
| Improvement + external instability | mixed, not internal | ✅ mixed, confidence = 0.50 |
| No signals at all | zero attributions | ✅ empty result |
| Non-applied policy changes | skipped entirely | ✅ skipped |

---

## Architecture Alignment

| Principle | Status |
|-----------|--------|
| Bus-first | ✅ Audit events emitted; no direct cross-service calls |
| Explicit state | ✅ All attributions persisted with evidence and competing explanations |
| No silent failure | ✅ Save failures logged; analysis never blocks system |
| Observability | ✅ 3 audit events (started/completed/created); 3 API endpoints |
| Deterministic | ✅ Pure `Analyze()` function; no randomness or LLM involvement |
| Minimal magic | ✅ Simple conditional rules; explicit confidence levels |

---

## Remaining Limitations

1. **Previous stability mode approximation**: Engine infers previous mode from current mode ordinal when exact history is unavailable
2. **Provider instability detection**: Currently derived from action memory failure rates; no direct provider health signal yet
3. **Cycle instability**: Not yet wired from scheduler cycle error counts (future integration point)
4. **Advisory only**: Causal attributions do not yet influence policy adaptation decisions — that is a future integration
5. **Single-pass analysis**: Each evaluation is independent; no temporal correlation across multiple analysis passes

---

## Audit Events

| Event | Payload |
|-------|---------|
| `causal.evaluation_started` | timestamp |
| `causal.evaluation_completed` | attributions_count, timestamp |
| `causal.attribution_created` | subject_type, subject_id, attribution, confidence |

---

## Files Created/Modified

### New Files
- `internal/db/migrations/000018_create_agent_causal_attributions.up.sql`
- `internal/db/migrations/000018_create_agent_causal_attributions.down.sql`
- `internal/agent/causal/types.go`
- `internal/agent/causal/store.go`
- `internal/agent/causal/analyzer.go`
- `internal/agent/causal/engine.go`
- `internal/agent/causal/analyzer_test.go`

### Modified Files
- `internal/api/handlers.go` — Added causal engine field, WithCausalEngine, 3 handler methods
- `internal/api/router.go` — Added 3 causal routes
- `cmd/api-gateway/main.go` — Wired causal store + engine
