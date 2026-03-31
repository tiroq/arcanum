#!/usr/bin/env python3
"""
Visualize Ollama benchmark results from one or more JSON benchmark files.

Supported input:
- one or more benchmark JSON files produced by the benchmark scripts

Outputs:
- summary CSV
- aggregated CSV
- multiple PNG charts

Example:
python3 visualize_ollama_bench.py \
  --inputs ollama_bench_light_3.json ollama_bench_heavy.json \
  --outdir bench_viz \
  --focus-scenario json_agent \
  --focus-run-kind warm

Example:
python3 visualize_ollama_bench.py \
  --inputs ollama_bench_heavy.json \
  --outdir bench_viz_heavy \
  --focus-scenario json_agent \
  --focus-run-kind cold
"""

import argparse
import json
from pathlib import Path
from typing import List, Dict, Any

import pandas as pd
import matplotlib.pyplot as plt


def load_results(paths: List[Path]) -> pd.DataFrame:
    rows = []
    for path in paths:
        with path.open("r", encoding="utf-8") as f:
            payload = json.load(f)

        meta = payload.get("meta", {})
        results = payload.get("results", [])
        run_name = path.stem

        for row in results:
            r = dict(row)
            r["_source_file"] = str(path)
            r["_run_name"] = run_name
            r["_base_url"] = meta.get("base_url")
            r["_json_quality_threshold"] = meta.get("json_quality_threshold")
            think_modes = meta.get("think_modes")
            r["_configured_think_modes"] = ",".join(think_modes) if isinstance(think_modes, list) else None
            rows.append(r)

    if not rows:
        return pd.DataFrame()

    df = pd.DataFrame(rows)

    # Normalize columns that may be missing in older/newer files
    defaults = {
        "model_resolved": None,
        "model_requested": None,
        "scenario": None,
        "expectation": None,
        "run_kind": None,
        "concurrency": None,
        "ok": None,
        "error": None,
        "requested_think_mode": None,
        "effective_think_mode": None,
        "think_mode_source": None,
        "wall_time_s": None,
        "prompt_tps": None,
        "gen_tps": None,
        "quality_score": None,
        "json_valid": None,
        "gated_usable": None,
        "scenario_rank_score": None,
        "rejection_reason": None,
    }
    for col, default in defaults.items():
        if col not in df.columns:
            df[col] = default

    return df


def ensure_outdir(path: Path) -> None:
    path.mkdir(parents=True, exist_ok=True)


def save_csv(df: pd.DataFrame, path: Path) -> None:
    df.to_csv(path, index=False)


def numeric_mean(series: pd.Series):
    clean = pd.to_numeric(series, errors="coerce").dropna()
    if clean.empty:
        return None
    return clean.mean()


def build_focus_table(df: pd.DataFrame, focus_scenario: str, focus_run_kind: str) -> pd.DataFrame:
    sub = df[
        (df["scenario"] == focus_scenario) &
        (df["run_kind"] == focus_run_kind) &
        (df["concurrency"] == 1)
    ].copy()

    if sub.empty:
        return sub

    group_cols = ["_run_name", "model_resolved", "effective_think_mode"]
    agg = sub.groupby(group_cols, as_index=False).agg(
        ok_count=("ok", lambda s: int(pd.Series(s).fillna(False).sum())),
        total_count=("ok", "size"),
        usable_count=("gated_usable", lambda s: int(pd.Series(s).fillna(False).sum())),
        wall_mean_s=("wall_time_s", numeric_mean),
        prompt_tps_mean=("prompt_tps", numeric_mean),
        gen_tps_mean=("gen_tps", numeric_mean),
        quality_mean=("quality_score", numeric_mean),
        rank_score_mean=("scenario_rank_score", numeric_mean),
    )

    # dominant rejection reason
    reasons = (
        sub.groupby(group_cols + ["rejection_reason"], dropna=False)
        .size()
        .reset_index(name="n")
        .sort_values(["_run_name", "model_resolved", "effective_think_mode", "n"], ascending=[True, True, True, False])
    )
    reasons = reasons.drop_duplicates(subset=group_cols)
    agg = agg.merge(
        reasons[group_cols + ["rejection_reason"]],
        on=group_cols,
        how="left"
    )

    agg["ok_ratio"] = agg["ok_count"].astype(str) + "/" + agg["total_count"].astype(str)
    agg["usable_ratio"] = agg["usable_count"].astype(str) + "/" + agg["total_count"].astype(str)
    return agg.sort_values(
        by=["_run_name", "usable_count", "quality_mean", "wall_mean_s"],
        ascending=[True, False, False, True],
    )


