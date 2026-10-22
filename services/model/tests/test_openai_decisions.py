"""The OpenAI Decisions adapter (D9, one decision-model vendor) and the harness's native path.

The wire fixtures are OpenAI's own documentation examples (tests/fixtures/providers/README.md): predicate, choice and score,
and the per-question refusal the reference describes. Not run against a live endpoint: no access.
"""

import json
from fractions import Fraction
from typing import Any

import pytest

from noema_model.harness import (
    ABSTAIN,
    AttemptOutcome,
    ChoiceAnswer,
    ChoiceQuestion,
    DecideRequest,
    DecisionTask,
    Harness,
    HarnessRejection,
    Level,
    Option,
    Reason,
    ScoreAnswer,
    ScoreQuestion,
    YesNoAnswer,
    YesNoQuestion,
)
from noema_model.harness.config import load_harness_config
from noema_model.platform.config import ConfigError
from noema_model.platform.telemetry import InMemoryTelemetry
from noema_model.providers.llm.base import (
    AdapterSettings,
    HttpResponse,
    NativeChoice,
    NativeChoiceAnswer,
    NativePredicate,
    NativePredicateAnswer,
    NativeQuestion,
    NativeRefusal,
    NativeRequest,
    NativeResponse,
    NativeScore,
    NativeScoreAnswer,
    ProviderError,
    ProviderFailure,
)
from noema_model.providers.llm.openai_decisions import MAX_CHOICES, MAX_QUESTIONS, OpenAIDecisions
from tests.support import FIXTURES, ManualClock, RecordingTransport, ScriptedProvider, raw_config, reply, run, timeout_error

PINNED = "gpt-6-luna"
PROVIDERS = FIXTURES / "providers"


def fixture(name: str) -> Any:
    return json.loads((PROVIDERS / name).read_text())


def fixture_bytes(name: str) -> bytes:
    return (PROVIDERS / name).read_bytes()


PREDICATE_Q = NativePredicate("damaged", "Does the customer report a damaged item?")
CHOICE_Q = NativeChoice("department", "Which department should handle this complaint?", (
    ("billing", "Payments, invoices, and refunds."), ("technical", "Problems using the product."),
    ("shipping", "Delivery and tracking."), ("other", "Requests outside these categories.")))
SCORE_Q = NativeScore("severity", "How severe is this issue?", (
    ("Cosmetic", "Appearance only; no lost functionality."), ("Workaround available", "A task fails, but another way works."),
    ("Fully blocked", "A task fails with no workaround.")))


def adapter(body: bytes, status: int = 200) -> tuple[OpenAIDecisions, RecordingTransport]:
    transport = RecordingTransport(HttpResponse(status, body))
    settings = AdapterSettings("https://vendor.example.invalid/", "CRED-VALUE", None, 1500, 65536)
    return OpenAIDecisions("dm", settings, transport), transport


def ask(*questions: NativeQuestion, text: str = "x") -> NativeRequest:
    return NativeRequest(PINNED, text, tuple(questions))


# --- the adapter, on the documentation's examples ---------------------------------------------------------------------------

@pytest.mark.parametrize(
    ("request_file", "response_file", "question", "text"),
    [("openai_decisions_request.json", "openai_decisions_ok.json", PREDICATE_Q, "The package arrived with a broken screen."),
     ("openai_decisions_choice_request.json", "openai_decisions_choice_ok.json", CHOICE_Q, "I was charged twice for my order."),
     ("openai_decisions_score_request.json", "openai_decisions_score_ok.json", SCORE_Q, "Export fails in Safari but works in Chrome.")],
)
def test_each_request_equals_the_documentation_example(request_file: str, response_file: str, question: NativeQuestion, text: str) -> None:
    decisions, transport = adapter(fixture_bytes(response_file))
    run(decisions.decide(ask(question, text=text)))
    url, headers, body = transport.requests[0]
    assert url == "https://vendor.example.invalid/v1/decisions" and headers["authorization"] == "Bearer CRED-VALUE"
    assert body == fixture(request_file)


def test_the_predicate_response_is_read_as_a_probability_and_usage() -> None:
    decisions, _ = adapter(fixture_bytes("openai_decisions_ok.json"))
    assert run(decisions.decide(ask(PREDICATE_Q))) == NativeResponse((NativePredicateAnswer("damaged", 0.95),), PINNED, 42, 0)


def test_the_choice_response_keeps_the_choice_the_distribution_and_the_confidence() -> None:
    decisions, _ = adapter(fixture_bytes("openai_decisions_choice_ok.json"))
    (answer,) = run(decisions.decide(ask(CHOICE_Q))).answers
    assert answer == NativeChoiceAnswer("department", "billing", 0.93, (("billing", 0.95), ("technical", 0.02), ("shipping", 0.01), ("other", 0.02)))


