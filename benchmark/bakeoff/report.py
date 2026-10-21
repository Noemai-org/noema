"""The bake-off comparison, the per-task table and the reliability diagrams (T4 item 9a).

Two paths are compared for each of the six understanding tasks: the LLM path and `decide()` backed by the OpenAI Decisions API
(decision-model scope update). The report is a mechanical comparison. It sets no threshold and adopts nothing: the founders
adopt providers and the calibration limit from it (D9, AGENT-16).

Recommendation, per task, from numbers alone:
  NOT_RUN                      a path did not run; the reason is printed. Nothing is substituted.
  ELIGIBLE_FOR_FOUNDER_REVIEW  in EVERY language bucket, decide() has strictly higher accuracy than the LLM path AND an expected
                               calibration error no higher than the LLM path's (PERD §19.1: adopt only where it beats the LLM on
                               accuracy and stays calibrated on en-NG, pcm, code-switched text and voice transcripts, measured
                               separately).
  KEEP_LLM                     otherwise, with the buckets that failed named.
Whether the calibration is good enough in absolute terms is a limit only the founders set; none is encoded here.
"""
import math
from typing import Any

from benchmark.bakeoff.cases import BUCKETS, TASKS, cases_digest
from benchmark.bakeoff.metrics import summarise

Json = dict[str, Any]

LLM = "LLM"
DECIDE = "DECIDE_OPENAI_DECISIONS"
PROVIDERS = (LLM, DECIDE)


def _disjoint(a: Json, b: Json) -> bool | None:
    if a["wilson95"] is None or b["wilson95"] is None:
        return None
    return bool(a["wilson95"][1] < b["wilson95"][0] or b["wilson95"][1] < a["wilson95"][0])


def _diff(a: float | None, b: float | None) -> float | None:
    return None if a is None or b is None else round(a - b, 6)


def _compare_cell(llm: Json, decide: Json) -> Json:
    return {
        "accuracy_difference": _diff(decide["accuracy"]["rate"], llm["accuracy"]["rate"]),
        "significant": _disjoint(decide["accuracy"], llm["accuracy"]),
        "ece_difference": _diff(decide["ece"], llm["ece"]),
        "flip_rate_difference": _diff(decide["flip_rate"]["rate"], llm["flip_rate"]["rate"]),
        "p50_ms_difference": _diff(decide["latency"]["p50_ms"], llm["latency"]["p50_ms"]),
        "cost_per_correct_difference": _diff(decide["cost_usd_micros_per_correct"], llm["cost_usd_micros_per_correct"]),
    }


def _recommend(task: str, runs: dict[str, Json], comparison: Json) -> tuple[str, str]:
    missing = [f"{p} ({runs[p].get('not_run_reason', 'no result')})" for p in PROVIDERS if runs[p]["status"] != "RAN"]
    if missing:
        return "NOT_RUN", "not run: " + "; ".join(missing)
    failed: list[str] = []
    for bucket in BUCKETS:
        c = comparison[bucket]
        if c["accuracy_difference"] is None or c["ece_difference"] is None:
            failed.append(f"{bucket} (no data to compare)")
        elif c["accuracy_difference"] <= 0:
            failed.append(f"{bucket} (accuracy not higher)")
        elif c["ece_difference"] > 0:
            failed.append(f"{bucket} (calibration error higher)")
    if failed:
        return "KEEP_LLM", "decide() does not beat the LLM path in: " + ", ".join(failed)
    return "ELIGIBLE_FOR_FOUNDER_REVIEW", "decide() has higher accuracy and no higher calibration error in every language bucket"


