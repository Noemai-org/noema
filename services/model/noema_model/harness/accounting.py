"""Per-call accounting: provider, model version, template version, tokens, latency and cost (REQ-NFR-007, REQ-OBS-002).

A CallRecord is what the Record stores about a model call: inputs by digest, never the provider's reasoning, and never
a reply the schema gate did not accept. Cost comes only from a configured rate card with its date; there is no price
in code, and a call with no rate or with missing token counts says so instead of guessing.
"""

from dataclasses import dataclass
from enum import StrEnum
from typing import Any


class AttemptOutcome(StrEnum):
    OK = "OK"
    TIMEOUT = "TIMEOUT"
    RATE_LIMITED = "RATE_LIMITED"
    UNAVAILABLE = "UNAVAILABLE"
    REJECTED = "REJECTED"
    MALFORMED = "MALFORMED"
    VERSION_MISMATCH = "VERSION_MISMATCH"
    BREAKER_OPEN = "BREAKER_OPEN"
    OUTPUT_MISMATCH = "OUTPUT_MISMATCH"
    OUTPUT_TRUNCATED = "OUTPUT_TRUNCATED"
    OUTPUT_REFUSED = "OUTPUT_REFUSED"


# Outcomes where the provider itself failed. They count against its circuit breaker.
PROVIDER_FAILURES = frozenset(
    {
        AttemptOutcome.TIMEOUT,
        AttemptOutcome.RATE_LIMITED,
        AttemptOutcome.UNAVAILABLE,
        AttemptOutcome.REJECTED,
        AttemptOutcome.MALFORMED,
        AttemptOutcome.VERSION_MISMATCH,
    }
)
# Outcomes where the provider answered and the schema gate refused the answer. The reply is rejected whole and never
# repaired; the next provider is then asked afresh (T5), and the refusing provider's breaker is not charged.
CONTENT_REJECTIONS = frozenset(
    {AttemptOutcome.OUTPUT_MISMATCH, AttemptOutcome.OUTPUT_TRUNCATED, AttemptOutcome.OUTPUT_REFUSED}
)
# Outcomes after which the next provider in the route is tried (REQ-INT-002).
FAILOVER_OUTCOMES = PROVIDER_FAILURES | CONTENT_REJECTIONS | {AttemptOutcome.BREAKER_OPEN}


@dataclass(frozen=True, slots=True)
class RateCard:
    """Micro-units of `currency` per million tokens, dated. Integers only, so no float reaches money (BD-08)."""

    input_micro_per_mtok: int
    output_micro_per_mtok: int
    currency: str
    as_of: str


@dataclass(frozen=True, slots=True)
class AttemptRecord:
    provider: str
    model_version: str
    model_version_reported: str | None
    outcome: AttemptOutcome
    latency_ms: int
    tokens_in: int | None
    tokens_out: int | None
    rate: RateCard | None
    cost_micro: int | None

    def to_dict(self) -> dict[str, Any]:
        return {
            "provider": self.provider,
            "model_version": self.model_version,
            "model_version_reported": self.model_version_reported,
            "outcome": self.outcome.value,
            "latency_ms": self.latency_ms,
            "tokens_in": self.tokens_in,
            "tokens_out": self.tokens_out,
            "price": None if self.rate is None else {
                "input_micro_per_mtok": self.rate.input_micro_per_mtok,
                "output_micro_per_mtok": self.rate.output_micro_per_mtok,
                "currency": self.rate.currency,
                "as_of": self.rate.as_of,
            },
            "cost_micro": self.cost_micro,
        }


@dataclass(frozen=True, slots=True)
class Failover:
    """The PROVIDER_FAILOVER audit payload (REQ-INT-002)."""

    from_provider: str
    to_provider: str
    reason: AttemptOutcome


@dataclass(frozen=True, slots=True)
class CallRecord:
    route: str
    template_id: str
    template_version: int
    template_digest: str
    input_digest: str
    truncated_fields: tuple[str, ...]
    attempts: tuple[AttemptRecord, ...]
    provider: str | None
    model_version: str | None
    latency_ms: int
    tokens_in: int
    tokens_out: int
    cost_micro: int | None
    cost_currency: str | None
    cost_rate_as_of: str | None
    cost_complete: bool

    @property
    def failovers(self) -> tuple[Failover, ...]:
        found = []
        for before, after in zip(self.attempts, self.attempts[1:], strict=False):
            if before.outcome in FAILOVER_OUTCOMES:
                found.append(Failover(before.provider, after.provider, before.outcome))
        return tuple(found)

    def to_dict(self) -> dict[str, Any]:
        """The call as plain JSON-safe data for the Record and for the benchmark runner (REQ-NFR-007): tokens, the price
        of each attempt, and the cost. Integers and strings only; a cost that cannot be known is null, never a guess."""
        return {
            "route": self.route,
            "template": {"id": self.template_id, "version": self.template_version, "digest": self.template_digest},
            "input_digest": self.input_digest,
            "truncated_fields": list(self.truncated_fields),
            "provider": self.provider,
            "model_version": self.model_version,
            "latency_ms": self.latency_ms,
            "tokens_in": self.tokens_in,
            "tokens_out": self.tokens_out,
            "cost": {
                "total_micro": self.cost_micro,
                "currency": self.cost_currency,
                "rate_as_of": self.cost_rate_as_of,
                "complete": self.cost_complete,
            },
            "attempts": [a.to_dict() for a in self.attempts],
        }


def _ceil_div(numerator: int, denominator: int) -> int:
    return -(-numerator // denominator)


def attempt_cost(tokens_in: int | None, tokens_out: int | None, rate: RateCard | None) -> int | None:
    """Micro-units of the rate's currency for one attempt, rounded up so it is never understated. None when the rate or
    either token count is unknown: the cost is not guessed."""
    if rate is None or tokens_in is None or tokens_out is None:
        return None
    return _ceil_div(tokens_in * rate.input_micro_per_mtok, 1_000_000) + _ceil_div(
        tokens_out * rate.output_micro_per_mtok, 1_000_000
    )


def cost_of(attempts: tuple[AttemptRecord, ...]) -> tuple[int | None, str | None, str | None, bool]:
    """(cost_micro, currency, rate_as_of, complete), summed from the attempts' own costs.

    Attempts refused before any call (an open breaker) cost nothing and do not make the figure incomplete. A made call
    whose cost is unknown makes the figure incomplete. Two currencies cannot be added, so the figure is then unknown.
    `rate_as_of` is that of the last priced attempt; each attempt carries its own.
    """
    total = 0
    currency: str | None = None
    as_of: str | None = None
    complete = True
    priced = False
    for attempt in attempts:
        if attempt.outcome is AttemptOutcome.BREAKER_OPEN:
            continue
        if attempt.cost_micro is None or attempt.rate is None:
            complete = False
            continue
        if currency not in (None, attempt.rate.currency):
            return None, None, None, False
        currency, as_of, priced = attempt.rate.currency, attempt.rate.as_of, True
        total += attempt.cost_micro
    return (total if priced else None), currency, as_of, complete and priced
