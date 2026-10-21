"""Bake-off metrics (T4 item 9a): per task, per language bucket, for one provider run.

accuracy                      correct answers over ALL cases in the cell. A case with no answer, a rejection or an error is
                              wrong, never missing; an abstention is wrong unless abstaining was the truth.
coverage                      answered over all cases.
expected calibration error    sum over equal-width confidence bins of (n_bin / n) * |accuracy_bin - mean_confidence_bin|,
                              over answered BASE calls; BINS bins on [0, 1], the last one closed. Bins are reported so a
                              reliability diagram can be drawn. `confidence` is the probability of the selected answer
                              (decide()) or the model's self-reported confidence (LLM path): information, never authority.
perturbation flip rate        cases whose answer changes under any meaning-preserving rewrite, over cases with an answered
                              BASE call and at least one answered perturbed call.
latency                       P50 and P95 of BASE calls that returned an answer.
cost per correct outcome      the cost of every call (BASE and perturbed), divided by the number of correct cases, rounded down.

Nothing here is compared to a target: the founders adopt providers and thresholds from the report (D9, AGENT-16).
"""
from typing import Any

from benchmark.bakeoff.cases import BUCKETS, TASKS, cases_digest
from benchmark.scoring.stats import make_rate, percentile

Json = dict[str, Any]

BINS = 10
ABSTENTIONS = frozenset({"none", "other"})
# The confidence at or above which a wrong answer is reported as "confidently wrong" (the evil provider uses 0.97). It labels
# a count in the report; no decision uses it.
CONFIDENTLY_WRONG_REPORT_AT = 0.95
PLACES = 6


def ece_and_bins(samples: list[tuple[float, bool]]) -> tuple[float | None, list[Json]]:
    """Expected calibration error and the reliability bins for (confidence, correct) pairs."""
    counts = [0] * BINS
    confidence_sum = [0.0] * BINS
    correct_sum = [0] * BINS
    for confidence, ok in samples:
        index = min(int(confidence * BINS), BINS - 1)
        counts[index] += 1
        confidence_sum[index] += confidence
        correct_sum[index] += 1 if ok else 0
    bins: list[Json] = []
    error = 0.0
    for i in range(BINS):
        n = counts[i]
        mean_conf = confidence_sum[i] / n if n else None
        accuracy = correct_sum[i] / n if n else None
        bins.append({"lo": round(i / BINS, PLACES), "hi": round((i + 1) / BINS, PLACES), "n": n,
                     "mean_confidence": round(mean_conf, PLACES) if mean_conf is not None else None,
                     "accuracy": round(accuracy, PLACES) if accuracy is not None else None})
        if n and mean_conf is not None and accuracy is not None:
            error += (n / len(samples)) * abs(accuracy - mean_conf)
    return (round(error, PLACES) if samples else None), bins


def _summarise_cell(cases: list[Json], by_case: dict[str, dict[str, Json]]) -> Json:
    n = len(cases)
    correct = answered = abstained = 0
    samples: list[tuple[float, bool]] = []
    confidently_wrong = 0
    flipped = compared = 0
    latencies: list[int] = []
    cost_total = 0
    for case in cases:
        calls = by_case.get(case["case_id"], {})
        cost_total += sum(r["cost_usd_micros"] for r in calls.values())
        base = calls.get("BASE")
        if base is None or base["outcome"] != "ANSWERED":
            continue
        answered += 1
        latencies.append(base["latency_ms"])
        ok = base["selected"] == case["expected"]
        correct += ok
        abstained += base["selected"] in ABSTENTIONS
        samples.append((base["confidence"], ok))
        if not ok and base["confidence"] >= CONFIDENTLY_WRONG_REPORT_AT:
            confidently_wrong += 1
        perturbed = [r for v, r in calls.items() if v != "BASE" and r["outcome"] == "ANSWERED"]
        if perturbed:
            compared += 1
            flipped += any(r["selected"] != base["selected"] for r in perturbed)
    error, bins = ece_and_bins(samples)
    return {
        "n": n,
        "accuracy": make_rate(correct, n),
        "coverage": make_rate(answered, n),
        "abstained": make_rate(abstained, answered),
        "ece": error,
        "reliability": bins,
        "overconfident_wrong": make_rate(confidently_wrong, answered, failure=True),
        "flip_rate": make_rate(flipped, compared, failure=True),
        "latency": {"n": len(latencies), "p50_ms": percentile(latencies, 50), "p95_ms": percentile(latencies, 95)},
        "cost_usd_micros_total": cost_total,
        "cost_usd_micros_per_correct": cost_total // correct if correct else None,
    }


def summarise(cases: list[Json], run: Json) -> Json:
    """{task: {bucket: cell, ..., 'ALL': cell}} for one RAN run. Refuses a run answered against a different case set."""
    if run["status"] != "RAN":
        raise ValueError(f"run {run['run_id']} ({run['provider']}) did not run: {run.get('not_run_reason')}")
    if run["cases_version"] != cases_digest(cases):
        raise ValueError(f"run {run['run_id']} answered a different case set than the one given")
    by_case: dict[str, dict[str, Json]] = {}
    for result in run["results"]:
        calls = by_case.setdefault(result["case_id"], {})
        if result["variant"] in calls:
            raise ValueError(f"two results for {result['case_id']} {result['variant']}")
        calls[result["variant"]] = result
    known = {c["case_id"] for c in cases}
    stray = sorted(set(by_case) - known)
    if stray:
        raise ValueError(f"results for cases that are not in the set: {stray[:5]}")
    summary: Json = {}
    for task in TASKS:
        in_task = [c for c in cases if c["task"] == task]
        summary[task] = {bucket: _summarise_cell([c for c in in_task if c["language_bucket"] == bucket], by_case) for bucket in BUCKETS}
        summary[task]["ALL"] = _summarise_cell(in_task, by_case)
    return summary
