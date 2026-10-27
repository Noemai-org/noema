"""The live model-selection run (benchmark/decisions/2026-10-27-model-selection.md): pilot or full, for both models, one budget.

    python -m benchmark.bakeoff.live_run --phase pilot|full --prior-spent-micros N --out DIR

It is started only by the `bench-live` workflow, whose Environment has a required reviewer; the key is the process environment's
`OPENAI_API_KEY` and nothing here reads it, prints it or writes it. Before any request it checks that the record, the corpus, the
scoring code, the harness configurations and the prompt template are the frozen ones; a difference stops the run with nothing sent.

pilot  the first two base cases of every task and bucket (decision D13), within $2: it measures tokens and cost per call and
       estimates the full corpus. It has no verdict.
full   the whole frozen corpus for each model within $25 in total, then every gate for each model, safety failures listed apart.
"""
import argparse
import hashlib
import json
import sys
from pathlib import Path
from typing import Any, Callable, Mapping

from benchmark.bakeoff import gates
from benchmark.bakeoff.budget import BudgetGuard
from benchmark.bakeoff.cases import BUCKETS, TASKS, generate
from benchmark.bakeoff.freeze import CORPUS_PER_CELL, CORPUS_SEED, corpus_hash, scoring_code_hash
from benchmark.bakeoff.run import run_provider
from benchmark.scoring.stats import percentile

Json = dict[str, Any]
ROOT = Path(__file__).resolve().parents[2]
RECORD = ROOT / "benchmark" / "decisions" / "2026-10-27-model-selection.md"
PILOT_PER_CELL = 2
FETCHED_ON = "2026-10-27"
SOURCES = {"gpt-6-luna": "https://developers.openai.com/api/docs/models/gpt-6-luna",
           "gpt-6.1-terra": "https://developers.openai.com/api/docs/models/gpt-6.1-terra"}
MODELS = (("OPENAI_GPT_6_LUNA", "harness-gpt-6-luna.json"), ("OPENAI_GPT_6_1_TERRA", "harness-gpt-6.1-terra.json"))
KEY_VARIABLE = "OPENAI_API_KEY"


class PreflightError(Exception):
    """A frozen value differs from the one in the record; nothing was sent."""


def frozen_block() -> Json:
    import re
    block = re.search(r"```json frozen\n(.*?)\n```", RECORD.read_text(encoding="utf-8"), re.S)
    if block is None:
        raise PreflightError("the record has no frozen block")
    return dict(json.loads(block.group(1)))


def preflight(frozen: Json, template_digest: str, template_version: int) -> None:
    if corpus_hash() != frozen["corpus"]["corpus_hash"]:
        raise PreflightError("the corpus differs from the frozen one")
    if scoring_code_hash(ROOT) != frozen["scoring_code"]["scoring_code_hash"]:
        raise PreflightError("the scoring code differs from the frozen one")
    if (template_version, template_digest) != (frozen["prompt_template"]["version"], frozen["prompt_template"]["digest"]):
        raise PreflightError("the prompt template differs from the frozen one")
    for path, digest in frozen["harness_configs"].items():
        if hashlib.sha256((ROOT / path).read_bytes()).hexdigest() != digest:
            raise PreflightError(f"{path} differs from the frozen one")


def pilot_cases(cases: list[Json]) -> list[Json]:
    """The first PILOT_PER_CELL base cases of every task and language bucket, in case-id order (decision D13)."""
    chosen: list[Json] = []
    for task in TASKS:
        for bucket in BUCKETS:
            cell = sorted((c for c in cases if c["task"] == task and c["language_bucket"] == bucket), key=lambda c: c["case_id"])
            chosen.extend(cell[:PILOT_PER_CELL])
    return sorted(chosen, key=lambda c: c["case_id"])


def pin_for(decider: Any, frozen: Json, config_path: Path, label: str) -> Json:
    config = json.loads(config_path.read_text(encoding="utf-8"))
    (provider,) = config["providers"].values()
    return {
        "model_id": provider["model_version"], "reported_model_id": decider.reported_model_id, "fetched_on": FETCHED_ON,
        "source": SOURCES[provider["model_version"]], "prompt_template": {**frozen["prompt_template"]},
        "reasoning_effort": provider["reasoning_effort"], "rate_card": {**provider["rate"], "source": SOURCES[provider["model_version"]]},
        "max_output_tokens": provider["max_output_tokens"], "corpus_hash": frozen["corpus"]["corpus_hash"],
        "scoring_code_hash": frozen["scoring_code"]["scoring_code_hash"], "retry_policy": {**frozen["retry_policy"]},
        "harness_config_sha256": hashlib.sha256(config_path.read_bytes()).hexdigest(), "label": label,
    }


