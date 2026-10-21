"""The harness: the only road to a model (D2).

generate(...)  one structured answer, validated against a caller-supplied output type.
decide(...)    answers to enumerated questions for the six eligible understanding tasks (D9).

Both go through the same path: render the prompt from a versioned template with masked, capped, delimited data; force
the output schema; call the first provider in the configured route whose breaker allows it; on a provider error, a
timeout or an unpinned reply, go to the next; validate strictly and reject, never repair, a reply that does not match.
If every provider fails the result is a PROVIDER_UNAVAILABLE rejection, never a heuristic guess (REQ-INT-002).
"""

import asyncio
import hashlib
import json
import re
from collections.abc import Callable, Mapping
from dataclasses import dataclass
from typing import Any, Generic, TypeVar

from pydantic import BaseModel, Field, ValidationError
from pydantic import ConfigDict as PydanticConfig

from noema_model.harness.accounting import (
    CONTENT_REJECTIONS,
    PROVIDER_FAILURES,
    AttemptOutcome,
    AttemptRecord,
    CallRecord,
    RateCard,
    attempt_cost,
    cost_of,
)
from noema_model.harness.breaker import Breaker, BreakerState
from noema_model.harness.config import HarnessConfig
from noema_model.harness.decide_types import (
    ABSTAIN,
    Answer,
    ChoiceAnswer,
    ChoiceQuestion,
    DecisionTask,
    Question,
    ScoreAnswer,
    ScoreQuestion,
    YesNoAnswer,
    YesNoQuestion,
    validate_questions,
)
from noema_model.harness.errors import HarnessRejection, Reason
from noema_model.harness.fields import RenderedField, clean, data_block, render_fields
from noema_model.harness.templates import DECIDE_V1, PromptTemplate
from noema_model.platform.clock import Clock
from noema_model.platform.redaction import Redactor
from noema_model.platform.telemetry import Telemetry
from noema_model.providers.llm.base import (
    FinishReason,
    LLMProvider,
    ProviderError,
    ProviderFailure,
    ProviderRequest,
    ProviderResponse,
)

T = TypeVar("T", bound=BaseModel)
R = TypeVar("R")

_SCHEMA_NAME = re.compile(r"[^A-Za-z0-9_-]")
_CONTENT_REASON = {
    AttemptOutcome.OUTPUT_MISMATCH: Reason.OUTPUT_MISMATCH,
    AttemptOutcome.OUTPUT_TRUNCATED: Reason.OUTPUT_TRUNCATED,
    AttemptOutcome.OUTPUT_REFUSED: Reason.OUTPUT_REFUSED,
}
_FAILURE_OUTCOME = {
    ProviderFailure.TIMEOUT: AttemptOutcome.TIMEOUT,
    ProviderFailure.RATE_LIMITED: AttemptOutcome.RATE_LIMITED,
    ProviderFailure.UNAVAILABLE: AttemptOutcome.UNAVAILABLE,
    ProviderFailure.REJECTED: AttemptOutcome.REJECTED,
    ProviderFailure.MALFORMED: AttemptOutcome.MALFORMED,
}


class _Wire(BaseModel):
    model_config = PydanticConfig(extra="forbid", strict=True, frozen=True)


class _WireProbability(_Wire):
    option: str
    p: float = Field(allow_inf_nan=False)


class _WireAnswer(_Wire):
    question_id: str
    selected: str
    probabilities: list[_WireProbability]


class _WireDecision(_Wire):
    answers: list[_WireAnswer]


@dataclass(frozen=True, slots=True)
class GenerateRequest(Generic[T]):
    route: str
    template: PromptTemplate
    fields: Mapping[str, str]
    output_type: type[T]
    locale: str | None
    correlation_id: str


@dataclass(frozen=True, slots=True)
class GenerateResult(Generic[T]):
    value: T
    call: CallRecord


@dataclass(frozen=True, slots=True)
class DecideRequest:
    task: DecisionTask
    state: Mapping[str, str]
    questions: tuple[Question, ...]
    locale: str | None
    correlation_id: str


@dataclass(frozen=True, slots=True)
class DecideResult:
    task: DecisionTask
    answers: tuple[Answer, ...]
    call: CallRecord


def _digest(text: str) -> str:
    return hashlib.sha256(text.encode()).hexdigest()


def _no_constants(token: str) -> Any:
    raise ValueError(f"non-finite number {token}")


