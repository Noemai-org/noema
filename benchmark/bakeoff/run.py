"""Run one provider path over the bake-off cases (T4 item 9a): one code path, the single door being a `Decider`.

The bake-off calls the model service only through `harness.decide()` (the decision path) or `harness.generate()` (the LLM path).
Those are session D's. Until they exist, a path is NOT_RUN with its reason: "no access" when there is no key, and "blocked on
session D" when there is a key but no harness. Nothing is faked: no stub answers, no recorded answers passed off as a run.

A typed rejection from the harness is recorded as REJECTED (reject, never repair), a provider error as ERROR, and the run goes on.
Any other exception is a bug and propagates.
"""
import time
from dataclasses import dataclass
from typing import Any, Callable, Mapping, Protocol


Json = dict[str, Any]
Timer = Callable[[], int]

PROVIDERS = ("LLM", "DECIDE_OPENAI_DECISIONS")


class DeciderError(Exception):
    """The provider path failed (timeout, 5xx, circuit open). Recorded as ERROR."""


class DeciderRejected(DeciderError):
    """The harness rejected the provider's output against the schema or the menu. Recorded as REJECTED."""


class DeciderUnavailable(DeciderError):
    """The path cannot run at all: no key, no harness. The whole run is NOT_RUN."""


@dataclass(frozen=True)
class Answer:
    selected: str
    confidence: float
    cost_usd_micros: int


class Decider(Protocol):
    provider: str

    def ready(self) -> None:
        """Raise DeciderUnavailable if the path cannot run."""

    def model_info(self) -> Json:
        """{'provider', 'name', 'version'}; version is the pinned snapshot or None when none could be pinned."""

    def decide(self, question: Json, text: str, context: Json | None) -> Answer:
        """Answer one typed question about `text`. The case's expected answer is never passed."""


class NotRunDecider:
    def __init__(self, provider: str, reason: str) -> None:
        self.provider, self.reason = provider, reason

    def ready(self) -> None:
        raise DeciderUnavailable(self.reason)

    def model_info(self) -> Json:
        raise DeciderUnavailable(self.reason)

    def decide(self, question: Json, text: str, context: Json | None) -> Answer:
        raise DeciderUnavailable(self.reason)


def get_decider(provider: str, environ: Mapping[str, str]) -> Decider:
    """The decider for a provider path. Today every path is NOT_RUN: there is no vendor key anywhere (decision-model scope
    update), and `harness.decide()` / `harness.generate()` are not merged (blocked on session D). The key is only tested for
    presence, never read into a message."""
    if provider not in PROVIDERS:
        raise ValueError(f"unknown provider {provider!r}; one of {PROVIDERS}")
    if not environ.get("OPENAI_API_KEY"):
        return NotRunDecider(provider, "no access: OPENAI_API_KEY is not set")
    return NotRunDecider(provider, "blocked on session D: harness.decide() and harness.generate() are not available to the benchmark runner")


def _call(decider: Decider, case: Json, text: str, timer: Timer) -> Json:
    question = {k: v for k, v in case["question"].items()}
    started = timer()
    outcome, selected, confidence, cost = "ANSWERED", None, None, 0
    try:
        answer = decider.decide(question, text, case["input"].get("context"))
        cost = answer.cost_usd_micros
        if answer.selected not in question["options"] or not 0.0 <= answer.confidence <= 1.0:
            outcome = "REJECTED"
        else:
            selected, confidence = answer.selected, answer.confidence
    except DeciderRejected:
        outcome = "REJECTED"
    except DeciderError:
        outcome = "ERROR"
    finished = timer()
    return {"outcome": outcome, "selected": selected, "confidence": confidence, "cost_usd_micros": cost,
            "latency_ms": max(0, (finished - started) // 1_000_000)}


def run_provider(cases: list[Json], decider: Decider, *, run_id: str, runner_region: str, timer: Timer = time.monotonic_ns) -> Json:
    from benchmark.bakeoff.cases import cases_digest  # here, not at import: the case generator needs jsonschema and the deciders do not

    digest = cases_digest(cases)
    try:
        decider.ready()
    except DeciderUnavailable as exc:
        return {"run_id": run_id, "provider": decider.provider, "status": "NOT_RUN", "not_run_reason": str(exc)[:300],
                "runner_region": runner_region, "cases_version": digest, "results": []}
    results: list[Json] = []
    for case in sorted(cases, key=lambda c: c["case_id"]):
        variants = [("BASE", case["input"]["text"])] + [(f"P{i}", p["text"]) for i, p in enumerate(case["perturbations"])]
        for variant, text in variants:
            results.append({"case_id": case["case_id"], "variant": variant, **_call(decider, case, text, timer)})
    return {"run_id": run_id, "provider": decider.provider, "status": "RAN", "model": decider.model_info(),
            "runner_region": runner_region, "cases_version": digest, "results": results}
