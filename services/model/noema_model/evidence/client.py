"""The client of core's candidate-read API, written to contracts/schemas/candidates-request.v1.json and
candidates-response.v1.json and to nothing else (REQ-CTX-006, REQ-AMB-002).

The request carries exactly the contract's fields. The response is checked field by field against the contract and
refused whole if anything differs: an unknown or missing field, a wrong type, more than the bounded number of candidates,
a duplicate or out-of-order candidate, a tenant or correlation id that is not the one sent, or a display name with a long
digit run. A refused response is a dependency error, never a partly used answer. tests/test_evidence_client.py runs every
fixture in contracts/fixtures through this code, so a contract change that this file does not follow fails there.
"""

import json
import re
import unicodedata
from datetime import UTC, datetime
from typing import Any

from noema_model.evidence.transport import EvidenceTransport
from noema_model.platform.config import ConfigError, reject_unknown, require_int, require_mapping, require_str
from noema_model.understanding.pipeline.types import (
    CandidateQuery,
    CandidateRequestInvalid,
    CandidateScopeError,
    CandidateSourceError,
)
from noema_model.understanding.resolution import Candidate

PATH = "/internal/v1/evidence/candidates"
SCHEMA_VERSION = "1.0"  # the contract's own version (candidates-*.v1.json), not a setting
MAX_CANDIDATES = 20  # maxItems of the response contract; a test compares it with the schema
SURFACE_MAX = 64  # maxLength of counterparty_surface in the request contract; a test compares it with the schema
DIRECTIONS = frozenset({"OUTGOING", "INCOMING"})  # the contract's enum; a test compares it with the schema
MAX_MINOR = 9007199254740991  # MinorAmount maximum (money.v1.json)

_TENANT = re.compile(r"^ten_[0-9A-Za-z]{2,32}$")
_CORRELATION = re.compile(r"^corr_[0-9A-Za-z]{2,32}$")
_PRINCIPAL = re.compile(r"^usr_[0-9A-Za-z]{2,32}$")
_KIND = re.compile(r"^[a-z][a-z0-9]*\.[a-z][a-z0-9_]*$")
_VERSION = re.compile(r"^[0-9]+\.[0-9]+$")
_CANDIDATE_ID = re.compile(r"^cand_[0-9A-Za-z]{2,32}$")
_RESOURCE_REF = re.compile(r"^[a-z][a-z0-9]*\.[a-z][a-z0-9_]*:[A-Za-z0-9_.-]{1,64}$")
_CURRENCY = re.compile(r"^[A-Z]{3}$")
_TIMESTAMP = re.compile(r"^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?Z$")
_LAST4 = re.compile(r"^[0-9]{4}$")
_LONG_DIGITS = re.compile(r"[0-9]{5}")

_CANDIDATE_KEYS = frozenset({
    "candidate_id", "resource_ref", "amount_minor", "currency", "occurred_at", "counterparty_display", "account_last4",
    "direction", "beneficiary_alias_match",
})
_REQUIRED_CANDIDATE_KEYS = _CANDIDATE_KEYS - {"beneficiary_alias_match"}


class CandidateResponseInvalid(CandidateSourceError):
    """The answer is not what the contract allows."""


def _stamp(value: datetime) -> str:
    if value.tzinfo is None:
        raise CandidateRequestInvalid("time bounds must be timezone-aware")
    return value.astimezone(UTC).isoformat().replace("+00:00", "Z")


def _minor(value: Any) -> bool:
    return isinstance(value, int) and not isinstance(value, bool) and 0 <= value <= MAX_MINOR


def _surface(text: str) -> str | None:
    """The counterparty filter: control and format characters removed; left out when empty or over the contract's bound.
    Leaving it out only widens the read, which the principal-scoped, bounded API allows."""
    cleaned = "".join(c for c in text if unicodedata.category(c) not in ("Cc", "Cf"))
    return cleaned if 1 <= len(cleaned) <= SURFACE_MAX else None


def build_request(query: CandidateQuery) -> dict[str, Any]:
    correlation_id = query.correlation_id
    if not _PRINCIPAL.match(query.principal_ref) or not _TENANT.match(query.tenant_id) or not _CORRELATION.match(correlation_id):
        raise CandidateRequestInvalid("identity fields do not match the contract")
    if query.resource_kind is None or not _KIND.match(query.resource_kind):
        raise CandidateRequestInvalid("a resource kind of the form vertical.kind is required")
    if query.time_window is None:
        raise CandidateRequestInvalid("the contract requires a time window")
    start, end = query.time_window
    if end < start:
        raise CandidateRequestInvalid("the time window is inverted")
    filters: dict[str, Any] = {"time_window": {"from": _stamp(start), "to": _stamp(end)}}
    if query.amount_range is not None and query.currency is not None:
        low, high = query.amount_range
        if not (_minor(low) and _minor(high) and low <= high and _CURRENCY.match(query.currency)):
            raise CandidateRequestInvalid("the amount range does not match the contract")
        filters["amount_range"] = {"min_minor": low, "max_minor": high, "currency": query.currency}
    surface = _surface(query.counterparty_surface) if query.counterparty_surface else None
    if surface is not None:
        filters["counterparty_surface"] = surface
    return {
        "schema_version": SCHEMA_VERSION,
        "tenant_id": query.tenant_id,
        "correlation_id": correlation_id,
        "principal_ref": query.principal_ref,
        "resource_kind": query.resource_kind,
        "filters": filters,
    }


