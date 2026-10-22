"""Provider-neutral request, response and error types, and the transport seam (BD-16, INV-29).

An adapter turns a ProviderRequest into one vendor call and normalises the answer. It never validates the content
(the harness does), never retries, and never fails over (the harness does). Vendor reasoning output is not requested
and is dropped here, so it can reach neither the harness nor a log (PERD §19.2).
"""

import json
from collections.abc import Mapping
from dataclasses import dataclass
from enum import StrEnum
from typing import Any, Protocol


class FinishReason(StrEnum):
    COMPLETE = "COMPLETE"
    TRUNCATED = "TRUNCATED"
    REFUSED = "REFUSED"


class ProviderFailure(StrEnum):
    TIMEOUT = "TIMEOUT"
    RATE_LIMITED = "RATE_LIMITED"
    UNAVAILABLE = "UNAVAILABLE"
    REJECTED = "REJECTED"
    MALFORMED = "MALFORMED"


class ProviderError(Exception):
    """A call failed before there was an answer to judge. The message never carries a body, a header or a credential."""

    def __init__(self, kind: ProviderFailure, status: int | None = None) -> None:
        super().__init__(f"provider call failed: {kind.value}" + (f" (HTTP {status})" if status is not None else ""))
        self.kind = kind
        self.status = status


@dataclass(frozen=True, slots=True)
class ProviderRequest:
    model_version: str
    system: str
    user: str
    schema_name: str
    output_schema: Mapping[str, Any]
    max_output_tokens: int
    temperature: float | None


@dataclass(frozen=True, slots=True)
class ProviderResponse:
    output_json: str
    model_version_reported: str | None
    tokens_in: int | None
    tokens_out: int | None
    finish: FinishReason


@dataclass(frozen=True, slots=True)
class HttpResponse:
    status: int
    body: bytes


class Transport(Protocol):
    async def post(
        self, url: str, headers: Mapping[str, str], body: bytes, timeout_s: float, max_bytes: int
    ) -> HttpResponse:
        """POST once. Raise ProviderError for a timeout, a refused connection or an oversize body."""
        ...


class LLMProvider(Protocol):
    name: str

    async def complete(self, request: ProviderRequest) -> ProviderResponse: ...


@dataclass(frozen=True, slots=True)
class AdapterSettings:
    endpoint: str
    credential: str
    api_version: str | None
    timeout_ms: int
    max_response_bytes: int

    def __repr__(self) -> str:
        return f"AdapterSettings(endpoint={self.endpoint!r}, credential=<hidden>)"


def failure_for_status(status: int) -> ProviderFailure:
    if status == 408:
        return ProviderFailure.TIMEOUT
    if status == 429:
        return ProviderFailure.RATE_LIMITED
    if status >= 500:
        return ProviderFailure.UNAVAILABLE
    return ProviderFailure.REJECTED


def envelope(response: HttpResponse) -> Mapping[str, Any]:
    """The vendor's JSON object, or a ProviderError. A non-2xx status never reaches the content."""
    if not 200 <= response.status < 300:
        raise ProviderError(failure_for_status(response.status), response.status)
    try:
        parsed = json.loads(response.body)
    except (ValueError, UnicodeDecodeError) as exc:
        raise ProviderError(ProviderFailure.MALFORMED, response.status) from exc
    if not isinstance(parsed, dict):
        raise ProviderError(ProviderFailure.MALFORMED, response.status)
    return parsed


def count(value: Any) -> int | None:
    """A token count the vendor reported, or None. A bool or a negative number is not a count."""
    if isinstance(value, bool) or not isinstance(value, int) or value < 0:
        return None
    return value


def obj(value: Any) -> Mapping[str, Any]:
    """The value if it is a JSON object, else an empty one, so absent and malformed look the same to the caller."""
    return value if isinstance(value, dict) else {}


def arr(value: Any) -> list[Any]:
    return value if isinstance(value, list) else []


def text_or_none(value: Any) -> str | None:
    return value if isinstance(value, str) and value else None


def dump(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"))
