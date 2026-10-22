"""Adapter contract tests (REQ-INT-002). They play fixtures through the adapters and check the request each one sends.

The fixtures are synthetic, written from the vendors' published shapes; no live call was made (tests/fixtures/providers/
README.md): not run: no access.
"""

import json
from pathlib import Path
from typing import Any

import pytest

from noema_model.providers.llm.anthropic_messages import AnthropicMessages
from noema_model.providers.llm.base import (
    AdapterSettings,
    FinishReason,
    HttpResponse,
    ProviderError,
    ProviderFailure,
    ProviderRequest,
)
from noema_model.providers.llm.gemini_generate import GeminiGenerate
from noema_model.providers.llm.openai_chat import OpenAIChat
from tests.support import FIXTURES, RecordingTransport, run

MARKER = "REASONING-MARKER-must-never-surface"
SCHEMA: dict[str, Any] = {"type": "object", "properties": {"answers": {"type": "array"}}, "required": ["answers"],
                          "additionalProperties": False}
REQUEST = ProviderRequest("pinned-model-2026-01-01", "SYSTEM", "USER", "harness_decide", SCHEMA, 512, None)


def fixture(name: str, status: int = 200) -> HttpResponse:
    return HttpResponse(status=status, body=(FIXTURES / "providers" / name).read_bytes())


def settings(**over: Any) -> AdapterSettings:
    base: dict[str, Any] = dict(endpoint="https://vendor.example.invalid/", credential="CRED-VALUE", api_version="2023-06-01",
                                timeout_ms=1500, max_response_bytes=65536)
    return AdapterSettings(**{**base, **over})


def anthropic(name: str, status: int = 200) -> tuple[AnthropicMessages, RecordingTransport]:
    transport = RecordingTransport(fixture(name, status))
    return AnthropicMessages("a", settings(), transport), transport


def openai(name: str, status: int = 200) -> tuple[OpenAIChat, RecordingTransport]:
    transport = RecordingTransport(fixture(name, status))
    return OpenAIChat("o", settings(api_version=None), transport), transport


def gemini(name: str, status: int = 200) -> tuple[GeminiGenerate, RecordingTransport]:
    transport = RecordingTransport(fixture(name, status))
    return GeminiGenerate("g", settings(api_version=None), transport), transport


# --- Anthropic ----------------------------------------------------------------------------------------------------

def test_anthropic_request_shape_and_forced_tool() -> None:
    adapter, transport = anthropic("anthropic_ok.json")
    run(adapter.complete(REQUEST))
    url, headers, body = transport.requests[0]
    assert url == "https://vendor.example.invalid/v1/messages"
    assert headers["x-api-key"] == "CRED-VALUE" and headers["anthropic-version"] == "2023-06-01"
    assert body["model"] == "pinned-model-2026-01-01" and body["max_tokens"] == 512
    assert body["tool_choice"] == {"type": "tool", "name": "harness_decide"}
    assert body["tools"][0]["input_schema"] == SCHEMA
    assert "temperature" not in body


def test_anthropic_parses_tool_input_and_drops_reasoning() -> None:
    adapter, _ = anthropic("anthropic_ok.json")
    response = run(adapter.complete(REQUEST))
    assert json.loads(response.output_json) == {"answers": []}
    assert MARKER not in repr(response)
    assert (response.model_version_reported, response.tokens_in, response.tokens_out) == ("pinned-model-2026-01-01", 120, 30)
    assert response.finish is FinishReason.COMPLETE


def test_anthropic_text_instead_of_tool_is_returned_raw_for_the_gate_to_reject() -> None:
    adapter, _ = anthropic("anthropic_text_only.json")
    response = run(adapter.complete(REQUEST))
    assert response.output_json == 'Sure! {"answers": []}'  # not extracted, not repaired


def test_anthropic_max_tokens_is_truncated() -> None:
    adapter, _ = anthropic("anthropic_truncated.json")
    assert run(adapter.complete(REQUEST)).finish is FinishReason.TRUNCATED


def test_anthropic_requires_api_version() -> None:
    with pytest.raises(ValueError):
        AnthropicMessages("a", settings(api_version=None), RecordingTransport(fixture("anthropic_ok.json")))


# --- OpenAI -------------------------------------------------------------------------------------------------------

