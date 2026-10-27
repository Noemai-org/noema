"""OpenAI Responses API adapter (`POST /v1/responses`) for reasoning models. Structured output is requested with a strict
`json_schema` text format, and the response is stored nowhere (`store: false`).

Written from the endpoint reference read on 2026-10-27 (developers.openai.com/api/reference/resources/responses/methods/create).
Contract tests run against fixtures written from that shape, not recorded from a live call: not run: no access.

The reply is one `message` item whose content is one `output_text` (or a `refusal`). Reasoning items are dropped unread.
Reasoning tokens are part of `usage.output_tokens`, so they are billed and counted as output. A response that is not
`completed` or `incomplete` is malformed, and so is one with no message, or with more than one: the reply must be one JSON
document, so nothing is joined or guessed.
"""

from typing import Any

from noema_model.providers.llm.base import (
    AdapterSettings,
    FinishReason,
    ProviderError,
    ProviderFailure,
    ProviderRequest,
    ProviderResponse,
    Transport,
    arr,
    count,
    dump,
    envelope,
    obj,
    text_or_none,
)


class OpenAIResponses:
    def __init__(self, name: str, settings: AdapterSettings, transport: Transport) -> None:
        self.name = name
        self._settings = settings
        self._transport = transport

    async def complete(self, request: ProviderRequest) -> ProviderResponse:
        body: dict[str, Any] = {
            "model": request.model_version,
            "instructions": request.system,
            "input": request.user,
            "text": {"format": {"type": "json_schema", "name": request.schema_name, "schema": request.output_schema, "strict": True}},
            "max_output_tokens": request.max_output_tokens,
            "store": False,
        }
        if self._settings.reasoning_effort is not None:
            body["reasoning"] = {"effort": self._settings.reasoning_effort}
        headers = {"content-type": "application/json", "authorization": f"Bearer {self._settings.credential}"}
        http = await self._transport.post(
            self._settings.endpoint.rstrip("/") + "/v1/responses",
            headers,
            dump(body).encode(),
            self._settings.timeout_ms / 1000,
            self._settings.max_response_bytes,
        )
        data = envelope(http)
        status = data.get("status")
        usage = obj(data.get("usage"))
        tokens = (count(usage.get("input_tokens")), count(usage.get("output_tokens")))
        version = text_or_none(data.get("model"))
        if status == "incomplete":
            reason = obj(data.get("incomplete_details")).get("reason")
            if reason == "content_filter":
                finish = FinishReason.REFUSED
            elif reason == "max_output_tokens":
                finish = FinishReason.TRUNCATED
            else:
                raise ProviderError(ProviderFailure.MALFORMED, http.status)
            return ProviderResponse("", version, tokens[0], tokens[1], finish)
        if status != "completed":
            raise ProviderError(ProviderFailure.MALFORMED, http.status)
        messages = [item for item in arr(data.get("output")) if isinstance(item, dict) and item.get("type") == "message"]
        if len(messages) != 1:
            raise ProviderError(ProviderFailure.MALFORMED, http.status)
        content = arr(messages[0].get("content"))
        if len(content) != 1 or not isinstance(content[0], dict):
            raise ProviderError(ProviderFailure.MALFORMED, http.status)
        part = content[0]
        if part.get("type") == "refusal":
            return ProviderResponse("", version, tokens[0], tokens[1], FinishReason.REFUSED)
        text = part.get("text")
        if part.get("type") != "output_text" or not isinstance(text, str):
            raise ProviderError(ProviderFailure.MALFORMED, http.status)
        return ProviderResponse(text, version, tokens[0], tokens[1], FinishReason.COMPLETE)
