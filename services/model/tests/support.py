"""Shared test doubles. They are seams, not mocks of behaviour under test: a clock the test advances, a provider that
does what a script says (answers, errors, hangs), and a transport that records the request and plays a fixture."""

import asyncio
import json
from collections.abc import Awaitable, Callable, Mapping
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from noema_model.harness.config import HarnessConfig, load_harness_config
from noema_model.providers.llm.base import (
    FinishReason,
    HttpResponse,
    ProviderError,
    ProviderFailure,
    ProviderRequest,
    ProviderResponse,
)

FIXTURES = Path(__file__).parent / "fixtures"


def run[T](coro: Awaitable[T]) -> T:
    return asyncio.run(coro)  # type: ignore[arg-type]


class ManualClock:
    def __init__(self) -> None:
        self.ms = 1_000

    def now_utc(self):  # type: ignore[no-untyped-def]
        raise NotImplementedError

    def monotonic_ms(self) -> int:
        return self.ms

    def advance(self, ms: int) -> None:
        self.ms += ms


def raw_config(
    *,
    providers: tuple[str, ...] = ("alpha", "beta"),
    routes: Mapping[str, Any] | None = None,
    timeout_ms: int = 200,
    failure_threshold: int = 2,
    cooldown_ms: int = 1_000,
    rate: bool = True,
) -> dict[str, Any]:
    """A test configuration. Every number here is a test value, chosen for the test, and none is a product setting."""
    body: dict[str, Any] = {}
    for name in providers:
        body[name] = {
            "kind": "openai_chat",
            "endpoint": f"https://{name}.example.invalid",
            "model_version": f"model-{name}-2026-01-01",
            "credential_file": str(FIXTURES / "credential.txt"),
            "timeout_ms": timeout_ms,
            "max_output_tokens": 512,
            "max_response_bytes": 65536,
        }
        if rate:
            body[name]["rate"] = {
                "input_micro_per_mtok": 3_000_000,
                "output_micro_per_mtok": 15_000_000,
                "currency": "USD",
                "as_of": "2026-10-01",
            }
    default_routes: dict[str, Any] = {
        "slot_values": {"default": list(providers), "by_locale": {}},
        **{
            f"decide.{task}": {"default": list(providers), "by_locale": {}}
            for task in (
                "intent_node_classification", "enumerated_slot", "ambiguity_flag",
                "locale_detection", "answer_binding", "instruction_likeness",
            )
        },
    }
    return {
        "masking": {"account_run_min_digits": 8},
        "data_limits": {"default_max_chars": 256, "overrides": {"utterance": 2000}},
        "breaker": {"failure_threshold": failure_threshold, "cooldown_ms": cooldown_ms},
        "providers": body,
        "routes": routes if routes is not None else default_routes,
    }


def make_config(**kwargs: Any) -> HarnessConfig:
    return load_harness_config(raw_config(**kwargs))


Script = Callable[[ProviderRequest], Awaitable[ProviderResponse]]


def reply(
    payload: Any,
    *,
    version: str | None,
    tokens: tuple[int, int] | None = (100, 20),
    finish: FinishReason = FinishReason.COMPLETE,
    raw: str | None = None,
) -> ProviderResponse:
    return ProviderResponse(
        output_json=raw if raw is not None else json.dumps(payload),
        model_version_reported=version,
        tokens_in=tokens[0] if tokens else None,
        tokens_out=tokens[1] if tokens else None,
        finish=finish,
    )


@dataclass
class ScriptedProvider:
    """Plays one scripted step per call, then repeats the last. A step is a response, a ProviderError, or 'hang'."""

    name: str
    steps: list[ProviderResponse | ProviderError | str]
    calls: list[ProviderRequest] = field(default_factory=list)

    async def complete(self, request: ProviderRequest) -> ProviderResponse:
        self.calls.append(request)
        step = self.steps[min(len(self.calls) - 1, len(self.steps) - 1)]
        if step == "hang":
            await asyncio.sleep(3600)
        if isinstance(step, ProviderError):
            raise step
        assert isinstance(step, ProviderResponse)
        return step


def timeout_error() -> ProviderError:
    return ProviderError(ProviderFailure.TIMEOUT)


@dataclass
class RecordingTransport:
    """Records each request and plays back one canned HTTP response."""

    response: HttpResponse
    requests: list[tuple[str, Mapping[str, str], dict[str, Any]]] = field(default_factory=list)

    async def post(
        self, url: str, headers: Mapping[str, str], body: bytes, timeout_s: float, max_bytes: int
    ) -> HttpResponse:
        self.requests.append((url, dict(headers), json.loads(body)))
        return self.response
