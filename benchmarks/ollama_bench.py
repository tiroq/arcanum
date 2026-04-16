#!/usr/bin/env python3
"""
ollama_bench.py — Chiefly-aware Ollama benchmark harness.

Evaluates local models across role-relevant scenarios to answer practical
architecture questions for the Chiefly task-processing system.

Roles evaluated:
  fast_classifier   — inbox task classification, priority suggestion
  project_router    — task-to-project assignment
  translator        — RU<->EN translation
  rewriter          — messy task cleanup to clean user-facing text
  short_summarizer  — single-sentence actionable summaries
  json_controller   — structured review payloads, admin proposals
  bulk_text_worker  — cheap, fast, good-enough text tasks
  long_reasoner     — complex multi-step proposals and analysis
"""

import argparse
import asyncio
import csv
import difflib
import json
import os
import platform
import re
import statistics
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, asdict
from typing import Any, Dict, List, Optional, Tuple


DEFAULT_BASE_URL = "http://localhost:11434"
THINK_MODES = {"thinking", "nothinking", "provider_default"}


# ---------------------------------------------------------------------------
# Scenario taxonomy
# ---------------------------------------------------------------------------

@dataclass
class Scenario:
    name: str
    category: str           # classification | routing | translation | rewrite | summary |
                            # extraction | structured_json | long_reasoning | generic_text
    prompt: str
    expectation: str        # "text" | "json"
    role_target: str        # fast_classifier | project_router | translator | rewriter |
                            # short_summarizer | json_controller | bulk_text_worker | long_reasoner
    priority_weight: float  # 0-1, used in role fit weighting
    latency_sensitivity: str    # "high" | "medium" | "low"
    strictness: str             # "strict" | "normal" | "relaxed"
    schema_fields: Optional[List[str]] = None       # required JSON keys for schema_valid
    semantic_keywords: Optional[List[str]] = None   # concept coverage check
    forbidden_markers: Optional[List[str]] = None   # penalized if present in output
    expected_length: Optional[str] = None           # "concise" | "medium" | "long"


# ---------------------------------------------------------------------------
# Prompt corpus (Chiefly-specific, realistic examples)
# ---------------------------------------------------------------------------

_CLASSIFY_PROMPT = (
    "You are a task classification assistant for Chiefly, a task-processing system.\n\n"
    "Classify the following inbox task. Respond with ONLY a JSON object matching this schema:\n"
    '{"type": "bug|feature|question|admin|personal|research|infra", '
    '"category": "string (short label)", "confidence": 0.0-1.0}\n\n'
    'Task: "нужно разобраться с тем почему деплой падает на staging при запуске миграций '
    '— кажется проблема в порядке выполнения хуков"'
)

_ROUTE_PROMPT = (
    "You are a routing assistant for Chiefly. Given a task description, assign it to the best matching project.\n\n"
    'Available projects: ["infra-ops", "backend-api", "mobile-app", "data-pipeline", "admin-tooling", "user-research"]\n\n'
    "Return ONLY a JSON object:\n"
    '{"project": "string", "confidence": 0.0-1.0, "reason": "one sentence"}\n\n'
    'Task: "Add Telegram webhook retry logic with exponential backoff and dead-letter queue support"'
)

_TRANSLATE_RU_EN_PROMPT = (
    "Translate the following Russian task text to English. Return ONLY the translated text, nothing else.\n\n"
    "Text: \"Реализовать механизм повторной отправки уведомлений при временной недоступности "
    "Telegram API с поддержкой экспоненциального бэкоффа и логированием каждой попытки\""
)

_TRANSLATE_EN_RU_PROMPT = (
    "Translate the following English task text to Russian. Return ONLY the translated text, nothing else.\n\n"
    "Text: \"Implement a webhook delivery retry mechanism with exponential backoff, "
    "dead-letter queue support, and per-attempt audit logging\""
)

_REWRITE_PROMPT = (
    "You are an editor for Chiefly. Rewrite the following messy task note into a clean, "
    "professional, user-facing task description. Be concise. Do not add information that is "
    "not present. Return ONLY the rewritten text.\n\n"
    "Original: \"надо сделать чтоб апи не падало когда пользак шлёт кривой json "
    "— щас 500 возвращает вместо 400, это бесит\""
)

_SUMMARY_PROMPT = (
    "Generate a single short actionable sentence (max 20 words) summarizing this task "
    "for a Telegram review message. Return ONLY the sentence.\n\n"
    "Task: \"Investigate and resolve the staging deployment failure caused by incorrect "
    "migration hook execution order. The current deployment pipeline runs post-deploy hooks "
    "before the database schema migration is complete, causing foreign key constraint errors "
    "on first boot.\""
)

_EXTRACT_REVIEW_PAYLOAD_PROMPT = (
    "You are extracting a structured review payload for Chiefly's Telegram review flow.\n\n"
    "Return ONLY valid JSON matching this schema:\n"
    "{\n"
    '  "task_id": "string",\n'
    '  "title": "string",\n'
    '  "priority": "low|medium|high|critical",\n'
    '  "project": "string",\n'
    '  "summary": "string (max 30 words)",\n'
    '  "suggested_action": "approve|reject|defer|escalate",\n'
    '  "requires_human_review": true|false\n'
    "}\n\n"
    "Task data: \"Task #4821 — Fix staging deploy failure in migration hook order. "
    "Affects release pipeline. Reported by: DevOps. Project: infra-ops. Severity: high. "
    "Last attempt to fix: 2 days ago.\""
)

_PRIORITY_PROMPT = (
    "You are a priority classifier for Chiefly.\n\n"
    "Return ONLY valid JSON:\n"
    '{"priority": "low|medium|high|critical", "confidence": 0.0-1.0, "reasoning": "one sentence"}\n\n'
    "Task: \"Users are unable to complete checkout because the payment gateway returns a 500 error "
    "intermittently. Affects ~30% of transactions in the last 2 hours.\""
)

_DUPLICATE_DETECT_PROMPT = (
    "You are a duplicate detection assistant for Chiefly.\n\n"
    "Given a new task and a list of existing tasks, determine if the new task is a duplicate "
    "or has significant overlap.\n\n"
    "Return ONLY valid JSON:\n"
    '{"is_duplicate": true|false, "overlap_score": 0.0-1.0, '
    '"closest_match_id": "string or null", "reason": "one sentence"}\n\n'
    "New task: \"Fix 500 error on payment API when gateway is unavailable\"\n\n"
    "Existing tasks:\n"
    "- #1201: \"Handle payment gateway timeout gracefully\"\n"
    "- #1205: \"Add fallback for stripe API failures\"\n"
    "- #1210: \"Improve checkout error messages for users\""
)

_ADMIN_PROPOSAL_PROMPT = (
    "You are an admin assistant for Chiefly. Produce a structured admin proposal "
    "for the following change request.\n\n"
    "Return ONLY valid JSON:\n"
    "{\n"
    '  "title": "string",\n'
    '  "priority": "low|medium|high|critical",\n'
    '  "affected_components": ["string"],\n'
    '  "steps": ["string"],\n'
    '  "risks": ["string"],\n'
    '  "estimated_effort": "string",\n'
    '  "review_required": true|false\n'
    "}\n\n"
    "Request: \"We need to migrate the existing job queue from in-memory Redis lists to "
    "NATS JetStream to improve durability and observability. This will affect the worker "
    "service, the orchestrator, and the writeback service.\""
)

_GENERIC_SHORT_PROMPT = (
    "Explain in two sentences why job idempotency matters in distributed task queues."
)

_GENERIC_LONG_PROMPT = (
    "You are a systems architect.\n\n"
    "Design a fault-tolerant, observable task processing pipeline that:\n"
    "- ingests tasks from multiple upstream sources\n"
    "- supports durable job queuing\n"
    "- routes tasks to specialized workers by type\n"
    "- persists all outputs as structured records\n"
    "- supports retry with backoff and dead-letter queues\n"
    "- maintains full audit history\n"
    "- integrates local LLMs for classification and rewrite\n\n"
    "Include:\n"
    "- component breakdown\n"
    "- data flow description\n"
    "- 5 failure modes and mitigations\n"
    "- 3 operational metrics per component\n"
    "- recommendation on model role assignments (classifier, rewriter, controller)"
)


# ---------------------------------------------------------------------------
# Scenario catalog
# ---------------------------------------------------------------------------

