"""The fixed DAG (D3): classify, then (enumerated slots | flags | locale | screening | slot values) in parallel, then
normalise, gate, read candidates and rank.

Every step is a stateless function of its arguments; the only I/O is the harness and the candidate source it is given.
The pipeline proposes. It never decides, writes a clarification question or checks policy: when it cannot proceed it
says so in the result and stops, and a provider failure is a typed rejection at the gate, never a heuristic guess
(REQ-INT-002).
"""

import asyncio
from collections.abc import Mapping
from datetime import timedelta
from fractions import Fraction

from noema_model.harness import CallRecord, Harness, HarnessRejection
from noema_model.understanding.pipeline import steps
from noema_model.understanding.pipeline.types import (
    CandidateQuery,
    CandidateRequestInvalid,
    CandidateScopeError,
    CandidateSource,
    CandidateSourceError,
    Gate,
    MentionProposal,
    Normalise,
    NormalisedSlots,
    Outcome,
    PipelineConfig,
    PipelineInput,
    PipelineResult,
    Proposal,
)
from noema_model.understanding.resolution import Candidate, Context, Resolution, Target, rank

GATE_INPUT_INVALID = "INPUT_INVALID"
GATE_EVIDENCE_UNAVAILABLE = "DEPENDENCY_UNAVAILABLE"
GATE_OUT_OF_BOUNDS = "SCHEMA_INVALID"
GATE_TENANT_SCOPE = "TENANT_SCOPE_MISMATCH"


def _stopped(
    outcome: Outcome, calls: list[CallRecord], flags: tuple[str, ...] = (), errors: tuple[str, ...] = (),
    proposal: Proposal | None = None, normalised: NormalisedSlots | None = None,
    signals: Mapping[str, bool] | None = None,
) -> PipelineResult:
    return PipelineResult(
        outcome=outcome, proposal=proposal, normalised=normalised, ambiguity_flags=flags, gate_errors=errors,
        query=None, resolution=None, injection_signals=signals or {}, calls=tuple(calls),
    )


def _rejected(rejection: HarnessRejection, calls: list[CallRecord]) -> PipelineResult:
    if rejection.call is not None:
        calls.append(rejection.call)
    return _stopped(Outcome.REJECTED_AT_GATE, calls, errors=(rejection.code,))


