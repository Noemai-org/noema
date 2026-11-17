"""UND-13, REQ-INT-006 (DEC-2026-10-31 D25, AGENT-28): the model-drift canary.

It sends the qualification subset through the service's own provider adapters and writes the run record the benchmark module defines (`qualification-run.v1`).
`fixtures/qualification/canary_golden.json` holds a SYNTHETIC subset, baseline and five scenarios with the run records the benchmark module's `compare` produced; the canary
must produce each one exactly. Generated layer only: a held-out or partner case is refused (INV-22).
"""

import asyncio
import hashlib
import json
from collections.abc import Callable, Mapping
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
    from noema_model.qualification import canary as module

    baseline["cases"][first]["slots"]["nested"] = {"flag": 1, "xs": [1, 0]}
    baseline["cases"][first]["slots_sha256"] = module._digest(baseline["cases"][first]["slots"])  # the entry stays self-consistent
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


def _digest_of(document: Any) -> str:
    return "sha256:" + hashlib.sha256(json.dumps(document, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode("utf-8")).hexdigest()


def _subset_with(change: Callable[[dict[str, Any]], Any], *, rehash: bool = True) -> tuple[dict[str, Any], dict[str, Any]]:
    """A subset (and the baseline taken on it) after `change`, with the digest recomputed so only the one rule under test is broken: the benchmark module's `subset_problems`
    refuses each of these, and the canary must agree (noema#199 at its merge)."""
    subset: dict[str, Any] = json.loads(json.dumps(SUBSET))
    baseline: dict[str, Any] = json.loads(json.dumps(BASELINE))
    change(subset)
    if rehash:
        subset["cases_sha256"] = _digest_of(subset["cases"])
    baseline["cases_sha256"], baseline["subset_id"] = subset.get("cases_sha256", SUBSET["cases_sha256"]), subset.get("subset_id", SUBSET["subset_id"])
    return subset, baseline


def test_a_subset_the_benchmark_module_refuses_is_refused_by_the_canary_before_any_call() -> None:
    asked: list[str] = []

    class Spy:
        async def ask(self, case: Mapping[str, Any]) -> CaseAnswer | None:
            asked.append(str(case.get("case_id")))
            return CaseAnswer(slots={}, answered_by=PINNED["version"])

    def empty_id(s: dict[str, Any]) -> None:
        s["cases"][0]["case_id"] = ""
        s["case_ids"][0] = ""

    def not_an_object(s: dict[str, Any]) -> None:
        s["cases"][0] = "x"

    def ids_resorted(s: dict[str, Any]) -> None:
        s["cases"].reverse()
        s["case_ids"].reverse()

    refused: dict[str, Callable[[dict[str, Any]], Any]] = {
        "another schema": lambda s: s.update(schema="qualification-subset.v2"),
        "no schema": lambda s: s.pop("schema"),
        "a subset id with surrounding space": lambda s: s.update(subset_id=" qs-synthetic "),
        "an empty subset id": lambda s: s.update(subset_id=""),
        "a subset id that is not a string": lambda s: s.update(subset_id=7),
        "n_cases a float equal to the count": lambda s: s.update(n_cases=float(len(s["cases"]))),
        "n_cases a bool": lambda s: s.update(n_cases=True),
        "a case with no case id": lambda s: s["cases"][0].pop("case_id"),
        "a case id that is not a string": lambda s: s["cases"][0].update(case_id=5),
        "a case id that is the empty string in the documents and the id list": empty_id,
        "a case document that is not an object": not_an_object,
        "cases not a list": lambda s: s.update(cases={}),
        "case ids that are not sorted": ids_resorted,
        "a case twice": lambda s: (s["cases"].append(json.loads(json.dumps(s["cases"][-1]))), s["case_ids"].append(s["case_ids"][-1]), s.update(n_cases=len(s["cases"]))),
        "no layer": lambda s: s.pop("layer"),
        "no cases_sha256": lambda s: s.pop("cases_sha256"),
    }
    for name, change in refused.items():
        rehash = name not in {"no cases_sha256", "cases not a list", "a case document that is not an object"}
        subset, baseline = _subset_with(change, rehash=rehash)
        with pytest.raises(ValueError, match="INV-22|the subset is not consistent"):  # the subset's own check, not a later one that happens to refuse the baseline
            canary(Spy(), subset=subset, baseline=baseline)
        assert asked == [], f"{name}: a provider was called"


def test_a_baseline_with_a_member_missing_is_refused_before_any_call_not_after_the_last() -> None:
    asked: list[str] = []

    class Spy:
        async def ask(self, case: Mapping[str, Any]) -> CaseAnswer | None:
            asked.append(case["case_id"])
            return CaseAnswer(slots={}, answered_by=PINNED["version"])

    for member in ("subset_id", "cases_sha256", "pinned", "commit_sha", "cases"):
        broken = json.loads(json.dumps(BASELINE))
        broken.pop(member)
        with pytest.raises(ValueError, match="baseline"):
            canary(Spy(), baseline=broken)
        assert asked == [], f"a baseline with no {member}: a provider was called"


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


def test_a_baseline_case_whose_slots_no_longer_match_its_digest_is_refused() -> None:
    # Codex review of noema#215 (round four): the stored digest is copied into the audit record, so it must be the digest of the slots compared.
    ok = ScriptedAsker(SCENARIOS["pass"]["answers"])
    bad = json.loads(json.dumps(BASELINE))
    first = next(iter(bad["cases"]))
    bad["cases"][first]["slots"]["amount_minor"] = 1
    with pytest.raises(ValueError, match="baseline"):
        canary(ok, baseline=bad)
    truncated = json.loads(json.dumps(BASELINE))
    truncated["cases"][first]["slots"] = {}
    with pytest.raises(ValueError, match="baseline"):
        canary(ok, baseline=truncated)


@pytest.mark.parametrize("version", ["latest", "stable-2", "alpha", ""])
def test_the_baseline_must_name_a_pinned_version_but_may_differ_from_the_current_one(version: str) -> None:
    ok = ScriptedAsker(SCENARIOS["pass"]["answers"])
    bad = json.loads(json.dumps(BASELINE))
    bad["pinned"]["version"] = version
    with pytest.raises(ValueError, match="baseline"):
        canary(ok, baseline=bad)
    older = json.loads(json.dumps(BASELINE))
    older["pinned"]["version"] = "2026-01-02"
    assert canary(ok, baseline=older)["result"] == "PASS"


def test_an_empty_subset_is_refused_not_reported_as_a_clean_pass() -> None:
    from noema_model.qualification import canary as module  # the digest of an empty list is what a stripped suite would carry

    empty = {**SUBSET, "cases": [], "case_ids": [], "n_cases": 0, "cases_sha256": module._digest([])}
    base = {**BASELINE, "cases": {}, "cases_sha256": module._digest([])}
    with pytest.raises(ValueError, match="empty"):
        canary(ScriptedAsker({}), subset=empty, baseline=base)


# EL's reviewer-role statement on noema#215 (non-blocking 2): the deadline is an argument and must be a positive finite number; None, NaN, zero, a negative, a bool or infinity
# would defeat it (asyncio.timeout(None) waits forever).
@pytest.mark.parametrize("bad", [None, float("nan"), 0, -1.0, True, float("inf"), "5"], ids=repr)
def test_a_timeout_that_is_not_a_positive_finite_number_is_refused_before_any_call(bad: Any) -> None:
    class Never:
        async def ask(self, case: Mapping[str, Any]) -> CaseAnswer | None:
            raise AssertionError("the asker was called")

    with pytest.raises(ValueError, match="timeout_s"):
        canary(Never(), timeout_s=bad)


# The Voice Engineer's independent read of noema#215 (note 1, a defect; note 2): `$` accepts a trailing newline, so `^[0-9a-f]{40}$` with `.match` took a 40-hex string followed by "\n"; and the
# baseline's own commit_sha went into the run record unchecked. A commit SHA is exactly 40 lower-case hex characters, whole string, in the run and in the baseline.
BAD_SHAS: list[Any] = [GOLDEN["commit_sha"] + "\n", "\n" + GOLDEN["commit_sha"], GOLDEN["commit_sha"] + " ", GOLDEN["commit_sha"].upper(), GOLDEN["commit_sha"][:39], GOLDEN["commit_sha"] + "0", "",
                       None, 40, b"0" * 40]


class Never:
    async def ask(self, case: Mapping[str, Any]) -> CaseAnswer | None:
        raise AssertionError("the asker was called")


@pytest.mark.parametrize("bad", BAD_SHAS, ids=repr)
def test_a_run_commit_sha_that_is_not_exactly_forty_lowercase_hex_is_refused_before_any_call(bad: Any) -> None:
    with pytest.raises(ValueError, match="commit_sha"):
        canary(Never(), commit_sha=bad)


@pytest.mark.parametrize("bad", BAD_SHAS, ids=repr)
def test_a_baseline_commit_sha_that_is_not_exactly_forty_lowercase_hex_is_refused_before_any_call(bad: Any) -> None:
    broken = json.loads(json.dumps(BASELINE))
    broken["commit_sha"] = bad
    with pytest.raises(ValueError, match="baseline"):
        canary(Never(), baseline=broken)
