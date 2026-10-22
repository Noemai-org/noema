"""OpenAI Decisions adapter: POST /v1/decisions with predicate (yes/no) questions.

Written from the request and response example the founder pasted on 22 Oct 2026 (tests/fixtures/providers/README.md); that
is all the format the project holds. It covers the predicate question type only: the example does not show the choice or
score shapes, how abstention is returned, or an error body, so none of those is built or guessed. A question set with any
other kind is not sent to this provider (the harness skips it and asks the next provider).

The answer is a probability per question. Nothing here turns it into a yes or a no.

Contract tests run against the pasted example and invented edge cases, not a live call: not run: no access.
"""

from typing import Any

from noema_model.providers.llm.base import (
    AdapterSettings,
    NativeAnswer,
    NativeRequest,
    NativeResponse,
    ProviderError,
    ProviderFailure,
    Transport,
    arr,
    count,
    dump,
    envelope,
    obj,
    text_or_none,
)


class OpenAIDecisions:
    def __init__(self, name: str, settings: AdapterSettings, transport: Transport) -> None:
        self.name = name
        self._settings = settings
        self._transport = transport

    async def decide(self, request: NativeRequest) -> NativeResponse:
        body: dict[str, Any] = {
            "model": request.model_version,
            "input": request.input_text,
            "questions": [{"type": "predicate", "name": q.name, "instructions": q.instructions} for q in request.questions],
        }
        headers = {"content-type": "application/json", "authorization": f"Bearer {self._settings.credential}"}
        http = await self._transport.post(
            self._settings.endpoint.rstrip("/") + "/v1/decisions",
            headers,
            dump(body).encode(),
            self._settings.timeout_ms / 1000,
            self._settings.max_response_bytes,
        )
        data = envelope(http)
        wanted = [q.name for q in request.questions]
        answers: dict[str, float] = {}
        for item in arr(data.get("answers")):
            entry = obj(item)
            name, probability = entry.get("name"), entry.get("probability")
            if not (
                entry.get("type") == "predicate"
                and isinstance(name, str)
                and name not in answers
                and isinstance(probability, (int, float))
                and not isinstance(probability, bool)
                and 0 <= probability <= 1
            ):
                raise ProviderError(ProviderFailure.MALFORMED, http.status)
            answers[name] = float(probability)
        if sorted(answers) != sorted(wanted):  # one answer per question asked, no more, no fewer
            raise ProviderError(ProviderFailure.MALFORMED, http.status)
        usage = obj(data.get("usage"))
        return NativeResponse(
            answers=tuple(NativeAnswer(name, answers[name]) for name in wanted),
            model_version_reported=text_or_none(data.get("model")),
            tokens_in=count(usage.get("input_tokens")),
            tokens_out=count(usage.get("output_tokens")),
        )