def test_openai_request_shape_and_strict_schema() -> None:
    adapter, transport = openai("openai_ok.json")
    run(adapter.complete(REQUEST))
    url, headers, body = transport.requests[0]
    assert url == "https://vendor.example.invalid/v1/chat/completions"
    assert headers["authorization"] == "Bearer CRED-VALUE"
    fmt = body["response_format"]
    assert fmt["type"] == "json_schema" and fmt["json_schema"]["strict"] is True and fmt["json_schema"]["schema"] == SCHEMA
    assert [m["role"] for m in body["messages"]] == ["system", "user"]


def test_openai_parses_content_and_usage() -> None:
    adapter, _ = openai("openai_ok.json")
    response = run(adapter.complete(REQUEST))
    assert json.loads(response.output_json) == {"answers": []}
    assert (response.tokens_in, response.tokens_out, response.finish) == (120, 30, FinishReason.COMPLETE)


def test_openai_refusal_and_length() -> None:
    assert run(openai("openai_refusal.json")[0].complete(REQUEST)).finish is FinishReason.REFUSED
    assert run(openai("openai_length.json")[0].complete(REQUEST)).finish is FinishReason.TRUNCATED


def test_temperature_is_sent_only_when_configured() -> None:
    adapter, transport = openai("openai_ok.json")
    run(adapter.complete(ProviderRequest("pinned-model-2026-01-01", "S", "U", "n", SCHEMA, 8, 0.0)))
    assert transport.requests[0][2]["temperature"] == 0.0


# --- Gemini -------------------------------------------------------------------------------------------------------

def test_gemini_request_shape() -> None:
    adapter, transport = gemini("gemini_ok.json")
    run(adapter.complete(REQUEST))
    url, headers, body = transport.requests[0]
    assert url == "https://vendor.example.invalid/v1beta/models/pinned-model-2026-01-01:generateContent"
    assert headers["x-goog-api-key"] == "CRED-VALUE"
    assert body["generationConfig"]["responseMimeType"] == "application/json"
    assert body["generationConfig"]["responseJsonSchema"] == SCHEMA


def test_gemini_drops_thought_parts() -> None:
    adapter, _ = gemini("gemini_ok.json")
    response = run(adapter.complete(REQUEST))
    assert json.loads(response.output_json) == {"answers": []}
    assert MARKER not in repr(response)
    assert (response.tokens_in, response.tokens_out) == (120, 30)


def test_gemini_safety_and_max_tokens() -> None:
    assert run(gemini("gemini_safety.json")[0].complete(REQUEST)).finish is FinishReason.REFUSED
    assert run(gemini("gemini_max_tokens.json")[0].complete(REQUEST)).finish is FinishReason.TRUNCATED


def test_gemini_refuses_a_model_name_that_could_alter_the_path() -> None:
    adapter, transport = gemini("gemini_ok.json")
    bad = ProviderRequest("../../evil", "S", "U", "n", SCHEMA, 8, None)
    with pytest.raises(ProviderError):
        run(adapter.complete(bad))
    assert transport.requests == []


# --- All adapters: error mapping -----------------------------------------------------------------------------------

@pytest.mark.parametrize("make", [anthropic, openai, gemini])
@pytest.mark.parametrize(
    ("status", "kind"),
    [(408, ProviderFailure.TIMEOUT), (429, ProviderFailure.RATE_LIMITED), (500, ProviderFailure.UNAVAILABLE),
     (503, ProviderFailure.UNAVAILABLE), (401, ProviderFailure.REJECTED), (400, ProviderFailure.REJECTED)],
)
def test_http_status_maps_to_a_failure_kind(make: Any, status: int, kind: ProviderFailure) -> None:
    adapter, _ = make(f"{make.__name__}_ok.json", status)
    with pytest.raises(ProviderError) as caught:
        run(adapter.complete(REQUEST))
    assert caught.value.kind is kind
    assert "CRED-VALUE" not in str(caught.value)


@pytest.mark.parametrize("make", [anthropic, openai, gemini])
@pytest.mark.parametrize("body", [b"not json", b"[]", b"\xff\xfe", b'"text"'])
def test_a_non_object_body_is_malformed(make: Any, body: bytes) -> None:
    transport = RecordingTransport(HttpResponse(200, body))
    adapter: Any = {
        "anthropic": AnthropicMessages("a", settings(), transport),
        "openai": OpenAIChat("o", settings(api_version=None), transport),
        "gemini": GeminiGenerate("g", settings(api_version=None), transport),
    }[make.__name__]
    with pytest.raises(ProviderError) as caught:
        run(adapter.complete(REQUEST))
    assert caught.value.kind is ProviderFailure.MALFORMED


def test_settings_repr_hides_the_credential() -> None:
    assert "CRED-VALUE" not in repr(settings())