def compare(cases: list[Json], runs: list[Json]) -> Json:
    """Compare the two paths. Exactly one run per provider is required (a NOT_RUN run counts)."""
    by_provider: dict[str, Json] = {}
    for run in runs:
        if run["provider"] in by_provider:
            raise ValueError(f"two runs for {run['provider']}")
        by_provider[run["provider"]] = run
    if set(by_provider) != set(PROVIDERS):
        raise ValueError(f"need one run each for {PROVIDERS}; got {sorted(by_provider)}")
    for run in runs:
        if run["cases_version"] != cases_digest(cases):
            raise ValueError(f"run {run['run_id']} ({run['provider']}) answered a different case set than the one given")
    summaries: dict[str, Json | None] = {p: (summarise(cases, by_provider[p]) if by_provider[p]["status"] == "RAN" else None) for p in PROVIDERS}
    tasks: Json = {}
    for task in TASKS:
        buckets: Json = {}
        comparison: Json = {}
        for bucket in (*BUCKETS, "ALL"):
            cells = {p: (s[task][bucket] if (s := summaries[p]) is not None else None) for p in PROVIDERS}
            buckets[bucket] = cells
            left, right = cells[LLM], cells[DECIDE]
            comparison[bucket] = _compare_cell(left, right) if left is not None and right is not None else None
        recommendation, reason = _recommend(task, by_provider, comparison)
        tasks[task] = {"buckets": buckets, "comparison": comparison, "recommendation": recommendation, "reason": reason}
    return {
        "cases": {"n": len(cases), "digest": cases_digest(cases), "generator": cases[0]["generator"] if cases else None},
        "runs": {p: {k: by_provider[p].get(k) for k in ("run_id", "status", "not_run_reason", "model", "runner_region")} for p in PROVIDERS},
        "tasks": tasks,
    }


# --- markdown -----------------------------------------------------------------------------------------------------

def _pct(v: float) -> str:
    return f"{v * 100:.2f}%"


def _rate(r: Json) -> str:
    if r["n"] == 0:
        return "0 of 0"
    lo, hi = r["wilson95"]
    return f"{r['k']} of {r['n']} ({_pct(r['rate'])}; 95% Wilson {_pct(lo)} to {_pct(hi)})"


def _ms(v: int | None) -> str:
    return "n/a" if v is None else f"{v} ms"


def render_markdown(result: Json) -> str:
    gen = result["cases"]["generator"]
    out: list[str] = [
        "# Decision-model bake-off: the LLM path against decide() backed by OpenAI Decisions", "",
        "This is a mechanical comparison of two paths on synthetic, generated cases. It changes no threshold and adopts nothing. "
        "The founders adopt providers and set the calibration limit from it (D9, AGENT-16). Confidence is information, never "
        "authority.", "",
        f"Cases: {result['cases']['n']}, from {gen['name']} {gen['version']} seed {gen['seed']} (case set {result['cases']['digest'][:19]}). "
        "Nigerian-language phrasing is template text not yet reviewed by native speakers.", "",
        "## Runs", "", "| Path | Status | Model | Pinned version | Runner region | Lagos-region measurement |", "|---|---|---|---|---|---|",
    ]
    for p in PROVIDERS:
        run = result["runs"][p]
        if run["status"] == "RAN":
            model = run["model"]
            pinned = model["version"] or "NOT PINNED"
            lagos = "yes" if "lagos" in run["runner_region"] else f"no (runner region {run['runner_region']})"
            out.append(f"| {p} | RAN | {model['provider']} {model['name']} | {pinned} | {run['runner_region']} | Lagos-region measurement: {lagos} |")
        else:
            out.append(f"| {p} | NOT RUN | not run: {run['not_run_reason']} | n/a | n/a | n/a |")
    out += ["", "## Per-task recommendation (not a decision)", "",
            "| Task | LLM accuracy (all buckets) | decide() accuracy (all buckets) | Comparison | Why |", "|---|---|---|---|---|"]
    for task in TASKS:
        t = result["tasks"][task]
        llm, dec = t["buckets"]["ALL"][LLM], t["buckets"]["ALL"][DECIDE]
        out.append(f"| {task} | {_rate(llm['accuracy']) if llm else 'not run'} | {_rate(dec['accuracy']) if dec else 'not run'} | "
                   f"{t['recommendation']} | {t['reason']} |")
    out += ["", "ELIGIBLE_FOR_FOUNDER_REVIEW means only this: in every language bucket, decide() has strictly higher accuracy than the LLM "
            "path and an expected calibration error no higher than the LLM path's (PERD §19.1). It is not an adoption, and no "
            "absolute calibration limit is applied because none has been set. KEEP_LLM and NOT_RUN keep today's path.", ""]
    for task in TASKS:
        out += [f"## {task}", "",
                "| Bucket | Path | Accuracy | Coverage | ECE | Flip rate | P50 | P95 | Cost per correct (millionths of USD) |",
                "|---|---|---|---|---|---|---|---|---|"]
        for bucket in (*BUCKETS, "ALL"):
            for p in PROVIDERS:
                cell = result["tasks"][task]["buckets"][bucket][p]
                if cell is None:
                    out.append(f"| {bucket} | {p} | not run | | | | | | |")
                    continue
                ece = "n/a" if cell["ece"] is None else f"{cell['ece']:.4f}"
                cost = "n/a" if cell["cost_usd_micros_per_correct"] is None else str(cell["cost_usd_micros_per_correct"])
                out.append(f"| {bucket} | {p} | {_rate(cell['accuracy'])} | {_rate(cell['coverage'])} | {ece} | {_rate(cell['flip_rate'])} | "
                           f"{_ms(cell['latency']['p50_ms'])} | {_ms(cell['latency']['p95_ms'])} | {cost} |")
        out.append("")
    out += ["## How to read this", "",
            "- Accuracy counts a case with no answer, a rejection or an error as wrong, and an abstention as wrong unless abstaining was right.",
            "- ECE is the expected calibration error over equal-width confidence bins on answered calls; the reliability diagrams are in the "
            "`reliability_<TASK>.svg` files.",
            "- The flip rate is the share of cases whose answer changes under a meaning-preserving rewrite.",
            "- Latency is of answered base calls. It is a Lagos-region measurement only where the table says so.",
            "- Cost counts every call, base and perturbed.", ""]
    return "\n".join(out)