def test_the_score_response_keeps_the_score_the_levels_and_the_confidence() -> None:
    decisions, _ = adapter(fixture_bytes("openai_decisions_score_ok.json"))
    (answer,) = run(decisions.decide(ask(SCORE_Q))).answers
    assert answer == NativeScoreAnswer("severity", 1.1, 0.55, (("Cosmetic", 0, 0.1), ("Workaround available", 1, 0.7), ("Fully blocked", 2, 0.2)))


def body_of(*answers: Any) -> bytes:
    return json.dumps({"model": PINNED, "answers": list(answers), "usage": {"input_tokens": 1, "output_tokens": 0}}).encode()


PRED = {"type": "predicate", "name": "damaged", "probability": 0.9}


def test_a_refusal_for_one_question_is_read_and_the_others_still_answer() -> None:
    decisions, _ = adapter(body_of(PRED, {"type": "refusal", "name": "department"}))
    got = run(decisions.decide(ask(PREDICATE_Q, CHOICE_Q)))
    assert got.answers == (NativePredicateAnswer("damaged", 0.9), NativeRefusal("department"))


@pytest.mark.parametrize(
    ("status", "kind"),
    [(408, ProviderFailure.TIMEOUT), (429, ProviderFailure.RATE_LIMITED), (500, ProviderFailure.UNAVAILABLE),
     (503, ProviderFailure.UNAVAILABLE), (401, ProviderFailure.REJECTED), (400, ProviderFailure.REJECTED)],
)
def test_http_status_maps_to_a_failure_kind_without_reading_the_body(status: int, kind: ProviderFailure) -> None:
    decisions, _ = adapter(b'{"error": {"message": "CRED-VALUE"}}', status)
    with pytest.raises(ProviderError) as caught:
        run(decisions.decide(ask(PREDICATE_Q)))
    assert caught.value.kind is kind and "CRED-VALUE" not in str(caught.value)


CHOICE_OK: dict[str, Any] = {"type": "choice", "name": "department", "choice": "billing", "confidence": 0.9,
             "probabilities": [{"value": "billing", "probability": 0.9}, {"value": "other", "probability": 0.1}]}
SCORE_OK: dict[str, Any] = {"type": "score", "name": "severity", "score": 1.0, "confidence": 0.5, "probabilities": [
    {"value": 0, "label": "Cosmetic", "probability": 0.2}, {"value": 1, "label": "Workaround available", "probability": 0.6},
    {"value": 2, "label": "Fully blocked", "probability": 0.2}]}


def with_(base: dict[str, Any], **over: Any) -> dict[str, Any]:
    return {**base, **over}


