"""The assembler: the pipeline's result and the canonical input in, the bytes of an `intent-object.v3` proposal out (UND-05; REQ-INT-001, REQ-INT-003, REQ-INT-005).

It is a pure function: no clock, no randomness, no I/O. The proposal is status EXTRACTED with no `sig`, no `principal_type` and no `asserted_by` (ruling A166); it is untrusted
until `noema_model.understanding.gate` has held it. Every value comes from one of three places: the canonical input (identity, channel, mode), the deterministic lexicon (amount,
currency, window: they win over a model's reading, REQ-INT-003) and the ranking (the resource). A mention the lexicon cannot read is left out, never guessed. A confidence is
floored to integer basis points (ruling A173), so it never claims more than the model did.

`intent_id`, `created_at` and `source.raw_ref` have no owner yet (noema#580); they arrive in one `Identifiers` value, so the answer changes one place.
"""

import json
import re
from collections.abc import Mapping
from dataclasses import dataclass
from datetime import UTC
from fractions import Fraction
from math import floor
from typing import Any

from noema_model.understanding.pipeline import MentionProposal, NodeCatalogue, NormalisedSlots, Outcome, PipelineResult, Proposal
from noema_model.understanding.pipeline.catalogue import SlotInfo
from noema_model.understanding.resolution import Resolution, ResolutionState

_PACK_REF = re.compile(r"([a-z][a-z0-9]*)@([0-9]+\.[0-9]+\.[0-9]+)")
_CANDIDATE_SET = re.compile(r"cns_[0-9A-Za-z]{2,32}")
_LOCALE = re.compile(r"[a-z]{2,3}(-[A-Za-z0-9]{2,8})*")
_SCHEMA_VERSION = "3.0"
_BASIS_POINTS = 10000
_RESOLVED, _UNRESOLVED, _AMBIGUOUS = "RESOLVED", "UNRESOLVED", "AMBIGUOUS"
_ONE_PER_NODE = ("Quantity", "TimeRef", "Party")  # the lexicon reads one amount, one window and one counterparty, so a node with two slots of one of these types cannot be assembled


@dataclass(frozen=True, slots=True)
class Identifiers:
    """The three members of the proposal that nothing in the canonical input supplies (noema#580). Validated by the schema gate, not here."""

    intent_id: str
    version: int
    supersedes: str | None
    created_at: str
    raw_ref: str


def _basis_points(value: float | Fraction) -> int:
    """Floored, clamped to [0, 10000]. A float is read as its shortest decimal (0.29 is 2900, not 2899)."""
    exact = Fraction(str(value)) if isinstance(value, float) else value
    return max(0, min(_BASIS_POINTS, floor(exact * _BASIS_POINTS)))