# --- reliability diagram ------------------------------------------------------------------------------------------

COLOURS = {LLM: "#1f6feb", DECIDE: "#bc4c00"}
DASH = {LLM: "", DECIDE: "6 3"}


def reliability_svg(result: Json, task: str) -> str:
    """One diagram per task: accuracy against confidence over all buckets, a curve per path that ran. Plain SVG, no script."""
    size, pad = 360, 40
    span = size - 2 * pad

    def x(v: float) -> float:
        return round(pad + v * span, 2)

    def y(v: float) -> float:
        return round(size - pad - v * span, 2)

    parts = [f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {size} {size}" width="{size}" height="{size}" role="img" '
             f'aria-labelledby="t">', f"<title id=\"t\">{task} reliability</title>",
             f'<rect x="0" y="0" width="{size}" height="{size}" fill="#ffffff"/>',
             f'<line x1="{x(0)}" y1="{y(0)}" x2="{x(1)}" y2="{y(1)}" stroke="#8c959f" stroke-width="1" stroke-dasharray="2 2"/>',
             f'<line x1="{x(0)}" y1="{y(0)}" x2="{x(1)}" y2="{y(0)}" stroke="#24292f"/>',
             f'<line x1="{x(0)}" y1="{y(0)}" x2="{x(0)}" y2="{y(1)}" stroke="#24292f"/>',
             f'<text x="{x(0.5)}" y="{size - 8}" font-size="11" text-anchor="middle" fill="#24292f">confidence</text>',
             f'<text x="12" y="{y(0.5)}" font-size="11" text-anchor="middle" fill="#24292f" transform="rotate(-90 12 {y(0.5)})">accuracy</text>']
    legend_y = 16
    for p in PROVIDERS:
        cell = result["tasks"][task]["buckets"]["ALL"][p]
        if cell is None:
            continue
        points = [(b["mean_confidence"], b["accuracy"]) for b in cell["reliability"] if b["n"]]
        dash = f' stroke-dasharray="{DASH[p]}"' if DASH[p] else ""
        if len(points) > 1:
            coords = " ".join(f"{x(cx)},{y(ay)}" for cx, ay in points)
            parts.append(f'<polyline points="{coords}" fill="none" stroke="{COLOURS[p]}" stroke-width="2"{dash}/>')
        for cx, ay in points:
            parts.append(f'<circle cx="{x(cx)}" cy="{y(ay)}" r="3.5" fill="{COLOURS[p]}"/>')
        parts.append(f'<line x1="{pad}" y1="{legend_y}" x2="{pad + 24}" y2="{legend_y}" stroke="{COLOURS[p]}" stroke-width="2"{dash}/>')
        label = f"{p} (ECE {cell['ece']:.3f})" if cell["ece"] is not None and not math.isnan(cell["ece"]) else p
        parts.append(f'<text x="{pad + 30}" y="{legend_y + 4}" font-size="11" fill="#24292f">{label}</text>')
        legend_y += 14
    parts.append("</svg>")
    return "\n".join(parts) + "\n"
