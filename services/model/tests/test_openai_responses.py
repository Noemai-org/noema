"""The OpenAI Responses adapter (REQ-INT-002). Fixtures are SYNTHETIC, written from the endpoint reference read on 2026-10-27
(developers.openai.com/api/reference/resources/responses/methods/create); no live call was made: not run: no access."""

import json
from typing import Any

import pytest

from noema_model.providers.llm.base import (
    FinishReason, HttpResponse, ProviderError, ProviderFailure, ProviderRequest,
)
from noema_model.providers.llm.openai_responses import OpenAIResponses
from noema_model.providers.llm.registry import KINDS
from tests.support import RecordingTransport, run
from tests.test_providers_contract import REQUEST, SCHEMA, fixture, settings


def responses(name: str, status: int = 200, **over: Any) -> tuple[OpenAIResponses, RecordingTransport]:
    transport = RecordingTransport(fixture(name, status))
    return OpenAIResponses("r", settings(api_version=None, reasoning_effort="medium", **over), transport), transport


def raw(body: Any) -> HttpResponse:
    return HttpResponse(200, json.dumps(body).encode())


def adapter_for(body: Any) -> OpenAIResponses:
    return OpenAIResponses("r", settings(api_version=None, reasoning_effort="medium"), RecordingTransport(raw(body)))


def test_the_kind_is_registered() -> None:
    assert KINDS["openai_responses"] is OpenAIResponses


def test_request_shape_is_the_responses_api_with_a_strict_schema_and_medium_effort() -> None:
    adapter, transport = responses("openai_responses_ok.json")
    run(adapter.complete(REQUEST))
    url, headers, body = transport.requests[0]
    assert url == "https://vendor.example.invalid/v1/responses"
    assert headers["authorization"] == "Bearer CRED-VALUE"
    assert body["model"] == "pinned-model-2026-01-01"
    assert body["instructions"] == "SYSTEM" and body["input"] == "USER"
    assert body["reasoning"] == {"effort": "medium"}
    assert body["max_output_tokens"] == 512
    assert body["store"] is False
    assert body["text"]["format"] == {"type": "json_schema", "name": "harness_decide", "schema": SCHEMA, "strict": True}
    assert "temperature" not in body and "messages" not in body


def test_the_effort_comes_from_the_settings_not_from_the_adapter() -> None:
    adapter = OpenAIResponses("r", settings(api_version=None, reasoning_effort="high"), RecordingTransport(fixture("openai_responses_ok.json")))
    run(adapter.complete(REQUEST))
    assert adapter._transport.requests[0][2]["reasoning"] == {"effort": "high"}  # type: ignore[attr-defined]


def test_without_an_effort_the_field_is_not_sent() -> None:
    transport = RecordingTransport(fixture("openai_responses_ok.json"))
    run(OpenAIResponses("r", settings(api_version=None), transport).complete(REQUEST))
    assert "reasoning" not in transport.requests[0][2]


def test_temperature_is_never_sent() -> None:
    transport = RecordingTransport(fixture("openai_responses_ok.json"))
    run(OpenAIResponses("r", settings(api_version=None), transport).complete(
        ProviderRequest("pinned-model-2026-01-01", "S", "U", "n", SCHEMA, 8, 0.0)))
    assert "temperature" not in transport.requests[0][2]


def test_parses_the_message_text_the_version_and_the_tokens_with_reasoning_billed_as_output() -> None:
    adapter, _ = responses("openai_responses_ok.json")
    response = run(adapter.complete(REQUEST))
    assert json.loads(response.output_json) == {"answers": []}
    assert response.model_version_reported == "pinned-model-2026-01-01"
    assert (response.tokens_in, response.tokens_out, response.finish) == (120, 340, FinishReason.COMPLETE)


def test_an_incomplete_response_is_truncated_and_carries_no_output() -> None:
    response = run(responses("openai_responses_incomplete.json")[0].complete(REQUEST))
    assert response.finish is FinishReason.TRUNCATED and response.output_json == ""
    assert response.tokens_out == 512


