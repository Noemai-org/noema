"""Builders for bake-off tests: recorded-style run files with a known accuracy and calibration. Test support only; SYNTHETIC."""
from typing import Any, Callable

from benchmark.bakeoff.cases import cases_digest

Json = dict[str, Any]


def wrong_answer(case: Json) -> str:
    return str(next(o for o in case["question"]["options"] if o != case["expected"] and o not in ("none", "other")))


def make_run(
    cases: list[Json],
    provider: str = "LLM",
    *,
    correct: Callable[[Json], bool] = lambda case: True,
    confidence: Callable[[Json, bool], float] = lambda case, ok: 0.9,
    flip: Callable[[Json, int], bool] = lambda case, index: False,
    latency: Callable[[Json], int] = lambda case: 100,
    cost: int = 10,
    region: str = "af-lagos",
    run_id: str = "bakeoff_test",
) -> Json:
    results: list[Json] = []
    for case in cases:
        ok = correct(case)
        selected = case["expected"] if ok else wrong_answer(case)
        results.append({"case_id": case["case_id"], "variant": "BASE", "outcome": "ANSWERED", "selected": selected,
                        "confidence": confidence(case, ok), "latency_ms": latency(case), "cost_usd_micros": cost})
        for index, _ in enumerate(case["perturbations"]):
            flipped = flip(case, index)
            other = wrong_answer(case) if (flipped and selected == case["expected"]) else (case["expected"] if flipped else selected)
            results.append({"case_id": case["case_id"], "variant": f"P{index}", "outcome": "ANSWERED", "selected": other,
                            "confidence": confidence(case, ok), "latency_ms": latency(case), "cost_usd_micros": cost})
    return {
        "run_id": run_id, "provider": provider, "status": "RAN",
        "model": {"provider": "example", "name": "example-model", "version": "2026-01-01"},
        "runner_region": region, "cases_version": cases_digest(cases), "results": results,
    }


def not_run(cases: list[Json], provider: str = "DECIDE_OPENAI_DECISIONS", reason: str = "no access: no API key") -> Json:
    return {"run_id": "bakeoff_test", "provider": provider, "status": "NOT_RUN", "not_run_reason": reason,
            "runner_region": "unknown", "cases_version": cases_digest(cases), "results": []}
