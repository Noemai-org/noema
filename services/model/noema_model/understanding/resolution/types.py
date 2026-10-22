"""Inputs and outputs of ranking. Plain frozen data: no behaviour, no I/O, no clock (D3).

`Candidate` is the masked, principal-scoped row of REQ-CTX-006. It is built here from the PERD field list so ranking can
be written and tested now; the core's candidate-read contract (T2) will own the wire shape, and a client that
fills this type is the only place the two meet.
"""

import re
from collections.abc import Mapping
from dataclasses import dataclass
from datetime import datetime
from enum import StrEnum
from fractions import Fraction

_LAST4 = re.compile(r"^[0-9]{4}$")
FEATURES = ("amount", "recipient", "temporal", "relationship", "type_direction")


class ResolutionState(StrEnum):
    """What the ranking found. A proposal for Trust, which decides: ranking never allows, clarifies or escalates."""

    NO_CANDIDATES = "NO_CANDIDATES"
    AUTO_RESOLVED = "AUTO_RESOLVED"
    AMBIGUOUS = "AMBIGUOUS"
    TOO_MANY_CANDIDATES = "TOO_MANY_CANDIDATES"


class Why(StrEnum):
    """Every rule that kept a top candidate from auto-resolving (all that apply are listed)."""

    TIE_AT_TOP = "TIE_AT_TOP"
    MARGIN_NOT_MET = "MARGIN_NOT_MET"
    BELOW_FLOOR = "BELOW_FLOOR"
    MATERIAL_FLAG = "MATERIAL_FLAG"
    LOW_ASR_CONFIDENCE = "LOW_ASR_CONFIDENCE"
    TOO_MANY_ABOVE_FLOOR = "TOO_MANY_ABOVE_FLOOR"


@dataclass(frozen=True, slots=True)
class Candidate:
    candidate_id: str
    resource_ref: str
    amount_minor: int
    currency: str
    occurred_at: datetime
    counterparty_display: str
    account_last4: str
    direction: str
    beneficiary_alias_match: bool | None

    def __post_init__(self) -> None:
        if not self.candidate_id or not self.resource_ref:
            raise ValueError("a candidate needs an id and a resource reference")
        if isinstance(self.amount_minor, bool) or not isinstance(self.amount_minor, int) or self.amount_minor < 0:
            raise ValueError("amount_minor is a non-negative integer in minor units")
        if self.occurred_at.tzinfo is None or self.occurred_at.utcoffset() is None:
            raise ValueError("occurred_at must be timezone-aware")
        if not _LAST4.match(self.account_last4):
            raise ValueError("account_last4 is exactly four digits")


@dataclass(frozen=True, slots=True)
class Target:
    """What the customer described, after the deterministic lexicon: the values ranking compares candidates to."""

    amount_minor: int | None
    currency: str | None
    window: tuple[datetime, datetime] | None
    counterparty_surface: str | None
    expected_direction: str | None

    def __post_init__(self) -> None:
        if self.window is not None:
            start, end = self.window
            if start.tzinfo is None or end.tzinfo is None or end < start:
                raise ValueError("window must be timezone-aware and ordered")
        if self.amount_minor is not None and (isinstance(self.amount_minor, bool) or self.amount_minor < 0):
            raise ValueError("amount_minor is a non-negative integer in minor units")


@dataclass(frozen=True, slots=True)
class Context:
    """Facts from outside ranking that the auto-resolve rule needs (REQ-AMB-003, REQ-AMB-007)."""

    material_flag_present: bool
    key_slot_low_asr: bool
    phonetic: bool


@dataclass(frozen=True, slots=True)
class ScoredCandidate:
    candidate: Candidate
    features: Mapping[str, Fraction]
    score: Fraction


@dataclass(frozen=True, slots=True)
class Resolution:
    state: ResolutionState
    ranked: tuple[ScoredCandidate, ...]
    resolved_candidate_id: str | None
    top_score: Fraction
    second_score: Fraction
    margin: Fraction
    above_floor_count: int
    why_not: tuple[Why, ...]
    candidate_set_id: str
    candidate_set_version: int
    content_hash: str
