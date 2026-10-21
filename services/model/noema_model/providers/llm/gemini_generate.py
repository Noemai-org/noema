"""Gemini generateContent adapter. Structured output is requested with a JSON mime type and a JSON schema.

Contract tests run against fixtures written from the vendor's published shape, not recorded from a live call:
not run: no access (see tests/fixtures/providers/README.md).
"""

import re
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

_MODEL_PATH = re.compile(r"^[A-Za-z0-9._-]{1,128}$")


class GeminiGenerate:
    def __init__(self, name: str, settings: AdapterSettings, transport: Transport) -> None:
        self.name = name
        self._settings = settings
        self._transport = transport

    async def complete(self, request: ProviderRequest) -> ProviderResponse:
        if not _MODEL_PATH.match(request.model_version):
            raise ProviderError(ProviderFailure.REJECTED)
        generation: dict[str, Any] = {
            "responseMimeType": "application/json",
            "responseJsonSchema": request.output_schema,
            "maxOutputTokens": request.max_output_tokens,
        }
        if request.temperature is not None:
            generation["temperature"] = request.temperature
        body = {
            "systemInstruction": {"parts": [{"text": request.system}]},
            "contents": [{"role": "user", "parts": [{"text": request.user}]}],
            "generationConfig": generation,
        }
        headers = {"content-type": "application/json", "x-goog-api-key": self._settings.credential}
        url = f"{self._settings.endpoint.rstrip('/')}/v1beta/models/{request.model_version}:generateContent"
        http = await self._transport.post(
            url, headers, dump(body).encode(), self._settings.timeout_ms / 1000, self._settings.max_response_bytes
        )
        data = envelope(http)
        candidates = arr(data.get("candidates"))
        if not candidates or not isinstance(candidates[0], dict):
            raise ProviderError(ProviderFailure.MALFORMED, http.status)
        candidate = candidates[0]
        parts = arr(obj(candidate.get("content")).get("parts"))
        # A part flagged as a thought is the model's reasoning: dropped here, never read or logged.
        texts = [
            p["text"] for p in parts if isinstance(p, dict) and isinstance(p.get("text"), str) and not p.get("thought")
        ]
        reason = candidate.get("finishReason")
        if reason == "MAX_TOKENS":
            finish = FinishReason.TRUNCATED
        elif reason in (None, "STOP"):
            finish = FinishReason.COMPLETE
        else:
            finish = FinishReason.REFUSED
        usage = obj(data.get("usageMetadata"))
        return ProviderResponse(
            output_json="".join(texts),
            model_version_reported=text_or_none(data.get("modelVersion")),
            tokens_in=count(usage.get("promptTokenCount")),
            tokens_out=count(usage.get("candidatesTokenCount")),
            finish=finish,
        )
