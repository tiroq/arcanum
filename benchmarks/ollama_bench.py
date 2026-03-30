#!/usr/bin/env python3

import argparse
import asyncio
import json
import math
import statistics
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass, asdict
from typing import Any, Dict, List, Optional


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
    model: str
    scenario: str
    concurrency: int
    run_kind: str  # cold, warm, concurrent
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


def ns_to_s(value: Optional[int]) -> Optional[float]:
    if value is None:
        return None
    return value / 1_000_000_000.0


def safe_div(num: Optional[float], den: Optional[float]) -> Optional[float]:
    if num is None or den is None or den == 0:
        return None
    return num / den


def compute_prompt_tps(prompt_eval_count: Optional[int], prompt_eval_duration_ns: Optional[int]) -> Optional[float]:
    if prompt_eval_count is None or prompt_eval_duration_ns in (None, 0):
        return None
    return prompt_eval_count / (prompt_eval_duration_ns / 1_000_000_000.0)


def compute_gen_tps(eval_count: Optional[int], eval_duration_ns: Optional[int]) -> Optional[float]:
    if eval_count is None or eval_duration_ns in (None, 0):
        return None
    return eval_count / (eval_duration_ns / 1_000_000_000.0)


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


def ollama_ps(base_url: str, timeout_s: int) -> Dict[str, Any]:
    try:
        return http_post_json(f"{base_url.rstrip('/')}/api/ps", {}, timeout_s=timeout_s)
    except Exception:
        return {"models": []}


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


def measure_one(base_url: str, model: str, prompt: str, scenario: str, concurrency: int, run_kind: str, timeout_s: int) -> RunResult:
    start = time.perf_counter()
    try:
        data = ollama_generate(base_url, model, prompt, timeout_s)
        end = time.perf_counter()

        total_duration_ns = data.get("total_duration")
        load_duration_ns = data.get("load_duration")
        prompt_eval_count = data.get("prompt_eval_count")
        prompt_eval_duration_ns = data.get("prompt_eval_duration")
        eval_count = data.get("eval_count")
        eval_duration_ns = data.get("eval_duration")
        response_text = data.get("response", "")

        return RunResult(
            model=model,
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
        )
    except urllib.error.HTTPError as e:
        end = time.perf_counter()
        return RunResult(
            model=model,
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
        )
    except Exception as e:
        end = time.perf_counter()
        return RunResult(
            model=model,
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
        )


async def measure_one_async(base_url: str, model: str, prompt: str, scenario: str, concurrency: int, run_kind: str, timeout_s: int) -> RunResult:
    return await asyncio.to_thread(
        measure_one, base_url, model, prompt, scenario, concurrency, run_kind, timeout_s
    )


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


def print_section(title: str) -> None:
    print()
    print("=" * len(title))
    print(title)
    print("=" * len(title))


def fmt(value: Optional[float], digits: int = 2) -> str:
    if value is None:
        return "-"
    return f"{value:.{digits}f}"


def format_seconds_from_ns(ns: Optional[int]) -> str:
    if ns is None:
        return "-"
    return fmt(ns / 1_000_000_000.0, 2)


def table(rows: List[List[str]]) -> None:
    if not rows:
        return
    widths = [max(len(str(row[i])) for row in rows) for i in range(len(rows[0]))]
    for idx, row in enumerate(rows):
        line = " | ".join(str(cell).ljust(widths[i]) for i, cell in enumerate(row))
        print(line)
        if idx == 0:
            print("-+-".join("-" * w for w in widths))


