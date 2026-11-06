"""UND-13, REQ-INT-006 (DEC-2026-10-31 D25, AGENT-28): the model-drift canary.

It sends the qualification subset through the service's own provider adapters and writes the run record the benchmark module defines (`qualification-run.v1`).
`fixtures/qualification/canary_golden.json` holds a SYNTHETIC subset, baseline and five scenarios with the run records the benchmark module's `compare` produced; the canary
must produce each one exactly. Generated layer only: a held-out or partner case is refused (INV-22).
"""

import asyncio
import json
from collections.abc import Mapping
from typing import Any

import pytest

from noema_model.providers.llm.base import (
    AdapterSettings,
    HttpResponse,
    ProviderRequest,
)
from noema_model.providers.llm.openai_chat import OpenAIChat
from noema_model.harness.canary_asker import ProviderAsker
from noema_model.qualification.canary import AskerUnavailable, CaseAnswer, run_canary
from tests.support import FIXTURES

GOLDEN: dict[str, Any] = json.loads((FIXTURES / "qualification" / "canary_golden.json").read_text())
SCENARIOS = GOLDEN["scenarios"]
SUBSET, BASELINE, PINNED = GOLDEN["subset"], GOLDEN["baseline"], GOLDEN["pinned"]
TIMEOUT_S = 5.0  # test value; the service reads its own from configuration (AGENT-16)


class ScriptedAsker:
    """Plays the provider's answers from a scenario; no model is called."""

    def __init__(self, answers: Mapping[str, Any]) -> None:
        self._answers = answers

    async def ask(self, case: Mapping[str, Any]) -> CaseAnswer | None:
        a = self._answers[case["case_id"]]
        return None if a is None else CaseAnswer(slots=a["slots"], answered_by=a["answered_by"])


def canary(asker: Any, **over: Any) -> dict[str, Any]:
    args: dict[str, Any] = {"subset": SUBSET, "baseline": BASELINE, "pinned": PINNED, "asker": asker, "commit_sha": GOLDEN["commit_sha"],
                            "recorded_on": GOLDEN["recorded_on"], "timeout_s": TIMEOUT_S}
    args.update(over)
    return asyncio.run(run_canary(**args))


@pytest.mark.parametrize("name", sorted(SCENARIOS))
def test_the_run_record_is_exactly_the_benchmark_modules_for_each_scenario(name: str) -> None:
    assert canary(ScriptedAsker(SCENARIOS[name]["answers"])) == SCENARIOS[name]["run"]


def test_results_are_pass_drift_and_provider_unavailable_with_drift_outranking() -> None:
    results = {n: canary(ScriptedAsker(s["answers"]))["result"] for n, s in SCENARIOS.items()}
    assert results == {"pass": "PASS", "drift_slots": "DRIFT", "drift_version_only": "DRIFT", "provider_unavailable": "PROVIDER_UNAVAILABLE", "drift_outranks_unavailable": "DRIFT"}


def test_a_version_that_answers_under_another_version_is_drift_even_when_every_slot_matches() -> None:
    run = canary(ScriptedAsker(SCENARIOS["drift_version_only"]["answers"]))
    mismatched = [c for c in run["cases"] if c["version_mismatch"]]
    assert len(mismatched) == 1 and mismatched[0]["differing_slots"] == [] and mismatched[0]["status"] == "DRIFT"


class RaisingAsker:
    def __init__(self, exc: Exception) -> None:
        self._exc = exc

    async def ask(self, case: Mapping[str, Any]) -> CaseAnswer | None:
        raise self._exc


def test_a_provider_error_is_unavailable_but_a_bug_in_the_canary_is_not_hidden() -> None:
    run = canary(RaisingAsker(AskerUnavailable("timeout")))
    assert run["result"] == "PROVIDER_UNAVAILABLE" and {c["status"] for c in run["cases"]} == {"UNAVAILABLE"}
    with pytest.raises(KeyError):
        canary(RaisingAsker(KeyError("bug")))


