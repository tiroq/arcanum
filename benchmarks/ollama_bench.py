#!/usr/bin/env python3

import argparse
import asyncio
import csv
import difflib
import json
import math
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

SHORT_PROMPT = "Summarize why idempotent job orchestration matters in two short paragraphs."

MEDIUM_PROMPT = """
You are evaluating a local autonomous agent platform.

Explain the difference between:
1. task ingestion
2. change detection
3. durable queueing
4. worker execution
5. writeback
6. rollback metadata

Return a structured answer with clear section headings.
""".strip()

LONG_PROMPT = """
You are a systems architect designing a self-hosted autonomous agent platform.

Requirements:
- decouple sync from processing
- support append-only history
- use durable jobs
- support local LLMs via Ollama
- keep processor logic separate from provider logic
- support retries and failure visibility
- preserve source-of-truth semantics for upstream systems
- support prompt versioning
- support planner, reviewer, and fast-task model roles
- support auditability and future rollback

Write a detailed technical explanation of:
- why sync and processing must be separated
- why agent outputs should be persisted as structured records
- why provider abstraction matters
- why prompt file versioning matters
- why role-based model selection improves system behavior
- what to observe in benchmarking local inference hardware

Also include:
- 5 risks
- 5 mitigations
- 5 operational metrics
""".strip()

JSON_AGENT_PROMPT = """
Return ONLY valid JSON.

Task:
Refactor NATS retry scheduler for a multi-agent local platform.

Required JSON schema:
{
  "title": "string",
  "priority": "low|medium|high|critical",
  "steps": ["string"],
  "risks": ["string"],
  "review_model_role": "string"
}
""".strip()

SCENARIOS = [
    {
        "name": "short",
        "prompt": SHORT_PROMPT,
        "expectation": "text",
    },
    {
        "name": "medium",
        "prompt": MEDIUM_PROMPT,
        "expectation": "text",
    },
    {
        "name": "long",
        "prompt": LONG_PROMPT,
        "expectation": "text",
    },
    {
        "name": "json_agent",
        "prompt": JSON_AGENT_PROMPT,
        "expectation": "json_agent",
    },
]

THINK_MODES = {"thinking", "nothinking", "provider_default"}


@dataclass
class RunResult:
    model_requested: str
    model_resolved: str
    scenario: str
    expectation: str
    concurrency: int
    run_kind: str
    ok: bool
    error: Optional[str]

    requested_think_mode: str
    effective_think_mode: str
    think_mode_source: str

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

    json_valid: Optional[bool]
    schema_score: Optional[float]
    usefulness_score: Optional[float]
    quality_score: Optional[float]

    gated_usable: Optional[bool]
    scenario_rank_score: Optional[float]
    rejection_reason: Optional[str]


def ns_to_s(value: Optional[int]) -> Optional[float]:
    if value is None:
        return None
    return value / 1_000_000_000.0


def compute_prompt_tps(prompt_eval_count: Optional[int], prompt_eval_duration_ns: Optional[int]) -> Optional[float]:
    if prompt_eval_count is None or prompt_eval_duration_ns in (None, 0):
        return None
    return prompt_eval_count / (prompt_eval_duration_ns / 1_000_000_000.0)


def compute_gen_tps(eval_count: Optional[int], eval_duration_ns: Optional[int]) -> Optional[float]:
    if eval_count is None or eval_duration_ns in (None, 0):
        return None
    return eval_count / (eval_duration_ns / 1_000_000_000.0)


def summarize_numeric(values: List[Optional[float]]) -> Dict[str, Optional[float]]:
    cleaned = [v for v in values if v is not None]
    if not cleaned:
        return {"mean": None, "median": None, "min": None, "max": None}
    return {
        "mean": statistics.mean(cleaned),
        "median": statistics.median(cleaned),
        "min": min(cleaned),
        "max": max(cleaned),
    }


def fmt(value: Optional[float], digits: int = 2) -> str:
    if value is None:
        return "-"
    return f"{value:.{digits}f}"


def format_seconds_from_ns(ns: Optional[int]) -> str:
    if ns is None:
        return "-"
    return fmt(ns / 1_000_000_000.0, 2)


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
        line = " | ".join(str(cell).ljust(widths[i]) for i, cell in enumerate(row))
        print(line)
        if idx == 0:
            print("-+-".join("-" * w for w in widths))


