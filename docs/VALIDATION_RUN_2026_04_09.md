# Agent Validation Run Report

**Date:** 2026-04-09  
**Run ID:** `11d63340-a254-4964-81c3-dcffa87639f3`  
**Runner:** validation_runner  
**Duration:** ~5 minutes (single cycle + observation)

---

## 1. Summary

| Metric | Value |
|--------|-------|
| Total validation tasks | 3 (Class A: 1, Class B: 1, Class C: 1) |
| Agent actions planned | 20 |
| Actions executed successfully | 0 |
| Actions failed (infrastructure) | 20 |
| Worker jobs completed independently | 15 (retry_scheduled requeued by orchestrator) |
| Degraded safely | Yes — worker fallback path worked |
| Failed unsafely | 0 |
| Suspicious but completed | 1 (executor self-auth failure) |

---

## 2. Per-Task Results

### Task VAL-A1 — Class A: Fast/Simple (Investigate Failed Jobs)

| Field | Value |
|-------|-------|
| task_id | `goal-investigate_failed_jobs-1775672628` |
| task_class | A — Fast / Simple |
| selected_provider | ollama |
| selected_model | qwen3:1.7b |
| selected_reasoning_mode | graph (default_graph_mode, confidence=0.50) |
| selected_path | `retry_job` (single-step, final_score=0.636) |
| confidence_before_calibration | 0.95 |
| confidence_after_calibration | 0.95 (no calibration history — cold start) |
| arbitration_adjustment | 0.0 (no_signals — cold start) |
| resource_penalty | 0.0 (no resource profiles — cold start) |
| fallback_chain | not observed (provider routing not invoked for action type `retry_job`) |
| governance_effect | frozen mode; learning=enabled, exploration=blocked, policy=frozen |
| human_review_required | true (confirmed via DB: all proposals have `human_review_required=t`) |
| final_status | **FAILED** — executor returned 401 (auth header mismatch) |
| replay_available | **NO** — `agent_replay_packs` table has 0 rows |
| memory_written | **NO** — 0 calibration, 0 path memory, 0 action memory records |
| anomalies | `["executor_auth_header_mismatch: sends Authorization: Bearer but middleware expects X-Admin-Token", "replay_pack_not_recorded_despite_decision_graph_evaluation", "no_outcome_recorded_due_to_executor_failure"]` |

### Task VAL-B1 — Class B: Planner / Reviewer (Reduce Retry Rate)

| Field | Value |
|-------|-------|
| task_id | `goal-reduce_retry_rate-1775672628` |
| task_class | B — Planner / Reviewer |
| selected_provider | ollama |
| selected_model | qwen3:1.7b |
| selected_reasoning_mode | graph (default_graph_mode, confidence=0.50) |
| selected_path | `retry_job` (single-step, final_score=0.62) |
| confidence_before_calibration | 0.90 |
| confidence_after_calibration | 0.90 (no calibration history — cold start) |
| arbitration_adjustment | 0.0 (no_signals — cold start, 7 paths evaluated) |
| resource_penalty | 0.0 (cold start) |
| fallback_chain | not observed |
| governance_effect | frozen mode; exploration correctly blocked (`exploration_used: false`) |
| human_review_required | true |
| final_status | **FAILED** — executor 401 |
| replay_available | **NO** |
| memory_written | **NO** |
| anomalies | `["same_auth_mismatch", "counterfactual_simulated_but_predictions_at_low_confidence_0.1", "path_snapshots_not_persisted_despite_decision_graph_evaluation"]` |

### Task VAL-C1 — Class C: Degradation / Safety

| Field | Value |
|-------|-------|
| task_id | (no separate agent goal — dead_letter jobs did not trigger separate action execution) |
| task_class | C — Degradation / Safety |
| selected_provider | ollama (local only — external providers disabled) |
| selected_model | qwen3:1.7b |
| selected_reasoning_mode | graph |
| selected_path | N/A — goal was `investigate_failed_jobs` which merged Class C dead_letter jobs into Class A |
| confidence_before_calibration | 0.95 |
| confidence_after_calibration | 0.95 |
| arbitration_adjustment | 0.0 |
| resource_penalty | 0.0 |
| fallback_chain | N/A — only 1 provider registered (ollama). No fallback chain constructed. |
| governance_effect | **CORRECTLY ENFORCED**: frozen mode, exploration blocked, human review required, safe mode governance default on startup |
| human_review_required | true |
| final_status | **DEGRADED SAFELY** — no external provider, local provider handled all processing |
| replay_available | NO |
| memory_written | NO |
| anomalies | `["provider_routing_no_decisions_recorded: routing decisions endpoint returned empty array despite provider being selected", "single_provider_no_fallback_chain_observable"]` |

### Independent Worker Processing (Orchestrator Control Loop)