def _timestamp(moment: Any) -> str:
    return str(moment.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ"))


def _quantity(mention: MentionProposal | None, normalised: NormalisedSlots) -> dict[str, Any] | None:
    if mention is None or not mention.present or normalised.amount_minor is None or not normalised.currency:
        return None
    slot: dict[str, Any] = {"type": "Quantity", "value": normalised.amount_minor, "unit": normalised.currency, "status": _RESOLVED, "confidence": _basis_points(mention.confidence)}
    if mention.surface_form:
        slot["surface_form"] = mention.surface_form
    return slot


def _party(mention: MentionProposal | None) -> dict[str, Any] | None:
    if mention is None or not mention.present or not mention.surface_form:
        return None
    return {"type": "Party", "surface_form": mention.surface_form, "confidence": _basis_points(mention.confidence)}


def _time_ref(mention: MentionProposal | None, normalised: NormalisedSlots) -> dict[str, Any] | None:
    if mention is None or not mention.present or not mention.surface_form:
        return None
    slot: dict[str, Any] = {"type": "TimeRef", "surface_form": mention.surface_form, "confidence": _basis_points(mention.confidence)}
    if normalised.window is None:
        slot["status"] = _UNRESOLVED
    else:
        slot["status"] = _RESOLVED
        slot["window"] = [_timestamp(normalised.window[0]), _timestamp(normalised.window[1])]
    return slot


def _resource(resolution: Resolution | None) -> dict[str, Any]:
    """The ranking's finding, as the contract's slot (INV-06: RESOLVED only with a resolved_ref). Ranking proposes; Trust decides what an AMBIGUOUS slot means."""
    slot: dict[str, Any] = {"type": "Resource", "status": _UNRESOLVED}
    if resolution is None:
        return slot
    if not _CANDIDATE_SET.fullmatch(resolution.candidate_set_id):
        raise ValueError("the candidate set id is not the contract's")  # the caller's bug, never hidden by leaving the member out
    slot["candidates_ref"] = resolution.candidate_set_id
    if resolution.ranked:
        slot["confidence"] = _basis_points(resolution.top_score)
    if resolution.state is ResolutionState.AUTO_RESOLVED:
        chosen = next(s.candidate for s in resolution.ranked if s.candidate.candidate_id == resolution.resolved_candidate_id)
        slot["status"], slot["resolved_ref"] = _RESOLVED, chosen.resource_ref
    elif resolution.state in (ResolutionState.AMBIGUOUS, ResolutionState.TOO_MANY_CANDIDATES):
        slot["status"] = _AMBIGUOUS
    return slot


def _slot(info: SlotInfo, proposal: Proposal, normalised: NormalisedSlots, resolution: Resolution | None) -> dict[str, Any] | None:
    mention = proposal.mentions.get(info.name)
    if info.slot_type == "Quantity":
        return _quantity(mention, normalised)
    if info.slot_type == "Party":
        return _party(mention)
    if info.slot_type == "TimeRef":
        return _time_ref(mention, normalised)
    if info.slot_type == "Resource":
        return _resource(resolution) if info.candidate_search else None
    if info.slot_type == "Reference":
        value = proposal.reference_slots.get(info.name)
        return {"type": "Reference", "value": value} if value else None
    raise ValueError(f"slot type {info.slot_type} is not one the assembler knows")


def _slots(result: PipelineResult, catalogue: NodeCatalogue, node_id: str) -> dict[str, Any]:
    info = catalogue.nodes[node_id]
    kinds = [s.slot_type for s in info.slots if s.slot_type in _ONE_PER_NODE]
    if len(kinds) != len(set(kinds)):
        raise ValueError("a node with two slots of one lexicon type cannot be assembled")
    if result.proposal is None or result.normalised is None:
        raise ValueError("a proposed result carries its proposal and its normalised slots")
    built = {s.name: _slot(s, result.proposal, result.normalised, result.resolution) for s in info.slots}
    return {name: slot for name, slot in built.items() if slot is not None}


def _node_and_slots(result: PipelineResult, catalogue: NodeCatalogue) -> tuple[str, dict[str, Any]]:
    if result.outcome is Outcome.PROPOSED and result.proposal is not None and result.proposal.intent_node is not None:
        return result.proposal.intent_node, _slots(result, catalogue, result.proposal.intent_node)
    if result.outcome in (Outcome.OUT_OF_SCOPE, Outcome.UNCLEAR):
        return catalogue.out_of_scope_node, {}  # exactly two nodes, never another (the card); an unclear request is read as out of scope with its flag (a default, noema#546)
    raise ValueError("only a proposed, out-of-scope or unclear result is assembled; a rejection is never turned into a proposal (REQ-INT-002)")


def _locale(proposal: Proposal | None) -> list[str]:
    locale = proposal.locale if proposal is not None else None
    return [locale] if locale and _LOCALE.fullmatch(locale) else []


def assemble(canonical_input: Mapping[str, Any], result: PipelineResult, catalogue: NodeCatalogue, pack_ref: str, ids: Identifiers) -> bytes:
    """The proposal's bytes: compact, keys sorted, no float. Raises ValueError for a pack reference that is not `<vertical>@<x.y.z>`, a result that is a rejection or a
    candidate set id that is not the contract's; the compile service reads any of those as a bug of the caller."""
    pack = _PACK_REF.fullmatch(pack_ref)
    if pack is None:
        raise ValueError("the pack reference is not <vertical>@<x.y.z>")
    node_id, slots = _node_and_slots(result, catalogue)
    doc: dict[str, Any] = {
        "schema_version": _SCHEMA_VERSION, "intent_id": ids.intent_id, "version": ids.version, "supersedes": ids.supersedes,
        "tenant_id": str(canonical_input["tenant_id"]), "correlation_id": str(canonical_input["correlation_id"]), "created_at": ids.created_at,
        "principal": {"principal_id": str(canonical_input["principal_ref"]), "authn_ref": str(canonical_input["session_id"])},
        "source": {"mode": str(canonical_input["mode"]), "channel": str(canonical_input["channel"]), "locale_detected": _locale(result.proposal),
                   "asr_confidence": None, "raw_ref": ids.raw_ref},
        "pack": {"pack_id": pack.group(1), "pack_version": pack.group(2)}, "intent_node": node_id, "slots": slots,
        "ambiguity_flags": sorted(set(result.ambiguity_flags)), "desired_outcome_refs": [], "status": "EXTRACTED",
    }
    return json.dumps(doc, sort_keys=True, separators=(",", ":"), ensure_ascii=False, allow_nan=False).encode("utf-8")
