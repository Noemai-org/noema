"""PipelineCompiler: the `Compiler` the compile service calls, built from the pipeline, the pack's configuration, the lexicon and the assembler (UND-05; REQ-INT-001, REQ-INT-002,
REQ-INT-003, REQ-INT-005; INV-01, INV-02, INV-12, INV-21, INV-29; AGENT-09, AGENT-16).

One request in, the bytes of an `intent-object.v3` proposal out. The model's words reach the proposal only through the pipeline (which asks closed questions and takes slot mentions) and the
assembler (which lets the lexicon win and sets identity from the canonical input alone). Nothing here calls a provider directly: the harness does, behind `providers/` (INV-29). Nothing here
is trusted by the caller: `CompileService` still holds the result to the schema gate (INV-02).

Three members are not this module's to decide and come from outside, so the answer to noema#580 changes one place: the `Minter` gives the proposal's `Identifiers` and the id of the candidate set.
No clock and no randomness are read here: the lexicon's reference instant is the canonical input's own `received_at` (the core's clock), so the same request compiles to the same bytes (INV-21).
"""

import re
from collections.abc import Callable, Mapping
from dataclasses import dataclass
from datetime import datetime
from fractions import Fraction
from typing import Any, Protocol

from noema_model.harness import Harness
from noema_model.understanding.compiler.assemble import Identifiers, assemble
from noema_model.understanding.compiler.lexicon import Lexicon, Normaliser
from noema_model.understanding.compiler.service import InputInvalid, ProviderUnavailable, ScreenNotRun
from noema_model.understanding.pipeline import (
    CandidateSource,
    InputInvalidReason,
    Outcome,
    PipelineConfig,
    PipelineInput,
    NormalisedSlots,
    Proposal,
    run_pipeline,
)
from noema_model.understanding.pipeline.run import GATE_INPUT_INVALID, GATE_TENANT_SCOPE

_PACK_REF = re.compile(r"[a-z][a-z0-9]*@[0-9]+\.[0-9]+\.[0-9]+")
_CANDIDATE_SET_VERSION = 1  # a new compile reads a new candidate set (the first version); a re-read is a later turn's concern


@dataclass(frozen=True, slots=True)
class SlotChoice:
    """What the model chose for one closed-vocabulary slot of a turn, for the benchmark's job log (UND-17, A602): the slot, the option it chose (None: it chose none_or_other), the probability of every option
    the question offered (the abstention included) and the threshold applied to the choice (None: none is applied to a choice today). Option ids and numbers only: never a word of the request (AGENT-13)."""

    slot: str
    chosen: str | None
    probabilities: Mapping[str, float]
    threshold: Fraction | None


DiagnosticsSink = Callable[[str, tuple[SlotChoice, ...]], None]  # (correlation id, the turn's choices); internal, never part of the route's answer


class TenantScopeMismatch(Exception):
    """The core refused the tenant of a candidate read (REQ-CTX-006). Never a provider failure and never retried: the route answers it as an internal safety block."""


class Minter(Protocol):
    """The members of a proposal that nothing in the canonical input supplies (noema#580). Who sets them is not decided here."""

    def identifiers(self, canonical_input: Mapping[str, Any]) -> Identifiers: ...

    def candidate_set_id(self, canonical_input: Mapping[str, Any]) -> str: ...


def _no_pre_gate_errors(proposal: Proposal, normalised: NormalisedSlots) -> tuple[str, ...]:
    """The pipeline's own `Gate` seam. The schema gate on the assembled object is `CompileService`'s (and the core's second gate), so it is not repeated here on a half-built value."""
    return ()


