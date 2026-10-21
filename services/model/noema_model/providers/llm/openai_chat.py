"""OpenAI Chat Completions adapter. Structured output is requested with a strict json_schema response format.

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
    arr,
    count,
    dump,
    envelope,
    obj,
    text_or_none,
)


class OpenAIChat:
    def __init__(self, name: str, settings: AdapterSettings, transport: Transport) -> None:
        self.name = name
        self._settings = settings
        self._transport = transport

    async def complete(self, request: ProviderRequest) -> ProviderResponse:
        body: dict[str, Any] = {
            "model": request.model_version,
            "messages": [
                {"role": "system", "content": request.system},
                {"role": "user", "content": request.user},
            ],
            "response_format": {
                "type": "json_schema",
                "json_schema": {"name": request.schema_name, "strict": True, "schema": request.output_schema},
            },
            "max_completion_tokens": request.max_output_tokens,
        }
        if request.temperature is not None:
            body["temperature"] = request.temperature
        headers = {"content-type": "application/json", "authorization": f"Bearer {self._settings.credential}"}
        http = await self._transport.post(
            self._settings.endpoint.rstrip("/") + "/v1/chat/completions",
            headers,
            dump(body).encode(),
            self._settings.timeout_ms / 1000,
            self._settings.max_response_bytes,
        )
        data = envelope(http)
        choices = arr(data.get("choices"))
        if not choices or not isinstance(choices[0], dict):
            raise ProviderError(ProviderFailure.MALFORMED, http.status)
        choice = choices[0]
        message = obj(choice.get("message"))
        content = message.get("content")
        finish_reason = choice.get("finish_reason")
        if message.get("refusal") or finish_reason == "content_filter":
            finish = FinishReason.REFUSED
        elif finish_reason == "length":
            finish = FinishReason.TRUNCATED
        else:
            finish = FinishReason.COMPLETE
        usage = obj(data.get("usage"))
        return ProviderResponse(
            output_json=content if isinstance(content, str) else "",
            model_version_reported=text_or_none(data.get("model")),
            tokens_in=count(usage.get("prompt_tokens")),
            tokens_out=count(usage.get("completion_tokens")),
            finish=finish,
        )