def http_get_json(url: str, timeout_s: int) -> Any:
    req = urllib.request.Request(url, method="GET")
    with urllib.request.urlopen(req, timeout=timeout_s) as resp:
        return json.loads(resp.read().decode("utf-8"))


def http_post_json(url: str, payload: Dict[str, Any], timeout_s: int) -> Dict[str, Any]:
    body = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=body,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=timeout_s) as resp:
        return json.loads(resp.read().decode("utf-8"))


def ollama_tags(base_url: str, timeout_s: int) -> List[str]:
    data = http_get_json(f"{base_url.rstrip('/')}/api/tags", timeout_s)
    models = data.get("models", [])
    names = []
    for m in models:
        name = m.get("name")
        if name:
            names.append(name)
    return names


def ollama_generate(
    base_url: str,
    model: str,
    prompt: str,
    timeout_s: int,
    think_mode: str,
) -> Dict[str, Any]:
    payload: Dict[str, Any] = {
        "model": model,
        "prompt": prompt,
        "stream": False,
    }

    # provider_default means: do not send "think" at all
    if think_mode == "thinking":
        payload["think"] = True
    elif think_mode == "nothinking":
        payload["think"] = False
    elif think_mode == "provider_default":
        pass
    else:
        raise ValueError(f"Unsupported think_mode: {think_mode}")

    return http_post_json(
        f"{base_url.rstrip('/')}/api/generate",
        payload,
        timeout_s=timeout_s,
    )