def _instant(value: Any) -> datetime:
    if not isinstance(value, str) or not _TIMESTAMP.match(value):
        raise CandidateResponseInvalid("a timestamp is not RFC 3339 UTC")
    return datetime.fromisoformat(value).astimezone(UTC)


def _candidate(raw: Any) -> Candidate:
    if not isinstance(raw, dict) or not _REQUIRED_CANDIDATE_KEYS <= set(raw) <= _CANDIDATE_KEYS:
        raise CandidateResponseInvalid("a candidate has missing or unknown fields")
    display = raw["counterparty_display"]
    alias = raw.get("beneficiary_alias_match")
    checks = (
        isinstance(raw["candidate_id"], str) and _CANDIDATE_ID.match(raw["candidate_id"]),
        isinstance(raw["resource_ref"], str) and _RESOURCE_REF.match(raw["resource_ref"]),
        _minor(raw["amount_minor"]),
        isinstance(raw["currency"], str) and _CURRENCY.match(raw["currency"]),
        isinstance(display, str) and 1 <= len(display) <= SURFACE_MAX and "\n" not in display and not _LONG_DIGITS.search(display),
        isinstance(raw["account_last4"], str) and _LAST4.match(raw["account_last4"]),
        raw["direction"] in DIRECTIONS,
        alias is None or isinstance(alias, bool),
    )
    if not all(checks):
        raise CandidateResponseInvalid("a candidate field does not match the contract")
    return Candidate(
        candidate_id=raw["candidate_id"], resource_ref=raw["resource_ref"], amount_minor=raw["amount_minor"],
        currency=raw["currency"], occurred_at=_instant(raw["occurred_at"]), counterparty_display=display,
        account_last4=raw["account_last4"], direction=raw["direction"], beneficiary_alias_match=alias,
    )


def _no_constants(token: str) -> Any:
    raise ValueError(f"non-finite number {token}")


def parse_response(body: bytes, *, tenant_id: str, correlation_id: str) -> tuple[Candidate, ...]:
    try:
        data = json.loads(body, parse_constant=_no_constants)
    except (ValueError, UnicodeDecodeError) as exc:
        raise CandidateResponseInvalid("the answer is not JSON") from exc
    if not isinstance(data, dict) or set(data) != {"schema_version", "tenant_id", "correlation_id", "candidates"}:
        raise CandidateResponseInvalid("the answer has missing or unknown fields")
    if not isinstance(data["schema_version"], str) or not _VERSION.match(data["schema_version"]):
        raise CandidateResponseInvalid("bad schema version")
    if data["tenant_id"] != tenant_id or data["correlation_id"] != correlation_id:
        raise CandidateResponseInvalid("the answer is not for this request")
    rows = data["candidates"]
    if not isinstance(rows, list) or len(rows) > MAX_CANDIDATES:
        raise CandidateResponseInvalid("more candidates than the contract allows")
    try:
        candidates = tuple(_candidate(r) for r in rows)
    except ValueError as exc:
        raise CandidateResponseInvalid("a candidate value is out of range") from exc
    ids = [c.candidate_id for c in candidates]
    if len(set(ids)) != len(ids):
        raise CandidateResponseInvalid("a candidate appears twice")
    # Newest first, ties by id ascending (the contract's rule), compared on exact instants.
    keyed = [(c.occurred_at, c.candidate_id) for c in candidates]
    for before, after in zip(keyed, keyed[1:], strict=False):
        if before[0] < after[0] or (before[0] == after[0] and before[1] > after[1]):
            raise CandidateResponseInvalid("candidates are not newest first")
    return candidates


class EvidenceClientConfig:
    """Endpoint, deadline and size cap, from data. Nothing is defaulted."""

    def __init__(self, endpoint: str, timeout_ms: int, max_response_bytes: int) -> None:
        self.endpoint = endpoint
        self.timeout_ms = timeout_ms
        self.max_response_bytes = max_response_bytes

    @classmethod
    def from_mapping(cls, raw: Any) -> "EvidenceClientConfig":
        src = require_mapping(raw, "evidence")
        reject_unknown(src, frozenset({"endpoint", "timeout_ms", "max_response_bytes"}), "evidence")
        endpoint = require_str(src, "endpoint", "evidence")
        if not endpoint.startswith("https://"):
            raise ConfigError("evidence.endpoint: must be https")
        return cls(
            endpoint, require_int(src, "timeout_ms", "evidence", minimum=1),
            require_int(src, "max_response_bytes", "evidence", minimum=1),
        )


class EvidenceClient:
    """Implements the pipeline's CandidateSource over the contract."""

    def __init__(self, config: EvidenceClientConfig, transport: EvidenceTransport) -> None:
        self._config = config
        self._transport = transport

    async def fetch(self, query: CandidateQuery) -> tuple[Candidate, ...]:
        correlation_id = query.correlation_id
        request = build_request(query)
        response = await self._transport.post(
            self._config.endpoint.rstrip("/") + PATH,
            {"content-type": "application/json"},
            json.dumps(request, ensure_ascii=False, separators=(",", ":")).encode(),
            self._config.timeout_ms / 1000,
            self._config.max_response_bytes,
        )
        if response.status == 200:
            return parse_response(response.body, tenant_id=query.tenant_id, correlation_id=correlation_id)
        if response.status == 403:
            raise CandidateScopeError("the tenant does not match the caller")
        if response.status in (400, 422):
            raise CandidateRequestInvalid("core refused the query as malformed")
        raise CandidateSourceError(f"the evidence API answered HTTP {response.status}")


__all__ = ("CandidateResponseInvalid", "EvidenceClient", "EvidenceClientConfig", "build_request", "parse_response")