class PipelineCompiler:
    """Build once per process, from configuration: the pipeline configuration of each pack it holds (by `<vertical>@<x.y.z>`), the lexicon, the harness, the candidate source and the minter."""

    def __init__(self, packs: Mapping[str, PipelineConfig], lexicon: Lexicon, harness: Harness, source: CandidateSource, minter: Minter, diagnostics: DiagnosticsSink | None = None) -> None:
        if not packs:
            raise ValueError("the compiler needs at least one pack")
        for ref in packs:
            if _PACK_REF.fullmatch(ref) is None:
                raise ValueError("a pack is held by <vertical>@<x.y.z>")
        self._packs = dict(packs)
        self._lexicon = lexicon
        self._harness = harness
        self._source = source
        self._minter = minter
        self._diagnostics = diagnostics  # internal and optional: the benchmark's job log reads the choices of a turn here; the proposal and the route's answer do not change with it (UND-17, A602)

    async def compile(self, canonical_input: Mapping[str, Any], pack_ref: str, open_intent: Mapping[str, Any] | None) -> bytes:
        """`open_intent` is not read: it is unvalidated input and no turn of the first slice continues an earlier intent (README, precondition 3)."""
        config = self._packs.get(pack_ref)
        if config is None:
            raise InputInvalid(InputInvalidReason.UNKNOWN_PACK)
        if canonical_input.get("channel") != "text":
            # A voice request needs the low-confidence rule for a key slot, which is a founder decision (AGENT-16); it is not invented here.
            raise InputInvalid(InputInvalidReason.NOT_TEXT_CHANNEL)
        now = _instant(canonical_input.get("received_at"))
        inp = PipelineInput(
            correlation_id=str(canonical_input["correlation_id"]),
            principal_ref=str(canonical_input["principal_ref"]),
            tenant_id=str(canonical_input["tenant_id"]),
            channel="text",
            text=str(canonical_input["text"]),
            locale_hint=_hint(canonical_input.get("locale_hint")),
            transcript_is_final=True,
            key_slot_low_asr=False,
            candidate_set_id=self._minter.candidate_set_id(canonical_input),
            candidate_set_version=_CANDIDATE_SET_VERSION,
        )
        result = await run_pipeline(inp, config, self._harness, self._source, Normaliser(self._lexicon, now).normalise, _no_pre_gate_errors)
        if self._diagnostics is not None and result.proposal is not None:
            self._diagnostics(inp.correlation_id, _choices(result.proposal))
        if result.outcome is Outcome.REJECTED_AT_GATE:
            if GATE_TENANT_SCOPE in result.gate_errors:
                raise TenantScopeMismatch
            if GATE_INPUT_INVALID in result.gate_errors:
                if result.input_invalid is None:
                    raise ValueError("a turn refused as invalid input names its reason")  # a bug, never a guess at a class
                raise InputInvalid(result.input_invalid)
            if result.screen_not_run:
                raise ScreenNotRun(tuple(stage.value for stage in result.screen_not_run))  # A437: internal class; to every caller still a ProviderUnavailable
            raise ProviderUnavailable  # a provider, the schema of its reply or the Evidence API failed: a rejection, never a guess (REQ-INT-002)
        return assemble(canonical_input, result, config.catalogue, pack_ref, self._minter.identifiers(canonical_input))


def _choices(proposal: Proposal) -> tuple[SlotChoice, ...]:
    """The closed-vocabulary choices of a turn, in slot order. No threshold is applied to a choice (`asserted` applies the pack's threshold to a yes/no answer only), so none is named."""
    return tuple(SlotChoice(slot, chosen, dict(proposal.probabilities.get(slot, {})), None) for slot, chosen in sorted(proposal.reference_slots.items()))


def _instant(value: Any) -> datetime:
    if not isinstance(value, str):
        raise InputInvalid(InputInvalidReason.BAD_RECEIVED_AT)
    try:
        parsed = datetime.fromisoformat(value)
    except ValueError as err:
        raise InputInvalid(InputInvalidReason.BAD_RECEIVED_AT) from err
    if parsed.tzinfo is None or parsed.utcoffset() is None:
        raise InputInvalid(InputInvalidReason.BAD_RECEIVED_AT)
    return parsed


def _hint(value: Any) -> str | None:
    return value if isinstance(value, str) else None