def test_a_refusal_is_a_refusal_and_its_text_never_surfaces() -> None:
    response = run(responses("openai_responses_refusal.json")[0].complete(REQUEST))
    assert response.finish is FinishReason.REFUSED
    assert "REASONING-MARKER" not in response.output_json


def test_content_filter_incompleteness_is_a_refusal() -> None:
    body = json.loads(fixture("openai_responses_incomplete.json").body)
    body["incomplete_details"] = {"reason": "content_filter"}
    assert run(adapter_for(body).complete(REQUEST)).finish is FinishReason.REFUSED


def test_reasoning_items_are_dropped_and_only_the_message_text_is_returned() -> None:
    body = json.loads(fixture("openai_responses_ok.json").body)
    body["output"][0]["summary"] = [{"type": "summary_text", "text": "REASONING-MARKER-must-never-surface"}]
    assert "REASONING-MARKER" not in run(adapter_for(body).complete(REQUEST)).output_json


@pytest.mark.parametrize("mutate", [
    lambda b: b.update(output=[]),
    lambda b: b.update(output="x"),
    lambda b: b["output"].pop(),  # no message item at all
    lambda b: b["output"][1]["content"].clear(),
    lambda b: b["output"][1]["content"].__setitem__(0, {"type": "output_text", "text": 7}),
    lambda b: b.update(status="in_progress"),
    lambda b: b.update(status="failed"),
    lambda b: b.update(status="cancelled"),
    lambda b: b.pop("status"),
])
def test_a_body_that_is_not_a_finished_answer_is_malformed(mutate: Any) -> None:
    body = json.loads(fixture("openai_responses_ok.json").body)
    mutate(body)
    with pytest.raises(ProviderError) as caught:
        run(adapter_for(body).complete(REQUEST))
    assert caught.value.kind is ProviderFailure.MALFORMED


def test_two_message_texts_are_not_joined_the_reply_must_be_one_json_document() -> None:
    body = json.loads(fixture("openai_responses_ok.json").body)
    body["output"].append(json.loads(json.dumps(body["output"][1])))
    with pytest.raises(ProviderError):
        run(adapter_for(body).complete(REQUEST))


def test_missing_usage_is_unknown_not_zero() -> None:
    body = json.loads(fixture("openai_responses_ok.json").body)
    del body["usage"]
    response = run(adapter_for(body).complete(REQUEST))
    assert (response.tokens_in, response.tokens_out) == (None, None)


@pytest.mark.parametrize("status,failure", [(408, ProviderFailure.TIMEOUT), (429, ProviderFailure.RATE_LIMITED),
                                            (500, ProviderFailure.UNAVAILABLE), (503, ProviderFailure.UNAVAILABLE),
                                            (400, ProviderFailure.REJECTED), (401, ProviderFailure.REJECTED)])
def test_http_failures_map_to_provider_failures_and_the_error_body_is_not_read(status: int, failure: ProviderFailure) -> None:
    adapter = OpenAIResponses("r", settings(api_version=None), RecordingTransport(HttpResponse(status, b"REASONING-MARKER-must-never-surface")))
    with pytest.raises(ProviderError) as caught:
        run(adapter.complete(REQUEST))
    assert caught.value.kind is failure and "REASONING-MARKER" not in str(caught.value)


def test_the_credential_is_never_in_a_repr() -> None:
    assert "CRED-VALUE" not in repr(settings(reasoning_effort="medium"))


def test_two_content_parts_in_one_message_are_malformed() -> None:
    body = json.loads(fixture("openai_responses_ok.json").body)
    body["output"][1]["content"].append(dict(body["output"][1]["content"][0]))
    with pytest.raises(ProviderError):
        run(adapter_for(body).complete(REQUEST))


def test_a_part_that_is_not_output_text_is_malformed_even_when_it_carries_a_text_field() -> None:
    body = json.loads(fixture("openai_responses_ok.json").body)
    body["output"][1]["content"][0]["type"] = "input_text"
    with pytest.raises(ProviderError):
        run(adapter_for(body).complete(REQUEST))
