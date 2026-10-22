"""The harness call path: generate, failover, the circuit breaker, version pinning, accounting (REQ-INT-002, REQ-NFR-007,
REQ-CTX-007, INV-02). Faults are scripted at the provider seam; the wire path is covered in test_transport.py."""

import asyncio
import json
from typing import Any

import pytest
from pydantic import BaseModel, ConfigDict

from noema_model.harness import (
    AttemptOutcome,
    GenerateRequest,
    Harness,
    HarnessRejection,
    PromptTemplate,
    Reason,
)
from noema_model.platform.telemetry import InMemoryTelemetry
from noema_model.providers.llm.base import FinishReason, ProviderError, ProviderFailure
from tests.support import ManualClock, ScriptedProvider, make_config, raw_config, reply, run, timeout_error
from noema_model.harness.config import load_harness_config

TEMPLATE = PromptTemplate("test.slots", 3, "SYSTEM", "INSTRUCTION")
GOOD = {"surface": "20k", "confidence": 0.9}


class Out(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True, frozen=True)
    surface: str
    confidence: float


def version(name: str) -> str:
    return f"model-{name}-2026-01-01"


def good(name: str, **kw: Any) -> Any:
    return reply(GOOD, version=version(name), **kw)


def build(scripts: dict[str, list[Any]], **cfg: Any) -> tuple[Harness, dict[str, ScriptedProvider], ManualClock, InMemoryTelemetry]:
    config = make_config(providers=tuple(scripts), **cfg)
    providers = {n: ScriptedProvider(n, steps) for n, steps in scripts.items()}
    clock, telemetry = ManualClock(), InMemoryTelemetry()
    return Harness(config, providers, clock, telemetry), providers, clock, telemetry


def request(fields: dict[str, str] | None = None, locale: str | None = None, output: type[BaseModel] = Out) -> GenerateRequest[Any]:
    return GenerateRequest("slot_values", TEMPLATE, fields or {"utterance": "I sent 20k to Tunde"}, output, locale, "cor_test")


def rejection(h: Harness, req: GenerateRequest[Any]) -> HarnessRejection:
    with pytest.raises(HarnessRejection) as caught:
        run(h.generate(req))
    return caught.value


# --- the happy path and what is recorded ------------------------------------------------------------------------

def test_generate_returns_a_validated_value_and_a_full_record() -> None:
    h, providers, _, telemetry = build({"alpha": [good("alpha")], "beta": [good("beta")]})
    result = run(h.generate(request()))
    assert result.value == Out(surface="20k", confidence=0.9)
    call = result.call
    assert (call.provider, call.model_version) == ("alpha", version("alpha"))
    assert (call.template_id, call.template_version, call.template_digest) == ("test.slots", 3, TEMPLATE.digest)
    assert (call.tokens_in, call.tokens_out) == (100, 20)
    assert len(call.input_digest) == 64 and call.attempts[0].outcome is AttemptOutcome.OK
    assert providers["beta"].calls == []
    assert telemetry.total("noema_provider_call_total", provider="alpha", outcome="OK") == 1
    assert [s[0] for s in telemetry.spans] == ["harness.call", "harness.provider_call"]
    assert all(s[1] == "cor_test" for s in telemetry.spans)


def test_the_provider_sees_the_pinned_version_the_forced_schema_and_the_template() -> None:
    h, providers, _, _ = build({"alpha": [good("alpha")]})
    run(h.generate(request()))
    sent = providers["alpha"].calls[0]
    assert sent.model_version == version("alpha")
    assert sent.system == "SYSTEM" and sent.user.startswith("INSTRUCTION\n\n<<<DATA")
    assert sent.output_schema["additionalProperties"] is False
    assert set(sent.output_schema["required"]) == {"surface", "confidence"}


def test_cost_comes_only_from_the_dated_rate_card_and_rounds_up() -> None:
    h, _, _, _ = build({"alpha": [good("alpha", tokens=(1, 1))]})
    call = run(h.generate(request())).call
    # 1 input token at 3,000,000 per million is 3; 1 output token at 15,000,000 per million is 15.
    assert (call.cost_micro, call.cost_currency, call.cost_rate_as_of, call.cost_complete) == (18, "USD", "2026-10-01", True)