async def run_benchmarks(args: argparse.Namespace) -> int:
    scenarios = [
        ("short", SHORT_PROMPT),
        ("medium", MEDIUM_PROMPT),
        ("long", LONG_PROMPT),
        ("json_agent", JSON_AGENT_PROMPT),
    ]

    all_results: List[RunResult] = []

    print_section("Ollama benchmark configuration")
    print(f"Base URL:       {args.base_url}")
    print(f"Models:         {', '.join(args.models)}")
    print(f"Warm runs:      {args.warm_runs}")
    print(f"Concurrency:    {', '.join(str(c) for c in args.concurrency)}")
    print(f"Timeout:        {args.timeout_seconds}s")
    print(f"Output:         {args.output}")

    for model in args.models:
        print_section(f"Model: {model}")

        if args.force_unload:
            print("Attempting unload before cold runs...")
            ollama_unload(args.base_url, model, args.timeout_seconds)

        for scenario_name, prompt in scenarios:
            print(f"\nScenario: {scenario_name}")

            if args.force_unload:
                ollama_unload(args.base_url, model, args.timeout_seconds)

            cold = measure_one(
                args.base_url,
                model,
                prompt,
                scenario_name,
                concurrency=1,
                run_kind="cold",
                timeout_s=args.timeout_seconds,
            )
            all_results.append(cold)

            print(
                f"  cold: ok={cold.ok} wall={fmt(cold.wall_time_s)}s "
                f"load={format_seconds_from_ns(cold.load_duration_ns)}s "
                f"prompt_tps={fmt(cold.prompt_tps)} gen_tps={fmt(cold.gen_tps)}"
            )

            warm_results: List[RunResult] = []
            for i in range(args.warm_runs):
                warm = measure_one(
                    args.base_url,
                    model,
                    prompt,
                    scenario_name,
                    concurrency=1,
                    run_kind="warm",
                    timeout_s=args.timeout_seconds,
                )
                warm_results.append(warm)
                all_results.append(warm)
                print(
                    f"  warm#{i+1}: ok={warm.ok} wall={fmt(warm.wall_time_s)}s "
                    f"load={format_seconds_from_ns(warm.load_duration_ns)}s "
                    f"prompt_tps={fmt(warm.prompt_tps)} gen_tps={fmt(warm.gen_tps)}"
                )

            for concurrency in args.concurrency:
                if concurrency <= 1:
                    continue
                tasks = [
                    measure_one_async(
                        args.base_url,
                        model,
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

                wall_summary = summarize_numeric([r.wall_time_s for r in concurrent_results if r.ok])
                prompt_summary = summarize_numeric([r.prompt_tps for r in concurrent_results if r.ok])
                gen_summary = summarize_numeric([r.gen_tps for r in concurrent_results if r.ok])

                ok_count = sum(1 for r in concurrent_results if r.ok)
                print(
                    f"  concurrent x{concurrency}: ok={ok_count}/{concurrency} "
                    f"wall_mean={fmt(wall_summary['mean'])}s "
                    f"prompt_tps_mean={fmt(prompt_summary['mean'])} "
                    f"gen_tps_mean={fmt(gen_summary['mean'])}"
                )

    write_output(args.output, all_results)
    print_summary(all_results)
    return 0


def write_output(path: str, results: List[RunResult]) -> None:
    payload = {
        "generated_at_unix": int(time.time()),
        "results": [asdict(r) for r in results],
    }
    with open(path, "w", encoding="utf-8") as f:
        json.dump(payload, f, indent=2)
    print_section("Raw results written")
    print(path)


def print_summary(results: List[RunResult]) -> None:
    print_section("Summary by model + scenario + run_kind")

    groups: Dict[str, List[RunResult]] = {}
    for r in results:
        key = f"{r.model}|{r.scenario}|{r.run_kind}|c{r.concurrency}"
        groups.setdefault(key, []).append(r)

    rows = [[
        "model", "scenario", "run_kind", "conc",
        "ok", "wall_mean_s", "load_mean_s", "prompt_tps_mean", "gen_tps_mean"
    ]]

    for key, group in sorted(groups.items()):
        ok_group = [r for r in group if r.ok]
        model, scenario, run_kind, conc = key.split("|")

        wall_mean = summarize_numeric([r.wall_time_s for r in ok_group])["mean"]
        load_mean = summarize_numeric([ns_to_s(r.load_duration_ns) for r in ok_group])["mean"]
        prompt_mean = summarize_numeric([r.prompt_tps for r in ok_group])["mean"]
        gen_mean = summarize_numeric([r.gen_tps for r in ok_group])["mean"]

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
        ])

    table(rows)

    print_section("Model leaderboard: warm json_agent scenario")
    leaderboard_rows = [[
        "model", "wall_mean_s", "prompt_tps_mean", "gen_tps_mean"
    ]]

    leaderboard: List[tuple] = []
    for model in sorted(set(r.model for r in results)):
        subset = [
            r for r in results
            if r.model == model and r.scenario == "json_agent" and r.run_kind == "warm" and r.ok
        ]
        if not subset:
            continue

        wall_mean = summarize_numeric([r.wall_time_s for r in subset])["mean"]
        prompt_mean = summarize_numeric([r.prompt_tps for r in subset])["mean"]
        gen_mean = summarize_numeric([r.gen_tps for r in subset])["mean"]

        leaderboard.append((
            model,
            wall_mean if wall_mean is not None else math.inf,
            prompt_mean,
            gen_mean,
        ))

    leaderboard.sort(key=lambda x: x[1])

    for model, wall_mean, prompt_mean, gen_mean in leaderboard:
        leaderboard_rows.append([
            model,
            fmt(wall_mean),
            fmt(prompt_mean),
            fmt(gen_mean),
        ])

    table(leaderboard_rows)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Benchmark Ollama models on local hardware.")
    parser.add_argument(
        "--base-url",
        default=DEFAULT_BASE_URL,
        help=f"Ollama base URL (default: {DEFAULT_BASE_URL})",
    )
    parser.add_argument(
        "--models",
        nargs="+",
        required=True,
        help="List of Ollama model tags, e.g. qwen2.5:7b qwen3:8b",
    )
    parser.add_argument(
        "--warm-runs",
        type=int,
        default=3,
        help="Number of warm runs per scenario (default: 3)",
    )
    parser.add_argument(
        "--concurrency",
        nargs="+",
        type=int,
        default=[1, 4],
        help="Concurrency levels to test (default: 1 4)",
    )
    parser.add_argument(
        "--timeout-seconds",
        type=int,
        default=600,
        help="HTTP timeout per request in seconds (default: 600)",
    )
    parser.add_argument(
        "--output",
        default="ollama_bench_results.json",
        help="Path to JSON output file",
    )
    parser.add_argument(
        "--force-unload",
        action="store_true",
        help="Attempt to unload model before cold runs with keep_alive=0",
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