def _no_duplicate_keys(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    keys = [k for k, _ in pairs]
    if len(set(keys)) != len(keys):
        raise ValueError("duplicate key")
    return dict(pairs)


def _check_json(text: str) -> None:
    """Refuse what a lenient parser would quietly accept: NaN, Infinity and a repeated key (the last would win)."""
    json.loads(text, parse_constant=_no_constants, object_pairs_hook=_no_duplicate_keys)


def _strict_shape(node: Any) -> bool:
    """Every object in the schema lists all its properties as required and forbids others (what strict structured
    output modes need, and what makes a missing field a rejection instead of a default)."""
    if isinstance(node, dict):
        if "properties" in node and (
            set(node.get("required", ())) != set(node["properties"]) or node.get("additionalProperties") is not False
        ):
            return False
        return all(_strict_shape(v) for v in node.values())
    if isinstance(node, list):
        return all(_strict_shape(v) for v in node)
    return True


class Harness:
    def __init__(
        self,
        config: HarnessConfig,
        providers: Mapping[str, LLMProvider],
        clock: Clock,
        telemetry: Telemetry,
    ) -> None:
        missing = set(config.providers) - set(providers)
        if missing:
            raise ValueError(f"no adapter for configured providers {sorted(missing)}")
        self._config = config
        self._providers = providers
        self._clock = clock
        self._telemetry = telemetry
        self._redactor = Redactor(config.account_run_min_digits)
        self._breakers = {
            name: Breaker(config.breaker.failure_threshold, config.breaker.cooldown_ms) for name in config.providers
        }
        self._rates: dict[str, RateCard] = {n: c.rate for n, c in config.providers.items() if c.rate is not None}
        self._schemas: dict[type[BaseModel], dict[str, Any]] = {}

    # -- public interfaces ---------------------------------------------------------------------------------------

    async def generate(self, request: GenerateRequest[T]) -> GenerateResult[T]:
        if request.route.startswith("decide."):
            raise HarnessRejection(Reason.ROUTE_MISSING)  # decide routes are for the six eligible tasks only
        schema = self._schema_for(request.output_type)
        rendered = render_fields(request.fields, self._config.data_limits, self._redactor)
        user = f"{request.template.instruction}\n\n{data_block(rendered)}"
        parsed, call = await self._call(
            route=request.route,
            template=request.template,
            user=user,
            rendered=rendered,
            schema=schema,
            output_type=request.output_type,
            finalize=lambda value: value,
            locale=request.locale,
            correlation_id=request.correlation_id,
        )
        return GenerateResult(value=parsed, call=call)

    async def decide(self, request: DecideRequest) -> DecideResult:
        if not isinstance(request.task, DecisionTask):
            raise HarnessRejection(Reason.TASK_NOT_ELIGIBLE)
        validate_questions(request.questions)
        rendered = render_fields(request.state, self._config.data_limits, self._redactor)
        user = f"{DECIDE_V1.instruction}\n\nQUESTIONS:\n{self._questions_block(request.questions)}\n\n{data_block(rendered)}"
        answers, call = await self._call(
            route=f"decide.{request.task.value}",
            template=DECIDE_V1,
            user=user,
            rendered=rendered,
            schema=self._schema_for(_WireDecision),
            output_type=_WireDecision,
            finalize=lambda wire: self._answers(request.questions, wire),
            locale=request.locale,
            correlation_id=request.correlation_id,
        )
        return DecideResult(task=request.task, answers=answers, call=call)

    # -- decide: question rendering and answer checking ------------------------------------------------------------

    def _questions_block(self, questions: tuple[Question, ...]) -> str:
        limit = self._config.data_limits.default_max_chars

        def text(value: str) -> str:
            return self._redactor.mask_account_numbers(clean(value))[:limit]

        items: list[dict[str, Any]] = []
        for q in questions:
            if isinstance(q, ChoiceQuestion):
                items.append({
                    "question_id": q.question_id, "kind": "choice", "prompt": text(q.prompt),
                    "options": [{"option_id": o.option_id, "label": text(o.label)} for o in q.options],
                })
            elif isinstance(q, YesNoQuestion):
                items.append({"question_id": q.question_id, "kind": "yes_no", "prompt": text(q.prompt)})
            else:
                items.append({"question_id": q.question_id, "kind": "score", "prompt": text(q.prompt)})
        encoded = json.dumps(items, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
        return encoded.replace("<", "\\u003c").replace(">", "\\u003e")

    @staticmethod
    def _answers(questions: tuple[Question, ...], wire: _WireDecision) -> tuple[Answer, ...]:
        by_id = {a.question_id: a for a in wire.answers}
        if len(by_id) != len(wire.answers) or set(by_id) != {q.question_id for q in questions}:
            raise HarnessRejection(Reason.OUTPUT_MISMATCH)
        out: list[Answer] = []
        for q in questions:
            answer = by_id[q.question_id]
            probabilities: dict[str, float] = {}
            for entry in answer.probabilities:
                if entry.option in probabilities or not 0.0 <= entry.p <= 1.0:
                    raise HarnessRejection(Reason.OUTPUT_MISMATCH)
                probabilities[entry.option] = entry.p
            if isinstance(q, ChoiceQuestion):
                ids = {o.option_id for o in q.options}
                if answer.selected not in ids or not set(probabilities) <= ids:
                    raise HarnessRejection(Reason.OUTPUT_MISMATCH)
                out.append(ChoiceAnswer(q.question_id, answer.selected, probabilities))
            elif isinstance(q, YesNoQuestion):
                if answer.selected not in ("yes", "no") or set(probabilities) != {"yes", "no"}:
                    raise HarnessRejection(Reason.OUTPUT_MISMATCH)
                out.append(YesNoAnswer(q.question_id, answer.selected == "yes", probabilities["yes"]))
            else:
                assert isinstance(q, ScoreQuestion)
                if answer.selected != "score" or set(probabilities) != {"score"}:
                    raise HarnessRejection(Reason.OUTPUT_MISMATCH)
                out.append(ScoreAnswer(q.question_id, probabilities["score"]))
        return tuple(out)

    # -- the shared call path --------------------------------------------------------------------------------------

    def _schema_for(self, output_type: type[BaseModel]) -> dict[str, Any]:
        cached = self._schemas.get(output_type)
        if cached is not None:
            return cached
        config = output_type.model_config
        schema = output_type.model_json_schema()
        if config.get("extra") != "forbid" or config.get("strict") is not True or not _strict_shape(schema):
            raise HarnessRejection(Reason.OUTPUT_TYPE_UNSAFE)
        self._schemas[output_type] = schema
        return schema

    async def _call(
        self,
        *,
        route: str,
        template: PromptTemplate,
        user: str,
        rendered: tuple[RenderedField, ...],
        schema: dict[str, Any],
        output_type: type[T],
        finalize: Callable[[T], R],
        locale: str | None,
        correlation_id: str,
    ) -> tuple[R, CallRecord]:
        route_config = self._config.routes.get(route)
        if route_config is None:
            raise HarnessRejection(Reason.ROUTE_MISSING)
        names = route_config.providers_for(locale)
        schema_name = _SCHEMA_NAME.sub("_", template.template_id)[:64]
        attempts: list[AttemptRecord] = []
        breakers_opened: list[str] = []
        result: R | None = None
        winner: str | None = None

        with self._telemetry.span("harness.call", correlation_id, {"route": route}) as span:
            for name in names:
                cfg = self._config.providers[name]
                breaker = self._breakers[name]
                started = self._clock.monotonic_ms()
                if not breaker.allow(started):
                    attempts.append(
                        AttemptRecord(name, cfg.model_version, None, AttemptOutcome.BREAKER_OPEN, 0, None, None, None, 0)
                    )
                    continue
                provider_request = ProviderRequest(
                    model_version=cfg.model_version,
                    system=template.system,
                    user=user,
                    schema_name=schema_name,
                    output_schema=schema,
                    max_output_tokens=cfg.max_output_tokens,
                    temperature=cfg.temperature,
                )
                response: ProviderResponse | None = None
                outcome: AttemptOutcome
                with self._telemetry.span("harness.provider_call", correlation_id,
                                          {"route": route, "provider": name}) as attempt_span:
                    try:
                        response = await asyncio.wait_for(
                            self._providers[name].complete(provider_request), cfg.timeout_ms / 1000
                        )
                        outcome = AttemptOutcome.OK
                    except TimeoutError:
                        outcome = AttemptOutcome.TIMEOUT
                    except ProviderError as exc:
                        outcome = _FAILURE_OUTCOME[exc.kind]
                    except BaseException:
                        breaker.release_probe()
                        raise
                    if response is not None and response.model_version_reported != cfg.model_version:
                        outcome = AttemptOutcome.VERSION_MISMATCH
                    if outcome in PROVIDER_FAILURES:
                        breaker.record_failure(self._clock.monotonic_ms())
                        if breaker.state is BreakerState.OPEN:
                            breakers_opened.append(name)
                    else:
                        breaker.record_success()
                    value: R | None = None
                    if outcome is AttemptOutcome.OK and response is not None:
                        value, outcome = self._judge(response, output_type, finalize)
                    attempt_span.set_attribute("outcome", outcome.value)
                latency = max(0, self._clock.monotonic_ms() - started)
                tokens_in = response.tokens_in if response is not None else None
                tokens_out = response.tokens_out if response is not None else None
                rate = self._rates.get(name)
                attempts.append(
                    AttemptRecord(
                        name, cfg.model_version,
                        response.model_version_reported if response is not None else None,
                        outcome, latency, tokens_in, tokens_out, rate, attempt_cost(tokens_in, tokens_out, rate),
                    )
                )
                if outcome is AttemptOutcome.OK:
                    result, winner = value, name
                    break

            call = self._record(route, template, user, rendered, attempts, winner)
            span.set_attribute("outcome", "OK" if winner else "REJECTED")
            span.set_attribute("attempts", len(attempts))
        self._emit(call, correlation_id, route, breakers_opened)
        if winner is None or result is None:
            real = [a.outcome for a in attempts if a.outcome is not AttemptOutcome.BREAKER_OPEN]
            if real and all(o in CONTENT_REJECTIONS for o in real):
                raise HarnessRejection(_CONTENT_REASON[real[-1]], call)
            raise HarnessRejection(Reason.NO_PROVIDER_ANSWERED, call)
        return result, call

    @staticmethod
    def _judge(
        response: ProviderResponse, output_type: type[T], finalize: Callable[[T], R]
    ) -> tuple[R | None, AttemptOutcome]:
        """Apply the schema gate to one reply: whole-reply, strict, no repair."""
        if response.finish is FinishReason.TRUNCATED:
            return None, AttemptOutcome.OUTPUT_TRUNCATED
        if response.finish is FinishReason.REFUSED:
            return None, AttemptOutcome.OUTPUT_REFUSED
        try:
            _check_json(response.output_json)
            return finalize(output_type.model_validate_json(response.output_json, strict=True)), AttemptOutcome.OK
        except (ValueError, ValidationError, HarnessRejection):
            return None, AttemptOutcome.OUTPUT_MISMATCH

    def _record(
        self,
        route: str,
        template: PromptTemplate,
        user: str,
        rendered: tuple[RenderedField, ...],
        attempts: list[AttemptRecord],
        provider: str | None,
    ) -> CallRecord:
        frozen = tuple(attempts)
        cost, currency, as_of, complete = cost_of(frozen)
        chosen = next((a for a in reversed(frozen) if a.provider == provider), None) if provider else None
        return CallRecord(
            route=route,
            template_id=template.template_id,
            template_version=template.version,
            template_digest=template.digest,
            input_digest=_digest(template.system + "\n" + user),
            truncated_fields=tuple(f.name for f in rendered if f.truncated),
            attempts=frozen,
            provider=provider,
            model_version=chosen.model_version if chosen else None,
            latency_ms=sum(a.latency_ms for a in frozen),
            tokens_in=sum(a.tokens_in or 0 for a in frozen),
            tokens_out=sum(a.tokens_out or 0 for a in frozen),
            cost_micro=cost,
            cost_currency=currency,
            cost_rate_as_of=as_of,
            cost_complete=complete,
        )

    def _emit(self, call: CallRecord, correlation_id: str, route: str, breakers_opened: list[str]) -> None:
        t = self._telemetry
        for a in call.attempts:
            t.count("noema_provider_call_total", {"route": route, "provider": a.provider, "outcome": a.outcome.value})
            if a.outcome is not AttemptOutcome.BREAKER_OPEN:
                t.observe("noema_provider_call_latency_ms", {"route": route, "provider": a.provider}, a.latency_ms)
            for direction, tokens in (("in", a.tokens_in), ("out", a.tokens_out)):
                if tokens:
                    t.count("noema_provider_tokens_total", {"route": route, "provider": a.provider, "direction": direction}, tokens)
        for f in call.failovers:
            t.count("noema_provider_failover_total", {"route": route, "from_provider": f.from_provider, "to_provider": f.to_provider})
            t.event("PROVIDER_FAILOVER", correlation_id, {
                "route": route, "from_provider": f.from_provider, "to_provider": f.to_provider, "reason": f.reason.value,
            })
        for name in breakers_opened:
            t.count("noema_provider_breaker_open_total", {"provider": name})
        if call.truncated_fields:
            t.event("DATA_FIELD_TRUNCATED", correlation_id, {"route": route, "fields": ",".join(call.truncated_fields)})