def run_one(cases: list[Json], decider: Any, frozen: Json, config_path: Path, label: str, run_id: str, region: str) -> tuple[Json, Json]:
    """One model's run file (with its pin and the stop flags) and its diagnostics."""
    run = run_provider(cases, decider, run_id=run_id, runner_region=region)
    if run["status"] == "RAN":
        run["pin"] = pin_for(decider, frozen, config_path, label)
        run["budget_stopped"] = decider._guard.stopped  # noqa: SLF001  (the run file says whether the guard stopped it)
        run["pin_drift"] = decider.drifted
    diagnostics = {"label": label, "calls": decider.diagnostics.calls, "projection_exceeded": decider.diagnostics.projection_exceeded}
    return run, diagnostics


# --- reports ------------------------------------------------------------------------------------------------------------

def _usd(micros: int | None) -> str:
    return "n/a" if micros is None else f"${micros / 1_000_000:.4f}"


def pilot_summary(runs: list[Json], diagnostics: list[Json], full_calls: int, remaining_micros: int) -> Json:
    models = []
    for run, diag in zip(runs, diagnostics, strict=True):
        if run["status"] != "RAN":
            models.append({"label": diag["label"], "status": run["status"], "not_run_reason": run.get("not_run_reason")})
            continue
        calls = [c for c in diag["calls"][1:]]  # the first is the identifier probe, not a corpus call
        costs = [r["cost_usd_micros"] for r in run["results"]]
        n = len(costs)
        mean = sum(costs) // n if n else None
        models.append({
            "label": diag["label"], "status": "RAN", "model": run["model"], "calls": n,
            "outcomes": {o: sum(1 for r in run["results"] if r["outcome"] == o) for o in ("ANSWERED", "REJECTED", "ERROR")},
            "tokens_in_mean": sum(c["tokens_in"] or 0 for c in calls) // len(calls) if calls else None,
            "tokens_out_mean": sum(c["tokens_out"] or 0 for c in calls) // len(calls) if calls else None,
            "cost_mean_micros": mean, "cost_p95_micros": percentile(costs, 95), "cost_max_micros": max(costs) if costs else None,
            "probe_cost_micros": diag["calls"][0]["cost_micro"] if diag["calls"] else None,
            "projection_exceeded": diag["projection_exceeded"],
            "full_corpus_estimate_micros": None if mean is None else mean * full_calls,
            "full_corpus_upper_micros": None if not costs else (percentile(costs, 95) or 0) * full_calls,
            "budget_stopped": run.get("budget_stopped"), "pin_drift": run.get("pin_drift"),
        })
    estimates = [m["full_corpus_estimate_micros"] for m in models if m.get("full_corpus_estimate_micros") is not None]
    uppers = [m["full_corpus_upper_micros"] for m in models if m.get("full_corpus_upper_micros") is not None]
    complete = len(estimates) == len(models)
    return {"models": models, "full_calls": full_calls, "remaining_budget_micros": remaining_micros,
            "full_estimate_total_micros": sum(estimates) if complete else None, "full_upper_total_micros": sum(uppers) if complete else None,
            "fits_in_remaining_budget": (sum(uppers) <= remaining_micros) if complete else None}


def pilot_markdown(summary: Json) -> str:
    lines = ["## Pilot: measured tokens and cost per call", "",
             "| Model | Calls | Answered / rejected / error | Mean tokens in / out | Mean cost | P95 cost | Max cost | Full corpus (mean x calls) | Full corpus (P95 x calls) |",
             "|---|---|---|---|---|---|---|---|---|"]
    for m in summary["models"]:
        if m["status"] != "RAN":
            lines.append(f"| {m['label']} | not run: {m.get('not_run_reason')} | | | | | | | |")
            continue
        o = m["outcomes"]
        lines.append(f"| {m['label']} | {m['calls']} | {o['ANSWERED']} / {o['REJECTED']} / {o['ERROR']} | {m['tokens_in_mean']} / {m['tokens_out_mean']} | "
                     f"{_usd(m['cost_mean_micros'])} | {_usd(m['cost_p95_micros'])} | {_usd(m['cost_max_micros'])} | "
                     f"{_usd(m['full_corpus_estimate_micros'])} | {_usd(m['full_corpus_upper_micros'])} |")
    lines += ["", f"Full corpus per model: {summary['full_calls']} calls. Estimate for both models: {_usd(summary['full_estimate_total_micros'])} "
              f"(mean) to {_usd(summary['full_upper_total_micros'])} (P95 per call); remaining budget {_usd(summary['remaining_budget_micros'])}; "
              f"fits: {summary['fits_in_remaining_budget']}."]
    return "\n".join(lines) + "\n"


