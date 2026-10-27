"""Configuration for the OpenAI Responses provider: reasoning effort, the credential by environment variable, and the identifier
the API reports (REQ-INT-002, INV-29; decisions D8 and D9 of benchmark/decisions/2026-10-27-model-selection.md)."""

import json
from typing import Any

import pytest

from noema_model.harness import AttemptOutcome, Harness, HarnessRejection, build_providers, load_harness_config
from noema_model.platform.config import ConfigError
from noema_model.platform.telemetry import InMemoryTelemetry
from noema_model.providers.llm.base import HttpResponse
from tests.support import ManualClock, RecordingTransport, raw_config, run
from tests.test_harness_call import request

SECRET = "sk-synthetic-not-a-real-key"


def responses_raw(**over: Any) -> dict[str, Any]:
    raw = raw_config(providers=("alpha",))
    provider = raw["providers"]["alpha"]
    provider.update(kind="openai_responses", reasoning_effort="medium", max_output_tokens=4000)
    provider.update(over)
    return raw


def without(raw: dict[str, Any], *keys: str) -> dict[str, Any]:
    for key in keys:
        raw["providers"]["alpha"].pop(key, None)
    return raw


def test_a_responses_provider_requires_a_known_reasoning_effort() -> None:
    assert load_harness_config(responses_raw()).providers["alpha"].reasoning_effort == "medium"
    for bad in ("", "MEDIUM", "maximum", 3):
        with pytest.raises(ConfigError):
            load_harness_config(responses_raw(reasoning_effort=bad))
    with pytest.raises(ConfigError):
        load_harness_config(without(responses_raw(), "reasoning_effort"))


def test_the_effort_values_are_the_published_ones() -> None:
    for effort in ("none", "minimal", "low", "medium", "high", "xhigh", "max"):
        assert load_harness_config(responses_raw(reasoning_effort=effort)).providers["alpha"].reasoning_effort == effort


def test_other_kinds_and_decision_models_take_no_reasoning_effort() -> None:
    raw = raw_config(providers=("alpha",))
    raw["providers"]["alpha"]["reasoning_effort"] = "medium"
    with pytest.raises(ConfigError):
        load_harness_config(raw)


def test_exactly_one_of_credential_file_and_credential_env() -> None:
    env = responses_raw(credential_env="OPENAI_API_KEY")
    del env["providers"]["alpha"]["credential_file"]
    assert load_harness_config(env).providers["alpha"].credential_env == "OPENAI_API_KEY"
    both = responses_raw(credential_env="OPENAI_API_KEY")
    with pytest.raises(ConfigError):
        load_harness_config(both)
    with pytest.raises(ConfigError):
        load_harness_config(without(responses_raw(), "credential_file"))


@pytest.mark.parametrize("name", ["", "openai_api_key", "1KEY", "A B", "A-B", "X" * 65, 3])
def test_the_environment_variable_name_is_checked(name: Any) -> None:
    raw = without(responses_raw(credential_env=name), "credential_file")
    with pytest.raises(ConfigError):
        load_harness_config(raw)


def test_the_credential_is_read_from_the_environment_and_never_kept_in_the_configuration() -> None:
    raw = without(responses_raw(credential_env="OPENAI_API_KEY"), "credential_file")
    config = load_harness_config(raw)
    assert SECRET not in repr(config) and SECRET not in json.dumps(raw)
    transport = RecordingTransport(HttpResponse(200, b"{}"))
    provider = build_providers(config, transport, {"OPENAI_API_KEY": SECRET})["alpha"]
    assert SECRET not in repr(getattr(provider, "_settings"))  # the adapter holds it; its settings hide it in repr