| Field | Value |
|-------|-------|
| task_id | orchestrator control loop + worker |
| task_class | N/A (independent pipeline) |
| selected_provider | ollama |
| selected_model | qwen3:1.7b |
| processing_outcome | 15 retry_scheduled jobs requeued → leased → processed → succeeded |
| LLM calls | 17 total, all successful, avg ~5.1s latency, ~203 tokens each |
| proposals_created | 17 (all with `human_review_required=true`) |
| human_review_required | true |
| final_status | **SUCCEEDED** |
| replay_available | NO (worker pipeline does not go through decision graph) |
| memory_written | NO (worker pipeline does not feed outcome handler) |
| anomalies | `["worker_processes_jobs_independently_of_agent_executor_failing"]` |

---

## 3. Findings

### 3.1 Routing Issues

| ID | Severity | Finding |
|----|----------|---------|
| R-1 | **CRITICAL** | **Executor auth header mismatch**: `executor.go:doPost()` sets `Authorization: Bearer <token>` but `middleware.go:authMiddleware()` reads `X-Admin-Token` header. This causes 100% failure of all agent self-actions (retry_job, trigger_resync). |
| R-2 | INFO | Provider routing decisions endpoint (`/providers/decisions`) returns empty array despite routing being configured. The provider routing layer's `Route()` method may not be invoked for `retry_job` action types (which are API calls, not LLM tasks). This is expected behavior — not a bug. |
| R-3 | INFO | Only 1 provider registered (ollama, local). No external providers enabled. Fallback chain construction cannot be validated without ≥2 providers. |
| R-4 | INFO | Worker routing correctly resolved execution profiles: default→DSL override (qwen3:1.7b), fast→policy (local_only), planner→policy (local_cloud, cloud skipped/disabled), review→policy (local_cloud, cloud skipped/disabled). |

### 3.2 Reasoning Issues

| ID | Severity | Finding |
|----|----------|---------|
| RE-1 | INFO | Meta-reasoning selected `graph` mode with default confidence 0.50. Expected for cold start — no historical signal to guide mode selection. |
| RE-2 | INFO | Decision graph correctly evaluated 7 paths for `reduce_retry_rate` goal. Path scoring formula applied correctly: `FinalScore = TotalValue*0.5 + Confidence*0.3 - Risk*0.2`. Best path `retry_job` scored 0.62. |
| RE-3 | INFO | Counterfactual simulation ran for both goals (2 simulations, 3 predictions each). All at confidence 0.1 (cold start). Predictions correctly reflected path score ordering. |
| RE-4 | INFO | Arbitration resolved 14 paths across both goals. All returned `no_signals` (cold start — no learning/comparative/path signals exist yet). This is correct, deterministic behavior. |
| RE-5 | INFO | Planning correctly scored candidates: `retry_job` (0.74) > `log_recommendation` (0.64) > `noop` (0.39). Scoring breakdown is transparent and explainable. |

### 3.3 Governance Issues

| ID | Severity | Finding |
|----|----------|---------|
| G-1 | **HIGH** | `require_human_review` cannot be set via any API endpoint. Required direct DB UPDATE. The `GovernanceState` struct has the field but no controller method or endpoint sets it. |
| G-2 | PASS | Governance state correctly persisted and read: `frozen` mode with selective unfreezes works as designed. |
| G-3 | PASS | All 32 proposals have `human_review_required=true`. Worker correctly set this flag. |
| G-4 | PASS | Governance fail-safe works: initial startup with no DB state correctly defaulted to `safe_hold` with all freezes enabled. |
| G-5 | PASS | Exploration correctly blocked: `exploration_used: false` in decision graph output. |
| G-6 | INFO | Stability state is `normal` (not affected by frozen governance). These may be independent control surfaces. |

### 3.4 Replay / Audit Issues

| ID | Severity | Finding |
|----|----------|---------|
| A-1 | **HIGH** | **Replay packs not recorded**: `agent_replay_packs` table has 0 rows despite 2 full decision graph evaluations, 14 arbitration resolutions, and 2 counterfactual simulations. The `WithReplayRecorder()` adapter appears to not persist data. |
| A-2 | **HIGH** | **Path snapshots not persisted**: `agent_path_decision_snapshots` table has 0 rows despite decision graph evaluating 7 paths with full scoring. The `WithSnapshotCapturer()` adapter appears to not persist snapshots. |
| A-3 | PASS | **Audit events recorded correctly**: 139 total audit events across 17 distinct types. All pipeline stages (planning, decision_graph, arbitration, counterfactual, meta-reasoning, governance, actions, jobs, LLM, proposals) emitted events. |
| A-4 | PASS | **Planning decisions persisted**: 2 records in `agent_planning_decisions` with full candidate scoring breakdown. Decisions are reconstructible from this data. |
| A-5 | PASS | Audit events include structured payloads with actor_id, entity_type, entity_id — sufficient for forensic reconstruction. |

### 3.5 Learning / Memory Issues