def build_text_table(df: pd.DataFrame) -> pd.DataFrame:
    sub = df[
        (df["scenario"].isin(["short", "medium", "long"])) &
        (df["concurrency"] == 1)
    ].copy()
    if sub.empty:
        return sub

    group_cols = ["_run_name", "model_resolved", "effective_think_mode", "run_kind"]
    agg = sub.groupby(group_cols, as_index=False).agg(
        avg_text_quality=("quality_score", numeric_mean),
        avg_text_wall_s=("wall_time_s", numeric_mean),
        avg_text_prompt_tps=("prompt_tps", numeric_mean),
        avg_text_gen_tps=("gen_tps", numeric_mean),
    )
    return agg.sort_values(
        by=["_run_name", "run_kind", "avg_text_quality", "avg_text_wall_s"],
        ascending=[True, True, False, True]
    )


def plot_json_scatter(focus_df: pd.DataFrame, outpath: Path, title_suffix: str) -> None:
    if focus_df.empty:
        return

    plt.figure(figsize=(12, 8))
    used_labels = set()

    for _, row in focus_df.iterrows():
        usable = bool(row.get("usable_count", 0) > 0)
        label = "usable" if usable else "rejected"
        if label in used_labels:
            plot_label = None
        else:
            plot_label = label
            used_labels.add(label)

        plt.scatter(
            row["wall_mean_s"],
            row["quality_mean"],
            s=120,
            label=plot_label
        )
        name = f'{row["model_resolved"]} [{row["effective_think_mode"]}]'
        plt.annotate(
            name,
            (row["wall_mean_s"], row["quality_mean"]),
            textcoords="offset points",
            xytext=(5, 5),
            fontsize=8
        )

    plt.xlabel("Wall time, seconds (lower is better)")
    plt.ylabel("Quality score (higher is better)")
    plt.title(f"JSON focus: quality vs latency {title_suffix}")
    plt.grid(True, alpha=0.3)
    if used_labels:
        plt.legend()
    plt.tight_layout()
    plt.savefig(outpath, dpi=160)
    plt.close()


def plot_usable_json_speed(focus_df: pd.DataFrame, outpath: Path, title_suffix: str) -> None:
    if focus_df.empty:
        return

    usable = focus_df[focus_df["usable_count"] > 0].copy()
    if usable.empty:
        return

    usable["label"] = usable["model_resolved"] + " [" + usable["effective_think_mode"] + "]"
    usable = usable.sort_values("wall_mean_s", ascending=True)

    plt.figure(figsize=(12, max(6, 0.45 * len(usable))))
    plt.barh(usable["label"], usable["wall_mean_s"])
    plt.xlabel("Wall time, seconds")
    plt.ylabel("Model [think mode]")
    plt.title(f"Usable JSON candidates ranked by speed {title_suffix}")
    plt.tight_layout()
    plt.savefig(outpath, dpi=160)
    plt.close()


def plot_rank_score(focus_df: pd.DataFrame, outpath: Path, title_suffix: str) -> None:
    if focus_df.empty:
        return

    usable = focus_df[focus_df["usable_count"] > 0].copy()
    if usable.empty:
        return

    usable["label"] = usable["model_resolved"] + " [" + usable["effective_think_mode"] + "]"
    usable = usable.sort_values("rank_score_mean", ascending=False)

    plt.figure(figsize=(12, max(6, 0.45 * len(usable))))
    plt.barh(usable["label"], usable["rank_score_mean"])
    plt.xlabel("Scenario rank score")
    plt.ylabel("Model [think mode]")
    plt.title(f"Usable JSON candidates ranked by scenario score {title_suffix}")
    plt.tight_layout()
    plt.savefig(outpath, dpi=160)
    plt.close()


