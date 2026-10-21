"""Anthropic Messages adapter. Structured output is forced with a single tool the model must call.

Contract tests run against fixtures written from the vendor's published shape, not recorded from a live call:
not run: no access (see tests/fixtures/providers/README.md).
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
    count,
    dump,
    envelope,
    obj,
    text_or_none,
)


class AnthropicMessages:
    def __init__(self, name: str, settings: AdapterSettings, transport: Transport) -> None:
        if not settings.api_version:
            raise ValueError("the Anthropic adapter needs api_version")
        self.name = name
        self._settings = settings
        self._transport = transport

    async def complete(self, request: ProviderRequest) -> ProviderResponse:
        body: dict[str, Any] = {
            "model": request.model_version,
            "max_tokens": request.max_output_tokens,
            "system": request.system,
            "messages": [{"role": "user", "content": request.user}],
            "tools": [
                {
                    "name": request.schema_name,
                    "description": "Return the answer in this exact shape.",
                    "input_schema": request.output_schema,
                }
            ],
            "tool_choice": {"type": "tool", "name": request.schema_name},
        }
        if request.temperature is not None:
            body["temperature"] = request.temperature
        headers = {
            "content-type": "application/json",
            "x-api-key": self._settings.credential,
            "anthropic-version": self._settings.api_version or "",
        }
        http = await self._transport.post(
            self._settings.endpoint.rstrip("/") + "/v1/messages",
            headers,
            dump(body).encode(),
            self._settings.timeout_ms / 1000,
            self._settings.max_response_bytes,
        )
        data = envelope(http)
        blocks = data.get("content")
        if not isinstance(blocks, list):
            raise ProviderError(ProviderFailure.MALFORMED, http.status)
        output = ""
        texts: list[str] = []
        for block in blocks:
            if not isinstance(block, dict):
                continue
            if block.get("type") == "tool_use" and block.get("name") == request.schema_name:
                output = dump(block.get("input"))
                break
            if block.get("type") == "text" and isinstance(block.get("text"), str):
                texts.append(block["text"])
            # Any other block (thinking, redacted thinking) is dropped here, never read or logged.
        if not output:
            output = "".join(texts)
        stop = data.get("stop_reason")
        finish = {"max_tokens": FinishReason.TRUNCATED, "refusal": FinishReason.REFUSED}.get(
            stop if isinstance(stop, str) else "", FinishReason.COMPLETE
        )
        usage = obj(data.get("usage"))
        return ProviderResponse(
            output_json=output,
            model_version_reported=text_or_none(data.get("model")),
            tokens_in=count(usage.get("input_tokens")),
            tokens_out=count(usage.get("output_tokens")),
            finish=finish,
        )
