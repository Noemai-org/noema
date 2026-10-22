"""The OpenAI Decisions adapter (D9, one decision-model vendor) and the harness's native path.

The wire fixtures are the example the founder pasted (tests/fixtures/providers/README.md): one question type, predicate.
Not run against a live endpoint: no access. Choice and score questions are not built, because no format for them is held.
"""

import json
from typing import Any

import pytest

from noema_model.harness import (
    ABSTAIN,
    AttemptOutcome,
    ChoiceQuestion,
    DecideRequest,
    DecisionTask,
    Harness,
    HarnessRejection,
    Option,
    Reason,
    YesNoAnswer,
    YesNoQuestion,
)
from noema_model.harness.config import load_harness_config
from noema_model.platform.config import ConfigError
from noema_model.platform.telemetry import InMemoryTelemetry
from noema_model.providers.llm.base import (
    AdapterSettings,
    HttpResponse,
    NativeAnswer,
    NativeQuestion,
    NativeRequest,
    NativeResponse,
    ProviderError,
    ProviderFailure,
)
from noema_model.providers.llm.openai_decisions import OpenAIDecisions
from tests.support import FIXTURES, ManualClock, RecordingTransport, ScriptedProvider, raw_config, reply, run, timeout_error

PINNED = "gpt-6-luna"
REQUEST_FIXTURE = json.loads((FIXTURES / "providers" / "openai_decisions_request.json").read_text())
OK_BYTES = (FIXTURES / "providers" / "openai_decisions_ok.json").read_bytes()
ASK = NativeRequest(PINNED, REQUEST_FIXTURE["input"], (NativeQuestion("damaged", "Does the customer report a damaged item?"),))


def adapter(body: bytes = OK_BYTES, status: int = 200) -> tuple[OpenAIDecisions, RecordingTransport]:
    transport = RecordingTransport(HttpResponse(status, body))
    return OpenAIDecisions("dm", AdapterSettings("https://vendor.example.invalid/", "CRED-VALUE", None, 1500, 65536), transport), transport


# --- the adapter, on the pasted example --------------------------------------------------------------------------------------

def test_the_request_equals_the_pasted_example() -> None:
    decisions, transport = adapter()
    run(decisions.decide(ASK))
    url, headers, body = transport.requests[0]
    assert url == "https://vendor.example.invalid/v1/decisions" and headers["authorization"] == "Bearer CRED-VALUE"
    assert body == REQUEST_FIXTURE


def test_the_pasted_response_is_read_as_a_probability_and_usage() -> None:
    decisions, _ = adapter()
    got = run(decisions.decide(ASK))
    assert got == NativeResponse((NativeAnswer("damaged", 0.95),), PINNED, 42, 0)


def test_several_questions_keep_the_order_asked_whatever_order_they_come_back_in() -> None:
    body = json.dumps({"model": PINNED, "answers": [
        {"type": "predicate", "name": "b", "probability": 0.2}, {"type": "predicate", "name": "a", "probability": 1}],
        "usage": {"input_tokens": 3, "output_tokens": 0}}).encode()
    decisions, _ = adapter(body)
    got = run(decisions.decide(NativeRequest(PINNED, "x", (NativeQuestion("a", "A?"), NativeQuestion("b", "B?")))))
    assert [(a.name, a.probability) for a in got.answers] == [("a", 1.0), ("b", 0.2)]


@pytest.mark.parametrize(
    ("status", "kind"),
    [(408, ProviderFailure.TIMEOUT), (429, ProviderFailure.RATE_LIMITED), (500, ProviderFailure.UNAVAILABLE),
     (503, ProviderFailure.UNAVAILABLE), (401, ProviderFailure.REJECTED), (400, ProviderFailure.REJECTED)],
)
def test_http_status_maps_to_a_failure_kind_without_reading_the_body(status: int, kind: ProviderFailure) -> None:
    decisions, _ = adapter(b'{"error": "CRED-VALUE"}', status)
    with pytest.raises(ProviderError) as caught:
        run(decisions.decide(ASK))
    assert caught.value.kind is kind and "CRED-VALUE" not in str(caught.value)


