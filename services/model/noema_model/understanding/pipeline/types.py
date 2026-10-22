"""Plain data for the pipeline, and the three seams it is given: the candidate source, the normaliser and the gate.

The seams exist because three pieces belong to other tracks. The candidate source is the client of the core's Evidence
API (REQ-CTX-006; blocked on the T2 contract). The normaliser is the deterministic lexicon (REQ-INT-003, UND-05), whose
values win over a model's. The gate is the schema gate on the assembled Intent Object (REQ-INT-001), which needs the
generated contract types (CON-03). Each is a function the caller supplies; none is faked here.
"""

from collections.abc import Awaitable, Callable, Mapping
from dataclasses import dataclass
from datetime import datetime
from enum import StrEnum
from typing import Protocol

from noema_model.harness import CallRecord
from noema_model.understanding.pipeline.catalogue import NodeCatalogue
from noema_model.understanding.resolution import (
    Candidate,
    RankingConfig,
    RecipientMatcher,
    Resolution,
    ResolutionState,
)


class Outcome(StrEnum):
    PROPOSED = "PROPOSED"
    OUT_OF_SCOPE = "OUT_OF_SCOPE"
    UNCLEAR = "UNCLEAR"
    REJECTED_AT_GATE = "REJECTED_AT_GATE"


@dataclass(frozen=True, slots=True)
class FlagSpec:
    flag: str
    question: str


@dataclass(frozen=True, slots=True)
class LocaleOption:
    code: str
    label: str


@dataclass(frozen=True, slots=True)
class PipelineConfig:
    """Everything the pipeline needs from the pack. All of it is data the caller loads; none is written in code."""

    catalogue: NodeCatalogue
    ranking: RankingConfig
    matcher: RecipientMatcher
    proposable_flags: tuple[FlagSpec, ...]
    locales: tuple[LocaleOption, ...]
    material_flags: frozenset[str]
    intent_unclear_flag: str
    out_of_scope_flag: str
    state_flags: Mapping[ResolutionState, str]


@dataclass(frozen=True, slots=True)
class PipelineInput:
    correlation_id: str
    principal_ref: str
    tenant_id: str
    channel: str
    text: str
    locale_hint: str | None
    transcript_is_final: bool
    key_slot_low_asr: bool
    candidate_set_id: str
    candidate_set_version: int


@dataclass(frozen=True, slots=True)
class MentionProposal:
    present: bool
    surface_form: str
    proposed_value: str
    confidence: float


@dataclass(frozen=True, slots=True)
class Proposal:
    """What the models proposed. Every field is untrusted until the gate, the lexicon and Trust have judged it."""

    intent_node: str | None
    reference_slots: Mapping[str, str | None]
    mentions: Mapping[str, MentionProposal]
    ambiguity_flags: tuple[str, ...]
    locale: str | None
    code_switching: bool | None
    probabilities: Mapping[str, Mapping[str, float]]


@dataclass(frozen=True, slots=True)
class NormalisedSlots:
    """The deterministic lexicon's reading. Where it exists it wins over the model's (REQ-INT-003)."""

    amount_minor: int | None
    currency: str | None
    amount_uncertain: bool
    window: tuple[datetime, datetime] | None
    counterparty_surface: str | None
    expected_direction: str | None


@dataclass(frozen=True, slots=True)
class CandidateQuery:
    """The filters of REQ-CTX-006. The principal and tenant come from the authenticated session, never from text."""

    correlation_id: str
    principal_ref: str
    tenant_id: str
    resource_kind: str | None
    amount_range: tuple[int, int] | None
    currency: str | None
    time_window: tuple[datetime, datetime] | None
    counterparty_surface: str | None


class CandidateSourceError(Exception):
    """The Evidence API could not answer, or answered with something the contract does not allow. The pipeline turns it
    into a typed, retryable dependency rejection."""


class CandidateRequestInvalid(Exception):
    """The query cannot be sent as the contract allows (a malformed id, a missing resource kind, an inverted range) or core
    refused it as malformed. Not retryable: the same query fails the same way."""


class CandidateScopeError(Exception):
    """Core answered 403: the tenant does not match the authenticated caller (REQ-CTX-006). Never retried, and the caller
    raises a security event."""


class CandidateSource(Protocol):
    def fetch(self, query: CandidateQuery) -> Awaitable[tuple[Candidate, ...]]: ...


Normalise = Callable[[PipelineInput, Proposal], NormalisedSlots]
Gate = Callable[[Proposal, NormalisedSlots], tuple[str, ...]]


@dataclass(frozen=True, slots=True)
class PipelineResult:
    outcome: Outcome
    proposal: Proposal | None
    normalised: NormalisedSlots | None
    ambiguity_flags: tuple[str, ...]
    gate_errors: tuple[str, ...]
    query: CandidateQuery | None
    resolution: Resolution | None
    injection_signals: Mapping[str, bool]
    calls: tuple[CallRecord, ...]
