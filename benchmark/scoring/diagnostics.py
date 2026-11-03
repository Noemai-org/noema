"""The four Tier 1 diagnostics of REQ-BEN-009 (BEN-30; DEC-2026-10-31-proof-depth-and-completeness D5, D6): pass^k, turns to resolution, the
unnecessary-clarification rate and auto-resolve precision. Diagnostics, not gates: nothing here sets or applies a threshold (AGENT-16).

The definitions are PRE-REGISTERED (Architect, 2 Nov 2026, before any live run) and published in benchmark/scoring/README.md. A changed definition is a new
DEFINITIONS_VERSION. Pure functions of judgements and result sets: no I/O, no clock. SYNTHETIC test data only until a live run is approved."""
import math
from collections.abc import Sequence
from typing import Any

from benchmark.scoring.judge import Judgement
from benchmark.scoring.stats import make_rate, percentile

Json = dict[str, Any]

DEFINITIONS_VERSION = "2026-11-02"
# Pre-registered: the cases a model decides. B1 Understanding by definition; B2 State was named in the accepted proposal and is a founder review item (SoT §38 lists B2
# as deterministic reconstruction), kept here as one named constant so an amendment is one visible edit and a new DEFINITIONS_VERSION.
UNDERSTANDING_FAMILIES = ("B1", "B2")
PASS_K = 5  # REQ-BEN-009, D6: fixed; a different number of runs is reported as unavailable, never rescaled.


def understanding_dependent(case: Json) -> bool:
    return any(family in UNDERSTANDING_FAMILIES for family in case["families"])


def definitions() -> Json:
    return {"version": DEFINITIONS_VERSION, "understanding_families": list(UNDERSTANDING_FAMILIES)}


def check_runs(primary: Sequence[Json], repeat_runs: Sequence[Sequence[Json]]) -> None:
    """Every result set is one run, and no run is counted twice: a repeated run would inflate pass^k."""
    seen: dict[str, int] = {}
    for index, results in enumerate([primary, *repeat_runs]):
        ids = {r["run_id"] for r in results}
        if len(ids) > 1:
            raise ValueError(f"result set {index + 1} mixes run ids {sorted(ids)}")
        for run_id in ids:
            if run_id in seen:
                raise ValueError(f"run {run_id} appears in result sets {seen[run_id] + 1} and {index + 1}; each of the {PASS_K} runs needs its own run id")
            seen[run_id] = index


def pass_k(cases: Sequence[Json], handled_per_run: Sequence[dict[str, bool]]) -> Json:
    """The share of Understanding-dependent cases handled correctly in every one of exactly PASS_K runs. A case absent from, or errored in, any run fails."""
    runs = len(handled_per_run)
    if runs != PASS_K:
        noun = "run" if runs == 1 else "runs"
        return {"k_required": PASS_K, "runs_supplied": runs, "available": False, "reason": f"{runs} {noun} supplied; pass^k needs exactly {PASS_K}",
                "rate": None, "failing_cases": []}
    dependent = sorted(c["case_id"] for c in cases if understanding_dependent(c))
    failing = [case_id for case_id in dependent if not all(run.get(case_id, False) for run in handled_per_run)]
    return {"k_required": PASS_K, "runs_supplied": runs, "available": True, "reason": None,
            "rate": make_rate(len(dependent) - len(failing), len(dependent)), "failing_cases": failing}


def turns_to_resolution(judgements: Sequence[Judgement]) -> Json:
    """Customer turns over the cases the system auto-resolved correctly: handled, ALLOW, no clarification, no escalation."""
    turns = [j.turns for j in judgements if j.completed and j.handled and j.auto_resolved]
    if not turns:
        return {"n": 0, "mean": None, "median": None, "p90": None, "max": None}
    return {"n": len(turns), "mean": round(math.fsum(turns) / len(turns), 6), "median": percentile(turns, 50), "p90": percentile(turns, 90), "max": max(turns)}


def unnecessary_clarification(judgements: Sequence[Judgement]) -> Json:
    """Of the completed cases that were safe to resolve and expected no clarification, those in which the system asked for one."""
    pool = [j for j in judgements if j.completed and j.clarification_eligible]
    return make_rate(sum(1 for j in pool if j.clarification_asked), len(pool), failure=True)


def auto_resolve_precision(judgements: Sequence[Judgement]) -> Json:
    """Of the completed cases the system auto-resolved, those resolved to the oracle's correct target."""
    pool = [j for j in judgements if j.completed and j.auto_resolved]
    return make_rate(sum(1 for j in pool if j.auto_resolved_correct), len(pool))


