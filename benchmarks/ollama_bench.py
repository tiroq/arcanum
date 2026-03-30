#!/usr/bin/env python3

import argparse
import asyncio
import difflib
import json
import math
import os
import platform
import re
import statistics
import subprocess
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


@dataclass
class RunResult:
    model_requested: str
    model_resolved: str
    scenario: str
    concurrency: int
    run_kind: str
    ok: bool
    error: Optional[str]
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
    json_valid: Optional[bool]
    json_quality_score: Optional[float]
    quality_notes: Optional[List[str]]


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


def ollama_ps(base_url: str, timeout_s: int) -> Dict[str, Any]:
    try:
        return http_get_json(f"{base_url.rstrip('/')}/api/ps", timeout_s)
    except Exception:
        return {"models": []}


def ollama_generate(base_url: str, model: str, prompt: str, timeout_s: int) -> Dict[str, Any]:
    return http_post_json(
        f"{base_url.rstrip('/')}/api/generate",
        {
            "model": model,
            "prompt": prompt,
            "stream": False,
        },
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
        # stream=false is not universally guaranteed for pull, so use urllib raw read
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
    """
    Best-effort public library lookup.
    This is not required for local benchmarking, but helps suggest correct names.
    """
    candidates: List[str] = []
    try:
        encoded = urllib.parse.quote(query)
        url = f"https://ollama.com/search?q={encoded}"
        html = urllib.request.urlopen(url, timeout=timeout_s).read().decode("utf-8", errors="ignore")

        # Best-effort parse of /library/<name> links from search results
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
    """
    Create useful fallback guesses.
    Example:
    qwen2.5:7b-instruct -> [qwen2.5:7b-instruct, qwen2.5:7b, qwen2.5]
    """
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
        # if no tag, try common instruct-ish defaults
        guesses.extend([
            f"{name}:latest",
            f"{name}:7b",
            f"{name}:8b",
        ])

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

    # Strong family-based match first
    req = normalize_model_hint(requested)
    family = req.split(":")[0]
    family_matches = [c for c in candidates if c.lower().startswith(family)]
    if family_matches:
        close = difflib.get_close_matches(req, family_matches, n=5, cutoff=0.0)
        if close:
            return close[0], close
        return family_matches[0], family_matches[:5]

    # General fuzzy match
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
    """
    Returns:
      resolved_name,
      suggestions,
      pulled
    """
    installed = ollama_tags(base_url, timeout_s)
    installed_set = set(installed)

    # Exact installed
    if requested in installed_set:
        return requested, [], False

    # Try guesses
    guesses = expand_model_guesses(requested)
    for g in guesses:
        if g in installed_set:
            return g, [f"resolved installed alias from {requested} -> {g}"], False

    # Search remote hints
    remote_hints = search_ollama_library(requested)
    best, suggestions = pick_closest_model(requested, installed, remote_hints)

    # Auto-pull exact requested first
    if auto_pull:
        print(f"Model '{requested}' is not installed. Attempting pull...")
        if ollama_pull(base_url, requested, timeout_s):
            return requested, [f"pulled requested model {requested}"], True

        # Try guessed names
        for g in guesses:
            if g == requested:
                continue
            print(f"Pull failed for '{requested}'. Trying guessed model '{g}'...")
            if ollama_pull(base_url, g, timeout_s):
                return g, [f"requested model not found; pulled guessed model {g}"], True

        # Try best fuzzy suggestion
        if best:
            print(f"Trying suggested model '{best}'...")
            if ollama_pull(base_url, best, timeout_s):
                return best, suggestions, True

    if resolve_closest and best:
        return best, suggestions, False

    suggestion_msgs = suggestions or [f"no exact installed match for {requested}"]
    raise ValueError(
        f"Model '{requested}' not found locally. Suggestions: {', '.join(suggestion_msgs)}"
    )


def score_json_quality(response_text: str) -> Tuple[bool, float, List[str]]:
    notes: List[str] = []

    try:
        data = json.loads(response_text)
    except Exception as e:
        return False, 0.0, [f"invalid JSON: {e}"]

    score = 0.0

    if isinstance(data, dict):
        score += 1.0
    else:
        return False, 0.5, ["JSON parsed but top-level object is not a dictionary"]

    required_keys = ["title", "priority", "steps", "risks", "review_model_role"]
    for key in required_keys:
        if key in data:
            score += 1.0
        else:
            notes.append(f"missing key: {key}")

    title = data.get("title")
    if isinstance(title, str) and title.strip():
        score += 1.0
    else:
        notes.append("title is empty or not a string")

    priority = data.get("priority")
    if priority in {"low", "medium", "high", "critical"}:
        score += 1.0
    else:
        notes.append("priority is not one of low|medium|high|critical")

    steps = data.get("steps")
    if isinstance(steps, list) and steps and all(isinstance(x, str) and x.strip() for x in steps):
        score += 1.0
    else:
        notes.append("steps is not a non-empty list of strings")

    risks = data.get("risks")
    if isinstance(risks, list) and risks and all(isinstance(x, str) and x.strip() for x in risks):
        score += 1.0
    else:
        notes.append("risks is not a non-empty list of strings")

    review_model_role = data.get("review_model_role")
    if isinstance(review_model_role, str) and review_model_role.strip():
        score += 1.0
    else:
        notes.append("review_model_role is empty or not a string")

    # Normalize to 0..10
    max_score = 10.0
    score = min(score, max_score)

    return True, score, notes


def measure_one(
    base_url: str,
    requested_model: str,
    resolved_model: str,
    prompt: str,
    scenario: str,
    concurrency: int,
    run_kind: str,
    timeout_s: int,
) -> RunResult:
    start = time.perf_counter()
    try:
        data = ollama_generate(base_url, resolved_model, prompt, timeout_s)
        end = time.perf_counter()

        total_duration_ns = data.get("total_duration")
        load_duration_ns = data.get("load_duration")
        prompt_eval_count = data.get("prompt_eval_count")
        prompt_eval_duration_ns = data.get("prompt_eval_duration")
        eval_count = data.get("eval_count")
        eval_duration_ns = data.get("eval_duration")
        response_text = data.get("response", "")

        json_valid = None
        json_quality_score = None
        quality_notes = None
        if scenario == "json_agent":
            json_valid, json_quality_score, quality_notes = score_json_quality(response_text)

        return RunResult(
            model_requested=requested_model,
            model_resolved=resolved_model,
            scenario=scenario,
            concurrency=concurrency,
            run_kind=run_kind,
            ok=True,
            error=None,
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
            json_valid=json_valid,
            json_quality_score=json_quality_score,
            quality_notes=quality_notes,
        )
    except urllib.error.HTTPError as e:
        end = time.perf_counter()
        return RunResult(
            model_requested=requested_model,
            model_resolved=resolved_model,
            scenario=scenario,
            concurrency=concurrency,
            run_kind=run_kind,
            ok=False,
            error=f"HTTPError {e.code}: {e.reason}",
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
            json_valid=None,
            json_quality_score=None,
            quality_notes=None,
        )
    except Exception as e:
        end = time.perf_counter()
        return RunResult(
            model_requested=requested_model,
            model_resolved=resolved_model,
            scenario=scenario,
            concurrency=concurrency,
            run_kind=run_kind,
            ok=False,
            error=str(e),
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
            json_valid=None,
            json_quality_score=None,
            quality_notes=None,
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


def write_output(path: str, results: List[RunResult], meta: Dict[str, Any]) -> None:
    payload = {
        "generated_at_unix": int(time.time()),
        "meta": meta,
        "results": [asdict(r) for r in results],
    }
    with open(path, "w", encoding="utf-8") as f:
        json.dump(payload, f, indent=2)
    print_section("Raw results written")
    print(path)


def print_summary(results: List[RunResult]) -> None:
    print_section("Summary by resolved model + scenario + run_kind")

    groups: Dict[str, List[RunResult]] = {}
    for r in results:
        key = f"{r.model_resolved}|{r.scenario}|{r.run_kind}|c{r.concurrency}"
        groups.setdefault(key, []).append(r)

    rows = [[
        "resolved_model", "scenario", "run_kind", "conc", "ok",
        "wall_mean_s", "load_mean_s", "prompt_tps_mean", "gen_tps_mean", "json_q_mean"
    ]]

    for key, group in sorted(groups.items()):
        ok_group = [r for r in group if r.ok]
        model, scenario, run_kind, conc = key.split("|")

        wall_mean = summarize_numeric([r.wall_time_s for r in ok_group])["mean"]
        load_mean = summarize_numeric([ns_to_s(r.load_duration_ns) for r in ok_group])["mean"]
        prompt_mean = summarize_numeric([r.prompt_tps for r in ok_group])["mean"]
        gen_mean = summarize_numeric([r.gen_tps for r in ok_group])["mean"]
        quality_mean = summarize_numeric([r.json_quality_score for r in ok_group])["mean"]

        rows.append([
            model,
            scenario,
            run_kind,
            conc.replace("c", ""),
            f"{len(ok_group)}/{len(group)}",
            fmt(wall_mean),
            fmt(load_mean),
            fmt(prompt_mean),
            fmt(gen_mean),
            fmt(quality_mean),
        ])

    table(rows)

    print_section("Leaderboard: warm json_agent")
    leaderboard_rows = [[
        "model", "wall_mean_s", "prompt_tps_mean", "gen_tps_mean", "json_quality_mean", "combined_score"
    ]]

    leaderboard: List[Tuple[str, float, Optional[float], Optional[float], Optional[float], float]] = []
    for model in sorted(set(r.model_resolved for r in results)):
        subset = [
            r for r in results
            if r.model_resolved == model and r.scenario == "json_agent" and r.run_kind == "warm" and r.ok
        ]
        if not subset:
            continue

        wall_mean = summarize_numeric([r.wall_time_s for r in subset])["mean"]
        prompt_mean = summarize_numeric([r.prompt_tps for r in subset])["mean"]
        gen_mean = summarize_numeric([r.gen_tps for r in subset])["mean"]
        quality_mean = summarize_numeric([r.json_quality_score for r in subset])["mean"]

        # heuristic combined score: quality is positive, latency negative
        combined = 0.0
        if quality_mean is not None:
            combined += quality_mean * 10.0
        if gen_mean is not None:
            combined += gen_mean
        if prompt_mean is not None:
            combined += prompt_mean * 0.25
        if wall_mean is not None:
            combined -= wall_mean * 5.0

        leaderboard.append((model, wall_mean or math.inf, prompt_mean, gen_mean, quality_mean, combined))

    leaderboard.sort(key=lambda x: x[5], reverse=True)

    for model, wall_mean, prompt_mean, gen_mean, quality_mean, combined in leaderboard:
        leaderboard_rows.append([
            model,
            fmt(wall_mean),
            fmt(prompt_mean),
            fmt(gen_mean),
            fmt(quality_mean),
            fmt(combined),
        ])

    table(leaderboard_rows)


async def run_benchmarks(args: argparse.Namespace) -> int:
    scenarios = [
        ("short", SHORT_PROMPT),
        ("medium", MEDIUM_PROMPT),
        ("long", LONG_PROMPT),
        ("json_agent", JSON_AGENT_PROMPT),
    ]

    all_results: List[RunResult] = []

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
                print(f"  note: model was pulled")
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

    print_section("Benchmark configuration")
    print(f"Base URL:       {args.base_url}")
    print(f"Warm runs:      {args.warm_runs}")
    print(f"Concurrency:    {', '.join(str(c) for c in args.concurrency)}")
    print(f"Timeout:        {args.timeout_seconds}s")
    print(f"Output:         {args.output}")

    for requested_model, resolved_model in resolved_models:
        print_section(f"Model requested: {requested_model} | resolved: {resolved_model}")

        if args.force_unload:
            print("Attempting unload before cold runs...")
            ollama_unload(args.base_url, resolved_model, args.timeout_seconds)

        for scenario_name, prompt in scenarios:
            print(f"\nScenario: {scenario_name}")

            if args.force_unload:
                ollama_unload(args.base_url, resolved_model, args.timeout_seconds)

            cold = measure_one(
                args.base_url,
                requested_model,
                resolved_model,
                prompt,
                scenario_name,
                concurrency=1,
                run_kind="cold",
                timeout_s=args.timeout_seconds,
            )
            all_results.append(cold)

            extra = ""
            if scenario_name == "json_agent":
                extra = f" json_valid={cold.json_valid} json_q={fmt(cold.json_quality_score)}"
            print(
                f"  cold: ok={cold.ok} wall={fmt(cold.wall_time_s)}s "
                f"load={format_seconds_from_ns(cold.load_duration_ns)}s "
                f"prompt_tps={fmt(cold.prompt_tps)} gen_tps={fmt(cold.gen_tps)}{extra}"
            )

            for i in range(args.warm_runs):
                warm = measure_one(
                    args.base_url,
                    requested_model,
                    resolved_model,
                    prompt,
                    scenario_name,
                    concurrency=1,
                    run_kind="warm",
                    timeout_s=args.timeout_seconds,
                )
                all_results.append(warm)
                extra = ""
                if scenario_name == "json_agent":
                    extra = f" json_valid={warm.json_valid} json_q={fmt(warm.json_quality_score)}"
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
                        args.base_url,
                        requested_model,
                        resolved_model,
                        prompt,
                        scenario_name,
                        concurrency=concurrency,
                        run_kind="concurrent",
                        timeout_s=args.timeout_seconds,
                    )
                    for _ in range(concurrency)
                ]
                concurrent_results = await asyncio.gather(*tasks)
                all_results.extend(concurrent_results)

                ok_count = sum(1 for r in concurrent_results if r.ok)
                wall_summary = summarize_numeric([r.wall_time_s for r in concurrent_results if r.ok])
                prompt_summary = summarize_numeric([r.prompt_tps for r in concurrent_results if r.ok])
                gen_summary = summarize_numeric([r.gen_tps for r in concurrent_results if r.ok])
                quality_summary = summarize_numeric([r.json_quality_score for r in concurrent_results if r.ok])

                extra = ""
                if scenario_name == "json_agent":
                    extra = f" json_q_mean={fmt(quality_summary['mean'])}"
                print(
                    f"  concurrent x{concurrency}: ok={ok_count}/{concurrency} "
                    f"wall_mean={fmt(wall_summary['mean'])}s "
                    f"prompt_tps_mean={fmt(prompt_summary['mean'])} "
                    f"gen_tps_mean={fmt(gen_summary['mean'])}{extra}"
                )

    meta = {
        "system": get_system_info(),
        "base_url": args.base_url,
        "models_requested": args.models,
        "models_resolved": [{"requested": r, "resolved": m} for r, m in resolved_models],
        "warm_runs": args.warm_runs,
        "concurrency": args.concurrency,
        "timeout_seconds": args.timeout_seconds,
        "force_unload": args.force_unload,
        "auto_pull": args.auto_pull,
        "resolve_closest": args.resolve_closest,
    }

    write_output(args.output, all_results, meta)
    print_summary(all_results)
    return 0


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Benchmark Ollama models with resolution, auto-pull, and quality checks.")
    parser.add_argument(
        "--base-url",
        default=DEFAULT_BASE_URL,
        help=f"Ollama base URL (default: {DEFAULT_BASE_URL})",
    )
    parser.add_argument(
        "--models",
        nargs="+",
        required=True,
        help="Requested model tags, e.g. qwen2.5:7b-instruct qwen3:8b llama3.2:3b",
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
        "--output",
        default="ollama_bench_v2_results.json",
        help="Output JSON file",
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
        help="If requested model is not found, resolve to the closest installed/remote hint instead of failing",
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