def ollama_unload(base_url: str, model: str, timeout_s: int) -> None:
    try:
        http_post_json(
            f"{base_url.rstrip('/')}/api/generate",
            {
                "model": model,
                "prompt": "",
                "stream": False,
                "keep_alive": 0,
            },
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
        encoded = urllib.parse.quote(query)
        url = f"https://ollama.com/search?q={encoded}"
        html = urllib.request.urlopen(url, timeout=timeout_s).read().decode("utf-8", errors="ignore")
        found = re.findall(r'href="/library/([^"/?]+)', html)
        for item in found:
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

    out = []
    seen = set()
    for g in guesses:
        if g and g not in seen:
            out.append(g)
            seen.add(g)
    return out


def pick_closest_model(requested: str, installed: List[str], remote_hints: List[str]) -> Tuple[Optional[str], List[str]]:
    candidates = list(dict.fromkeys(installed + remote_hints))
    if not candidates:
        return None, []

    req = normalize_model_hint(requested)
    family = req.split(":")[0]
    family_matches = [c for c in candidates if c.lower().startswith(family)]
    if family_matches:
        close = difflib.get_close_matches(req, family_matches, n=5, cutoff=0.0)
        if close:
            return close[0], close
        return family_matches[0], family_matches[:5]

    close = difflib.get_close_matches(req, candidates, n=5, cutoff=0.0)
    if close:
        return close[0], close

    return None, []


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
            return g, [f"resolved installed alias from {requested} -> {g}"], False

    remote_hints = search_ollama_library(requested)
    best, suggestions = pick_closest_model(requested, installed, remote_hints)

    if auto_pull:
        print(f"Model '{requested}' is not installed. Attempting pull...")
        if ollama_pull(base_url, requested, timeout_s):
            return requested, [f"pulled requested model {requested}"], True

        for g in guesses:
            if g == requested:
                continue
            print(f"Pull failed for '{requested}'. Trying guessed model '{g}'...")
            if ollama_pull(base_url, g, timeout_s):
                return g, [f"requested model not found; pulled guessed model {g}"], True

        if best:
            print(f"Trying suggested model '{best}'...")
            if ollama_pull(base_url, best, timeout_s):
                return best, suggestions, True

    if resolve_closest and best:
        return best, suggestions, False

    suggestion_msgs = suggestions or [f"no exact installed match for {requested}"]
    raise ValueError(f"Model '{requested}' not found locally. Suggestions: {', '.join(suggestion_msgs)}")


def try_parse_json(text: str) -> Tuple[bool, Optional[Any]]:
    try:
        parsed = json.loads(text)
        return True, parsed
    except Exception:
        return False, None


def score_json_agent_output(text: str) -> Tuple[bool, float, float, float, List[str]]:
    json_valid, parsed = try_parse_json(text)
    notes: List[str] = []

    if not json_valid:
        return False, 0.0, 0.0, 0.0, ["invalid JSON"]

    schema_score = 0.0
    usefulness_score = 0.0

    if not isinstance(parsed, dict):
        return True, 10.0, 5.0, 8.0, ["top-level JSON is not an object"]

    required_keys = ["title", "priority", "steps", "risks", "review_model_role"]
    present = sum(1 for k in required_keys if k in parsed)
    schema_score += (present / len(required_keys)) * 50.0

    title = parsed.get("title")
    if isinstance(title, str) and title.strip():
        schema_score += 10.0
        if len(title.strip()) >= 8:
            usefulness_score += 5.0
    else:
        notes.append("title invalid")

    priority = parsed.get("priority")
    if priority in {"low", "medium", "high", "critical"}:
        schema_score += 10.0
        usefulness_score += 5.0
    else:
        notes.append("priority invalid")

    steps = parsed.get("steps")
    if isinstance(steps, list):
        schema_score += 10.0
        non_empty_steps = [s for s in steps if isinstance(s, str) and s.strip()]
        if len(non_empty_steps) >= 3:
            usefulness_score += 15.0
        elif len(non_empty_steps) >= 1:
            usefulness_score += 8.0
        avg_step_len = statistics.mean([len(s.strip()) for s in non_empty_steps]) if non_empty_steps else 0
        if avg_step_len >= 12:
            usefulness_score += 5.0
        if not non_empty_steps:
            notes.append("steps empty")
    else:
        notes.append("steps invalid")

    risks = parsed.get("risks")
    if isinstance(risks, list):
        schema_score += 10.0
        non_empty_risks = [r for r in risks if isinstance(r, str) and r.strip()]
        if len(non_empty_risks) >= 2:
            usefulness_score += 10.0
        elif len(non_empty_risks) >= 1:
            usefulness_score += 5.0
        if not non_empty_risks:
            notes.append("risks empty")
    else:
        notes.append("risks invalid")

    review_model_role = parsed.get("review_model_role")
    if isinstance(review_model_role, str) and review_model_role.strip():
        schema_score += 10.0
        usefulness_score += 5.0
    else:
        notes.append("review_model_role invalid")

    combined_text = " ".join([
        title if isinstance(title, str) else "",
        " ".join(steps) if isinstance(steps, list) else "",
        " ".join(risks) if isinstance(risks, list) else "",
    ]).strip().lower()

    if len(combined_text) < 40:
        usefulness_score -= 10.0
        notes.append("combined content too short")

    vague_markers = ["todo", "tbd", "n/a", "none", "unknown"]
    if any(v in combined_text for v in vague_markers):
        usefulness_score -= 8.0
        notes.append("contains vague markers")

    usefulness_score = max(0.0, min(100.0, usefulness_score))
    quality_score = 0.6 * schema_score + 0.4 * usefulness_score
    return True, schema_score, usefulness_score, quality_score, notes


def score_text_output(text: str) -> Tuple[float, float, float, List[str]]:
    notes: List[str] = []
    stripped = text.strip()
    if not stripped:
        return 0.0, 0.0, 0.0, ["empty text output"]

    words = re.findall(r"\S+", stripped)
    headings = len(re.findall(r"(^|\n)(#+\s|[A-Z][A-Za-z0-9 _-]{2,}:)", stripped))
    bullets = len(re.findall(r"(^|\n)\s*[-*]\s+", stripped))

    schema_score = 0.0
    usefulness_score = 0.0

    if len(words) >= 40:
        schema_score += 30.0
    elif len(words) >= 20:
        schema_score += 15.0
    else:
        schema_score += 5.0
        notes.append("too short")

    if headings >= 2:
        schema_score += 20.0
    elif headings >= 1:
        schema_score += 10.0

    if bullets >= 3:
        usefulness_score += 15.0
    elif bullets >= 1:
        usefulness_score += 8.0

    keywords = [
        "idempot", "queue", "worker", "writeback", "rollback",
        "provider", "prompt", "audit", "retry", "history"
    ]
    matched = sum(1 for k in keywords if k in stripped.lower())
    usefulness_score += min(30.0, matched * 3.0)

    if len(words) >= 120:
        usefulness_score += 15.0
    elif len(words) >= 60:
        usefulness_score += 8.0

    low_info_markers = ["i don't know", "cannot", "unknown", "n/a"]
    if any(m in stripped.lower() for m in low_info_markers):
        usefulness_score -= 10.0
        notes.append("contains low-information markers")

    schema_score = max(0.0, min(100.0, schema_score))
    usefulness_score = max(0.0, min(100.0, usefulness_score))
    quality_score = 0.4 * schema_score + 0.6 * usefulness_score
    return schema_score, usefulness_score, quality_score, notes


def normalize_score_higher_better(value: Optional[float], best: float) -> float:
    if value is None or best <= 0:
        return 0.0
    return max(0.0, min(1.0, value / best))


def normalize_score_lower_better(value: Optional[float], best: float) -> float:
    if value is None or value <= 0 or best <= 0:
        return 0.0
    return max(0.0, min(1.0, best / value))


def evaluate_gate(expectation: str, result: RunResult, json_quality_threshold: float) -> Tuple[bool, Optional[str]]:
    if not result.ok:
        return False, "request failed"

    if expectation == "json_agent":
        if result.json_valid is not True:
            return False, "invalid json"
        if result.quality_score is None:
            return False, "missing quality score"
        if result.quality_score < json_quality_threshold:
            return False, f"json quality below threshold {json_quality_threshold}"
        return True, None

    if result.quality_score is None or result.quality_score <= 0:
        return False, "empty or unusable text output"

    return True, None


def assign_scenario_scores(results: List[RunResult], json_quality_threshold: float) -> None:
    # Evaluate gating first
    for r in results:
        usable, reason = evaluate_gate(r.expectation, r, json_quality_threshold)
        r.gated_usable = usable
        r.rejection_reason = reason

    # Group by scenario + run_kind + concurrency + think_mode
    groups: Dict[Tuple[str, str, int, str], List[RunResult]] = {}
    for r in results:
        key = (r.scenario, r.run_kind, r.concurrency, r.effective_think_mode)
        groups.setdefault(key, []).append(r)

    for (_, _, _, _), group in groups.items():
        ok_usable = [r for r in group if r.ok and r.gated_usable]
        if not ok_usable:
            for r in group:
                r.scenario_rank_score = 0.0
            continue

        best_wall = min(r.wall_time_s for r in ok_usable if r.wall_time_s is not None)
        best_prompt = max((r.prompt_tps or 0.0) for r in ok_usable)
        best_gen = max((r.gen_tps or 0.0) for r in ok_usable)
        best_quality = max((r.quality_score or 0.0) for r in ok_usable)

        for r in group:
            if not r.ok or not r.gated_usable:
                r.scenario_rank_score = 0.0
                continue

            wall_norm = normalize_score_lower_better(r.wall_time_s, best_wall)
            prompt_norm = normalize_score_higher_better(r.prompt_tps, best_prompt)
            gen_norm = normalize_score_higher_better(r.gen_tps, best_gen)
            quality_norm = normalize_score_higher_better(r.quality_score, best_quality)

            if r.expectation == "json_agent":
                # Structured tasks: quality first, speed second
                score = (
                    0.70 * quality_norm +
                    0.20 * wall_norm +
                    0.10 * gen_norm
                ) * 100.0
            else:
                # Text tasks: blended score
                score = (
                    0.35 * quality_norm +
                    0.30 * wall_norm +
                    0.20 * gen_norm +
                    0.15 * prompt_norm
                ) * 100.0

            r.scenario_rank_score = round(score, 2)


def measure_one(
    base_url: str,
    requested_model: str,
    resolved_model: str,
    prompt: str,
    scenario: str,
    expectation: str,
    concurrency: int,
    run_kind: str,
    timeout_s: int,
    requested_think_mode: str,
) -> RunResult:
    start = time.perf_counter()

    if requested_think_mode not in THINK_MODES:
        raise ValueError(f"Invalid think mode: {requested_think_mode}")

    effective_think_mode = requested_think_mode
    think_mode_source = "explicit" if requested_think_mode != "provider_default" else "provider_default"

    try:
        data = ollama_generate(
            base_url=base_url,
            model=resolved_model,
            prompt=prompt,
            timeout_s=timeout_s,
            think_mode=requested_think_mode,
        )
        end = time.perf_counter()

        total_duration_ns = data.get("total_duration")
        load_duration_ns = data.get("load_duration")
        prompt_eval_count = data.get("prompt_eval_count")
        prompt_eval_duration_ns = data.get("prompt_eval_duration")
        eval_count = data.get("eval_count")
        eval_duration_ns = data.get("eval_duration")
        response_text = data.get("response", "")

        json_valid = None
        schema_score = None
        usefulness_score = None
        quality_score = None

        if expectation == "json_agent":
            json_valid, schema_score, usefulness_score, quality_score, _notes = score_json_agent_output(response_text)
        else:
            schema_score, usefulness_score, quality_score, _notes = score_text_output(response_text)

        return RunResult(
            model_requested=requested_model,
            model_resolved=resolved_model,
            scenario=scenario,
            expectation=expectation,
            concurrency=concurrency,
            run_kind=run_kind,
            ok=True,
            error=None,
            requested_think_mode=requested_think_mode,
            effective_think_mode=effective_think_mode,
            think_mode_source=think_mode_source,
            total_duration_ns=total_duration_ns,
            load_duration_ns=load_duration_ns,
            prompt_eval_count=prompt_eval_count,
            prompt_eval_duration_ns=prompt_eval_duration_ns,
            eval_count=eval_count,
            eval_duration_ns=eval_duration_ns,
            wall_time_s=end - start,
            prompt_tps=compute_prompt_tps(prompt_eval_count, prompt_eval_duration_ns),
            gen_tps=compute_gen_tps(eval_count, eval_duration_ns),
            response_chars=len(response_text),
            response_text=response_text,
            json_valid=json_valid,
            schema_score=schema_score,
            usefulness_score=usefulness_score,
            quality_score=quality_score,
            gated_usable=None,
            scenario_rank_score=None,
            rejection_reason=None,
        )
    except urllib.error.HTTPError as e:
        end = time.perf_counter()
        return RunResult(
            model_requested=requested_model,
            model_resolved=resolved_model,
            scenario=scenario,
            expectation=expectation,
            concurrency=concurrency,
            run_kind=run_kind,
            ok=False,
            error=f"HTTPError {e.code}: {e.reason}",
            requested_think_mode=requested_think_mode,
            effective_think_mode=effective_think_mode,
            think_mode_source=think_mode_source,
            total_duration_ns=None,
            load_duration_ns=None,
            prompt_eval_count=None,
            prompt_eval_duration_ns=None,
            eval_count=None,
            eval_duration_ns=None,
            wall_time_s=end - start,
            prompt_tps=None,
            gen_tps=None,
            response_chars=None,
            response_text=None,
            json_valid=None,
            schema_score=None,
            usefulness_score=None,
            quality_score=None,
            gated_usable=None,
            scenario_rank_score=None,
            rejection_reason=None,
        )
    except Exception as e:
        end = time.perf_counter()
        return RunResult(
            model_requested=requested_model,
            model_resolved=resolved_model,
            scenario=scenario,
            expectation=expectation,
            concurrency=concurrency,
            run_kind=run_kind,
            ok=False,
            error=str(e),
            requested_think_mode=requested_think_mode,
            effective_think_mode=effective_think_mode,
            think_mode_source=think_mode_source,
            total_duration_ns=None,
            load_duration_ns=None,
            prompt_eval_count=None,
            prompt_eval_duration_ns=None,
            eval_count=None,
            eval_duration_ns=None,
            wall_time_s=end - start,
            prompt_tps=None,
            gen_tps=None,
            response_chars=None,
            response_text=None,
            json_valid=None,
            schema_score=None,
            usefulness_score=None,
            quality_score=None,
            gated_usable=None,
            scenario_rank_score=None,
            rejection_reason=None,
        )


async def measure_one_async(*args, **kwargs) -> RunResult:
    return await asyncio.to_thread(measure_one, *args, **kwargs)


def get_system_info() -> Dict[str, Any]:
    return {
        "platform": platform.platform(),
        "python": sys.version,
        "machine": platform.machine(),
        "processor": platform.processor(),
        "hostname": platform.node(),
        "cpu_count": os.cpu_count(),
    }


def write_json_output(path: str, results: List[RunResult], meta: Dict[str, Any]) -> None:
    payload = {
        "generated_at_unix": int(time.time()),
        "meta": meta,
        "results": [asdict(r) for r in results],
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


def print_summary(results: List[RunResult]) -> None:
    print_section("Summary by resolved model + think_mode + scenario + run_kind")

    groups: Dict[str, List[RunResult]] = {}
    for r in results:
        key = f"{r.model_resolved}|{r.effective_think_mode}|{r.scenario}|{r.run_kind}|c{r.concurrency}"
        groups.setdefault(key, []).append(r)

    rows = [[
        "resolved_model", "think_mode", "scenario", "run_kind", "conc", "ok",
        "usable", "wall_mean_s", "prompt_tps_mean", "gen_tps_mean",
        "quality_mean", "rank_score_mean"
    ]]

    for key, group in sorted(groups.items()):
        model, think_mode, scenario, run_kind, conc = key.split("|")
        ok_group = [r for r in group if r.ok]
        usable_group = [r for r in group if r.gated_usable]

        rows.append([
            model,
            think_mode,
            scenario,
            run_kind,
            conc.replace("c", ""),
            f"{len(ok_group)}/{len(group)}",
            f"{len(usable_group)}/{len(group)}",
            fmt(summarize_numeric([r.wall_time_s for r in ok_group])["mean"]),
            fmt(summarize_numeric([r.prompt_tps for r in ok_group])["mean"]),
            fmt(summarize_numeric([r.gen_tps for r in ok_group])["mean"]),
            fmt(summarize_numeric([r.quality_score for r in ok_group])["mean"]),
            fmt(summarize_numeric([r.scenario_rank_score for r in usable_group])["mean"]),
        ])

    table(rows)


def print_json_leaderboards(results: List[RunResult]) -> None:
    print_section("Usable leaderboard: warm json_agent")

    usable_rows = [[
        "rank", "model", "think_mode", "wall_mean_s", "gen_tps_mean",
        "quality_mean", "rank_score_mean"
    ]]

    aggregates = []
    pairs = sorted(set(
        (r.model_resolved, r.effective_think_mode)
        for r in results
        if r.scenario == "json_agent" and r.run_kind == "warm" and r.concurrency == 1
    ))

    for model, think_mode in pairs:
        subset = [
            r for r in results
            if r.model_resolved == model
            and r.effective_think_mode == think_mode
            and r.scenario == "json_agent"
            and r.run_kind == "warm"
            and r.concurrency == 1
            and r.gated_usable
        ]
        if not subset:
            continue

        aggregates.append({
            "model": model,
            "think_mode": think_mode,
            "wall_mean": summarize_numeric([r.wall_time_s for r in subset])["mean"],
            "gen_mean": summarize_numeric([r.gen_tps for r in subset])["mean"],
            "quality_mean": summarize_numeric([r.quality_score for r in subset])["mean"],
            "rank_mean": summarize_numeric([r.scenario_rank_score for r in subset])["mean"],
        })

    aggregates.sort(key=lambda x: x["rank_mean"] if x["rank_mean"] is not None else -1, reverse=True)

    for idx, item in enumerate(aggregates, start=1):
        usable_rows.append([
            str(idx),
            item["model"],
            item["think_mode"],
            fmt(item["wall_mean"]),
            fmt(item["gen_mean"]),
            fmt(item["quality_mean"]),
            fmt(item["rank_mean"]),
        ])

    if len(usable_rows) == 1:
        usable_rows.append(["-", "-", "-", "-", "-", "-", "-"])

    table(usable_rows)

    print_section("Rejected leaderboard: warm json_agent")

    rejected_rows = [[
        "model", "think_mode", "reason", "quality_mean", "wall_mean_s"
    ]]

    for model, think_mode in pairs:
        subset = [
            r for r in results
            if r.model_resolved == model
            and r.effective_think_mode == think_mode
            and r.scenario == "json_agent"
            and r.run_kind == "warm"
            and r.concurrency == 1
            and not r.gated_usable
        ]
        if not subset:
            continue

        reason_counts: Dict[str, int] = {}
        for r in subset:
            reason = r.rejection_reason or "unknown"
            reason_counts[reason] = reason_counts.get(reason, 0) + 1

        main_reason = sorted(reason_counts.items(), key=lambda x: x[1], reverse=True)[0][0]

        rejected_rows.append([
            model,
            think_mode,
            main_reason,
            fmt(summarize_numeric([r.quality_score for r in subset])["mean"]),
            fmt(summarize_numeric([r.wall_time_s for r in subset])["mean"]),
        ])

    if len(rejected_rows) == 1:
        rejected_rows.append(["-", "-", "-", "-", "-"])

    table(rejected_rows)


async def run_benchmarks(args: argparse.Namespace) -> int:
    print_section("System info")
    print(json.dumps(get_system_info(), indent=2))

    print_section("Installed Ollama models before resolution")
    try:
        installed = ollama_tags(args.base_url, args.timeout_seconds)
        for m in installed:
            print(f"- {m}")
    except Exception as e:
        print(f"Could not list installed models: {e}")
        return 2

    think_modes = [m.strip() for m in args.think_modes]
    for tm in think_modes:
        if tm not in THINK_MODES:
            print(f"Invalid think mode: {tm}. Allowed: {sorted(THINK_MODES)}")
            return 2

    resolved_models: List[Tuple[str, str]] = []

    print_section("Model resolution")
    for requested in args.models:
        try:
            resolved, suggestions, pulled = resolve_model_name(
                base_url=args.base_url,
                requested=requested,
                timeout_s=args.timeout_seconds,
                auto_pull=args.auto_pull,
                resolve_closest=args.resolve_closest,
            )
            print(f"{requested} -> {resolved}")
            for s in suggestions:
                print(f"  note: {s}")
            if pulled:
                print("  note: model was pulled")
            resolved_models.append((requested, resolved))
        except Exception as e:
            print(f"{requested} -> ERROR: {e}")
            if args.skip_unresolved:
                print("  note: skipping unresolved model")
                continue
            return 3

    if not resolved_models:
        print("No models resolved successfully.")
        return 4

    all_results: List[RunResult] = []

    print_section("Benchmark configuration")
    print(f"Base URL:               {args.base_url}")
    print(f"Warm runs:              {args.warm_runs}")
    print(f"Concurrency:            {', '.join(str(c) for c in args.concurrency)}")
    print(f"Think modes:            {', '.join(think_modes)}")
    print(f"Timeout seconds:        {args.timeout_seconds}")
    print(f"JSON quality threshold: {args.json_quality_threshold}")
    print(f"JSON output:            {args.json_output}")
    print(f"CSV output:             {args.csv_output}")

    for requested_model, resolved_model in resolved_models:
        for think_mode in think_modes:
            print_section(f"Model requested: {requested_model} | resolved: {resolved_model} | think_mode: {think_mode}")

            if args.force_unload:
                print("Attempting unload before cold runs...")
                ollama_unload(args.base_url, resolved_model, args.timeout_seconds)

            for scenario in SCENARIOS:
                scenario_name = scenario["name"]
                prompt = scenario["prompt"]
                expectation = scenario["expectation"]

                print(f"\nScenario: {scenario_name}")

                if args.force_unload:
                    ollama_unload(args.base_url, resolved_model, args.timeout_seconds)

                cold = measure_one(
                    base_url=args.base_url,
                    requested_model=requested_model,
                    resolved_model=resolved_model,
                    prompt=prompt,
                    scenario=scenario_name,
                    expectation=expectation,
                    concurrency=1,
                    run_kind="cold",
                    timeout_s=args.timeout_seconds,
                    requested_think_mode=think_mode,
                )
                all_results.append(cold)

                extra = ""
                if scenario_name == "json_agent":
                    extra = f" json_valid={cold.json_valid} quality={fmt(cold.quality_score)}"
                print(
                    f"  cold: ok={cold.ok} wall={fmt(cold.wall_time_s)}s "
                    f"load={format_seconds_from_ns(cold.load_duration_ns)}s "
                    f"prompt_tps={fmt(cold.prompt_tps)} gen_tps={fmt(cold.gen_tps)}{extra}"
                )

                for i in range(args.warm_runs):
                    warm = measure_one(
                        base_url=args.base_url,
                        requested_model=requested_model,
                        resolved_model=resolved_model,
                        prompt=prompt,
                        scenario=scenario_name,
                        expectation=expectation,
                        concurrency=1,
                        run_kind="warm",
                        timeout_s=args.timeout_seconds,
                        requested_think_mode=think_mode,
                    )
                    all_results.append(warm)
                    extra = ""
                    if scenario_name == "json_agent":
                        extra = f" json_valid={warm.json_valid} quality={fmt(warm.quality_score)}"
                    print(
                        f"  warm#{i+1}: ok={warm.ok} wall={fmt(warm.wall_time_s)}s "
                        f"load={format_seconds_from_ns(warm.load_duration_ns)}s "
                        f"prompt_tps={fmt(warm.prompt_tps)} gen_tps={fmt(warm.gen_tps)}{extra}"
                    )

                for concurrency in args.concurrency:
                    if concurrency <= 1:
                        continue

                    tasks = [
                        measure_one_async(
                            base_url=args.base_url,
                            requested_model=requested_model,
                            resolved_model=resolved_model,
                            prompt=prompt,
                            scenario=scenario_name,
                            expectation=expectation,
                            concurrency=concurrency,
                            run_kind="concurrent",
                            timeout_s=args.timeout_seconds,
                            requested_think_mode=think_mode,
                        )
                        for _ in range(concurrency)
                    ]

                    concurrent_results = await asyncio.gather(*tasks)
                    all_results.extend(concurrent_results)

                    ok_count = sum(1 for r in concurrent_results if r.ok)
                    wall_summary = summarize_numeric([r.wall_time_s for r in concurrent_results if r.ok])
                    prompt_summary = summarize_numeric([r.prompt_tps for r in concurrent_results if r.ok])
                    gen_summary = summarize_numeric([r.gen_tps for r in concurrent_results if r.ok])
                    quality_summary = summarize_numeric([r.quality_score for r in concurrent_results if r.ok])

                    extra = ""
                    if scenario_name == "json_agent":
                        extra = f" quality_mean={fmt(quality_summary['mean'])}"
                    print(
                        f"  concurrent x{concurrency}: ok={ok_count}/{concurrency} "
                        f"wall_mean={fmt(wall_summary['mean'])}s "
                        f"prompt_tps_mean={fmt(prompt_summary['mean'])} "
                        f"gen_tps_mean={fmt(gen_summary['mean'])}{extra}"
                    )

    assign_scenario_scores(all_results, args.json_quality_threshold)

    meta = {
        "system": get_system_info(),
        "base_url": args.base_url,
        "models_requested": args.models,
        "models_resolved": [{"requested": r, "resolved": m} for r, m in resolved_models],
        "think_modes": think_modes,
        "warm_runs": args.warm_runs,
        "concurrency": args.concurrency,
        "timeout_seconds": args.timeout_seconds,
        "force_unload": args.force_unload,
        "auto_pull": args.auto_pull,
        "resolve_closest": args.resolve_closest,
        "json_quality_threshold": args.json_quality_threshold,
    }

    write_json_output(args.json_output, all_results, meta)
    write_csv_output(args.csv_output, all_results)

    print_summary(all_results)
    print_json_leaderboards(all_results)

    print_section("Files written")
    print(args.json_output)
    print(args.csv_output)

    return 0


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Benchmark Ollama models with think-mode awareness, quality gates, and scenario-specific scoring."
    )
    parser.add_argument(
        "--base-url",
        default=DEFAULT_BASE_URL,
        help=f"Ollama base URL (default: {DEFAULT_BASE_URL})",
    )
    parser.add_argument(
        "--models",
        nargs="+",
        required=True,
        help="Requested model tags, e.g. qwen2.5:7b qwen3:4b llama3.2:3b",
    )
    parser.add_argument(
        "--think-modes",
        nargs="+",
        default=["provider_default"],
        help="One or more of: thinking nothinking provider_default",
    )
    parser.add_argument(
        "--warm-runs",
        type=int,
        default=3,
        help="Number of warm runs per scenario",
    )
    parser.add_argument(
        "--concurrency",
        nargs="+",
        type=int,
        default=[1, 4],
        help="Concurrency levels to test",
    )
    parser.add_argument(
        "--timeout-seconds",
        type=int,
        default=900,
        help="HTTP timeout per request in seconds",
    )
    parser.add_argument(
        "--json-quality-threshold",
        type=float,
        default=6.0,
        help="Minimum quality score required for json_agent usability",
    )
    parser.add_argument(
        "--json-output",
        default="ollama_bench_v3.json",
        help="Path to JSON output file",
    )
    parser.add_argument(
        "--csv-output",
        default="ollama_bench_v3.csv",
        help="Path to CSV output file",
    )
    parser.add_argument(
        "--force-unload",
        action="store_true",
        help="Attempt to unload model before cold runs using keep_alive=0",
    )
    parser.add_argument(
        "--auto-pull",
        action="store_true",
        help="Automatically pull missing models if possible",
    )
    parser.add_argument(
        "--resolve-closest",
        action="store_true",
        help="Resolve to the closest installed/remote hint if requested model is not found",
    )
    parser.add_argument(
        "--skip-unresolved",
        action="store_true",
        help="Skip unresolved models instead of exiting",
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