# Invented edge cases (the documentation shows none of these): each must be refused whole, as MALFORMED.
@pytest.mark.parametrize(
    ("questions", "body"),
    [
        ((PREDICATE_Q,), b"not json"), ((PREDICATE_Q,), b"[]"), ((PREDICATE_Q,), b'{"model": "gpt-6-luna"}'),
        ((PREDICATE_Q,), body_of()), ((PREDICATE_Q,), b'{"answers": "none"}'),
        ((PREDICATE_Q,), body_of({"type": "predicate", "name": "damaged"})),
        ((PREDICATE_Q,), body_of(with_(PRED, probability=1.5))),
        ((PREDICATE_Q,), body_of(with_(PRED, probability=-0.1))),
        ((PREDICATE_Q,), body_of(with_(PRED, probability=True))),
        ((PREDICATE_Q,), body_of(with_(PRED, probability="0.9"))),
        ((PREDICATE_Q,), b'{"answers": [{"type": "predicate", "name": "damaged", "probability": NaN}]}'),
        ((PREDICATE_Q,), body_of(with_(PRED, type="choice"))),
        ((PREDICATE_Q,), body_of(with_(PRED, type="mystery"))),
        ((PREDICATE_Q,), body_of(with_(PRED, name="other"))),
        ((PREDICATE_Q,), body_of(with_(PRED, name=None))),
        ((PREDICATE_Q,), body_of(PRED, PRED)),
        ((PREDICATE_Q,), body_of(PRED, with_(PRED, name="extra"))),
        ((PREDICATE_Q, CHOICE_Q), body_of(CHOICE_OK, PRED)),                       # out of the order asked
        ((CHOICE_Q,), body_of(with_(CHOICE_OK, choice="unlisted"))),
        ((CHOICE_Q,), body_of(with_(CHOICE_OK, choice=True))),
        ((CHOICE_Q,), body_of(with_(CHOICE_OK, confidence=None))),
        ((CHOICE_Q,), body_of(with_(CHOICE_OK, confidence=True))),
        ((CHOICE_Q,), b'{"answers": [{"type": "choice", "name": "department", "choice": "billing", "confidence": Infinity, "probabilities": []}]}'),
        ((CHOICE_Q,), body_of(with_(CHOICE_OK, probabilities=[{"value": "billing", "probability": 1.2}]))),
        ((CHOICE_Q,), body_of(with_(CHOICE_OK, probabilities=[{"value": "ghost", "probability": 0.1}]))),
        ((CHOICE_Q,), body_of(with_(CHOICE_OK, probabilities=[{"value": "billing", "probability": 0.5}, {"value": "billing", "probability": 0.5}]))),
        ((CHOICE_Q,), body_of(with_(CHOICE_OK, probabilities="none"))),
        ((SCORE_Q,), body_of(with_(SCORE_OK, score=None))),
        ((SCORE_Q,), body_of(with_(SCORE_OK, confidence="high"))),
        ((SCORE_Q,), body_of(with_(SCORE_OK, probabilities=SCORE_OK["probabilities"][:2]))),
        ((SCORE_Q,), body_of(with_(SCORE_OK, probabilities=[*SCORE_OK["probabilities"][:2], with_(SCORE_OK["probabilities"][2], label="Other")]))),
        ((SCORE_Q,), body_of(with_(SCORE_OK, probabilities=[with_(SCORE_OK["probabilities"][0], value=1), *SCORE_OK["probabilities"][1:]]))),
        ((SCORE_Q,), body_of(with_(SCORE_OK, probabilities=[with_(SCORE_OK["probabilities"][0], value=True), *SCORE_OK["probabilities"][1:]]))),
        ((SCORE_Q,), body_of(with_(SCORE_OK, probabilities=[with_(SCORE_OK["probabilities"][0], probability=2), *SCORE_OK["probabilities"][1:]]))),
        ((SCORE_Q,), body_of(*[with_(SCORE_OK, name="severity")] * 2)),
    ],
)
def test_an_answer_that_does_not_fit_the_question_asked_is_malformed(questions: tuple[NativeQuestion, ...], body: bytes) -> None:
    decisions, _ = adapter(body)
    with pytest.raises(ProviderError) as caught:
        run(decisions.decide(ask(*questions)))
    assert caught.value.kind is ProviderFailure.MALFORMED


def many(n: int) -> tuple[NativeQuestion, ...]:
    return tuple(NativePredicate(f"q{i}", "x") for i in range(n))


def test_the_documented_limits_decide_what_the_provider_accepts() -> None:
    decisions, transport = adapter(body_of())
    assert decisions.accepts(many(1)) and decisions.accepts(many(MAX_QUESTIONS))
    assert not decisions.accepts(()) and not decisions.accepts(many(MAX_QUESTIONS + 1))

    def choices(n: int) -> NativeChoice:
        return NativeChoice("c", "x", tuple((f"v{i}", "d") for i in range(n)))

    assert decisions.accepts((choices(2),)) and decisions.accepts((choices(MAX_CHOICES),))
    assert not decisions.accepts((choices(1),)) and not decisions.accepts((choices(MAX_CHOICES + 1),))
    assert not decisions.accepts((NativeChoice("c", "x", (("same", "a"), ("same", "b"))),))
    with pytest.raises(ProviderError):  # defensive: a set the provider does not accept is never put on the wire
        run(decisions.decide(ask(*many(MAX_QUESTIONS + 1))))
    assert transport.requests == []


# --- through the harness ---------------------------------------------------------------------------------------------------------

class ScriptedDecisionModel:
    """A decision provider that answers from a script: a NativeResponse, a callable, a ProviderError, or 'hang'."""

    def __init__(self, name: str, steps: list[Any], accepting: bool = True) -> None:
        self.name, self.steps, self.accepting = name, steps, accepting
        self.calls: list[NativeRequest] = []

    def accepts(self, questions: tuple[NativeQuestion, ...]) -> bool:
        return self.accepting

    async def decide(self, request: NativeRequest) -> NativeResponse:
        import asyncio
        self.calls.append(request)
        step = self.steps[min(len(self.calls) - 1, len(self.steps) - 1)]
        if step == "hang":
            await asyncio.sleep(3600)
        if isinstance(step, ProviderError):
            raise step
        result: NativeResponse = step(request) if callable(step) else step
        return result


def native_reply(*answers: Any, version: str = PINNED, tokens: tuple[int | None, int | None] = (40, 0)) -> NativeResponse:
    return NativeResponse(tuple(answers), version, tokens[0], tokens[1])


def yes(name: str, p: float) -> NativePredicateAnswer:
    return NativePredicateAnswer(name, p)