class HangingAsker:
    async def ask(self, case: Mapping[str, Any]) -> CaseAnswer | None:
        await asyncio.sleep(3600)
        return None


def test_a_call_that_never_returns_is_unavailable_after_the_deadline_not_a_hang() -> None:
    run = canary(HangingAsker(), timeout_s=0.01)
    assert run["result"] == "PROVIDER_UNAVAILABLE"


def test_a_held_out_or_partner_case_in_the_subset_is_refused_and_nothing_is_asked() -> None:
    leaked = json.loads(json.dumps(SUBSET))
    leaked["cases"][0]["suite_layer"] = "HELD_OUT"
    asked: list[str] = []

    class Spy:
        async def ask(self, case: Mapping[str, Any]) -> CaseAnswer | None:
            asked.append(case["case_id"])
            return None

    with pytest.raises(ValueError, match="INV-22"):
        canary(Spy(), subset=leaked)
    assert asked == []
    wrong_layer = {**SUBSET, "layer": "HELD_OUT"}
    with pytest.raises(ValueError, match="INV-22"):
        canary(Spy(), subset=wrong_layer)


def test_an_unpinned_version_or_a_baseline_for_another_model_or_subset_is_refused() -> None:
    ok = ScriptedAsker(SCENARIOS["pass"]["answers"])
    with pytest.raises(ValueError, match="pinned"):
        canary(ok, pinned={**PINNED, "version": "latest"})
    with pytest.raises(ValueError, match="different subset"):
        canary(ok, baseline={**BASELINE, "cases_sha256": "sha256:" + "0" * 64})
    with pytest.raises(ValueError, match="gpt-other"):
        canary(ok, pinned={**PINNED, "model_id": "gpt-other"})


class FakeTransport:
    def __init__(self, body: dict[str, Any] | None, status: int = 200) -> None:
        self.body, self.status, self.calls = body, status, 0

    async def post(self, url: str, headers: Mapping[str, str], body: bytes, timeout_s: float, max_bytes: int) -> HttpResponse:
        self.calls += 1
        return HttpResponse(self.status, json.dumps(self.body).encode())


def chat_body(content: str, model: str = "2026-11-01") -> dict[str, Any]:
    return {"model": model, "choices": [{"message": {"content": content}, "finish_reason": "stop"}], "usage": {"prompt_tokens": 1, "completion_tokens": 1}}


def asker_over(transport: FakeTransport) -> ProviderAsker:
    settings = AdapterSettings(endpoint="https://provider.invalid", credential="not-a-secret", api_version=None, timeout_ms=1000, max_response_bytes=65536)
    schema = {"type": "object", "properties": {}, "additionalProperties": True}

    def build(case: Mapping[str, Any]) -> ProviderRequest:
        return ProviderRequest(model_version=PINNED["version"], system="SYNTHETIC", user=str(case["case_id"]), schema_name="slots", output_schema=schema,
                               max_output_tokens=256, temperature=0.0)

    return ProviderAsker(OpenAIChat("openai", settings, transport), build)


def test_through_the_real_adapter_the_reported_version_and_the_structured_slots_become_the_answer() -> None:
    slots = {"intent": "dispute_charge", "amount_minor": 1000, "currency": "NGN"}
    answer = asyncio.run(asker_over(FakeTransport(chat_body(json.dumps(slots)))).ask(SUBSET["cases"][0]))
    assert answer == CaseAnswer(slots=slots, answered_by="2026-11-01")


def test_a_reply_with_no_usable_structured_answer_is_unavailable_and_never_drift_or_a_guess() -> None:
    for content in ("not json", "[1, 2]", '"text"'):
        assert asyncio.run(asker_over(FakeTransport(chat_body(content))).ask(SUBSET["cases"][0])) is None