def plot_text_quality(text_df: pd.DataFrame, outpath: Path) -> None:
    if text_df.empty:
        return

    # focus on warm if available, else all
    warm = text_df[text_df["run_kind"] == "warm"].copy()
    use = warm if not warm.empty else text_df.copy()

    use["label"] = use["model_resolved"] + " [" + use["effective_think_mode"].fillna("na") + "]"
    use = use.sort_values("avg_text_quality", ascending=False)

    plt.figure(figsize=(12, max(6, 0.45 * len(use))))
    plt.barh(use["label"], use["avg_text_quality"])
    plt.xlabel("Average text quality score")
    plt.ylabel("Model [think mode]")
    plt.title("Average text quality across short / medium / long")
    plt.tight_layout()
    plt.savefig(outpath, dpi=160)
    plt.close()


def plot_best_mode_per_model(focus_df: pd.DataFrame, outpath: Path, title_suffix: str) -> None:
    if focus_df.empty:
        return

    usable = focus_df[focus_df["usable_count"] > 0].copy()
    if usable.empty:
        return

    usable = usable.sort_values(
        by=["_run_name", "model_resolved", "rank_score_mean", "wall_mean_s"],
        ascending=[True, True, False, True]
    )
    best = usable.drop_duplicates(subset=["_run_name", "model_resolved"], keep="first").copy()
    best["label"] = best["model_resolved"] + " -> " + best["effective_think_mode"]

    best = best.sort_values("rank_score_mean", ascending=False)

    plt.figure(figsize=(12, max(6, 0.45 * len(best))))
    plt.barh(best["label"], best["rank_score_mean"])
    plt.xlabel("Best rank score by model")
    plt.ylabel("Best mode per model")
    plt.title(f"Best think mode per model {title_suffix}")
    plt.tight_layout()
    plt.savefig(outpath, dpi=160)
    plt.close()


def print_console_summary(focus_df: pd.DataFrame) -> None:
    if focus_df.empty:
        print("No focus rows found.")
        return

    cols = [
        "_run_name", "model_resolved", "effective_think_mode",
        "wall_mean_s", "gen_tps_mean", "quality_mean",
        "usable_ratio", "rejection_reason"
    ]
    printable = focus_df[cols].copy()
    print("\nFocus summary:")
    print(printable.to_string(index=False))


def main():
    parser = argparse.ArgumentParser(description="Visualize one or more Ollama benchmark runs.")
    parser.add_argument(
        "--inputs",
        nargs="+",
        required=True,
        help="One or more benchmark JSON files."
    )
    parser.add_argument(
        "--outdir",
        required=True,
        help="Output directory for CSV and PNG files."
    )
    parser.add_argument(
        "--focus-scenario",
        default="json_agent",
        help="Scenario to focus on for main charts."
    )
    parser.add_argument(
        "--focus-run-kind",
        default="warm",
        help="Run kind to focus on for main charts (warm/cold/concurrent)."
    )
    args = parser.parse_args()

    inputs = [Path(p) for p in args.inputs]
    outdir = Path(args.outdir)
    ensure_outdir(outdir)

    df = load_results(inputs)
    if df.empty:
        raise SystemExit("No results found in the provided files.")

    all_csv = outdir / "all_results_flat.csv"
    save_csv(df, all_csv)

    focus_df = build_focus_table(df, args.focus_scenario, args.focus_run_kind)
    focus_csv = outdir / f"{args.focus_scenario}_{args.focus_run_kind}_summary.csv"
    save_csv(focus_df, focus_csv)

    text_df = build_text_table(df)
    text_csv = outdir / "text_summary.csv"
    save_csv(text_df, text_csv)

    title_suffix = f"({args.focus_scenario}, {args.focus_run_kind}, conc=1)"

    plot_json_scatter(
        focus_df,
        outdir / f"{args.focus_scenario}_{args.focus_run_kind}_quality_vs_latency.png",
        title_suffix
    )
    plot_usable_json_speed(
        focus_df,
        outdir / f"{args.focus_scenario}_{args.focus_run_kind}_usable_speed.png",
        title_suffix
    )
    plot_rank_score(
        focus_df,
        outdir / f"{args.focus_scenario}_{args.focus_run_kind}_rank_score.png",
        title_suffix
    )
    plot_text_quality(
        text_df,
        outdir / "text_quality_overview.png"
    )
    plot_best_mode_per_model(
        focus_df,
        outdir / f"{args.focus_scenario}_{args.focus_run_kind}_best_mode_per_model.png",
        title_suffix
    )

    print_console_summary(focus_df)
    print("\nWrote files to:", outdir)


if __name__ == "__main__":
    main()