def stack(native_steps: list[Any], llm_steps: list[Any] | None = None, *, route: tuple[str, ...] = ("dm", "gpt"), accepting: bool = True, **cfg: Any) -> tuple[Harness, ScriptedDecisionModel, ScriptedProvider]:
    raw = raw_config(providers=("gpt",), **cfg)
    raw["providers"]["dm"] = {
        "kind": "openai_decisions", "endpoint": "https://dm.example.invalid", "model_version": PINNED,
        "credential_file": str(FIXTURES / "credential.txt"), "timeout_ms": 200, "max_response_bytes": 65536,
        "rate": {"input_micro_per_mtok": 100_000, "output_micro_per_mtok": 0, "currency": "USD", "as_of": "2026-10-22"},
    }
    for key in raw["routes"]:
        if key.startswith("decide."):
            raw["routes"][key] = {"default": list(route), "by_locale": {}}
    dm = ScriptedDecisionModel("dm", native_steps, accepting)
    gpt = ScriptedProvider("gpt", llm_steps or [reply({"answers": []}, version="model-gpt-2026-01-01")])
    harness = Harness(load_harness_config(raw), {"dm": dm, "gpt": gpt}, ManualClock(), InMemoryTelemetry())
    return harness, dm, gpt


FLAG = YesNoQuestion("multiple_intents", "Does the customer ask for more than one thing?")
CHOICE = ChoiceQuestion("node", "Which?", (Option("o0", "A"), Option("o1", "B"), Option(ABSTAIN, "None")))
SCORE = ScoreQuestion("vagueness", "How vague is the time?", (Level("Exact", "A clock time."), Level("Vague", "A part of the day.")))
LLM_FLAG_NO = reply({"answers": [{"question_id": "multiple_intents", "selected": "no", "probabilities": [{"option": "yes", "p": 0.1}, {"option": "no", "p": 0.9}]}]}, version="model-gpt-2026-01-01")


def decide(harness: Harness, *questions: Any, state: dict[str, str] | None = None) -> Any:
    return run(harness.decide(DecideRequest(DecisionTask.AMBIGUITY_FLAG, state or {"utterance": "pay 0123456789 twice"}, tuple(questions), None, "corr_test")))


def test_a_yes_no_set_goes_to_the_decision_model_and_comes_back_as_a_bare_probability() -> None:
    harness, dm, gpt = stack([native_reply(yes("multiple_intents", 0.97))])
    result = decide(harness, FLAG)
    assert result.answers == (YesNoAnswer("multiple_intents", None, 0.97),)  # the harness selects nothing for the model
    assert result.call.provider == "dm" and gpt.calls == []
    attempt = result.call.attempts[0]
    assert (attempt.kind, attempt.tokens_in, attempt.tokens_out, attempt.rate.input_micro_per_mtok, attempt.cost_micro) == ("decision_model", 40, 0, 100_000, 4)
    assert result.call.model_version == PINNED and result.call.to_dict()["attempts"][0]["kind"] == "decision_model"
    assert dm.calls[0].questions == (NativePredicate("multiple_intents", FLAG.prompt),)


def test_a_choice_set_goes_native_and_the_providers_own_choice_and_confidence_come_back() -> None:
    reply_ = native_reply(NativeChoiceAnswer("node", "o1", 0.8, (("o0", 0.1), ("o1", 0.85), (ABSTAIN, 0.05))))
    harness, dm, gpt = stack([reply_])
    result = decide(harness, CHOICE)
    (answer,) = result.answers
    assert answer == ChoiceAnswer("node", "o1", {"o0": 0.1, "o1": 0.85, ABSTAIN: 0.05}, 0.8) and not answer.abstained
    assert gpt.calls == [] and result.call.provider == "dm"
    (sent,) = dm.calls[0].questions
    assert isinstance(sent, NativeChoice) and [v for v, _ in sent.choices] == ["o0", "o1", ABSTAIN]  # the abstention is a plain choice value


def test_the_abstention_comes_back_as_a_choice_not_a_gap() -> None:
    harness, _, _ = stack([native_reply(NativeChoiceAnswer("node", ABSTAIN, 0.9, ((ABSTAIN, 0.9), ("o0", 0.1))))])
    (answer,) = decide(harness, CHOICE).answers
    assert isinstance(answer, ChoiceAnswer) and answer.abstained


def test_a_score_set_goes_native_and_keeps_the_providers_score_and_the_levels() -> None:
    reply_ = native_reply(NativeScoreAnswer("vagueness", 0.7, 0.6, (("Exact", 0, 0.3), ("Vague", 1, 0.7))))
    harness, dm, _ = stack([reply_])
    (answer,) = decide(harness, SCORE).answers
    assert answer == ScoreAnswer("vagueness", 0.7, {"level_0": 0.3, "level_1": 0.7}, 0.6)
    (sent,) = dm.calls[0].questions
    assert sent == NativeScore("vagueness", SCORE.prompt, (("Exact", "A clock time."), ("Vague", "A part of the day.")))