def test_a_fractional_rate_is_rounded_up_so_cost_is_never_understated() -> None:
    raw = raw_config(providers=("alpha",))
    raw["providers"]["alpha"]["rate"]["input_micro_per_mtok"] = 1_500_000
    providers = {"alpha": ScriptedProvider("alpha", [good("alpha", tokens=(1, 1))])}
    h = Harness(load_harness_config(raw), providers, ManualClock(), InMemoryTelemetry())
    assert run(h.generate(request())).call.cost_micro == 2 + 15  # 1.5 rounds up to 2


def test_no_rate_card_means_no_cost_not_a_guess() -> None:
    h, _, _, _ = build({"alpha": [good("alpha")]}, rate=False)
    call = run(h.generate(request())).call
    assert (call.cost_micro, call.cost_complete) == (None, False)


def test_missing_token_counts_make_the_cost_incomplete() -> None:
    h, _, _, _ = build({"alpha": [good("alpha", tokens=None)]})
    call = run(h.generate(request())).call
    assert (call.cost_micro, call.cost_complete, call.tokens_in) == (None, False, 0)


def test_accounting_sums_match_provider_reported_usage_across_attempts() -> None:
    h, _, _, _ = build({"alpha": [reply(GOOD, version=version("alpha"), tokens=(11, 3), raw="{}")], "beta": [good("beta", tokens=(7, 5))]})
    call = run(h.generate(request())).call
    assert (call.tokens_in, call.tokens_out) == (11 + 7, 3 + 5)
    assert call.cost_micro == -(-11 * 3_000_000 // 1_000_000) + -(-3 * 15_000_000 // 1_000_000) + -(-7 * 3_000_000 // 1_000_000) + -(-5 * 15_000_000 // 1_000_000)


# --- reject, never repair; fail over on a refused reply ---------------------------------------------------------

BAD_OUTPUTS = [
    "",
    "not json",
    "[]",
    "null",
    '{"surface": "20k"}',
    '{"surface": "20k", "confidence": 0.9, "extra": 1}',
    '{"surface": 20, "confidence": 0.9}',
    '{"surface": "20k", "confidence": "0.9"}',
    '{"surface": "20k", "confidence": NaN}',
    '```json\n{"surface": "20k", "confidence": 0.9}\n```',
    'Here you go: {"surface": "20k", "confidence": 0.9}',
    '{"surface": "20k", "confidence": 0.9} trailing',
    '{"surface": "20k", "confidence": 0.9}{"surface": "30k", "confidence": 0.9}',
    '{"surface": null, "confidence": 0.9}',
    '{"surface": "20k", "confidence": Infinity}',
    '{"surface": "20k", "surface": "30k", "confidence": 0.9}',
]


@pytest.mark.parametrize("raw", BAD_OUTPUTS)
def test_every_malformed_output_is_rejected_whole_and_never_repaired(raw: str) -> None:
    h, _, _, _ = build({"alpha": [reply(None, version=version("alpha"), raw=raw)]})
    rejected = rejection(h, request())
    assert (rejected.reason, rejected.code, rejected.retryable) == (Reason.OUTPUT_MISMATCH, "SCHEMA_INVALID", False)
    assert rejected.call is not None and rejected.call.attempts[0].outcome is AttemptOutcome.OUTPUT_MISMATCH


def test_a_schema_rejection_fails_over_to_the_next_provider() -> None:
    h, providers, _, telemetry = build({"alpha": [reply(None, version=version("alpha"), raw="{}")], "beta": [good("beta")]})
    result = run(h.generate(request()))
    assert result.call.provider == "beta"
    assert [a.outcome for a in result.call.attempts] == [AttemptOutcome.OUTPUT_MISMATCH, AttemptOutcome.OK]
    assert len(providers["alpha"].calls) == 1
    assert [(f.from_provider, f.to_provider, f.reason) for f in result.call.failovers] == [("alpha", "beta", AttemptOutcome.OUTPUT_MISMATCH)]
    assert telemetry.events[0][0] == "PROVIDER_FAILOVER"


def test_truncated_and_refused_replies_are_rejected_and_fail_over() -> None:
    h, _, _, _ = build({
        "alpha": [good("alpha", finish=FinishReason.TRUNCATED)],
        "beta": [good("beta", finish=FinishReason.REFUSED)],
    })
    rejected = rejection(h, request())
    assert rejected.reason is Reason.OUTPUT_REFUSED
    assert [a.outcome for a in rejected.call.attempts] == [AttemptOutcome.OUTPUT_TRUNCATED, AttemptOutcome.OUTPUT_REFUSED]  # type: ignore[union-attr]


def test_a_content_rejection_does_not_charge_the_breaker() -> None:
    h, providers, _, _ = build({"alpha": [reply(None, version=version("alpha"), raw="{}")]}, failure_threshold=1)
    for _ in range(3):
        assert rejection(h, request()).reason is Reason.OUTPUT_MISMATCH
    assert len(providers["alpha"].calls) == 3


# --- failover on provider failure -------------------------------------------------------------------------------

@pytest.mark.parametrize(
    ("step", "outcome"),
    [
        (timeout_error(), AttemptOutcome.TIMEOUT),
        (ProviderError(ProviderFailure.UNAVAILABLE, 503), AttemptOutcome.UNAVAILABLE),
        (ProviderError(ProviderFailure.UNAVAILABLE, 500), AttemptOutcome.UNAVAILABLE),
        (ProviderError(ProviderFailure.RATE_LIMITED, 429), AttemptOutcome.RATE_LIMITED),
        (ProviderError(ProviderFailure.REJECTED, 401), AttemptOutcome.REJECTED),
        (ProviderError(ProviderFailure.MALFORMED, 200), AttemptOutcome.MALFORMED),
    ],
)
def test_a_provider_failure_fails_over(step: ProviderError, outcome: AttemptOutcome) -> None:
    h, providers, _, telemetry = build({"alpha": [step], "beta": [good("beta")]})
    result = run(h.generate(request()))
    assert result.call.provider == "beta"
    assert result.call.attempts[0].outcome is outcome
    assert telemetry.total("noema_provider_failover_total", from_provider="alpha", to_provider="beta") == 1


def test_a_hanging_provider_times_out_at_the_configured_deadline_and_fails_over() -> None:
    h, _, _, _ = build({"alpha": ["hang"], "beta": [good("beta")]}, timeout_ms=40)
    result = run(h.generate(request()))
    assert result.call.attempts[0].outcome is AttemptOutcome.TIMEOUT and result.call.provider == "beta"


def test_every_provider_failing_is_a_typed_retryable_rejection_not_a_guess() -> None:
    h, _, _, _ = build({"alpha": [timeout_error()], "beta": [ProviderError(ProviderFailure.UNAVAILABLE, 503)]})
    rejected = rejection(h, request())
    assert (rejected.reason, rejected.code, rejected.retryable) == (Reason.NO_PROVIDER_ANSWERED, "PROVIDER_UNAVAILABLE", True)
    body = rejected.error_body("cor_test")
    assert body["code"] == "PROVIDER_UNAVAILABLE" and body["retryable"] is True and body["correlation_id"] == "cor_test"
    assert rejected.call is not None and rejected.call.provider is None


def test_a_reply_from_another_version_than_the_pin_is_refused_and_fails_over() -> None:
    # An alias such as "gpt-4o" resolves to a snapshot; the reply says which, and it is not the pin.
    h, _, _, _ = build({"alpha": [reply(GOOD, version="model-alpha-2026-02-02")], "beta": [good("beta")]})
    result = run(h.generate(request()))
    assert result.call.attempts[0].outcome is AttemptOutcome.VERSION_MISMATCH and result.call.provider == "beta"


def test_a_reply_that_reports_no_version_is_refused() -> None:
    h, _, _, _ = build({"alpha": [reply(GOOD, version=None)]})
    assert rejection(h, request()).reason is Reason.NO_PROVIDER_ANSWERED


def test_the_locale_route_overrides_the_default_order() -> None:
    routes = {"slot_values": {"default": ["alpha", "beta"], "by_locale": {"yo": ["beta"]}}}
    config = make_config(routes=routes)
    providers = {"alpha": ScriptedProvider("alpha", [good("alpha")]), "beta": ScriptedProvider("beta", [good("beta")])}
    h = Harness(config, providers, ManualClock(), InMemoryTelemetry())
    assert run(h.generate(request(locale="yo"))).call.provider == "beta"
    assert run(h.generate(request(locale="pcm"))).call.provider == "alpha"
    assert run(h.generate(request(locale=None))).call.provider == "alpha"


def test_switching_the_default_provider_is_a_configuration_change_only() -> None:
    for order, expected in ((["alpha", "beta"], "alpha"), (["beta", "alpha"], "beta")):
        config = make_config(routes={"slot_values": {"default": order, "by_locale": {}}})
        providers = {"alpha": ScriptedProvider("alpha", [good("alpha")]), "beta": ScriptedProvider("beta", [good("beta")])}
        assert run(Harness(config, providers, ManualClock(), InMemoryTelemetry()).generate(request())).call.provider == expected


def test_generate_cannot_borrow_a_decide_route() -> None:
    h, providers, _, _ = build({"alpha": [good("alpha")]})
    req = GenerateRequest("decide.intent_node_classification", TEMPLATE, {"utterance": "x"}, Out, None, "cor_test")
    assert rejection(h, req).reason is Reason.ROUTE_MISSING and providers["alpha"].calls == []


def test_a_route_that_is_not_configured_is_refused() -> None:
    h, _, _, _ = build({"alpha": [good("alpha")]})
    req = GenerateRequest("unknown_route", TEMPLATE, {"utterance": "x"}, Out, None, "cor_test")
    assert rejection(h, req).reason is Reason.ROUTE_MISSING


# --- the circuit breaker -----------------------------------------------------------------------------------------

def test_the_breaker_opens_after_the_threshold_skips_the_provider_and_probes_after_the_cooldown() -> None:
    h, providers, clock, telemetry = build({"alpha": [timeout_error(), timeout_error(), good("alpha")], "beta": [good("beta")]})
    for _ in range(2):
        assert run(h.generate(request())).call.provider == "beta"
    assert len(providers["alpha"].calls) == 2
    third = run(h.generate(request())).call
    assert third.attempts[0].outcome is AttemptOutcome.BREAKER_OPEN and len(providers["alpha"].calls) == 2
    assert telemetry.total("noema_provider_breaker_open_total", provider="alpha") == 1  # one opening, not one per call
    clock.advance(1_000)
    probe = run(h.generate(request())).call
    assert probe.provider == "alpha" and len(providers["alpha"].calls) == 3
    again = run(h.generate(request())).call
    assert again.provider == "alpha"  # the probe's success closed the breaker


def test_a_failed_probe_reopens_the_breaker_for_a_full_cooldown() -> None:
    h, providers, clock, _ = build({"alpha": [timeout_error()], "beta": [good("beta")]}, failure_threshold=1)
    run(h.generate(request()))
    clock.advance(1_000)
    run(h.generate(request()))  # the probe fails
    calls = len(providers["alpha"].calls)
    clock.advance(999)
    run(h.generate(request()))
    assert len(providers["alpha"].calls) == calls
    clock.advance(1)
    run(h.generate(request()))
    assert len(providers["alpha"].calls) == calls + 1


def test_only_one_probe_is_let_through_while_half_open() -> None:
    async def scenario() -> tuple[int, list[str | None]]:
        h, providers, clock, _ = build({"alpha": [timeout_error(), "hang"], "beta": [good("beta")]}, failure_threshold=1, timeout_ms=100)
        await h.generate(request())
        clock.advance(1_000)
        results = await asyncio.gather(h.generate(request()), h.generate(request()), h.generate(request()))
        return len(providers["alpha"].calls), [r.call.provider for r in results]

    calls, served = asyncio.run(scenario())
    assert calls == 2  # the opening failure plus exactly one probe
    assert served == ["beta", "beta", "beta"]


def test_a_cancelled_probe_does_not_wedge_the_breaker() -> None:
    async def scenario() -> int:
        h, providers, clock, _ = build({"alpha": [timeout_error(), "hang", good("alpha")], "beta": [good("beta")]}, failure_threshold=1, timeout_ms=5_000)
        await h.generate(request())
        clock.advance(1_000)
        task = asyncio.create_task(h.generate(request()))
        await asyncio.sleep(0.05)
        task.cancel()
        with pytest.raises(asyncio.CancelledError):
            await task
        result = await h.generate(request())
        assert result.call.provider == "alpha"
        return len(providers["alpha"].calls)

    assert asyncio.run(scenario()) == 3


# --- input handling and output-type guard ---------------------------------------------------------------------------

def test_masking_delimiting_and_truncation_reach_the_prompt_and_the_record() -> None:
    h, providers, _, telemetry = build({"alpha": [good("alpha")]})
    long_name = "A" * 300
    result = run(h.generate(request({"utterance": "pay 0123456789 now", "beneficiary": long_name})))
    prompt = providers["alpha"].calls[0].user
    assert "0123456789" not in prompt and "******6789" in prompt
    assert "A" * 257 not in prompt and "A" * 256 in prompt
    assert result.call.truncated_fields == ("beneficiary",)
    assert any(e[0] == "DATA_FIELD_TRUNCATED" for e in telemetry.events)


def test_an_output_type_that_allows_extras_or_defaults_is_refused() -> None:
    class Loose(BaseModel):
        surface: str

    class Defaulted(BaseModel):
        model_config = ConfigDict(extra="forbid", strict=True)
        surface: str = "x"

    h, providers, _, _ = build({"alpha": [good("alpha")]})
    for bad in (Loose, Defaulted):
        assert rejection(h, request(output=bad)).reason is Reason.OUTPUT_TYPE_UNSAFE
    assert providers["alpha"].calls == []


def test_a_field_with_a_bad_name_is_refused_before_any_call() -> None:
    h, providers, _, _ = build({"alpha": [good("alpha")]})
    assert rejection(h, request({"Bad Name": "x"})).reason is Reason.FIELD_INVALID
    assert providers["alpha"].calls == []


def test_the_credential_and_reasoning_never_reach_a_record_or_telemetry() -> None:
    h, _, _, telemetry = build({"alpha": [good("alpha")]})
    result = run(h.generate(request()))
    everything = repr(result) + repr(telemetry)
    assert "test-credential-do-not-use" not in everything


def test_harness_refuses_to_start_without_an_adapter_for_a_configured_provider() -> None:
    with pytest.raises(ValueError):
        Harness(load_harness_config(raw_config()), {}, ManualClock(), InMemoryTelemetry())


# --- generated corpus (T5: the harness rejects every malformed provider output) ---------------------------------------

from hypothesis import HealthCheck, given, settings  # noqa: E402
from hypothesis import strategies as st  # noqa: E402

JSON_VALUES = st.recursive(
    st.none() | st.booleans() | st.integers() | st.floats(allow_nan=False, allow_infinity=False) | st.text(max_size=12),
    lambda inner: st.lists(inner, max_size=3) | st.dictionaries(st.text(max_size=8), inner, max_size=4),
    max_leaves=8,
)


@settings(max_examples=400, deadline=None, suppress_health_check=[HealthCheck.function_scoped_fixture])
@given(st.one_of(JSON_VALUES.map(json.dumps), st.text(max_size=60)))
def test_property_an_output_is_accepted_only_if_it_is_exactly_the_schema(raw: str) -> None:
    h, _, _, _ = build({"alpha": [reply(None, version=version("alpha"), raw=raw)]})
    try:
        result = run(h.generate(request()))
    except HarnessRejection as rejected:
        assert rejected.reason is Reason.OUTPUT_MISMATCH and rejected.call is not None
        return
    parsed = json.loads(raw)  # whatever was accepted must have been exactly the two typed fields, nothing more
    assert set(parsed) == {"surface", "confidence"} and isinstance(parsed["surface"], str)
    assert isinstance(parsed["confidence"], (int, float)) and not isinstance(parsed["confidence"], bool)
    assert result.value.surface == parsed["surface"]


# --- cost exposed on every call record (REQ-NFR-007) ------------------------------------------------------------------------

def two_rate_harness(steps: dict[str, list[Any]], *, beta_currency: str = "USD", beta_rate: bool = True) -> Harness:
    raw = raw_config(providers=tuple(steps))
    raw["providers"]["alpha"]["rate"] = {"input_micro_per_mtok": 1_500_000, "output_micro_per_mtok": 7_000_000, "currency": "USD", "as_of": "2026-10-01"}
    if beta_rate:
        raw["providers"]["beta"]["rate"] = {"input_micro_per_mtok": 400_000, "output_micro_per_mtok": 2_000_000, "currency": beta_currency, "as_of": "2026-10-15"}
    else:
        raw["providers"]["beta"].pop("rate")
    providers = {n: ScriptedProvider(n, s) for n, s in steps.items()}
    return Harness(load_harness_config(raw), providers, ManualClock(), InMemoryTelemetry())


def test_every_attempt_carries_its_tokens_its_price_and_its_cost() -> None:
    h = two_rate_harness({"alpha": [reply(None, version=version("alpha"), raw="{}", tokens=(10, 4))], "beta": [good("beta", tokens=(1000, 500))]})
    call = run(h.generate(request())).call
    first, second = call.attempts
    assert (first.tokens_in, first.tokens_out, first.rate.input_micro_per_mtok, first.cost_micro) == (10, 4, 1_500_000, 15 + 28)  # type: ignore[union-attr]
    assert (second.tokens_in, second.tokens_out, second.rate.as_of, second.cost_micro) == (1000, 500, "2026-10-15", 400 + 1000)  # type: ignore[union-attr]
    assert call.cost_micro == first.cost_micro + second.cost_micro == 15 + 28 + 400 + 1000  # type: ignore[operator]
    assert (call.tokens_in, call.tokens_out, call.cost_currency, call.cost_complete) == (1010, 504, "USD", True)


def test_the_call_record_as_data_is_complete_json_safe_and_exact() -> None:
    h = two_rate_harness({"alpha": [timeout_error()], "beta": [good("beta", tokens=(1000, 500))]})
    data = run(h.generate(request())).call.to_dict()
    assert json.loads(json.dumps(data)) == data
    assert data["cost"] == {"total_micro": 1400, "currency": "USD", "rate_as_of": "2026-10-15", "complete": False}
    assert (data["tokens_in"], data["tokens_out"], data["provider"], data["model_version"]) == (1000, 500, "beta", version("beta"))
    assert data["template"] == {"id": "test.slots", "version": 3, "digest": TEMPLATE.digest}
    timed_out, answered = data["attempts"]
    assert timed_out["outcome"] == "TIMEOUT" and timed_out["cost_micro"] is None and timed_out["tokens_in"] is None
    assert timed_out["price"] == {"input_micro_per_mtok": 1_500_000, "output_micro_per_mtok": 7_000_000, "currency": "USD", "as_of": "2026-10-01"}
    assert answered["price"]["input_micro_per_mtok"] == 400_000 and answered["cost_micro"] == 1400

    def no_floats(node: Any) -> None:
        assert not isinstance(node, float)
        for child in (node.values() if isinstance(node, dict) else node if isinstance(node, list) else ()):
            no_floats(child)

    no_floats(data)


def test_the_record_as_data_holds_no_prompt_text_credential_or_reasoning() -> None:
    h = two_rate_harness({"alpha": [good("alpha")], "beta": [good("beta")]})
    text = json.dumps(run(h.generate(request({"utterance": "I sent 20k to Tunde"}))).call.to_dict())
    assert "Tunde" not in text and "test-credential-do-not-use" not in text and "INSTRUCTION" not in text


def test_a_skipped_provider_costs_nothing_and_an_unpriced_one_has_no_price() -> None:
    h = two_rate_harness({"alpha": [timeout_error()], "beta": [good("beta")]}, beta_rate=False)
    first = run(h.generate(request())).call
    h2 = two_rate_harness({"alpha": [timeout_error()], "beta": [good("beta")]}, beta_rate=False)
    h2._breakers["alpha"].failure_threshold = 1
    run(h2.generate(request()))
    skipped = run(h2.generate(request())).call.attempts[0]
    assert (skipped.outcome, skipped.cost_micro, skipped.rate) == (AttemptOutcome.BREAKER_OPEN, 0, None)
    answered = first.attempts[1]
    assert answered.rate is None and answered.cost_micro is None and first.cost_micro is None and first.cost_complete is False
    assert first.to_dict()["attempts"][1]["price"] is None


def test_two_currencies_are_never_added() -> None:
    h = two_rate_harness({"alpha": [reply(None, version=version("alpha"), raw="{}")], "beta": [good("beta")]}, beta_currency="EUR")
    call = run(h.generate(request())).call
    assert call.cost_micro is None and call.cost_currency is None and call.cost_complete is False
    assert all(a.cost_micro is not None for a in call.attempts)  # each attempt is still priced in its own currency


def test_a_rejected_call_still_reports_what_it_cost() -> None:
    h = two_rate_harness({"alpha": [reply(None, version=version("alpha"), raw="{}", tokens=(10, 4))], "beta": [reply(None, version=version("beta"), raw="[]", tokens=(10, 4))]})
    rejected = rejection(h, request())
    assert rejected.call is not None and rejected.call.cost_micro == (15 + 28) + (4 + 8) and rejected.call.to_dict()["cost"]["total_micro"] == 55