def test_a_reply_that_does_not_report_its_version_counts_as_another_version() -> None:
    body = chat_body(json.dumps({"intent": "x"}))
    del body["model"]
    try:
        answer = asyncio.run(asker_over(FakeTransport(body)).ask(SUBSET["cases"][0]))
    except AskerUnavailable:
        return  # the adapter itself refuses an unreported version: also not a silent pass
    assert answer is not None and answer.answered_by != PINNED["version"]


def test_a_transport_error_surfaces_as_asker_unavailable_for_the_canary_to_record() -> None:
    with pytest.raises(AskerUnavailable):
        asyncio.run(asker_over(FakeTransport({"error": "x"}, status=503)).ask(SUBSET["cases"][0]))


def test_a_boolean_in_place_of_a_number_is_drift_not_a_match() -> None:
    # Codex review of noema#215: in Python True == 1, so a type change must be compared by JSON type.
    answers = json.loads(json.dumps(SCENARIOS["pass"]["answers"]))
    first = next(iter(answers))
    answers[first]["slots"]["amount_minor"] = True  # the baseline has the number 1000
    run = canary(ScriptedAsker(answers))
    assert run["result"] == "DRIFT"
    changed = [c for c in run["cases"] if c["case_id"] == first][0]
    assert [d["slot"] for d in changed["differing_slots"]] == ["amount_minor"]


def test_nested_values_are_compared_by_json_type_too_and_equal_numbers_still_match() -> None:
    baseline = json.loads(json.dumps(BASELINE))
    answers = json.loads(json.dumps(SCENARIOS["pass"]["answers"]))
    first = next(iter(answers))
    baseline["cases"][first]["slots"]["nested"] = {"flag": 1, "xs": [1, 0]}
    answers[first]["slots"]["nested"] = {"flag": True, "xs": [1, 0]}
    assert canary(ScriptedAsker(answers), baseline=baseline)["result"] == "DRIFT"
    answers[first]["slots"]["nested"] = {"flag": 1.0, "xs": [1, 0]}  # 1 and 1.0 are the same JSON number
    assert canary(ScriptedAsker(answers), baseline=baseline)["result"] == "PASS"


def test_a_subset_whose_contents_no_longer_match_its_header_is_refused_before_any_call() -> None:
    # Codex review of noema#215: the canary compared only declared digests, so a truncated subset reported the stale count and could PASS.
    asked: list[str] = []

    class Spy:
        async def ask(self, case: Mapping[str, Any]) -> CaseAnswer | None:
            asked.append(case["case_id"])
            return CaseAnswer(slots={}, answered_by=PINNED["version"])

    def tampered(change: Any) -> dict[str, Any]:
        s: dict[str, Any] = json.loads(json.dumps(SUBSET))
        change(s)
        return s

    for name, subset in {
        "a case removed, header unchanged": tampered(lambda s: s["cases"].pop()),
        "a case edited, digest unchanged": tampered(lambda s: s["cases"][0].update(utterance="changed")),
        "n_cases wrong": tampered(lambda s: s.update(n_cases=s["n_cases"] + 1)),
        "case_ids not the cases": tampered(lambda s: s.update(case_ids=list(reversed(s["case_ids"])))),
        "an extra case id": tampered(lambda s: s["case_ids"].append("zzz-extra")),
    }.items():
        with pytest.raises(ValueError, match="subset"):
            canary(Spy(), subset=subset)
        assert asked == [], f"{name}: a provider was called"


def test_a_baseline_that_does_not_cover_exactly_the_subset_is_refused() -> None:
    ok = ScriptedAsker(SCENARIOS["pass"]["answers"])
    short = json.loads(json.dumps(BASELINE))
    short["cases"].pop(next(iter(short["cases"])))
    with pytest.raises(ValueError, match="baseline"):
        canary(ok, baseline=short)
    extra = json.loads(json.dumps(BASELINE))
    extra["cases"]["zzz-extra"] = next(iter(extra["cases"].values()))
    with pytest.raises(ValueError, match="baseline"):
        canary(ok, baseline=extra)