def test_a_mixed_set_goes_in_one_native_call_and_the_answers_keep_their_order() -> None:
    harness, dm, _ = stack([native_reply(NativeChoiceAnswer("node", "o0", 0.7, (("o0", 0.7), (ABSTAIN, 0.3))), yes("multiple_intents", 0.2))])
    result = decide(harness, CHOICE, FLAG)
    assert [a.question_id for a in result.answers] == ["node", "multiple_intents"] and len(dm.calls) == 1


def test_a_probability_is_passed_through_untouched_even_when_it_is_certain() -> None:
    harness, _, _ = stack([native_reply(yes("multiple_intents", 1.0))])
    assert decide(harness, FLAG).answers[0].p_yes == 1.0


def test_the_decision_model_gets_the_same_masked_and_delimited_data_as_the_llm() -> None:
    harness, dm, _ = stack([native_reply(yes("multiple_intents", 0.1))])
    decide(harness, FLAG)
    sent = dm.calls[0]
    assert sent.model_version == PINNED and sent.input_text.startswith("<<<DATA\n") and sent.input_text.endswith("\nDATA>>>")
    assert "0123456789" not in sent.input_text and "******6789" in sent.input_text


def test_a_set_the_provider_does_not_accept_is_never_sent_and_the_breaker_is_not_charged() -> None:
    harness, dm, _ = stack([native_reply(yes("multiple_intents", 0.1))], [LLM_FLAG_NO], accepting=False, failure_threshold=1)
    for _ in range(3):
        result = decide(harness, FLAG)
        first, second = result.call.attempts
        assert (first.outcome, first.cost_micro, first.rate, first.kind) == (AttemptOutcome.UNSUPPORTED, 0, None, "decision_model")
        assert second.outcome is AttemptOutcome.OK and result.call.cost_complete is True and result.call.provider == "gpt"
    assert dm.calls == []


def test_when_only_a_decision_model_is_routed_an_unaccepted_set_has_no_provider() -> None:
    harness, _, _ = stack([native_reply(yes("multiple_intents", 0.5))], route=("dm",), accepting=False)
    with pytest.raises(HarnessRejection) as rejected:
        decide(harness, FLAG)
    assert (rejected.value.reason, rejected.value.code) == (Reason.NO_PROVIDER_ANSWERED, "PROVIDER_UNAVAILABLE")


def test_a_refused_question_spoils_the_set_and_the_next_provider_is_asked() -> None:
    refusal = native_reply(NativeRefusal("multiple_intents"))
    harness, _, _ = stack([refusal], [LLM_FLAG_NO])
    result = decide(harness, FLAG)
    assert result.call.attempts[0].outcome is AttemptOutcome.OUTPUT_REFUSED and result.call.provider == "gpt"
    sole, _, _ = stack([refusal], route=("dm",))
    with pytest.raises(HarnessRejection) as rejected:
        decide(sole, FLAG)
    assert rejected.value.reason is Reason.OUTPUT_REFUSED and rejected.value.code == "SCHEMA_INVALID"


def test_generate_never_uses_a_decision_model() -> None:
    from pydantic import BaseModel, ConfigDict

    from noema_model.harness import GenerateRequest, PromptTemplate

    class Out(BaseModel):
        model_config = ConfigDict(extra="forbid", strict=True, frozen=True)
        surface: str

    raw = raw_config(providers=("gpt",))
    raw["providers"]["dm"] = {"kind": "openai_decisions", "endpoint": "https://dm.example.invalid", "model_version": PINNED,
                              "credential_file": str(FIXTURES / "credential.txt"), "timeout_ms": 200, "max_response_bytes": 65536}
    raw["routes"]["slot_values"] = {"default": ["dm", "gpt"], "by_locale": {}}
    dm = ScriptedDecisionModel("dm", [native_reply()])
    gpt = ScriptedProvider("gpt", [reply({"surface": "20k"}, version="model-gpt-2026-01-01")])
    harness = Harness(load_harness_config(raw), {"dm": dm, "gpt": gpt}, ManualClock(), InMemoryTelemetry())
    result = run(harness.generate(GenerateRequest("slot_values", PromptTemplate("t", 1, "S", "I"), {"utterance": "x"}, Out, None, "corr_test")))
    assert result.value.surface == "20k" and dm.calls == [] and result.call.attempts[0].outcome is AttemptOutcome.UNSUPPORTED