CHIEFLY_SCENARIOS: List[Scenario] = [
    Scenario(
        name="classify_task",
        category="classification",
        prompt=_CLASSIFY_PROMPT,
        expectation="json",
        role_target="fast_classifier",
        priority_weight=1.0,
        latency_sensitivity="high",
        strictness="strict",
        schema_fields=["type", "category", "confidence"],
        semantic_keywords=["bug", "deploy", "migration", "staging", "infra"],
        forbidden_markers=["i cannot", "i don't know", "sorry"],
        expected_length="concise",
    ),
    Scenario(
        name="route_to_project",
        category="routing",
        prompt=_ROUTE_PROMPT,
        expectation="json",
        role_target="project_router",
        priority_weight=1.0,
        latency_sensitivity="high",
        strictness="strict",
        schema_fields=["project", "confidence", "reason"],
        semantic_keywords=["telegram", "webhook", "retry", "backend"],
        forbidden_markers=["i cannot", "i don't know", "sorry"],
        expected_length="concise",
    ),
    Scenario(
        name="translate_ru_en",
        category="translation",
        prompt=_TRANSLATE_RU_EN_PROMPT,
        expectation="text",
        role_target="translator",
        priority_weight=0.9,
        latency_sensitivity="medium",
        strictness="normal",
        semantic_keywords=["retry", "notification", "telegram", "backoff", "logging"],
        forbidden_markers=["translation:", "translated:", "here is", "sure,", "here's"],
        expected_length="medium",
    ),
    Scenario(
        name="translate_en_ru",
        category="translation",
        prompt=_TRANSLATE_EN_RU_PROMPT,
        expectation="text",
        role_target="translator",
        priority_weight=0.9,
        latency_sensitivity="medium",
        strictness="normal",
        semantic_keywords=["backoff", "webhook", "audit", "queue"],
        forbidden_markers=["translation:", "translated:", "here is", "sure,", "here's"],
        expected_length="medium",
    ),
    Scenario(
        name="rewrite_task",
        category="rewrite",
        prompt=_REWRITE_PROMPT,
        expectation="text",
        role_target="rewriter",
        priority_weight=0.9,
        latency_sensitivity="medium",
        strictness="normal",
        semantic_keywords=["api", "json", "error", "400", "validation"],
        forbidden_markers=["here is", "here's the", "i've rewritten", "sure,", "rewritten version:"],
        expected_length="concise",
    ),
    Scenario(
        name="short_summary",
        category="summary",
        prompt=_SUMMARY_PROMPT,
        expectation="text",
        role_target="short_summarizer",
        priority_weight=1.0,
        latency_sensitivity="high",
        strictness="strict",
        semantic_keywords=["staging", "deploy", "migration", "fix"],
        forbidden_markers=["here is", "here's", "sure,", "summary:"],
        expected_length="concise",
    ),
    Scenario(
        name="extract_review_payload",
        category="extraction",
        prompt=_EXTRACT_REVIEW_PAYLOAD_PROMPT,
        expectation="json",
        role_target="json_controller",
        priority_weight=1.0,
        latency_sensitivity="medium",
        strictness="strict",
        schema_fields=["task_id", "title", "priority", "project", "summary",
                       "suggested_action", "requires_human_review"],
        semantic_keywords=["infra-ops", "high", "staging"],
        forbidden_markers=["here is", "here's", "sure,"],
        expected_length="concise",
    ),
    Scenario(
        name="suggest_priority",
        category="structured_json",
        prompt=_PRIORITY_PROMPT,
        expectation="json",
        role_target="fast_classifier",
        priority_weight=0.9,
        latency_sensitivity="high",
        strictness="strict",
        schema_fields=["priority", "confidence", "reasoning"],
        semantic_keywords=["payment", "checkout", "critical", "high"],
        forbidden_markers=["i cannot", "sorry", "here is"],
        expected_length="concise",
    ),
    Scenario(
        name="detect_duplicate",
        category="structured_json",
        prompt=_DUPLICATE_DETECT_PROMPT,
        expectation="json",
        role_target="json_controller",
        priority_weight=0.8,
        latency_sensitivity="medium",
        strictness="strict",
        schema_fields=["is_duplicate", "overlap_score", "closest_match_id", "reason"],
        semantic_keywords=["payment", "gateway", "overlap"],
        forbidden_markers=["i cannot", "sorry", "here is"],
        expected_length="concise",
    ),
    Scenario(
        name="admin_proposal",
        category="structured_json",
        prompt=_ADMIN_PROPOSAL_PROMPT,
        expectation="json",
        role_target="long_reasoner",
        priority_weight=0.8,
        latency_sensitivity="low",
        strictness="normal",
        schema_fields=["title", "priority", "affected_components", "steps",
                       "risks", "estimated_effort", "review_required"],
        semantic_keywords=["nats", "jetstream", "worker", "orchestrator", "writeback", "durability"],
        forbidden_markers=["sorry", "i cannot"],
        expected_length="medium",
    ),
]

GENERIC_SCENARIOS: List[Scenario] = [
    Scenario(
        name="generic_short",
        category="generic_text",
        prompt=_GENERIC_SHORT_PROMPT,
        expectation="text",
        role_target="bulk_text_worker",
        priority_weight=0.6,
        latency_sensitivity="high",
        strictness="relaxed",
        semantic_keywords=["idempotency", "distributed", "queue", "duplicate"],
        forbidden_markers=["i cannot", "sorry"],
        expected_length="concise",
    ),
    Scenario(
        name="generic_long",
        category="long_reasoning",
        prompt=_GENERIC_LONG_PROMPT,
        expectation="text",
        role_target="long_reasoner",
        priority_weight=0.7,
        latency_sensitivity="low",
        strictness="relaxed",
        semantic_keywords=["ingestion", "queue", "worker", "retry", "audit",
                           "classifier", "rewriter", "metrics"],
        forbidden_markers=["i cannot", "sorry"],
        expected_length="long",
    ),
]

MINIMAL_SCENARIOS: List[Scenario] = [
    s for s in CHIEFLY_SCENARIOS
    if s.name in {"classify_task", "route_to_project", "extract_review_payload"}
] + [GENERIC_SCENARIOS[0]]

ALL_SCENARIOS: List[Scenario] = CHIEFLY_SCENARIOS + GENERIC_SCENARIOS

SCENARIO_SETS: Dict[str, List[Scenario]] = {
    "chiefly": CHIEFLY_SCENARIOS,
    "generic": GENERIC_SCENARIOS,
    "minimal": MINIMAL_SCENARIOS,
    "all": ALL_SCENARIOS,
}

# Lookup used during post-processing
_SCENARIO_BY_NAME: Dict[str, Scenario] = {sc.name: sc for sc in ALL_SCENARIOS}


# ---------------------------------------------------------------------------
# Per-category scoring weight profiles
# Dimensions: format | instruction | semantic | conciseness | actionability
# Each row must sum to 1.0.
# ---------------------------------------------------------------------------

CATEGORY_WEIGHTS: Dict[str, Dict[str, float]] = {
    "classification": {
        "instruction_score":  0.30,
        "conciseness_score":  0.25,
        "format_score":       0.20,
        "actionability_score": 0.15,
        "semantic_score":     0.10,
    },
    "routing": {
        "instruction_score":  0.30,
        "format_score":       0.25,
        "semantic_score":     0.20,
        "actionability_score": 0.15,
        "conciseness_score":  0.10,
    },
    "translation": {
        "semantic_score":     0.35,
        "instruction_score":  0.25,
        "conciseness_score":  0.20,
        "format_score":       0.10,
        "actionability_score": 0.10,
    },
    "rewrite": {
        "actionability_score": 0.30,
        "conciseness_score":  0.30,
        "instruction_score":  0.20,
        "semantic_score":     0.10,
        "format_score":       0.10,
    },
    "summary": {
        "conciseness_score":  0.35,
        "actionability_score": 0.25,
        "instruction_score":  0.25,
        "semantic_score":     0.10,
        "format_score":       0.05,
    },
    "extraction": {
        "format_score":       0.30,
        "instruction_score":  0.30,
        "semantic_score":     0.20,
        "actionability_score": 0.10,
        "conciseness_score":  0.10,
    },
    "structured_json": {
        "format_score":       0.30,
        "instruction_score":  0.25,
        "semantic_score":     0.20,
        "actionability_score": 0.15,
        "conciseness_score":  0.10,
    },
    "long_reasoning": {
        "semantic_score":     0.35,
        "instruction_score":  0.25,
        "actionability_score": 0.20,
        "format_score":       0.10,
        "conciseness_score":  0.10,
    },
    "generic_text": {
        "instruction_score":  0.25,
        "semantic_score":     0.25,
        "actionability_score": 0.20,
        "conciseness_score":  0.20,
        "format_score":       0.10,
    },
}


# ---------------------------------------------------------------------------
# RunResult dataclass
# ---------------------------------------------------------------------------

@dataclass
class RunResult:
    # Identity
    model_requested: str
    model_resolved: str
    scenario: str
    category: str
    expectation: str
    role_target: str
    concurrency: int
    run_kind: str
    ok: bool
    error: Optional[str]

    # Think mode
    requested_think_mode: str
    effective_think_mode: str
    think_mode_source: str

    # Ollama timing
    total_duration_ns: Optional[int]
    load_duration_ns: Optional[int]
    prompt_eval_count: Optional[int]
    prompt_eval_duration_ns: Optional[int]
    eval_count: Optional[int]
    eval_duration_ns: Optional[int]
    wall_time_s: float
    prompt_tps: Optional[float]
    gen_tps: Optional[float]
    response_chars: Optional[int]
    response_text: Optional[str]

    # JSON validity pipeline (None for text tasks)
    raw_json_valid: Optional[bool]
    sanitized_json_valid: Optional[bool]
    sanitization_applied: Optional[bool]
    sanitization_kind: Optional[str]    # e.g. "strip_fence,strip_prefix"
    schema_valid: Optional[bool]
    semantic_valid: Optional[bool]

    # Scoring dimensions (0-100 each)
    format_score: Optional[float]
    instruction_score: Optional[float]
    semantic_score: Optional[float]
    conciseness_score: Optional[float]
    actionability_score: Optional[float]
    latency_score: Optional[float]      # relative to peer group, filled post-run
    throughput_score: Optional[float]   # relative to peer group, filled post-run

    # Composite
    chiefly_fit_score: Optional[float]  # category-weighted blend
    quality_score: Optional[float]      # alias for chiefly_fit_score (backward compat)

    # Gating
    gated_usable: Optional[bool]
    strict_usable: Optional[bool]       # raw_json_valid + schema_valid
    recoverable_usable: Optional[bool]  # sanitized_json_valid + schema_valid
    scenario_rank_score: Optional[float]
    rejection_reason: Optional[str]