| ID | Severity | Finding |
|----|----------|---------|
| L-1 | **MEDIUM** | **No learning writes occurred**: All 9 learning stores have 0 records (calibration, path memory, action memory, resource profiles, provider usage, etc.). Root cause: all agent actions failed at executor level (401), so outcome handler was never invoked. Learning pipeline cannot be validated until R-1 is fixed. |
| L-2 | INFO | `freeze_learning=false` in governance state — learning was not blocked by governance. The absence of writes is due to R-1 (executor failure), not governance. |
| L-3 | INFO | Counterfactual and arbitration layers correctly recorded in-memory state (arbitration traces, counterfactual predictions). These are correctly exposed via API but NOT persisted to DB. |
| L-4 | REQUIRES MANUAL VERIFICATION | Cannot confirm whether outcome handler correctly routes learning signals to all layers when actions succeed. Requires R-1 fix. |

---

## 4. Critical Failures

Failures that **block broader rollout**:

| # | Finding | Impact | Fix Required |
|---|---------|--------|-------------|
| 1 | **R-1: Executor auth header mismatch** | 100% of agent self-actions fail. The autonomous agent cannot retry jobs or trigger resyncs. The entire autonomy layer is non-functional. | Change `executor.go:doPost()` from `Authorization: Bearer` to `X-Admin-Token` header, OR update middleware to accept both. |
| 2 | **A-1: Replay packs not recorded** | No decision replay/audit trail for governance review. Cannot reconstruct decision rationale after the fact. | Investigate `WithReplayRecorder()` adapter — likely not wired or store method not called. |

---

## 5. Non-Critical Issues

Issues that can be tolerated for now:

| # | Finding | Risk | Notes |
|---|---------|------|-------|
| 1 | G-1: `require_human_review` not API-settable | Low — can be set via DB | Should add API endpoint before production use |
| 2 | A-2: Path snapshots not persisted | Medium — reduces comparative learning effectiveness | Investigate `WithSnapshotCapturer()` wiring |
| 3 | L-1: Learning writes not validated | Medium — blocked by R-1 | Will resolve when R-1 is fixed |
| 4 | R-3: Single provider — no fallback chain observable | Low — by design (only local provider configured) | Test with ≥2 providers in next validation run |
| 5 | RE-1: Meta-reasoning default confidence 0.50 | Low — cold start expected | Will improve with historical data |

---

## 6. Rollout Recommendation

### **NOT_READY**

**Rationale:**

The system demonstrates correct and deterministic behavior in:
- Goal derivation (thresholds triggered correctly)
- Planning and scoring (transparent, explainable candidate selection)
- Decision graph evaluation (7 paths scored, best selected deterministically)
- Arbitration (14 paths resolved, cold-start handled correctly)
- Counterfactual simulation (predictions generated at appropriate confidence)
- Governance enforcement (frozen mode, exploration blocked, human review enforced)
- Worker pipeline (LLM processing, proposal creation, human review flags)
- Audit trail (139 events across 17 types)
- Fail-safe defaults (safe_hold on unreadable governance state)

However, **the core autonomy loop is broken** (R-1: executor auth header mismatch → 100% action failure rate). The agent can plan but cannot execute. This is a single-line fix but blocks all autonomous operation.

Additionally, **replay packs are not persisted** (A-1), which blocks the governance audit requirement for human review of decisions.

---

## 7. Required Fixes Before Wider Rollout

**Ordered by priority:**

1. **FIX R-1** — Change `internal/agent/actions/executor.go:doPost()` to use `X-Admin-Token` header instead of `Authorization: Bearer`
2. **FIX A-1** — Investigate and fix replay pack persistence in `WithReplayRecorder()` adapter
3. **FIX A-2** — Investigate and fix path snapshot persistence in `WithSnapshotCapturer()` adapter
4. **ADD G-1** — Add API endpoint to set `require_human_review` in governance controller
5. **REVALIDATE** — Run this validation again after fixes to confirm learning writes, outcome recording, and replay functionality
6. **VALIDATE FALLBACK** — Run with ≥2 providers enabled to validate fallback chain behavior

---

## Appendix: Environment Configuration

```
Services:      api-gateway (8090), orchestrator (8082), worker (8083)
Database:      PostgreSQL 16 (docker)
Message Bus:   NATS 2.10 (docker)
LLM Provider:  Ollama (local, qwen3:1.7b)
Governance:    frozen, learning=enabled, exploration=frozen, policy=frozen, human_review=true
Scheduler:     disabled (manual trigger via POST /api/v1/agent/run-actions)
External:      disabled (OLLAMA_CLOUD_ENABLED=false, OPENROUTER_ENABLED=false)
```

## Appendix: Audit Event Summary (This Run)

| Event Type | Count |
|------------|-------|
| action.planned | 20 |
| action.failed | 20 |
| llm.started | 17 |
| llm.finished | 17 |
| job.leased | 17 |
| job.completed | 17 |
| proposal.created | 17 |
| arbitration.resolved | 14 |
| meta.mode_selected | 2 |
| governance.state_changed | 2 |
| job.created | 2 |
| planning.evaluated | 2 |
| decision_graph.evaluated | 2 |
| counterfactual.simulated | 2 |
| planning.action_selected | 2 |
| exploration.considered | 2 |
| control.retry_requeue_completed | 1 |
| **Total** | **139** |