@pytest.mark.parametrize(
    ("step", "outcome"),
    [(timeout_error(), AttemptOutcome.TIMEOUT), (ProviderError(ProviderFailure.UNAVAILABLE, 503), AttemptOutcome.UNAVAILABLE),
     (ProviderError(ProviderFailure.RATE_LIMITED, 429), AttemptOutcome.RATE_LIMITED), (ProviderError(ProviderFailure.MALFORMED, 200), AttemptOutcome.MALFORMED)],
)
def test_a_decision_model_failure_falls_back_to_the_llm(step: ProviderError, outcome: AttemptOutcome) -> None:
    harness, _, _ = stack([step], [LLM_FLAG_NO])
    result = decide(harness, FLAG)
    assert result.call.attempts[0].outcome is outcome and result.call.provider == "gpt"
    assert result.answers[0] == YesNoAnswer("multiple_intents", False, 0.1)  # the LLM-backed path selects, as before
    assert [(f.from_provider, f.to_provider) for f in result.call.failovers] == [("dm", "gpt")]


def test_a_hanging_decision_model_times_out_and_falls_back() -> None:
    harness, _, _ = stack(["hang"], [LLM_FLAG_NO])
    assert decide(harness, FLAG).call.attempts[0].outcome is AttemptOutcome.TIMEOUT


def test_a_reply_from_another_version_than_the_pin_is_refused() -> None:
    harness, _, _ = stack([native_reply(yes("multiple_intents", 0.9), version="gpt-6-luna-2026-11-01")], route=("dm",))
    with pytest.raises(HarnessRejection) as rejected:
        decide(harness, FLAG)
    assert rejected.value.call is not None and rejected.value.call.attempts[0].outcome is AttemptOutcome.VERSION_MISMATCH


GOOD_CHOICE = NativeChoiceAnswer("node", "o0", 0.7, (("o0", 0.7), (ABSTAIN, 0.3)))


@pytest.mark.parametrize(
    ("questions", "bad"),
    [
        ((FLAG,), native_reply()),
        ((FLAG,), native_reply(yes("other", 0.5))),
        ((FLAG,), native_reply(yes("multiple_intents", 0.5), yes("extra", 0.5))),
        ((FLAG,), native_reply(yes("multiple_intents", 1.5))),
        ((FLAG,), native_reply(yes("multiple_intents", -0.5))),
        ((FLAG,), native_reply(yes("multiple_intents", 0.5), yes("multiple_intents", 0.5))),
        ((FLAG,), native_reply(GOOD_CHOICE)),                                            # the wrong kind of answer
        ((CHOICE,), native_reply(yes("node", 0.5))),
        ((CHOICE,), native_reply(NativeChoiceAnswer("node", "elsewhere", 0.7, (("o0", 1.0),)))),   # a choice that was never offered
        ((CHOICE,), native_reply(NativeChoiceAnswer("node", "o0", 0.7, (("ghost", 1.0),)))),
        ((CHOICE,), native_reply(NativeChoiceAnswer("node", "o0", 0.7, (("o0", 0.5), ("o0", 0.5))))),
        ((CHOICE,), native_reply(NativeChoiceAnswer("node", "o0", 0.7, (("o0", 1.5),)))),
        ((SCORE,), native_reply(NativeScoreAnswer("vagueness", 0.5, 0.5, (("Exact", 0, 1.0),)))),    # a level missing
        ((SCORE,), native_reply(NativeScoreAnswer("vagueness", 0.5, 0.5, (("Exact", 0, 0.5), ("Vague", 2, 0.5))))),
        ((SCORE,), native_reply(NativeScoreAnswer("vagueness", 0.5, 0.5, (("Exact", 0, 0.5), ("Vague", 1, 2.0))))),
    ],
)
def test_answers_that_do_not_fit_the_questions_are_rejected_whole(questions: tuple[Any, ...], bad: NativeResponse) -> None:
    harness, _, _ = stack([bad], route=("dm",))
    with pytest.raises(HarnessRejection) as rejected:
        decide(harness, *questions)
    assert rejected.value.reason is Reason.OUTPUT_MISMATCH and rejected.value.call is not None
    assert rejected.value.call.attempts[0].outcome is AttemptOutcome.OUTPUT_MISMATCH


def test_a_mismatched_native_answer_fails_over_to_the_llm() -> None:
    harness, _, _ = stack([native_reply(yes("other", 0.5))], [LLM_FLAG_NO])
    result = decide(harness, FLAG)
    assert [a.outcome for a in result.call.attempts] == [AttemptOutcome.OUTPUT_MISMATCH, AttemptOutcome.OK] and result.call.provider == "gpt"


def test_unknown_token_counts_make_the_cost_unknown_not_zero() -> None:
    harness, _, _ = stack([native_reply(yes("multiple_intents", 0.5), tokens=(None, None))])
    call = decide(harness, FLAG).call
    assert (call.cost_micro, call.cost_complete) == (None, False)


# --- configuration ----------------------------------------------------------------------------------------------------------------