# ---------------------------------------------------------------------------
# JSON sanitization pipeline
# ---------------------------------------------------------------------------

def _strip_markdown_fences(text: str) -> Tuple[str, bool]:
    """Remove ```json ... ``` or ``` ... ``` wrappers."""
    match = re.search(r"```(?:json)?\s*([\s\S]*?)```", text)
    if match:
        return match.group(1).strip(), True
    return text, False


def _strip_reasoning_prefix(text: str) -> Tuple[str, bool]:
    """
    Strip leading commentary before the first JSON token.
    Finds the first { or [ and discards everything before it.
    """
    for i, ch in enumerate(text):
        if ch in ("{", "["):
            if i > 0:
                return text[i:], True
            return text, False
    return text, False


def _extract_first_json_object(text: str) -> Tuple[str, bool]:
    """
    Extract the first complete top-level JSON object or array by brace matching.
    Returns (extracted, was_truncated).
    """
    stripped = text.strip()
    if not stripped or stripped[0] not in ("{", "["):
        return text, False

    start_char = stripped[0]
    end_char = "}" if start_char == "{" else "]"
    depth = 0
    in_string = False
    escape = False

    for i, ch in enumerate(stripped):
        if escape:
            escape = False
            continue
        if ch == "\\":
            escape = True
            continue
        if ch == '"' and not escape:
            in_string = not in_string
            continue
        if in_string:
            continue
        if ch == start_char:
            depth += 1
        elif ch == end_char:
            depth -= 1
            if depth == 0:
                candidate = stripped[:i + 1]
                return candidate, candidate != stripped

    return text, False


def sanitize_json(raw: str) -> Tuple[str, List[str]]:
    """
    Apply the sanitization pipeline in order.
    Returns (cleaned_text, list_of_applied_steps).
    """
    text = raw.strip()
    steps: List[str] = []

    text, applied = _strip_markdown_fences(text)
    if applied:
        steps.append("strip_fence")

    text, applied = _strip_reasoning_prefix(text)
    if applied:
        steps.append("strip_prefix")

    text, applied = _extract_first_json_object(text)
    if applied:
        steps.append("extract_first_object")

    return text.strip(), steps