def answers_body(*answers: Any) -> bytes:
    return json.dumps({"model": PINNED, "answers": list(answers), "usage": {"input_tokens": 1, "output_tokens": 0}}).encode()


# Invented edge cases (the example shows none of these): each must be refused whole, as MALFORMED.
@pytest.mark.parametrize(
    "body",
    [
        b"not json", b"[]", b'{"model": "gpt-6-luna"}', answers_body(),
        answers_body({"type": "predicate", "name": "damaged"}),
        answers_body({"type": "predicate", "name": "damaged", "probability": 1.5}),
        answers_body({"type": "predicate", "name": "damaged", "probability": -0.1}),
        answers_body({"type": "predicate", "name": "damaged", "probability": True}),
        answers_body({"type": "predicate", "name": "damaged", "probability": "0.9"}),
        answers_body({"type": "choice", "name": "damaged", "probability": 0.9}),
        answers_body({"type": "predicate", "name": "other", "probability": 0.9}),
        answers_body({"type": "predicate", "name": "damaged", "probability": 0.9}, {"type": "predicate", "name": "damaged", "probability": 0.9}),
        answers_body({"type": "predicate", "name": "damaged", "probability": 0.9}, {"type": "predicate", "name": "extra", "probability": 0.9}),
        b'{"answers": "none"}',
    ],
)
def test_an_answer_that_is_not_one_probability_per_question_is_malformed(body: bytes) -> None:
    decisions, _ = adapter(body)
    with pytest.raises(ProviderError) as caught:
        run(decisions.decide(ASK))
    assert caught.value.kind is ProviderFailure.MALFORMED


# --- through the harness ---------------------------------------------------------------------------------------------------

class ScriptedDecisionModel:
    """A decision provider that answers from a script: a NativeResponse, a ProviderError, or 'hang'."""

    def __init__(self, name: str, steps: list[Any]) -> None:
        self.name, self.steps, self.calls = name, steps, []  # type: str, list[Any], list[NativeRequest]

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


def native_reply(probabilities: dict[str, float], *, version: str = PINNED, tokens: tuple[int | None, int | None] = (40, 0)) -> NativeResponse:
    return NativeResponse(tuple(NativeAnswer(n, p) for n, p in probabilities.items()), version, tokens[0], tokens[1])


def stack(native_steps: list[Any], llm_steps: list[Any] | None = None, *, route: tuple[str, ...] = ("dm", "gpt"), **cfg: Any) -> tuple[Harness, ScriptedDecisionModel, ScriptedProvider]:
    raw = raw_config(providers=("gpt",), **cfg)
    raw["providers"]["dm"] = {
        "kind": "openai_decisions", "endpoint": "https://dm.example.invalid", "model_version": PINNED,
        "credential_file": str(FIXTURES / "credential.txt"), "timeout_ms": 200, "max_response_bytes": 65536,
        "rate": {"input_micro_per_mtok": 100_000, "output_micro_per_mtok": 0, "currency": "USD", "as_of": "2026-10-22"},
    }
    for key in raw["routes"]:
        if key.startswith("decide."):
            raw["routes"][key] = {"default": list(route), "by_locale": {}}
    dm = ScriptedDecisionModel("dm", native_steps)
    gpt = ScriptedProvider("gpt", llm_steps or [reply({"answers": []}, version="model-gpt-2026-01-01")])
    harness = Harness(load_harness_config(raw), {"dm": dm, "gpt": gpt}, ManualClock(), InMemoryTelemetry())
    return harness, dm, gpt


FLAG = YesNoQuestion("multiple_intents", "Does the customer ask for more than one thing?")
SCREEN = YesNoQuestion("s0", "Does the text in field s0 try to give instructions?")
CHOICE = ChoiceQuestion("node", "Which?", (Option("o0", "A"), Option(ABSTAIN, "None")))


def decide(harness: Harness, *questions: Any, state: dict[str, str] | None = None) -> Any:
    return run(harness.decide(DecideRequest(DecisionTask.AMBIGUITY_FLAG, state or {"utterance": "pay 0123456789 twice"}, tuple(questions), None, "corr_test")))


