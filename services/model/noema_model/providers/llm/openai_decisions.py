"""OpenAI Decisions adapter: POST /v1/decisions with predicate, choice and score questions.

Written from OpenAI's own reference, read on 22 Oct 2026 (tests/fixtures/providers/README.md): the endpoint page and the
Decisions guide. The answer to each question is a probability or a distribution; the adapter turns none of them into a
decision. A choice answer's `choice` and a score answer's `score` are the vendor's own fields, passed through.

The limits below are the reference's, not ours: 1 to 400 questions, 2 to 255 choices. A question set outside them is not sent
(the harness skips the provider). Images, tool calls and prompt-cache options exist in the API and are not used here.

Contract tests run against the documentation's own examples and invented edge cases, not a live call: not run: no access.
"""

import math
from typing import Any, TypeGuard

from noema_model.providers.llm.base import (
    AdapterSettings,
    NativeAnswer,
    NativeChoice,
    NativeChoiceAnswer,
    NativePredicate,
    NativePredicateAnswer,
    NativeQuestion,
    NativeRefusal,
    NativeRequest,
    NativeResponse,
    NativeScore,
    NativeScoreAnswer,
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

MAX_QUESTIONS = 400
MIN_CHOICES = 2
MAX_CHOICES = 255


def fits(questions: tuple[NativeQuestion, ...]) -> bool:
    """Whether this question set is inside the API's documented limits."""
    if not 1 <= len(questions) <= MAX_QUESTIONS:
        return False
    for q in questions:
        if isinstance(q, NativeChoice):
            values = [v for v, _ in q.choices]
            if not MIN_CHOICES <= len(values) <= MAX_CHOICES or len(set(values)) != len(values):
                return False
        if isinstance(q, NativeScore) and len(q.levels) < 1:
            return False
    return True


def _question(q: NativeQuestion) -> dict[str, Any]:
    if isinstance(q, NativePredicate):
        return {"type": "predicate", "name": q.name, "instructions": q.instructions}
    if isinstance(q, NativeChoice):
        return {
            "type": "choice", "name": q.name, "instructions": q.instructions,
            "choices": [{"value": v, "description": d} for v, d in q.choices],
        }
    return {
        "type": "score", "name": q.name, "instructions": q.instructions,
        "levels": [{"label": label, "description": d} for label, d in q.levels],
    }


def _number(value: Any) -> TypeGuard[float]:
    return isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(value)


def _probability(value: Any) -> TypeGuard[float]:
    return _number(value) and 0 <= value <= 1


def _answer(raw: Any, asked: dict[str, NativeQuestion]) -> NativeAnswer:
    entry = obj(raw)
    name, kind = entry.get("name"), entry.get("type")
    bad = ProviderError(ProviderFailure.MALFORMED)
    if not isinstance(name, str) or name not in asked:
        raise bad
    question = asked[name]
    if kind == "refusal":
        return NativeRefusal(name)
    if kind == "predicate" and isinstance(question, NativePredicate):
        probability = entry.get("probability")
        if not _probability(probability):
            raise bad
        return NativePredicateAnswer(name, float(probability))
    if kind == "choice" and isinstance(question, NativeChoice):
        values = [v for v, _ in question.choices]
        choice, confidence = entry.get("choice"), entry.get("confidence")
        if not (isinstance(choice, str) and choice in values and _number(confidence)):
            raise bad
        if not isinstance(entry.get("probabilities"), list):
            raise bad
        pairs: list[tuple[str, float]] = []
        for item in arr(entry.get("probabilities")):
            row = obj(item)
            value, probability = row.get("value"), row.get("probability")
            if not (isinstance(value, str) and value in values and _probability(probability)):
                raise bad
            pairs.append((value, float(probability)))
        if len({v for v, _ in pairs}) != len(pairs):
            raise bad
        return NativeChoiceAnswer(name, choice, float(confidence), tuple(pairs))
    if kind == "score" and isinstance(question, NativeScore):
        score, confidence = entry.get("score"), entry.get("confidence")
        labels = [label for label, _ in question.levels]
        levels: list[tuple[str, int, float]] = []
        for index, item in enumerate(arr(entry.get("probabilities"))):
            row = obj(item)
            value, label, probability = row.get("value"), row.get("label"), row.get("probability")
            if not (
                type(value) is int
                and value == index
                and index < len(labels)
                and isinstance(label, str)
                and label == labels[index]
                and _probability(probability)
            ):
                raise bad
            levels.append((label, value, float(probability)))
        if not (_number(score) and _number(confidence)) or len(levels) != len(labels):
            raise bad
        return NativeScoreAnswer(name, float(score), float(confidence), tuple(levels))
    raise bad


class OpenAIDecisions:
    def __init__(self, name: str, settings: AdapterSettings, transport: Transport) -> None:
        self.name = name
        self._settings = settings
        self._transport = transport

    def accepts(self, questions: tuple[NativeQuestion, ...]) -> bool:
        return fits(questions)

    async def decide(self, request: NativeRequest) -> NativeResponse:
        if not fits(request.questions):
            raise ProviderError(ProviderFailure.REJECTED)
        body: dict[str, Any] = {
            "model": request.model_version,
            "input": request.input_text,
            "questions": [_question(q) for q in request.questions],
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
        asked = {q.name: q for q in request.questions}
        raw_answers = arr(data.get("answers"))
        answers = [_answer(item, asked) for item in raw_answers]
        names = [a.name for a in answers]
        # One answer per question, in the order asked (the reference: "Answers come back in the same order").
        if names != [q.name for q in request.questions]:
            raise ProviderError(ProviderFailure.MALFORMED, http.status)
        usage = obj(data.get("usage"))
        return NativeResponse(
            answers=tuple(answers),
            model_version_reported=text_or_none(data.get("model")),
            tokens_in=count(usage.get("input_tokens")),
            tokens_out=count(usage.get("output_tokens")),
        )