def evaluate_json_pipeline(
    raw_text: str,
    schema_fields: Optional[List[str]],
    semantic_keywords: Optional[List[str]],
) -> Tuple[bool, bool, bool, Optional[str], bool, Optional[Any], bool]:
    """
    Full JSON validity pipeline.

    Returns:
        raw_json_valid, sanitized_json_valid, sanitization_applied,
        sanitization_kind, schema_valid, parsed_object, semantic_valid
    """
    parsed: Optional[Any] = None
    raw_valid = False
    try:
        parsed = json.loads(raw_text)
        raw_valid = True
    except Exception:
        pass

    sanitization_applied = False
    sanitization_kind: Optional[str] = None
    sanitized_valid = raw_valid

    if not raw_valid:
        cleaned, steps = sanitize_json(raw_text)
        if steps:
            sanitization_applied = True
            sanitization_kind = ",".join(steps)
        try:
            parsed = json.loads(cleaned)
            sanitized_valid = True
        except Exception:
            sanitized_valid = False

    # Schema validation: required field presence
    schema_valid = False
    if parsed is not None and isinstance(parsed, dict) and schema_fields:
        schema_valid = all(f in parsed for f in schema_fields)
    elif parsed is not None and not schema_fields:
        schema_valid = True

    # Semantic validation: keyword coverage in serialized output
    semantic_valid = False
    if parsed is not None and semantic_keywords:
        serialized = json.dumps(parsed).lower()
        hits = sum(1 for kw in semantic_keywords if kw.lower() in serialized)
        semantic_valid = hits >= max(1, len(semantic_keywords) // 3)
    elif parsed is not None and not semantic_keywords:
        semantic_valid = True

    return raw_valid, sanitized_valid, sanitization_applied, sanitization_kind, schema_valid, parsed, semantic_valid


# ---------------------------------------------------------------------------
# Scoring dimension functions — all return 0-100 floats
# ---------------------------------------------------------------------------

def score_format(
    text: str,
    expectation: str,
    raw_json_valid: bool,
    sanitized_json_valid: bool,
) -> float:
    """
    JSON: full credit for raw-valid; partial for sanitized; zero otherwise.
    Text: structural quality without overweighting bullets/headings.
    """
    if expectation == "json":
        if raw_json_valid:
            return 100.0
        if sanitized_json_valid:
            return 70.0   # recoverable but output was not clean
        return 0.0

    stripped = text.strip()
    if not stripped:
        return 0.0

    score = 40.0  # baseline for any non-empty response
    paragraphs = [p for p in stripped.split("\n\n") if p.strip()]
    if len(paragraphs) >= 2:
        score += 20.0
    if re.search(r"(^|\n)(#+\s|\d+\.\s|[-*]\s)", stripped):
        score += 15.0  # mild reward for structure, capped to prevent gaming

    # Penalize one long run-on block with no line breaks
    if len(stripped) > 300 and "\n" not in stripped:
        score -= 15.0

    return max(0.0, min(100.0, score))


def score_instruction(
    text: str,
    expected_length: Optional[str],
    forbidden_markers: Optional[List[str]],
    expectation: str,
    schema_valid: bool,
) -> float:
    """
    Instruction-following: forbidden markers, verbosity compliance, JSON schema adherence.
    """
    stripped = text.strip()
    lower = stripped.lower()
    score = 60.0

    # Forbidden content penalty
    if forbidden_markers:
        hits = sum(1 for m in forbidden_markers if m.lower() in lower)
        score -= hits * 15.0

    # Length compliance
    words = len(re.findall(r"\S+", stripped))
    if expected_length == "concise":
        if words <= 60:
            score += 20.0
        elif words <= 120:
            score += 5.0
        else:
            score -= min(20.0, (words - 120) * 0.1)
    elif expected_length == "medium":
        if 40 <= words <= 200:
            score += 20.0
        elif words < 20:
            score -= 15.0
        elif words > 400:
            score -= 10.0
    elif expected_length == "long":
        if words >= 200:
            score += 20.0
        elif words >= 100:
            score += 10.0
        else:
            score -= 15.0

    # For JSON tasks, schema compliance IS instruction compliance
    if expectation == "json":
        score += 20.0 if schema_valid else -20.0

    # Fluff penalty: filler phrases that add no signal
    fluff = [
        "of course", "certainly", "great question", "i'm happy to",
        "sure, here", "as requested", "hope this helps", "let me know", "feel free to",
    ]
    score -= sum(1 for p in fluff if p in lower) * 8.0

    return max(0.0, min(100.0, score))


def score_semantic(
    text: str,
    semantic_keywords: Optional[List[str]],
    parsed: Optional[Any],
) -> float:
    """
    Conceptual coverage check. Not length-based — checks that
    expected domain concepts appear in the output.
    """
    if not semantic_keywords:
        return 60.0

    search_text = json.dumps(parsed).lower() if parsed is not None else text.lower()
    hits = sum(1 for kw in semantic_keywords if kw.lower() in search_text)
    ratio = hits / len(semantic_keywords)

    score = ratio * 85.0
    if ratio >= 0.5:
        score += 10.0
    if ratio >= 0.8:
        score += 5.0

    return max(0.0, min(100.0, score))


def score_conciseness(text: str, expected_length: Optional[str]) -> float:
    """
    Penalizes verbose output for concise tasks.
    Rewards appropriate length per task type.
    """
    words = len(re.findall(r"\S+", text.strip()))
    if words == 0:
        return 0.0

    if expected_length == "concise":
        if words <= 30:
            return 100.0
        if words <= 60:
            return 85.0
        if words <= 100:
            return 65.0
        if words <= 150:
            return 45.0
        return max(10.0, 45.0 - (words - 150) * 0.1)

    if expected_length == "medium":
        if 40 <= words <= 150:
            return 90.0
        if words < 20:
            return 30.0
        if words > 300:
            return max(30.0, 90.0 - (words - 300) * 0.1)
        return 70.0

    if expected_length == "long":
        if words >= 300:
            return 90.0
        if words >= 150:
            return 70.0
        return max(20.0, words / 300 * 90.0)

    return 60.0


def score_actionability(
    text: str,
    category: str,
    expectation: str,
    parsed: Optional[Any],
) -> float:
    """
    Rewards outputs that enable a concrete next action.
    Signals are tuned per category.
    """
    lower = text.strip().lower()
    score = 40.0

    if expectation == "json" and parsed and isinstance(parsed, dict):
        serialized = json.dumps(parsed).lower()
        vague = ["todo", "tbd", "n/a", "none", "unknown", "string", "placeholder"]
        score -= sum(1 for v in vague if v in serialized) * 10.0
        for v in parsed.values():
            if isinstance(v, list) and len(v) >= 2:
                score += 10.0
                break
        actionable_verbs = ["implement", "fix", "migrate", "add", "remove",
                            "refactor", "deploy", "configure"]
        score += min(25.0, sum(1 for w in actionable_verbs if w in serialized) * 8.0)
        return max(0.0, min(100.0, score))

    if category in ("classification", "routing"):
        hedging = ["it depends", "unclear", "not sure", "might be", "could be", "possibly"]
        score -= sum(1 for h in hedging if h in lower) * 12.0
        if any(w in lower for w in ["high", "critical", "definitely", "clearly"]):
            score += 10.0
        score += 20.0
        return max(0.0, min(100.0, score))

    if category == "rewrite":
        direct = ["fix", "add", "implement", "return", "handle", "ensure", "update"]
        score += min(30.0, sum(1 for w in direct if w in lower) * 10.0)
        meta = ["i've rewritten", "here's a cleaner", "as requested"]
        score -= sum(1 for m in meta if m in lower) * 15.0
        return max(0.0, min(100.0, score))

    if category == "summary":
        words = len(re.findall(r"\S+", text.strip()))
        score += 40.0 if words <= 20 else (20.0 if words <= 35 else -10.0)
        return max(0.0, min(100.0, score))

    if category == "translation":
        meta = ["translation:", "translated text:", "here is the", "here's the"]
        score -= sum(1 for m in meta if m in lower) * 20.0
        words = len(re.findall(r"\S+", text.strip()))
        if 5 <= words <= 80:
            score += 30.0
        return max(0.0, min(100.0, score))

    if text.strip():
        score += 20.0
    return max(0.0, min(100.0, score))


def compute_chiefly_fit_score(
    format_s: float,
    instruction_s: float,
    semantic_s: float,
    conciseness_s: float,
    actionability_s: float,
    category: str,
) -> float:
    """Category-weighted blend of the five scoring dimensions."""
    w = CATEGORY_WEIGHTS.get(category, CATEGORY_WEIGHTS["generic_text"])
    return max(0.0, min(100.0,
        w["format_score"]        * format_s +
        w["instruction_score"]   * instruction_s +
        w["semantic_score"]      * semantic_s +
        w["conciseness_score"]   * conciseness_s +
        w["actionability_score"] * actionability_s
    ))


def score_run(response_text: str, scenario: Scenario) -> Dict[str, Any]:
    """
    Full scoring pipeline for one run's response.
    Returns a dict of all scoring and validity fields.
    """
    if not response_text or not response_text.strip():
        return {
            "raw_json_valid": False, "sanitized_json_valid": False,
            "sanitization_applied": False, "sanitization_kind": None,
            "schema_valid": False, "semantic_valid": False,
            "format_score": 0.0, "instruction_score": 0.0,
            "semantic_score": 0.0, "conciseness_score": 0.0,
            "actionability_score": 0.0, "chiefly_fit_score": 0.0,
            "quality_score": 0.0,
        }

    parsed: Optional[Any] = None
    raw_json_valid = False
    sanitized_json_valid = False
    sanitization_applied = False
    sanitization_kind: Optional[str] = None
    schema_valid = False
    semantic_valid = False

    if scenario.expectation == "json":
        (raw_json_valid, sanitized_json_valid, sanitization_applied,
         sanitization_kind, schema_valid, parsed, semantic_valid) = evaluate_json_pipeline(
            response_text,
            schema_fields=scenario.schema_fields,
            semantic_keywords=scenario.semantic_keywords,
        )
    else:
        if scenario.semantic_keywords:
            lower = response_text.lower()
            hits = sum(1 for kw in scenario.semantic_keywords if kw.lower() in lower)
            semantic_valid = hits >= max(1, len(scenario.semantic_keywords) // 3)
        else:
            semantic_valid = True

    format_s = score_format(
        response_text, scenario.expectation, raw_json_valid, sanitized_json_valid
    )
    instruction_s = score_instruction(
        response_text, scenario.expected_length,
        scenario.forbidden_markers, scenario.expectation, schema_valid,
    )
    semantic_s    = score_semantic(response_text, scenario.semantic_keywords, parsed)
    conciseness_s = score_conciseness(response_text, scenario.expected_length)
    actionability_s = score_actionability(
        response_text, scenario.category, scenario.expectation, parsed
    )
    chiefly_fit = compute_chiefly_fit_score(
        format_s, instruction_s, semantic_s, conciseness_s, actionability_s,
        scenario.category,
    )

    return {
        "raw_json_valid":       raw_json_valid,
        "sanitized_json_valid": sanitized_json_valid,
        "sanitization_applied": sanitization_applied,
        "sanitization_kind":    sanitization_kind,
        "schema_valid":         schema_valid,
        "semantic_valid":       semantic_valid,
        "format_score":         round(format_s, 2),
        "instruction_score":    round(instruction_s, 2),
        "semantic_score":       round(semantic_s, 2),
        "conciseness_score":    round(conciseness_s, 2),
        "actionability_score":  round(actionability_s, 2),
        "chiefly_fit_score":    round(chiefly_fit, 2),
        "quality_score":        round(chiefly_fit, 2),
    }


# ---------------------------------------------------------------------------
# Timing helpers
# ---------------------------------------------------------------------------

def ns_to_s(value: Optional[int]) -> Optional[float]:
    return None if value is None else value / 1_000_000_000.0


def compute_prompt_tps(
    prompt_eval_count: Optional[int],
    prompt_eval_duration_ns: Optional[int],
) -> Optional[float]:
    if prompt_eval_count is None or prompt_eval_duration_ns in (None, 0):
        return None
    return prompt_eval_count / (prompt_eval_duration_ns / 1_000_000_000.0)


def compute_gen_tps(
    eval_count: Optional[int],
    eval_duration_ns: Optional[int],
) -> Optional[float]:
    if eval_count is None or eval_duration_ns in (None, 0):
        return None
    return eval_count / (eval_duration_ns / 1_000_000_000.0)


def summarize_numeric(values: List[Optional[float]]) -> Dict[str, Optional[float]]:
    cleaned = [v for v in values if v is not None]
    if not cleaned:
        return {"mean": None, "median": None, "min": None, "max": None, "stdev": None}
    return {
        "mean":   statistics.mean(cleaned),
        "median": statistics.median(cleaned),
        "min":    min(cleaned),
        "max":    max(cleaned),
        "stdev":  statistics.stdev(cleaned) if len(cleaned) >= 2 else 0.0,
    }


def fmt(value: Optional[float], digits: int = 2) -> str:
    return "-" if value is None else f"{value:.{digits}f}"


def format_seconds_from_ns(ns: Optional[int]) -> str:
    return "-" if ns is None else fmt(ns / 1_000_000_000.0, 2)


# ---------------------------------------------------------------------------
# Print helpers
# ---------------------------------------------------------------------------

def print_section(title: str) -> None:
    print()
    print("=" * len(title))
    print(title)
    print("=" * len(title))


def table(rows: List[List[str]]) -> None:
    if not rows:
        return
    widths = [max(len(str(row[i])) for row in rows) for i in range(len(rows[0]))]
    for idx, row in enumerate(rows):
        print(" | ".join(str(cell).ljust(widths[i]) for i, cell in enumerate(row)))
        if idx == 0:
            print("-+-".join("-" * w for w in widths))


# ---------------------------------------------------------------------------
# Ollama API (preserved from v3)
# ---------------------------------------------------------------------------

def http_get_json(url: str, timeout_s: int) -> Any:
    req = urllib.request.Request(url, method="GET")
    with urllib.request.urlopen(req, timeout=timeout_s) as resp:
        return json.loads(resp.read().decode("utf-8"))


def http_post_json(url: str, payload: Dict[str, Any], timeout_s: int) -> Dict[str, Any]:
    body = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        url, data=body,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=timeout_s) as resp:
        return json.loads(resp.read().decode("utf-8"))


def ollama_tags(base_url: str, timeout_s: int) -> List[str]:
    data = http_get_json(f"{base_url.rstrip('/')}/api/tags", timeout_s)
    return [m["name"] for m in data.get("models", []) if m.get("name")]


def ollama_generate(
    base_url: str,
    model: str,
    prompt: str,
    timeout_s: int,
    think_mode: str,
) -> Dict[str, Any]:
    payload: Dict[str, Any] = {"model": model, "prompt": prompt, "stream": False}
    if think_mode == "thinking":
        payload["think"] = True
    elif think_mode == "nothinking":
        payload["think"] = False
    elif think_mode == "provider_default":
        pass
    else:
        raise ValueError(f"Unsupported think_mode: {think_mode}")
    return http_post_json(
        f"{base_url.rstrip('/')}/api/generate", payload, timeout_s=timeout_s
    )


def ollama_unload(base_url: str, model: str, timeout_s: int) -> None:
    try:
        http_post_json(
            f"{base_url.rstrip('/')}/api/generate",
            {"model": model, "prompt": "", "stream": False, "keep_alive": 0},
            timeout_s=timeout_s,
        )
    except Exception:
        pass


def ollama_pull(base_url: str, model: str, timeout_s: int) -> bool:
    try:
        body = json.dumps({"name": model, "stream": False}).encode("utf-8")
        req = urllib.request.Request(
            f"{base_url.rstrip('/')}/api/pull",
            data=body,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=timeout_s) as resp:
            _ = resp.read()
        return True
    except Exception:
        return False


def search_ollama_library(query: str, timeout_s: int = 20) -> List[str]:
    candidates: List[str] = []
    try:
        url = f"https://ollama.com/search?q={urllib.parse.quote(query)}"
        html = urllib.request.urlopen(url, timeout=timeout_s).read().decode("utf-8", errors="ignore")
        for item in re.findall(r'href="/library/([^"/?]+)', html):
            if item not in candidates:
                candidates.append(item)
    except Exception:
        pass
    return candidates


def normalize_model_hint(name: str) -> str:
    return name.strip().lower()


def expand_model_guesses(name: str) -> List[str]:
    name = normalize_model_hint(name)
    guesses = [name]
    if name.endswith("-instruct"):
        guesses.append(name[:-len("-instruct")])
    if ":" in name:
        family, tag = name.split(":", 1)
        guesses.append(family)
        if tag.endswith("-instruct"):
            guesses.append(f"{family}:{tag[:-len('-instruct')]}")
    else:
        guesses.extend([f"{name}:latest", f"{name}:7b", f"{name}:8b", f"{name}:1.5b", f"{name}:3b"])
    seen: set = set()
    out: List[str] = []
    for g in guesses:
        if g and g not in seen:
            out.append(g)
            seen.add(g)
    return out


def pick_closest_model(
    requested: str,
    installed: List[str],
    remote_hints: List[str],
) -> Tuple[Optional[str], List[str]]:
    candidates = list(dict.fromkeys(installed + remote_hints))
    if not candidates:
        return None, []
    req = normalize_model_hint(requested)
    family = req.split(":")[0]
    family_matches = [c for c in candidates if c.lower().startswith(family)]
    if family_matches:
        close = difflib.get_close_matches(req, family_matches, n=5, cutoff=0.0)
        return (close[0], close) if close else (family_matches[0], family_matches[:5])
    close = difflib.get_close_matches(req, candidates, n=5, cutoff=0.0)
    return (close[0], close) if close else (None, [])


def resolve_model_name(
    base_url: str,
    requested: str,
    timeout_s: int,
    auto_pull: bool,
    resolve_closest: bool,
) -> Tuple[str, List[str], bool]:
    installed = ollama_tags(base_url, timeout_s)
    installed_set = set(installed)

    if requested in installed_set:
        return requested, [], False

    guesses = expand_model_guesses(requested)
    for g in guesses:
        if g in installed_set:
            return g, [f"resolved installed alias: {requested} -> {g}"], False

    remote_hints = search_ollama_library(requested)
    best, suggestions = pick_closest_model(requested, installed, remote_hints)

    if auto_pull:
        print(f"  Model '{requested}' not installed. Attempting pull...")
        if ollama_pull(base_url, requested, timeout_s):
            return requested, [f"pulled {requested}"], True
        for g in guesses:
            if g == requested:
                continue
            print(f"  Trying '{g}'...")
            if ollama_pull(base_url, g, timeout_s):
                return g, [f"pulled {g}"], True
        if best and ollama_pull(base_url, best, timeout_s):
            return best, suggestions, True

    if resolve_closest and best:
        return best, suggestions, False

    raise ValueError(
        f"Model '{requested}' not found. Suggestions: "
        f"{', '.join(suggestions or [f'no match for {requested}'])}"
    )


# ---------------------------------------------------------------------------
# Gating and post-processing
# ---------------------------------------------------------------------------

def evaluate_gate(
    result: RunResult,
    strict_json_mode: str,
    json_quality_threshold: float,
) -> Tuple[bool, bool, bool, Optional[str]]:
    """
    Returns: gated_usable, strict_usable, recoverable_usable, rejection_reason

    strict_json_mode:
      "raw"         - only raw-valid JSON counts as usable
      "recoverable" - sanitized JSON is acceptable (default)
      "both"        - same as "recoverable" for gated_usable
    """
    if not result.ok:
        return False, False, False, "request failed"

    if result.expectation == "json":
        strict_ok  = bool(result.raw_json_valid and result.schema_valid)
        recover_ok = bool(result.sanitized_json_valid and result.schema_valid)
        gated = strict_ok if strict_json_mode == "raw" else recover_ok

        if result.chiefly_fit_score is not None and result.chiefly_fit_score < json_quality_threshold:
            return False, strict_ok, recover_ok, (
                f"fit_score {result.chiefly_fit_score:.1f} below threshold {json_quality_threshold:.1f}"
            )
        if not gated:
            if not result.sanitized_json_valid:
                return False, strict_ok, recover_ok, "json unparseable after sanitization"
            return False, strict_ok, recover_ok, "schema fields missing"
        return gated, strict_ok, recover_ok, None

    # Text task: gate on fit score
    fit = result.chiefly_fit_score
    if fit is None or fit <= 0:
        return False, False, False, "empty or zero fit score"
    if fit < json_quality_threshold:
        return False, False, False, f"fit_score {fit:.1f} below threshold {json_quality_threshold:.1f}"
    return True, True, True, None


def normalize_score_higher_better(value: Optional[float], best: float) -> float:
    if value is None or best <= 0:
        return 0.0
    return max(0.0, min(1.0, value / best))


def normalize_score_lower_better(value: Optional[float], best: float) -> float:
    if value is None or value <= 0 or best <= 0:
        return 0.0
    return max(0.0, min(1.0, best / value))


def assign_latency_throughput_scores(results: List[RunResult]) -> None:
    """Compute latency_score and throughput_score relative to peers in the same group."""
    groups: Dict[Tuple, List[RunResult]] = {}
    for r in results:
        key = (r.scenario, r.run_kind, r.concurrency, r.effective_think_mode)
        groups.setdefault(key, []).append(r)

    for _, group in groups.items():
        ok = [r for r in group if r.ok and r.wall_time_s is not None]
        if not ok:
            for r in group:
                r.latency_score = 0.0
                r.throughput_score = 0.0
            continue
        best_wall = min(r.wall_time_s for r in ok)  # type: ignore[arg-type]
        best_gen  = max((r.gen_tps or 0.0) for r in ok)
        for r in group:
            if not r.ok:
                r.latency_score = 0.0
                r.throughput_score = 0.0
                continue
            r.latency_score    = round(normalize_score_lower_better(r.wall_time_s, best_wall) * 100.0, 2)
            r.throughput_score = round(
                normalize_score_higher_better(r.gen_tps, best_gen) * 100.0
                if best_gen > 0 else 50.0, 2
            )


def assign_scenario_rank_scores(results: List[RunResult]) -> None:
    """
    Scenario rank score = weighted blend of chiefly_fit_score + latency_score.
    Latency weight varies by the scenario's latency_sensitivity.
    """
    groups: Dict[Tuple, List[RunResult]] = {}
    for r in results:
        key = (r.scenario, r.run_kind, r.concurrency, r.effective_think_mode)
        groups.setdefault(key, []).append(r)

    for _, group in groups.items():
        usable = [r for r in group if r.gated_usable]
        if not usable:
            for r in group:
                r.scenario_rank_score = 0.0
            continue

        best_fit  = max((r.chiefly_fit_score or 0.0) for r in usable)
        best_wall = min((r.wall_time_s or 9999.0) for r in usable)

        for r in group:
            if not r.gated_usable:
                r.scenario_rank_score = 0.0
                continue
            sc = _SCENARIO_BY_NAME.get(r.scenario)
            lat_sens = sc.latency_sensitivity if sc else "medium"
            lat_w = {"high": 0.35, "medium": 0.20, "low": 0.10}.get(lat_sens, 0.20)
            fit_w = 1.0 - lat_w
            fit_norm = normalize_score_higher_better(r.chiefly_fit_score, best_fit)
            lat_norm = normalize_score_lower_better(r.wall_time_s, best_wall)
            r.scenario_rank_score = round((fit_w * fit_norm + lat_w * lat_norm) * 100.0, 2)


def finalize_results(
    results: List[RunResult],
    strict_json_mode: str,
    json_quality_threshold: float,
) -> None:
    for r in results:
        gated, strict_ok, recov_ok, reason = evaluate_gate(r, strict_json_mode, json_quality_threshold)
        r.gated_usable        = gated
        r.strict_usable       = strict_ok
        r.recoverable_usable  = recov_ok
        r.rejection_reason    = reason
    assign_latency_throughput_scores(results)
    assign_scenario_rank_scores(results)


# ---------------------------------------------------------------------------
# Role fit computation
# ---------------------------------------------------------------------------

# Mapping from Chiefly role to the diagnostic scenario names.
ROLE_SCENARIO_MAP: Dict[str, List[str]] = {
    "fast_classifier":  ["classify_task", "suggest_priority"],
    "project_router":   ["route_to_project"],
    "translator":       ["translate_ru_en", "translate_en_ru"],
    "rewriter":         ["rewrite_task"],
    "short_summarizer": ["short_summary"],
    "json_controller":  ["extract_review_payload", "admin_proposal", "detect_duplicate"],
    "bulk_text_worker": ["generic_short", "rewrite_task", "short_summary"],
    "long_reasoner":    ["generic_long", "admin_proposal"],
}


def compute_role_fits(results: List[RunResult]) -> Dict[str, Dict[str, Any]]:
    """
    Per model, compute fit statistics for each Chiefly role.
    Uses only warm + concurrency=1 runs for role decisions.
    """
    model_role_fits: Dict[str, Dict[str, Any]] = {}

    for model in sorted(set(r.model_resolved for r in results)):
        model_role_fits[model] = {}
        for role, scenario_names in ROLE_SCENARIO_MAP.items():
            relevant = [
                r for r in results
                if r.model_resolved == model
                and r.scenario in scenario_names
                and r.run_kind == "warm"
                and r.concurrency == 1
            ]
            if not relevant:
                model_role_fits[model][role] = None
                continue

            total    = len(relevant)
            ok_runs  = [r for r in relevant if r.ok]
            usable   = [r for r in relevant if r.gated_usable]
            strict   = [r for r in relevant if r.strict_usable]
            recover  = [r for r in relevant if r.recoverable_usable]

            fit_scores  = [r.chiefly_fit_score for r in usable if r.chiefly_fit_score is not None]
            rank_scores = [r.scenario_rank_score for r in usable if r.scenario_rank_score is not None]
            latencies   = [r.wall_time_s for r in ok_runs if r.wall_time_s is not None]
            gen_tps_vals = [r.gen_tps for r in ok_runs if r.gen_tps is not None]

            model_role_fits[model][role] = {
                "fit_mean":               round(statistics.mean(fit_scores), 2) if fit_scores else 0.0,
                "rank_mean":              round(statistics.mean(rank_scores), 2) if rank_scores else 0.0,
                "usable_rate":            round(len(usable) / total, 3),
                "strict_usable_rate":     round(len(strict) / total, 3),
                "recoverable_usable_rate": round(len(recover) / total, 3),
                "failure_rate":           round(1.0 - len(ok_runs) / total, 3),
                "latency_mean_s":         round(statistics.mean(latencies), 2) if latencies else None,
                "latency_stdev_s":        round(statistics.stdev(latencies), 2) if len(latencies) >= 2 else 0.0,
                "gen_tps_mean":           round(statistics.mean(gen_tps_vals), 2) if gen_tps_vals else None,
                "run_count":              total,
            }

    return model_role_fits


# ---------------------------------------------------------------------------
# Recommendation engine
# ---------------------------------------------------------------------------

def _best_for_role(
    role: str,
    model_role_fits: Dict[str, Dict[str, Any]],
    min_usable_rate: float = 0.5,
) -> Tuple[Optional[str], Optional[Dict[str, Any]]]:
    candidates = [
        (model, fits[role])
        for model, fits in model_role_fits.items()
        if fits.get(role) and fits[role]["usable_rate"] >= min_usable_rate
    ]
    if not candidates:
        return None, None
    best = max(candidates, key=lambda x: x[1]["rank_mean"])
    return best


def build_recommendations(model_role_fits: Dict[str, Dict[str, Any]]) -> Dict[str, Any]:
    """
    Produce a structured recommendation report answering Chiefly architecture decisions.
    """
    rec: Dict[str, Any] = {}

    for role in ROLE_SCENARIO_MAP:
        best_model, best_stats = _best_for_role(role, model_role_fits)
        rec[f"best_{role}"] = {"model": best_model, "stats": best_stats}

    # Strict JSON: strict_usable_rate >= 0.7
    strict_candidates = [
        (model, fits["json_controller"])
        for model, fits in model_role_fits.items()
        if fits.get("json_controller") and fits["json_controller"]["strict_usable_rate"] >= 0.7
    ]
    if strict_candidates:
        best = max(strict_candidates, key=lambda x: x[1]["rank_mean"])
        rec["best_strict_json_model"] = {"model": best[0], "stats": best[1]}
    else:
        rec["best_strict_json_model"] = {"model": None, "stats": None}

    # Recoverable JSON: works with sanitization but not strictly
    recover_candidates = [
        (model, fits["json_controller"])
        for model, fits in model_role_fits.items()
        if fits.get("json_controller")
        and fits["json_controller"]["recoverable_usable_rate"] >= 0.5
        and fits["json_controller"]["strict_usable_rate"] < 0.7
    ]
    if recover_candidates:
        best = max(recover_candidates, key=lambda x: x[1]["rank_mean"])
        rec["best_recoverable_json_model"] = {"model": best[0], "stats": best[1]}
    else:
        rec["best_recoverable_json_model"] = {"model": None, "stats": None}

    # Models that need sanitization to produce valid JSON
    rec["models_needing_sanitization"] = sorted({
        model
        for model, roles in model_role_fits.items()
        for role in ["json_controller", "project_router", "fast_classifier"]
        if roles.get(role)
        and roles[role]["strict_usable_rate"] < 0.5
        and roles[role]["recoverable_usable_rate"] >= 0.5
    })

    # Models not suitable for controller tasks
    rec["models_unsuitable_for_controller"] = sorted(
        model
        for model, roles in model_role_fits.items()
        if roles.get("json_controller") and roles["json_controller"]["usable_rate"] < 0.3
    )

    # Hybrid pipeline suggestions
    rewriter_model = (rec.get("best_rewriter") or {}).get("model")
    json_model     = (rec.get("best_json_controller") or {}).get("model")
    fast_model     = (rec.get("best_fast_classifier") or {}).get("model")
    long_model     = (rec.get("best_long_reasoner") or {}).get("model")

    pairings = []
    if rewriter_model and json_model and rewriter_model != json_model:
        pairings.append(
            f"{rewriter_model} for rewrite draft + {json_model} for JSON normalization"
        )
    if fast_model and long_model and fast_model != long_model:
        pairings.append(
            f"{fast_model} for fast triage + {long_model} for complex admin proposals"
        )
    if not pairings:
        pairings.append("No clear hybrid pairing found — run with more models for differentiation")
    rec["hybrid_pairings"] = pairings

    return rec


# ---------------------------------------------------------------------------
# Stability stats
# ---------------------------------------------------------------------------

def compute_stability_stats(results: List[RunResult]) -> Dict[str, Any]:
    """Per (model, scenario) stats over warm + concurrency=1 runs."""
    groups: Dict[Tuple[str, str], List[RunResult]] = {}
    for r in results:
        if r.run_kind == "warm" and r.concurrency == 1:
            groups.setdefault((r.model_resolved, r.scenario), []).append(r)

    stats: Dict[str, Any] = {}
    for (model, scenario), group in sorted(groups.items()):
        total    = len(group)
        ok_runs  = [r for r in group if r.ok]
        usable   = [r for r in group if r.gated_usable]
        strict   = [r for r in group if r.strict_usable]
        recover  = [r for r in group if r.recoverable_usable]
        fit_scores  = [r.chiefly_fit_score for r in ok_runs if r.chiefly_fit_score is not None]
        wall_times  = [r.wall_time_s for r in ok_runs if r.wall_time_s is not None]

        stats[f"{model}|{scenario}"] = {
            "model":    model,
            "scenario": scenario,
            "run_count":               total,
            "failure_rate":            round(1.0 - len(ok_runs) / total, 3) if total else 1.0,
            "usable_rate":             round(len(usable) / total, 3) if total else 0.0,
            "strict_usable_rate":      round(len(strict) / total, 3) if total else 0.0,
            "recoverable_usable_rate": round(len(recover) / total, 3) if total else 0.0,
            "fit_mean":    round(statistics.mean(fit_scores), 2) if fit_scores else None,
            "fit_median":  round(statistics.median(fit_scores), 2) if fit_scores else None,
            "fit_stdev":   round(statistics.stdev(fit_scores), 2) if len(fit_scores) >= 2 else 0.0,
            "fit_min":     round(min(fit_scores), 2) if fit_scores else None,
            "fit_max":     round(max(fit_scores), 2) if fit_scores else None,
            "latency_mean_s":  round(statistics.mean(wall_times), 2) if wall_times else None,
            "latency_stdev_s": round(statistics.stdev(wall_times), 2) if len(wall_times) >= 2 else 0.0,
        }
    return stats


# ---------------------------------------------------------------------------
# Measurement
# ---------------------------------------------------------------------------

def _make_error_result(
    requested_model: str,
    resolved_model: str,
    scenario: Scenario,
    concurrency: int,
    run_kind: str,
    requested_think_mode: str,
    effective_think_mode: str,
    think_mode_source: str,
    error: str,
    wall: float,
) -> RunResult:
    return RunResult(
        model_requested=requested_model, model_resolved=resolved_model,
        scenario=scenario.name, category=scenario.category,
        expectation=scenario.expectation, role_target=scenario.role_target,
        concurrency=concurrency, run_kind=run_kind, ok=False, error=error,
        requested_think_mode=requested_think_mode,
        effective_think_mode=effective_think_mode,
        think_mode_source=think_mode_source,
        total_duration_ns=None, load_duration_ns=None,
        prompt_eval_count=None, prompt_eval_duration_ns=None,
        eval_count=None, eval_duration_ns=None,
        wall_time_s=wall, prompt_tps=None, gen_tps=None,
        response_chars=None, response_text=None,
        raw_json_valid=None, sanitized_json_valid=None,
        sanitization_applied=None, sanitization_kind=None,
        schema_valid=None, semantic_valid=None,
        format_score=None, instruction_score=None, semantic_score=None,
        conciseness_score=None, actionability_score=None,
        latency_score=None, throughput_score=None,
        chiefly_fit_score=None, quality_score=None,
        gated_usable=None, strict_usable=None, recoverable_usable=None,
        scenario_rank_score=None, rejection_reason=None,
    )


def measure_one(
    base_url: str,
    requested_model: str,
    resolved_model: str,
    scenario: Scenario,
    concurrency: int,
    run_kind: str,
    timeout_s: int,
    requested_think_mode: str,
) -> RunResult:
    if requested_think_mode not in THINK_MODES:
        raise ValueError(f"Invalid think mode: {requested_think_mode}")

    effective_think_mode = requested_think_mode
    think_mode_source = "explicit" if requested_think_mode != "provider_default" else "provider_default"
    start = time.perf_counter()

    try:
        data = ollama_generate(
            base_url=base_url,
            model=resolved_model,
            prompt=scenario.prompt,
            timeout_s=timeout_s,
            think_mode=requested_think_mode,
        )
        wall = time.perf_counter() - start
        response_text = data.get("response", "")
        scores = score_run(response_text, scenario)

        return RunResult(
            model_requested=requested_model,
            model_resolved=resolved_model,
            scenario=scenario.name,
            category=scenario.category,
            expectation=scenario.expectation,
            role_target=scenario.role_target,
            concurrency=concurrency,
            run_kind=run_kind,
            ok=True,
            error=None,
            requested_think_mode=requested_think_mode,
            effective_think_mode=effective_think_mode,
            think_mode_source=think_mode_source,
            total_duration_ns=data.get("total_duration"),
            load_duration_ns=data.get("load_duration"),
            prompt_eval_count=data.get("prompt_eval_count"),
            prompt_eval_duration_ns=data.get("prompt_eval_duration"),
            eval_count=data.get("eval_count"),
            eval_duration_ns=data.get("eval_duration"),
            wall_time_s=wall,
            prompt_tps=compute_prompt_tps(
                data.get("prompt_eval_count"), data.get("prompt_eval_duration")
            ),
            gen_tps=compute_gen_tps(data.get("eval_count"), data.get("eval_duration")),
            response_chars=len(response_text),
            response_text=response_text,
            raw_json_valid=scores["raw_json_valid"],
            sanitized_json_valid=scores["sanitized_json_valid"],
            sanitization_applied=scores["sanitization_applied"],
            sanitization_kind=scores["sanitization_kind"],
            schema_valid=scores["schema_valid"],
            semantic_valid=scores["semantic_valid"],
            format_score=scores["format_score"],
            instruction_score=scores["instruction_score"],
            semantic_score=scores["semantic_score"],
            conciseness_score=scores["conciseness_score"],
            actionability_score=scores["actionability_score"],
            latency_score=None,    # computed in finalize_results
            throughput_score=None, # computed in finalize_results
            chiefly_fit_score=scores["chiefly_fit_score"],
            quality_score=scores["quality_score"],
            gated_usable=None,
            strict_usable=None,
            recoverable_usable=None,
            scenario_rank_score=None,
            rejection_reason=None,
        )
    except urllib.error.HTTPError as e:
        return _make_error_result(
            requested_model, resolved_model, scenario, concurrency, run_kind,
            requested_think_mode, effective_think_mode, think_mode_source,
            f"HTTPError {e.code}: {e.reason}", time.perf_counter() - start,
        )
    except Exception as e:
        return _make_error_result(
            requested_model, resolved_model, scenario, concurrency, run_kind,
            requested_think_mode, effective_think_mode, think_mode_source,
            str(e), time.perf_counter() - start,
        )


async def measure_one_async(
    base_url: str,
    requested_model: str,
    resolved_model: str,
    scenario: Scenario,
    concurrency: int,
    run_kind: str,
    timeout_s: int,
    requested_think_mode: str,
) -> RunResult:
    return await asyncio.to_thread(
        measure_one,
        base_url, requested_model, resolved_model,
        scenario, concurrency, run_kind, timeout_s, requested_think_mode,
    )


# ---------------------------------------------------------------------------
# Output writers
# ---------------------------------------------------------------------------

def get_system_info() -> Dict[str, Any]:
    return {
        "platform":  platform.platform(),
        "python":    sys.version,
        "machine":   platform.machine(),
        "processor": platform.processor(),
        "hostname":  platform.node(),
        "cpu_count": os.cpu_count(),
    }


def write_json_output(
    path: str,
    results: List[RunResult],
    meta: Dict[str, Any],
    stability_stats: Dict[str, Any],
    role_fits: Dict[str, Any],
    recommendations: Dict[str, Any],
) -> None:
    payload = {
        "generated_at_unix": int(time.time()),
        "meta":              meta,
        "results":           [asdict(r) for r in results],
        "stability_stats":   stability_stats,
        "role_fits":         role_fits,
        "recommendations":   recommendations,
    }
    with open(path, "w", encoding="utf-8") as f:
        json.dump(payload, f, indent=2)


def write_csv_output(path: str, results: List[RunResult]) -> None:
    if not results:
        return
    fieldnames = list(asdict(results[0]).keys())
    with open(path, "w", encoding="utf-8", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        for r in results:
            writer.writerow(asdict(r))


def write_role_report_json(
    path: str,
    role_fits: Dict[str, Any],
    recommendations: Dict[str, Any],
) -> None:
    with open(path, "w", encoding="utf-8") as f:
        json.dump(
            {"generated_at_unix": int(time.time()),
             "role_fits": role_fits, "recommendations": recommendations},
            f, indent=2,
        )


def write_summary_json(
    path: str,
    stability_stats: Dict[str, Any],
    meta: Dict[str, Any],
) -> None:
    with open(path, "w", encoding="utf-8") as f:
        json.dump(
            {"generated_at_unix": int(time.time()),
             "meta": meta, "stability_stats": stability_stats},
            f, indent=2,
        )


# ---------------------------------------------------------------------------
# Print reports
# ---------------------------------------------------------------------------

def _print_run_line(label: str, r: RunResult) -> None:
    if r.expectation == "json":
        extra = (
            f" raw={r.raw_json_valid} san={r.sanitized_json_valid}"
            f" schema={r.schema_valid} fit={fmt(r.chiefly_fit_score)}"
        )
    else:
        extra = f" fit={fmt(r.chiefly_fit_score)}"
    print(f"    {label}: ok={r.ok} wall={fmt(r.wall_time_s)}s gen_tps={fmt(r.gen_tps)}{extra}")


def print_summary(results: List[RunResult]) -> None:
    print_section("Summary by model + scenario + run_kind")
    groups: Dict[str, List[RunResult]] = {}
    for r in results:
        key = f"{r.model_resolved}|{r.scenario}|{r.run_kind}|{r.effective_think_mode}|c{r.concurrency}"
        groups.setdefault(key, []).append(r)

    rows: List[List[str]] = [[
        "model", "scenario", "run_kind", "think", "conc",
        "ok/total", "usable/total", "wall_mean_s", "gen_tps", "fit_mean", "rank_mean",
    ]]
    for key, group in sorted(groups.items()):
        model, scenario, run_kind, think, conc = key.split("|")
        ok_g     = [r for r in group if r.ok]
        usable_g = [r for r in group if r.gated_usable]
        rows.append([
            model, scenario, run_kind, think, conc.replace("c", ""),
            f"{len(ok_g)}/{len(group)}",
            f"{len(usable_g)}/{len(group)}",
            fmt(summarize_numeric([r.wall_time_s for r in ok_g])["mean"]),
            fmt(summarize_numeric([r.gen_tps for r in ok_g])["mean"]),
            fmt(summarize_numeric([r.chiefly_fit_score for r in ok_g])["mean"]),
            fmt(summarize_numeric([r.scenario_rank_score for r in usable_g])["mean"]),
        ])
    table(rows)


def print_stability_table(stability_stats: Dict[str, Any]) -> None:
    print_section("Stability stats (warm runs, concurrency=1)")
    rows: List[List[str]] = [[
        "model", "scenario", "runs", "fail%", "usable%", "strict%",
        "fit_mean", "fit_stdev", "lat_mean_s", "lat_stdev_s",
    ]]
    for key in sorted(stability_stats.keys()):
        s = stability_stats[key]
        rows.append([
            s["model"], s["scenario"], str(s["run_count"]),
            f"{s['failure_rate']*100:.0f}",
            f"{s['usable_rate']*100:.0f}",
            f"{s['strict_usable_rate']*100:.0f}",
            fmt(s["fit_mean"]),
            fmt(s["fit_stdev"]),
            fmt(s["latency_mean_s"]),
            fmt(s["latency_stdev_s"]),
        ])
    table(rows)


def print_role_report(role_fits: Dict[str, Any], recommendations: Dict[str, Any]) -> None:
    print_section("Role Fit Report  (fit_mean / usable%)")
    roles = list(ROLE_SCENARIO_MAP.keys())
    rows: List[List[str]] = [["model"] + roles]
    for model in sorted(role_fits.keys()):
        row = [model]
        for role in roles:
            stats = role_fits[model].get(role)
            if stats is None:
                row.append("-")
            else:
                row.append(f"{stats['fit_mean']:.0f}/{stats['usable_rate']*100:.0f}%")
        rows.append(row)
    table(rows)
    print("  Format: fit_mean / usable_rate%")

    print_section("Chiefly Architecture Recommendations")
    role_labels = {
        "best_fast_classifier":        "Fast Classifier",
        "best_project_router":         "Project Router",
        "best_translator":             "Translator",
        "best_rewriter":               "Rewriter",
        "best_short_summarizer":       "Short Summarizer",
        "best_json_controller":        "JSON Controller",
        "best_bulk_text_worker":       "Bulk Text Worker",
        "best_long_reasoner":          "Long-form Analyst",
        "best_strict_json_model":      "Strict JSON (no sanitization needed)",
        "best_recoverable_json_model": "Recoverable JSON (sanitization acceptable)",
    }
    for key, label in role_labels.items():
        info = recommendations.get(key) or {}
        model = info.get("model")
        if model:
            stats = info.get("stats") or {}
            lat  = stats.get("latency_mean_s")
            rank = stats.get("rank_mean")
            suffix = (
                f"  (rank={rank:.1f}, latency={lat:.1f}s)"
                if rank is not None and lat is not None else ""
            )
            print(f"  {label}: {model}{suffix}")
        else:
            print(f"  {label}: — (no qualifying model)")

    flagged = recommendations.get("models_needing_sanitization", [])
    if flagged:
        print(f"\n  Acceptable only WITH JSON sanitization: {', '.join(flagged)}")

    unsuitable = recommendations.get("models_unsuitable_for_controller", [])
    if unsuitable:
        print(f"  NOT suitable for JSON controller role: {', '.join(unsuitable)}")

    pairings = recommendations.get("hybrid_pairings", [])
    if pairings:
        print("\n  Hybrid pipeline suggestions:")
        for p in pairings:
            print(f"    - {p}")


# ---------------------------------------------------------------------------
# Benchmark runner
# ---------------------------------------------------------------------------

async def run_benchmarks(args: argparse.Namespace) -> int:
    print_section("System info")
    print(json.dumps(get_system_info(), indent=2))

    print_section("Installed Ollama models")
    try:
        installed = ollama_tags(args.base_url, args.timeout_seconds)
        for m in installed:
            print(f"  - {m}")
    except Exception as e:
        print(f"Could not list models: {e}")
        return 2

    think_modes = [m.strip() for m in args.think_modes]
    for tm in think_modes:
        if tm not in THINK_MODES:
            print(f"Invalid think mode: {tm}. Allowed: {sorted(THINK_MODES)}")
            return 2

    print_section("Model resolution")
    resolved_models: List[Tuple[str, str]] = []
    for requested in args.models:
        try:
            resolved, suggestions, pulled = resolve_model_name(
                base_url=args.base_url,
                requested=requested,
                timeout_s=args.timeout_seconds,
                auto_pull=args.auto_pull,
                resolve_closest=args.resolve_closest,
            )
            print(f"  {requested} -> {resolved}")
            for s in suggestions:
                print(f"    note: {s}")
            if pulled:
                print("    note: model was pulled")
            resolved_models.append((requested, resolved))
        except Exception as e:
            print(f"  {requested} -> ERROR: {e}")
            if args.skip_unresolved:
                continue
            return 3

    if not resolved_models:
        print("No models resolved.")
        return 4

    scenarios = SCENARIO_SETS.get(args.scenario_set, CHIEFLY_SCENARIOS)

    print_section("Benchmark configuration")
    print(f"  Base URL:          {args.base_url}")
    print(f"  Scenario set:      {args.scenario_set} ({len(scenarios)} scenarios)")
    print(f"  Repeats:           {args.repeats}")
    print(f"  Concurrency:       {args.concurrency}")
    print(f"  Think modes:       {think_modes}")
    print(f"  Strict JSON mode:  {args.strict_json_mode}")
    print(f"  Quality threshold: {args.json_quality_threshold}")
    print(f"  Timeout:           {args.timeout_seconds}s")
    print(f"  JSON output:       {args.json_output}")
    print(f"  CSV output:        {args.csv_output}")

    all_results: List[RunResult] = []

    for requested_model, resolved_model in resolved_models:
        for think_mode in think_modes:
            print_section(f"Model: {resolved_model} | think: {think_mode}")

            if args.force_unload:
                ollama_unload(args.base_url, resolved_model, args.timeout_seconds)

            for scenario in scenarios:
                print(
                    f"\n  Scenario: {scenario.name} [{scenario.category}]"
                    f" -> {scenario.role_target}"
                )

                if args.force_unload:
                    ollama_unload(args.base_url, resolved_model, args.timeout_seconds)

                # Cold run — measures model load time
                cold = measure_one(
                    base_url=args.base_url,
                    requested_model=requested_model,
                    resolved_model=resolved_model,
                    scenario=scenario,
                    concurrency=1,
                    run_kind="cold",
                    timeout_s=args.timeout_seconds,
                    requested_think_mode=think_mode,
                )
                all_results.append(cold)
                _print_run_line("cold", cold)

                # Warm runs (--repeats)
                for i in range(args.repeats):
                    warm = measure_one(
                        base_url=args.base_url,
                        requested_model=requested_model,
                        resolved_model=resolved_model,
                        scenario=scenario,
                        concurrency=1,
                        run_kind="warm",
                        timeout_s=args.timeout_seconds,
                        requested_think_mode=think_mode,
                    )
                    all_results.append(warm)
                    _print_run_line(f"warm#{i+1}", warm)

                # Concurrent runs
                for c in args.concurrency:
                    if c <= 1:
                        continue
                    tasks = [
                        measure_one_async(
                            base_url=args.base_url,
                            requested_model=requested_model,
                            resolved_model=resolved_model,
                            scenario=scenario,
                            concurrency=c,
                            run_kind="concurrent",
                            timeout_s=args.timeout_seconds,
                            requested_think_mode=think_mode,
                        )
                        for _ in range(c)
                    ]
                    concurrent_results = await asyncio.gather(*tasks)
                    all_results.extend(concurrent_results)
                    ok_n   = sum(1 for r in concurrent_results if r.ok)
                    wall_s = summarize_numeric([r.wall_time_s for r in concurrent_results if r.ok])
                    print(f"    concurrent x{c}: ok={ok_n}/{c} wall_mean={fmt(wall_s['mean'])}s")

    # Post-processing: gate, latency/throughput, rank scores
    finalize_results(all_results, args.strict_json_mode, args.json_quality_threshold)

    stability_stats  = compute_stability_stats(all_results)
    role_fits        = compute_role_fits(all_results)
    recommendations  = build_recommendations(role_fits)

    meta = {
        "system":               get_system_info(),
        "base_url":             args.base_url,
        "scenario_set":         args.scenario_set,
        "scenarios":            [sc.name for sc in scenarios],
        "models_requested":     args.models,
        "models_resolved":      [{"requested": r, "resolved": m} for r, m in resolved_models],
        "think_modes":          think_modes,
        "repeats":              args.repeats,
        "concurrency":          args.concurrency,
        "timeout_seconds":      args.timeout_seconds,
        "strict_json_mode":     args.strict_json_mode,
        "json_quality_threshold": args.json_quality_threshold,
        "force_unload":         args.force_unload,
        "auto_pull":            args.auto_pull,
    }

    write_json_output(args.json_output, all_results, meta, stability_stats, role_fits, recommendations)
    write_csv_output(args.csv_output, all_results)

    if args.export_summary_json:
        write_summary_json(args.export_summary_json, stability_stats, meta)
        print(f"\nSummary JSON: {args.export_summary_json}")

    if args.export_role_report_json:
        write_role_report_json(args.export_role_report_json, role_fits, recommendations)
        print(f"Role report JSON: {args.export_role_report_json}")

    print_summary(all_results)
    print_stability_table(stability_stats)

    if args.role_report:
        print_role_report(role_fits, recommendations)

    print_section("Files written")
    print(f"  {args.json_output}")
    print(f"  {args.csv_output}")

    return 0


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Chiefly-aware Ollama benchmark harness — "
            "role-based scoring and architecture recommendations."
        )
    )
    parser.add_argument(
        "--base-url", default=DEFAULT_BASE_URL,
        help=f"Ollama base URL (default: {DEFAULT_BASE_URL})",
    )
    parser.add_argument(
        "--models", nargs="+", required=True,
        help="Model tags to benchmark, e.g. qwen2.5:7b gemma3:4b llama3.2:3b",
    )
    parser.add_argument(
        "--think-modes", nargs="+", default=["provider_default"],
        help="One or more of: thinking nothinking provider_default",
    )
    parser.add_argument(
        "--scenario-set", default="chiefly",
        choices=["chiefly", "generic", "minimal", "all"],
        help="Scenario set to run (default: chiefly)",
    )
    parser.add_argument(
        "--strict-json-mode", default="recoverable",
        choices=["raw", "recoverable", "both"],
        help=(
            "JSON usability gating: raw=strict parse only, "
            "recoverable=allow sanitization (default: recoverable)"
        ),
    )
    parser.add_argument(
        "--repeats", type=int, default=3,
        help="Warm runs per scenario per model (default: 3)",
    )
    parser.add_argument(
        "--concurrency", nargs="+", type=int, default=[1, 4],
        help="Concurrency levels to test (default: 1 4)",
    )
    parser.add_argument(
        "--timeout-seconds", type=int, default=900,
        help="HTTP timeout per request in seconds (default: 900)",
    )
    parser.add_argument(
        "--json-quality-threshold", type=float, default=20.0,
        help="Minimum chiefly_fit_score for usability gating (default: 20.0)",
    )
    parser.add_argument(
        "--role-report", action="store_true",
        help="Print role-based recommendation report at the end",
    )
    parser.add_argument(
        "--json-output", default="ollama_bench_chiefly.json",
        help="Path to full results JSON (default: ollama_bench_chiefly.json)",
    )
    parser.add_argument(
        "--csv-output", default="ollama_bench_chiefly.csv",
        help="Path to results CSV (default: ollama_bench_chiefly.csv)",
    )
    parser.add_argument(
        "--export-summary-json", default=None,
        help="Optional path to write condensed stability summary JSON",
    )
    parser.add_argument(
        "--export-role-report-json", default=None,
        help="Optional path to write role recommendation JSON",
    )
    parser.add_argument(
        "--force-unload", action="store_true",
        help="Unload model before cold runs via keep_alive=0",
    )
    parser.add_argument(
        "--auto-pull", action="store_true",
        help="Automatically pull missing models",
    )
    parser.add_argument(
        "--resolve-closest", action="store_true",
        help="Resolve to closest installed model if exact tag not found",
    )
    parser.add_argument(
        "--skip-unresolved", action="store_true",
        help="Skip unresolvable models instead of aborting",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        return asyncio.run(run_benchmarks(args))
    except KeyboardInterrupt:
        print("\nInterrupted.", file=sys.stderr)
        return 130


if __name__ == "__main__":
    raise SystemExit(main())