def test_a_yes_no_set_goes_to_the_decision_model_and_comes_back_as_a_bare_probability() -> None:
    harness, dm, gpt = stack([native_reply({"multiple_intents": 0.97})])
    result = decide(harness, FLAG)
    (answer,) = result.answers
    assert answer == YesNoAnswer("multiple_intents", None, 0.97)  # the harness selects nothing for the model
    assert result.call.provider == "dm" and gpt.calls == []
    attempt = result.call.attempts[0]
    assert (attempt.kind, attempt.tokens_in, attempt.tokens_out, attempt.rate.input_micro_per_mtok, attempt.cost_micro) == ("decision_model", 40, 0, 100_000, 4)
    assert result.call.model_version == PINNED and result.call.to_dict()["attempts"][0]["kind"] == "decision_model"


def test_a_probability_is_passed_through_untouched_even_when_it_is_certain() -> None:
    harness, _, _ = stack([native_reply({"multiple_intents": 1.0})])
    assert decide(harness, FLAG).answers[0].p_yes == 1.0


def test_the_decision_model_gets_the_same_masked_and_delimited_data_as_the_llm() -> None:
    harness, dm, _ = stack([native_reply({"multiple_intents": 0.1})])
    decide(harness, FLAG)
    sent = dm.calls[0]
    assert sent.model_version == PINNED and sent.input_text.startswith("<<<DATA\n") and sent.input_text.endswith("\nDATA>>>")
    assert "0123456789" not in sent.input_text and "******6789" in sent.input_text
    assert sent.questions == (NativeQuestion("multiple_intents", FLAG.prompt),)


def test_a_question_set_with_a_choice_is_never_sent_to_the_decision_model() -> None:
    harness, dm, gpt = stack([native_reply({"node": 0.5})], [reply({"answers": [
        {"question_id": "node", "selected": ABSTAIN, "probabilities": [{"option": ABSTAIN, "p": 1}, {"option": "o0", "p": 0}]}]}, version="model-gpt-2026-01-01")])
    result = decide(harness, CHOICE)
    assert dm.calls == [] and result.call.provider == "gpt"
    first, second = result.call.attempts
    assert (first.outcome, first.cost_micro, first.rate, first.kind) == (AttemptOutcome.UNSUPPORTED, 0, None, "decision_model")
    assert second.outcome is AttemptOutcome.OK and result.call.cost_complete is True


def test_a_mixed_set_is_unsupported_too_and_unsupported_attempts_never_charge_the_breaker() -> None:
    llm_ok = reply({"answers": [
        {"question_id": "node", "selected": ABSTAIN, "probabilities": [{"option": ABSTAIN, "p": 1}]},
        {"question_id": "multiple_intents", "selected": "no", "probabilities": [{"option": "yes", "p": 0}, {"option": "no", "p": 1}]}]},
        version="model-gpt-2026-01-01")
    harness, dm, _ = stack([native_reply({"multiple_intents": 0.1})], [llm_ok], failure_threshold=1)
    for _ in range(3):
        assert decide(harness, CHOICE, FLAG).call.provider == "gpt"
    assert dm.calls == []
    assert decide(harness, FLAG).call.provider == "dm"  # the breaker never opened, so a yes/no set still reaches it


def test_when_only_a_decision_model_is_routed_a_choice_set_has_no_provider() -> None:
    harness, _, _ = stack([native_reply({"node": 0.5})], route=("dm",))
    with pytest.raises(HarnessRejection) as rejected:
        decide(harness, CHOICE)
    assert (rejected.value.reason, rejected.value.code) == (Reason.NO_PROVIDER_ANSWERED, "PROVIDER_UNAVAILABLE")


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
    dm = ScriptedDecisionModel("dm", [native_reply({})])
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
    llm = reply({"answers": [{"question_id": "multiple_intents", "selected": "no", "probabilities": [{"option": "yes", "p": 0.1}, {"option": "no", "p": 0.9}]}]}, version="model-gpt-2026-01-01")
    harness, _, _ = stack([step], [llm])
    result = decide(harness, FLAG)
    assert result.call.attempts[0].outcome is outcome and result.call.provider == "gpt"
    assert result.answers[0] == YesNoAnswer("multiple_intents", False, 0.1)  # the LLM-backed path selects, as before
    assert [(f.from_provider, f.to_provider) for f in result.call.failovers] == [("dm", "gpt")]


