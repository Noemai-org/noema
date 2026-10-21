"""Per-call accounting: provider, model version, template version, tokens, latency and cost (REQ-NFR-007, REQ-OBS-002).

A CallRecord is what the Record stores about a model call: inputs by digest, never the provider's reasoning, and never
a reply the schema gate did not accept. Cost comes only from a configured rate card with its date; there is no price
in code, and a call with no rate or with missing token counts says so instead of guessing.
"""

from collections.abc import Mapping
from dataclasses import dataclass
from enum import StrEnum


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


def _ceil_div(numerator: int, denominator: int) -> int:
    return -(-numerator // denominator)


def cost_of(
    attempts: tuple[AttemptRecord, ...], rates: Mapping[str, RateCard]
) -> tuple[int | None, str | None, str | None, bool]:
    """(cost_micro, currency, rate_as_of, complete). Rounded up per attempt, so the cost is never understated.

    Attempts refused before any call (an open breaker) cost nothing and do not make the figure incomplete. A made call
    with no reported token counts, or with no rate card, makes the figure incomplete.
    """
    total = 0
    currency: str | None = None
    as_of: str | None = None
    complete = True
    priced = False
    for attempt in attempts:
        if attempt.outcome is AttemptOutcome.BREAKER_OPEN:
            continue
        card = rates.get(attempt.provider)
        if card is None or attempt.tokens_in is None or attempt.tokens_out is None:
            complete = False
            continue
        if currency not in (None, card.currency):
            return None, None, None, False
        currency, as_of, priced = card.currency, card.as_of, True
        total += _ceil_div(attempt.tokens_in * card.input_micro_per_mtok, 1_000_000)
        total += _ceil_div(attempt.tokens_out * card.output_micro_per_mtok, 1_000_000)
    return (total if priced else None), currency, as_of, complete and priced