def build_query(inp: PipelineInput, normalised: NormalisedSlots, resource_kind: str | None, config: PipelineConfig) -> CandidateQuery:
    """REQ-AMB-002: the time window widened by the pack tolerance, and the amount exact, or widened when uncertain."""
    tolerance = timedelta(seconds=config.ranking.time_tolerance_seconds)
    window = None
    if normalised.window is not None:
        window = (normalised.window[0] - tolerance, normalised.window[1] + tolerance)
    amount_range = None
    if normalised.amount_minor is not None:
        delta = 0
        if normalised.amount_uncertain:
            wide = Fraction(normalised.amount_minor) * config.ranking.amount_tolerance_ratio
            delta = -(-wide.numerator // wide.denominator)  # rounded up, so the search is never narrower than the ramp
        amount_range = (max(0, normalised.amount_minor - delta), normalised.amount_minor + delta)
    return CandidateQuery(
        correlation_id=inp.correlation_id,
        principal_ref=inp.principal_ref,
        tenant_id=inp.tenant_id,
        resource_kind=resource_kind,
        amount_range=amount_range,
        currency=normalised.currency,
        time_window=window,
        counterparty_surface=normalised.counterparty_surface,
    )


async def run_pipeline(
    inp: PipelineInput,
    config: PipelineConfig,
    harness: Harness,
    source: CandidateSource,
    normalise: Normalise,
    gate: Gate,
) -> PipelineResult:
    if inp.channel == "voice" and not inp.transcript_is_final:
        return _stopped(Outcome.REJECTED_AT_GATE, [], errors=(GATE_INPUT_INVALID,))  # REQ-INP-001: only final transcripts
    calls: list[CallRecord] = []

    # 1. The node.
    try:
        node_id, node_probabilities, call = await steps.classify_node(harness, config.catalogue, inp)
    except HarnessRejection as rejection:
        return _rejected(rejection, calls)
    calls.append(call)
    if node_id is None:
        return _stopped(Outcome.UNCLEAR, calls, flags=(config.intent_unclear_flag,))
    if node_id == config.catalogue.out_of_scope_node:
        return _stopped(Outcome.OUT_OF_SCOPE, calls, flags=(config.out_of_scope_flag,))
    node = config.catalogue.nodes[node_id]

    # 2. The independent steps, together. A rejection in any required one stops the turn; screening never does.
    gathered = await asyncio.gather(
        steps.enumerated_slots(harness, config.catalogue, node, inp),
        steps.propose_flags(harness, config.proposable_flags, inp, config.native_yes_threshold),
        steps.detect_locale(harness, config.locales, inp),
        steps.slot_mentions(harness, node, inp),
        steps.screen_quietly(harness, {"utterance": inp.text}, inp, config.native_yes_threshold),
        return_exceptions=True,
    )
    rejections = [g for g in gathered if isinstance(g, HarnessRejection)]
    others = [g for g in gathered if isinstance(g, BaseException) and not isinstance(g, HarnessRejection)]
    if others:
        raise others[0]
    if rejections:
        # Every call that was made is accounted for, in the order the steps were started, whether or not it succeeded.
        for g in gathered:
            made = g.call if isinstance(g, HarnessRejection) else g[-1]  # type: ignore[index]
            if made is not None:
                calls.append(made)
        return _stopped(Outcome.REJECTED_AT_GATE, calls, errors=(rejections[0].code,))
    (slot_values, slot_probs, c_slots), (flags, flag_probs, c_flags), (locale, switching, locale_probs, c_locale), (
        mentions, c_mentions), (signals, c_screen) = gathered  # type: ignore[misc]
    calls.extend(c for c in (c_slots, c_flags, c_locale, c_mentions, c_screen) if c is not None)

    raised = set(flags)
    if steps.required_reference_gaps(node, slot_values):
        raised.add(config.intent_unclear_flag)
    probabilities: dict[str, Mapping[str, float]] = {"intent_node": node_probabilities, **slot_probs, "flags": flag_probs, "locale": locale_probs}
    proposal = Proposal(
        intent_node=node_id,
        reference_slots=slot_values,
        mentions=mentions,
        ambiguity_flags=tuple(sorted(raised)),
        locale=locale,
        code_switching=switching,
        probabilities=probabilities,
    )

    # 3. The deterministic lexicon, then the gate.
    normalised = normalise(inp, proposal)
    errors = gate(proposal, normalised)
    if errors:
        return _stopped(Outcome.REJECTED_AT_GATE, calls, flags=proposal.ambiguity_flags, errors=errors,
                        proposal=proposal, normalised=normalised, signals=signals)

    # 4. Candidates and ranking, for a node that resolves a resource.
    search = next((s for s in node.slots if s.candidate_search), None)
    if search is None:
        return PipelineResult(Outcome.PROPOSED, proposal, normalised, proposal.ambiguity_flags, (), None, None, signals, tuple(calls))
    query = build_query(inp, normalised, search.resource_kind, config)
    if query.time_window is None:
        # The read contract requires a time window (REQ-CTX-006), so a request with none is invalid input: it is refused at
        # the gate and nothing is read. NO_TRANSACTION_MATCH (REQ-AMB-002) is for a search that ran and came back empty.
        return _stopped(Outcome.REJECTED_AT_GATE, calls, flags=proposal.ambiguity_flags, errors=(GATE_INPUT_INVALID,),
                        proposal=proposal, normalised=normalised, signals=signals)
    try:
        candidates = await source.fetch(query)
    except CandidateScopeError:
        return _stopped(Outcome.REJECTED_AT_GATE, calls, flags=proposal.ambiguity_flags, errors=(GATE_TENANT_SCOPE,),
                        proposal=proposal, normalised=normalised, signals=signals)
    except CandidateRequestInvalid:
        return _stopped(Outcome.REJECTED_AT_GATE, calls, flags=proposal.ambiguity_flags, errors=(GATE_INPUT_INVALID,),
                        proposal=proposal, normalised=normalised, signals=signals)
    except CandidateSourceError:
        return _stopped(Outcome.REJECTED_AT_GATE, calls, flags=proposal.ambiguity_flags, errors=(GATE_EVIDENCE_UNAVAILABLE,),
                        proposal=proposal, normalised=normalised, signals=signals)
    if len(candidates) > config.ranking.max_raw_candidates:
        return _stopped(Outcome.REJECTED_AT_GATE, calls, flags=proposal.ambiguity_flags, errors=(GATE_OUT_OF_BOUNDS,),
                        proposal=proposal, normalised=normalised, signals=signals)

    screening = asyncio.create_task(steps.screen_quietly(harness, {c.candidate_id: c.counterparty_display for c in candidates}, inp, config.native_yes_threshold))
    try:
        resolution = _rank(candidates, normalised, proposal, inp, config)
    finally:
        candidate_signals, c_candidate_screen = await screening
    if c_candidate_screen is not None:
        calls.append(c_candidate_screen)
    all_signals = {**signals, **{f"candidate:{k}": v for k, v in candidate_signals.items()}}

    final_flags = set(proposal.ambiguity_flags)
    state_flag = config.state_flags.get(resolution.state)
    if state_flag is not None:
        final_flags.add(state_flag)
    return PipelineResult(
        Outcome.PROPOSED, proposal, normalised, tuple(sorted(final_flags)), (), query, resolution, all_signals, tuple(calls)
    )


def _rank(
    candidates: tuple[Candidate, ...], normalised: NormalisedSlots, proposal: Proposal, inp: PipelineInput, config: PipelineConfig
) -> Resolution:
    target = Target(normalised.amount_minor, normalised.currency, normalised.window, normalised.counterparty_surface, normalised.expected_direction)
    context = Context(
        material_flag_present=bool(set(proposal.ambiguity_flags) & config.material_flags),
        key_slot_low_asr=inp.key_slot_low_asr,
        phonetic=inp.channel == "voice",
    )
    return rank(
        candidates, target, context, config.ranking, config.matcher,
        candidate_set_id=inp.candidate_set_id, candidate_set_version=inp.candidate_set_version,
    )