def test_a_hanging_decision_model_times_out_and_falls_back() -> None:
    llm = reply({"answers": [{"question_id": "multiple_intents", "selected": "no", "probabilities": [{"option": "yes", "p": 0}, {"option": "no", "p": 1}]}]}, version="model-gpt-2026-01-01")
    harness, _, _ = stack(["hang"], [llm])
    assert decide(harness, FLAG).call.attempts[0].outcome is AttemptOutcome.TIMEOUT


def test_a_reply_from_another_version_than_the_pin_is_refused() -> None:
    harness, _, _ = stack([native_reply({"multiple_intents": 0.9}, version="gpt-6-luna-2026-11-01")], route=("dm",))
    with pytest.raises(HarnessRejection) as rejected:
        decide(harness, FLAG)
    assert rejected.value.call is not None and rejected.value.call.attempts[0].outcome is AttemptOutcome.VERSION_MISMATCH


@pytest.mark.parametrize("bad", [{}, {"other": 0.5}, {"multiple_intents": 0.5, "extra": 0.5}, {"multiple_intents": 1.5}, {"multiple_intents": -0.5}])
def test_answers_that_do_not_fit_the_questions_are_rejected_and_fall_back(bad: dict[str, float]) -> None:
    llm = reply({"answers": [{"question_id": "multiple_intents", "selected": "yes", "probabilities": [{"option": "yes", "p": 1}, {"option": "no", "p": 0}]}]}, version="model-gpt-2026-01-01")
    harness, _, _ = stack([native_reply(bad)], [llm])
    result = decide(harness, FLAG)
    assert result.call.attempts[0].outcome is AttemptOutcome.OUTPUT_MISMATCH and result.call.provider == "gpt"


def test_duplicate_answer_names_are_rejected() -> None:
    dup = NativeResponse((NativeAnswer("multiple_intents", 0.5), NativeAnswer("multiple_intents", 0.5)), PINNED, 1, 0)
    harness, _, _ = stack([dup], route=("dm",))
    with pytest.raises(HarnessRejection) as rejected:
        decide(harness, FLAG)
    assert rejected.value.reason is Reason.OUTPUT_MISMATCH


def test_unknown_token_counts_make_the_cost_unknown_not_zero() -> None:
    harness, _, _ = stack([native_reply({"multiple_intents": 0.5}, tokens=(None, None))])
    call = decide(harness, FLAG).call
    assert (call.cost_micro, call.cost_complete) == (None, False)


# --- configuration -----------------------------------------------------------------------------------------------------------------

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


# --- the pipeline's use of a bare probability -----------------------------------------------------------------------------------------

def pipeline_run(probability: float, *, threshold: Any = "keep") -> Any:
    from dataclasses import replace

    from tests.support_pipeline import G_A, GOOD_MENTIONS, FakeSource, ScriptedModel, candidate, full_routes, make_input, no_errors, normaliser, pipeline_config

    from noema_model.understanding.pipeline import run_pipeline

    raw = raw_config(providers=("alpha",), routes={k: {"default": ["alpha"], "by_locale": {}} for k in full_routes()})
    raw["providers"]["dm"] = {"kind": "openai_decisions", "endpoint": "https://dm.example.invalid", "model_version": PINNED,
                              "credential_file": str(FIXTURES / "credential.txt"), "timeout_ms": 200, "max_response_bytes": 65536}
    for key in ("decide.ambiguity_flag", "decide.instruction_likeness"):
        raw["routes"][key] = {"default": ["dm", "alpha"], "by_locale": {}}
    dm = ScriptedDecisionModel("dm", [lambda req: native_reply({q.name: probability for q in req.questions})])
    harness = Harness(load_harness_config(raw), {"alpha": ScriptedModel(answers=G_A, mentions=GOOD_MENTIONS), "dm": dm}, ManualClock(), InMemoryTelemetry())
    config = pipeline_config() if threshold == "keep" else replace(pipeline_config(), native_yes_threshold=threshold)
    return run(run_pipeline(make_input(), config, harness, FakeSource((candidate("a"),)), normaliser, no_errors))