def full_markdown(evaluations: list[Json], chosen: Json, spent_micros: int) -> str:
    lines = ["## Full run: every gate, per model", "", f"Spent in total (all phases): {_usd(spent_micros)} of $25.00.", ""]
    for e in evaluations:
        label = (e.get("model") or {}).get("name") or "?"
        lines += [f"### {label}: {e['verdict']}", ""]
        if e["verdict"] == "NOT_RUN":
            lines += [f"Not run: {e.get('not_run_reason')}", ""]
            continue
        lines += ["| Gate | Result | Numbers |", "|---|---|---|"]
        for g in e["gates"]:
            numbers = ", ".join(f"{k}={v}" for k, v in g.items() if k not in ("gate", "passed"))
            lines.append(f"| {g['gate']} | {'pass' if g['passed'] else 'FAIL'} | {numbers} |")
        lines.append("")
        lines += [f"**Safety failures ({len(e['safety_failures'])})**", ""]
        lines += [f"- {f['case_id']} {f['variant']}: {f['outcome']}, selected {f['selected']}, expected {f['expected']}" for f in e["safety_failures"]] or ["- none"]
        lines.append("")
    lines += ["### Comparison among models that pass every gate", "", f"{chosen['note']} Models passing every gate: {chosen['models_passing_every_gate']}.", ""]
    for row in chosen["comparison"]:
        lines.append(f"- {(row['model'] or {}).get('name')}: classification {row['classification_correct']}/{row['classification_of']}, evidence "
                     f"{row['evidence_correct']}/{row['evidence_of']}, P95 {row['p95_ms']} ms, mean cost {_usd(row['mean_cost_micros'])} per call")
    return "\n".join(lines) + "\n"


# --- the command ----------------------------------------------------------------------------------------------------

Factory = Callable[[str, str, BudgetGuard], Any]


def run_phase(phase: str, prior_spent_micros: int, out: Path, environ: Mapping[str, str], factory: Factory, template: tuple[int, str]) -> int:
    frozen = frozen_block()
    preflight(frozen, template[1], template[0])
    if not environ.get(KEY_VARIABLE):
        sys.stderr.write(f"not run: no access: {KEY_VARIABLE} is not set in the environment\n")
        return 3
    cases = generate(CORPUS_SEED, CORPUS_PER_CELL)
    selected = pilot_cases(cases) if phase == "pilot" else cases
    limit = gates.BUDGET_PILOT_MICROS if phase == "pilot" else gates.BUDGET_TOTAL_MICROS
    guard = BudgetGuard(limit_micros=limit, spent_micros=prior_spent_micros)
    out.mkdir(parents=True, exist_ok=True)
    runs, diagnostics = [], []
    for label, file_name in MODELS:
        path = ROOT / "benchmark" / "decisions" / file_name
        decider = factory(label, str(path), guard)
        run, diag = run_one(selected, decider, frozen, path, label, f"{phase}-{label.lower()}", "ci-ubuntu")
        runs.append(run)
        diagnostics.append(diag)
        (out / f"run-{label}.json").write_text(json.dumps(run, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        (out / f"diagnostics-{label}.json").write_text(json.dumps(diag, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    if phase == "pilot":
        summary = pilot_summary(runs, diagnostics, sum(1 + len(c["perturbations"]) for c in cases), gates.BUDGET_TOTAL_MICROS - guard.spent_micros)
        text = pilot_markdown(summary)
    else:
        gate_frozen = {"corpus_hash": frozen["corpus"]["corpus_hash"], "template_digest": frozen["prompt_template"]["digest"],
                       "scoring_code_hash": frozen["scoring_code"]["scoring_code_hash"], "retry_policy": frozen["retry_policy"],
                       "reasoning_effort": frozen["reasoning_effort"]}
        evaluations = [gates.evaluate(cases, run, gate_frozen) for run in runs]
        summary = {"evaluations": evaluations, "selection": gates.select(evaluations), "spent_micros": guard.spent_micros}
        text = full_markdown(evaluations, summary["selection"], guard.spent_micros)
    summary["spent_micros"] = guard.spent_micros
    (out / "summary.json").write_text(json.dumps(summary, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    (out / "summary.md").write_text(text, encoding="utf-8")
    sys.stdout.write(text + f"\nspent_micros_total={guard.spent_micros}\n")
    return 0


def main(argv: list[str], environ: Mapping[str, str]) -> int:
    parser = argparse.ArgumentParser(prog="python -m benchmark.bakeoff.live_run", description=__doc__)
    parser.add_argument("--phase", choices=("pilot", "full"), required=True)
    parser.add_argument("--prior-spent-micros", type=int, required=True, help="spending already made against the same cap; 0 for the first pilot")
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args(argv)
    if args.prior_spent_micros < 0:
        parser.error("--prior-spent-micros must not be negative")
    if "benchmark-heldout" in " ".join(argv) or any("benchmark-heldout" in v for v in environ.values()):
        sys.stderr.write("refused: nothing here reads benchmark-heldout\n")
        return 2
    from importlib import import_module

    from benchmark.bakeoff.live import build_real_decider
    templates = import_module("noema_model.harness.templates")

    def factory(label: str, path: str, guard: BudgetGuard) -> Any:
        return build_real_decider(label, path, dict(environ), guard)

    return run_phase(args.phase, args.prior_spent_micros, args.out, environ, factory, (templates.DECIDE_V2.version, templates.DECIDE_V2.digest))


if __name__ == "__main__":
    import os
    sys.exit(main(sys.argv[1:], os.environ))