def test_a_missing_or_empty_variable_stops_startup_and_the_error_never_carries_a_value() -> None:
    config = load_harness_config(without(responses_raw(credential_env="OPENAI_API_KEY"), "credential_file"))
    for environ in ({}, {"OPENAI_API_KEY": ""}, {"OPENAI_API_KEY": "  \n"}):
        with pytest.raises(ConfigError) as caught:
            build_providers(config, RecordingTransport(HttpResponse(200, b"{}")), environ)
        assert "OPENAI_API_KEY" in str(caught.value) and SECRET not in str(caught.value)


def test_the_key_reaches_the_request_header_and_nothing_else() -> None:
    config = load_harness_config(without(responses_raw(credential_env="OPENAI_API_KEY", model_version="gpt-6-luna"), "credential_file"))
    body = {"status": "completed", "model": "gpt-6-luna",
            "output": [{"type": "message", "content": [{"type": "output_text", "text": json.dumps({"surface": "20k", "confidence": 0.9})}]}],
            "usage": {"input_tokens": 100, "output_tokens": 300}}
    transport = RecordingTransport(HttpResponse(200, json.dumps(body).encode()))
    telemetry = InMemoryTelemetry()
    harness = Harness(config, build_providers(config, transport, {"OPENAI_API_KEY": SECRET}), ManualClock(), telemetry)
    result = run(harness.generate(request()))
    url, headers, sent = transport.requests[0]
    assert headers["authorization"] == f"Bearer {SECRET}" and SECRET not in json.dumps(sent)
    assert sent["model"] == "gpt-6-luna" and sent["reasoning"] == {"effort": "medium"} and sent["max_output_tokens"] == 4000
    assert SECRET not in json.dumps(result.call.to_dict()) and SECRET not in repr(telemetry.events)


def test_reported_model_version_must_be_a_plain_identifier_and_is_optional() -> None:
    assert load_harness_config(responses_raw()).providers["alpha"].reported_model_version is None
    ok = load_harness_config(responses_raw(reported_model_version="gpt-6-luna-2026-10-20"))
    assert ok.providers["alpha"].reported_model_version == "gpt-6-luna-2026-10-20"
    for bad in ("", " ", "a b", 3, "x" * 200):
        with pytest.raises(ConfigError):
            load_harness_config(responses_raw(reported_model_version=bad))


def call(raw: dict[str, Any], reported: str) -> Any:
    config = load_harness_config(raw)
    body = {"status": "completed", "model": reported,
            "output": [{"type": "message", "content": [{"type": "output_text", "text": json.dumps({"surface": "20k", "confidence": 0.9})}]}],
            "usage": {"input_tokens": 1, "output_tokens": 1}}
    transport = RecordingTransport(HttpResponse(200, json.dumps(body).encode()))
    harness = Harness(config, build_providers(config, transport, {}), ManualClock(), InMemoryTelemetry())
    return harness, transport


def test_the_reply_must_report_the_configured_reported_version_when_one_is_set() -> None:
    raw = responses_raw(model_version="gpt-6-luna", reported_model_version="gpt-6-luna-2026-10-20")
    harness, transport = call(raw, "gpt-6-luna-2026-10-20")
    assert run(harness.generate(request())).value.surface == "20k"
    assert transport.requests[0][2]["model"] == "gpt-6-luna"  # the ID sent is model_version
    harness, _ = call(raw, "gpt-6-luna")  # the alias is not the pinned reported identifier
    with pytest.raises(HarnessRejection) as caught:
        run(harness.generate(request()))
    assert caught.value.call is not None and caught.value.call.attempts[0].outcome is AttemptOutcome.VERSION_MISMATCH
    assert caught.value.call.attempts[0].model_version_reported == "gpt-6-luna"


def test_without_a_reported_version_the_reply_must_report_the_model_version() -> None:
    raw = responses_raw(model_version="gpt-6-luna")
    harness, _ = call(raw, "gpt-6-luna")
    assert run(harness.generate(request())).value.surface == "20k"
    harness, _ = call(raw, "gpt-6-luna-2026-10-20")
    with pytest.raises(HarnessRejection):
        run(harness.generate(request()))