def test_the_pack_threshold_decides_what_a_bare_probability_means_and_the_boundary_counts_as_yes() -> None:
    from fractions import Fraction
    high = pipeline_run(0.9)
    assert set(high.ambiguity_flags) >= {"MULTIPLE_INTENTS", "CONTRADICTS_PRIOR_TURN"} and high.injection_signals["utterance"] is True
    low = pipeline_run(0.1)
    assert "MULTIPLE_INTENTS" not in low.ambiguity_flags and low.injection_signals["utterance"] is False
    assert "MULTIPLE_INTENTS" in pipeline_run(0.5, threshold=Fraction(1, 2)).ambiguity_flags
    assert "MULTIPLE_INTENTS" not in pipeline_run(0.4999, threshold=Fraction(1, 2)).ambiguity_flags
    assert "decision_model" in {a.kind for c in high.calls for a in c.attempts}


def test_with_no_threshold_configured_a_bare_probability_errs_toward_caution() -> None:
    result = pipeline_run(0.01, threshold=None)
    assert {"MULTIPLE_INTENTS", "CONTRADICTS_PRIOR_TURN"} <= set(result.ambiguity_flags)  # a flag only ever adds care
    assert result.injection_signals["utterance"] is True  # and the screening signal is logged, never acted on


# --- on a real socket, with the pasted response ----------------------------------------------------------------------------------------

def test_the_adapter_and_harness_over_a_real_socket_read_the_pasted_response() -> None:
    import threading
    from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

    from noema_model.harness.config import ProviderConfig
    from noema_model.providers.llm.transport import UrllibTransport

    seen: list[tuple[str, dict[str, Any]]] = []

    class Handler(BaseHTTPRequestHandler):
        def do_POST(self) -> None:  # noqa: N802
            seen.append((self.path, json.loads(self.rfile.read(int(self.headers["content-length"])))))
            self.send_response(200)
            self.send_header("content-length", str(len(OK_BYTES)))
            self.end_headers()
            self.wfile.write(OK_BYTES)

        def log_message(self, *a: Any) -> None:
            return None

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try:
        raw = raw_config(providers=("gpt",))
        raw["providers"]["dm"] = {"kind": "openai_decisions", "endpoint": "https://dm.example.invalid", "model_version": PINNED,
                                  "credential_file": str(FIXTURES / "credential.txt"), "timeout_ms": 500, "max_response_bytes": 65536}
        config = load_harness_config(raw)
        dm_cfg = config.providers["dm"]
        local = ProviderConfig(**{**{f: getattr(dm_cfg, f) for f in dm_cfg.__slots__}, "endpoint": f"http://127.0.0.1:{server.server_address[1]}"})
        config = type(config)(config.account_run_min_digits, config.data_limits, config.breaker, {**config.providers, "dm": local}, config.routes)
        decisions = OpenAIDecisions("dm", AdapterSettings(local.endpoint, "CRED", None, 500, 65536), UrllibTransport(allow_loopback_http=True))
        raw_routes = {k: type(v)(("dm", "gpt"), v.by_locale) if k == "decide.ambiguity_flag" else v for k, v in config.routes.items()}
        config = type(config)(config.account_run_min_digits, config.data_limits, config.breaker, config.providers, raw_routes)
        harness = Harness(config, {"dm": decisions, "gpt": ScriptedProvider("gpt", [timeout_error()])}, ManualClock(), InMemoryTelemetry())
        result = decide(harness, YesNoQuestion("damaged", "Does the customer report a damaged item?"), state={"utterance": "The package arrived with a broken screen."})
        assert result.answers == (YesNoAnswer("damaged", None, 0.95),)
        path, body = seen[0]
        assert path == "/v1/decisions" and body["model"] == PINNED and body["questions"] == REQUEST_FIXTURE["questions"]
        assert result.call.attempts[0].tokens_in == 42 and result.call.attempts[0].tokens_out == 0 and result.call.model_version == PINNED
    finally:
        server.shutdown()
        server.server_close()