def test_a_decision_model_config_has_no_token_cap_or_temperature() -> None:
    base = {"kind": "openai_decisions", "endpoint": "https://dm.example.invalid", "model_version": PINNED,
            "credential_file": str(FIXTURES / "credential.txt"), "timeout_ms": 200, "max_response_bytes": 65536}
    for extra in ({"max_output_tokens": 10}, {"temperature": 0}):
        raw = raw_config(providers=("gpt",))
        raw["providers"]["dm"] = {**base, **extra}
        with pytest.raises(ConfigError):
            load_harness_config(raw)
    raw = raw_config(providers=("gpt",))
    raw["providers"]["dm"] = base
    assert load_harness_config(raw).providers["dm"].max_output_tokens is None
    raw["providers"]["dm"] = {**base, "model_version": "latest"}
    with pytest.raises(ConfigError):
        load_harness_config(raw)
    raw["providers"]["dm"] = {**base, "endpoint": "http://dm.example.invalid"}
    with pytest.raises(ConfigError):
        load_harness_config(raw)


# --- the pipeline, with the decision model on every decide route -------------------------------------------------------------------------

class StubDecisionModel:
    """Answers each native question the way ScriptedModel answers an LLM one, so the two implementations can be compared."""

    def __init__(self, wanted: dict[str, Any], probability: float = 0.9) -> None:
        self.name, self.wanted, self.probability, self.calls = "dm", wanted, probability, []  # type: str, dict[str, Any], float, list[NativeRequest]

    def accepts(self, questions: tuple[NativeQuestion, ...]) -> bool:
        return True

    async def decide(self, request: NativeRequest) -> NativeResponse:
        self.calls.append(request)
        answers: list[Any] = []
        for q in request.questions:
            want = self.wanted.get(q.name)
            if isinstance(q, NativePredicate):
                answers.append(NativePredicateAnswer(q.name, self.probability if want else 1 - self.probability))
            elif isinstance(q, NativeChoice):
                pick = next((v for v, d in q.choices if want is not None and want != ABSTAIN and (want == d or f"({want})" in d)), ABSTAIN)
                rest = [v for v, _ in q.choices if v != pick]
                answers.append(NativeChoiceAnswer(q.name, pick, 0.8, ((pick, self.probability), *((v, (1 - self.probability) / len(rest)) for v in rest))))
            else:
                answers.append(NativeScoreAnswer(q.name, 0.0, 0.5, tuple((label, i, 1.0 if i == 0 else 0.0) for i, (label, _) in enumerate(q.levels))))
        return NativeResponse(tuple(answers), PINNED, 50, 0)


def pipeline_run(*, native: bool, probability: float = 0.9, threshold: Any = "keep", flags: bool = False) -> Any:
    from dataclasses import replace

    from noema_model.understanding.pipeline import run_pipeline
    from tests.support_pipeline import (
        G_A,
        GOOD_MENTIONS,
        FakeSource,
        ScriptedModel,
        candidate,
        full_routes,
        make_input,
        no_errors,
        normaliser,
        pipeline_config,
    )

    answers = {**G_A, **({"f0": True, "f1": True} if flags else {})}
    llm = ScriptedModel(answers=answers, mentions=GOOD_MENTIONS)
    raw = raw_config(providers=("alpha",), routes={k: {"default": ["alpha"], "by_locale": {}} for k in full_routes()})
    providers: dict[str, Any] = {"alpha": llm}
    stub = StubDecisionModel(answers, probability)
    if native:
        raw["providers"]["dm"] = {"kind": "openai_decisions", "endpoint": "https://dm.example.invalid", "model_version": PINNED,
                                  "credential_file": str(FIXTURES / "credential.txt"), "timeout_ms": 200, "max_response_bytes": 65536}
        for key in raw["routes"]:
            if key.startswith("decide."):
                raw["routes"][key] = {"default": ["dm", "alpha"], "by_locale": {}}
        providers["dm"] = stub
    harness = Harness(load_harness_config(raw), providers, ManualClock(), InMemoryTelemetry())
    config = pipeline_config() if threshold == "keep" else replace(pipeline_config(), native_yes_threshold=threshold)
    result = run(run_pipeline(make_input(), config, harness, FakeSource((candidate("a"),)), normaliser, no_errors))
    return result, stub, llm


def test_the_two_implementations_of_decide_give_the_pipeline_the_same_proposal() -> None:
    llm_result, _, _ = pipeline_run(native=False)
    native_result, stub, llm = pipeline_run(native=True)
    assert stub.calls and not [r for r in llm.requests if r.schema_name == "harness_decide"]  # every decide call went native
    a, b = llm_result.proposal, native_result.proposal
    assert (a.intent_node, a.reference_slots, a.locale, a.mentions) == (b.intent_node, b.reference_slots, b.locale, b.mentions)
    assert llm_result.ambiguity_flags == native_result.ambiguity_flags and llm_result.resolution == native_result.resolution
    kinds = {x.kind for c in native_result.calls for x in c.attempts}
    assert "decision_model" in kinds and native_result.outcome.value == "PROPOSED"
    assert native_result.resolution.state.value == "AUTO_RESOLVED"


def test_the_pack_threshold_decides_what_a_bare_probability_means_and_the_boundary_counts_as_yes() -> None:
    high, _, _ = pipeline_run(native=True, probability=0.9, flags=True)
    assert set(high.ambiguity_flags) >= {"MULTIPLE_INTENTS", "CONTRADICTS_PRIOR_TURN"}
    low, _, _ = pipeline_run(native=True, probability=0.9, flags=False)
    assert "MULTIPLE_INTENTS" not in low.ambiguity_flags and low.injection_signals["utterance"] is False
    assert "MULTIPLE_INTENTS" in pipeline_run(native=True, probability=0.5, threshold=Fraction(1, 2), flags=True)[0].ambiguity_flags
    assert "MULTIPLE_INTENTS" not in pipeline_run(native=True, probability=0.4999, threshold=Fraction(1, 2), flags=True)[0].ambiguity_flags


def test_with_no_threshold_configured_a_bare_probability_errs_toward_caution() -> None:
    result, _, _ = pipeline_run(native=True, probability=0.9, threshold=None, flags=False)
    # the stub says "no" at 0.1 for every yes/no question; with no threshold a bare probability counts as a yes
    assert {"MULTIPLE_INTENTS", "CONTRADICTS_PRIOR_TURN"} <= set(result.ambiguity_flags)
    assert result.injection_signals["utterance"] is True


# --- on a real socket, with the documentation's own responses -----------------------------------------------------------------------------

def test_the_adapter_and_harness_over_a_real_socket_read_the_documentation_responses() -> None:
    import threading
    from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

    from noema_model.harness.config import ProviderConfig
    from noema_model.providers.llm.transport import UrllibTransport

    seen: list[tuple[str, dict[str, Any]]] = []
    body = json.dumps({"model": PINNED, "answers": [
        {"type": "predicate", "name": "multiple_intents", "probability": 0.95},
        {**fixture("openai_decisions_choice_ok.json")["answers"][0], "name": "node", "choice": "o0",
         "probabilities": [{"value": "o0", "probability": 0.9}, {"value": ABSTAIN, "probability": 0.1}]},
    ], "usage": {"input_tokens": 42, "output_tokens": 0}}).encode()

    class Handler(BaseHTTPRequestHandler):
        def do_POST(self) -> None:  # noqa: N802
            seen.append((self.path, json.loads(self.rfile.read(int(self.headers["content-length"])))))
            self.send_response(200)
            self.send_header("content-length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, *a: Any) -> None:
            return None

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try:
        raw = raw_config(providers=("gpt",))
        raw["providers"]["dm"] = {"kind": "openai_decisions", "endpoint": "https://dm.example.invalid", "model_version": PINNED,
                                  "credential_file": str(FIXTURES / "credential.txt"), "timeout_ms": 500, "max_response_bytes": 65536}
        raw["routes"]["decide.ambiguity_flag"] = {"default": ["dm", "gpt"], "by_locale": {}}
        config = load_harness_config(raw)
        dm_cfg = config.providers["dm"]
        local = ProviderConfig(**{**{f: getattr(dm_cfg, f) for f in dm_cfg.__slots__}, "endpoint": f"http://127.0.0.1:{server.server_address[1]}"})
        config = type(config)(config.account_run_min_digits, config.data_limits, config.breaker, {**config.providers, "dm": local}, config.routes)
        decisions = OpenAIDecisions("dm", AdapterSettings(local.endpoint, "CRED", None, 500, 65536), UrllibTransport(allow_loopback_http=True))
        harness = Harness(config, {"dm": decisions, "gpt": ScriptedProvider("gpt", [timeout_error()])}, ManualClock(), InMemoryTelemetry())
        result = decide(harness, FLAG, CHOICE, state={"utterance": "The package arrived with a broken screen."})
        assert result.answers[0] == YesNoAnswer("multiple_intents", None, 0.95)
        assert result.answers[1] == ChoiceAnswer("node", "o0", {"o0": 0.9, ABSTAIN: 0.1}, 0.93)
        path, sent = seen[0]
        assert path == "/v1/decisions" and sent["model"] == PINNED
        assert [q["type"] for q in sent["questions"]] == ["predicate", "choice"] and sent["questions"][1]["choices"][-1]["value"] == ABSTAIN
        assert result.call.attempts[0].tokens_in == 42 and result.call.attempts[0].tokens_out == 0 and result.call.model_version == PINNED
    finally:
        server.shutdown()
        server.server_close()
