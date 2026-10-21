"""Deterministic ranking and the auto-resolve rule (REQ-AMB-003). No model, no clock, no randomness, no I/O.

score = sum of weight x feature, each feature in [0, 1]. Auto-resolve only if all of these hold:
  - exactly one candidate has the top score (a tie is never broken);
  - top - second >= auto_resolve_margin (second is 0 when there is only one candidate);
  - top >= the absolute floor;
  - no material ambiguity flag remains, and no key slot has low ASR confidence.
More than max_candidates_before_escalate candidates at or above the floor is TOO_MANY_CANDIDATES, and that is checked
first: a clear leader among a crowd is still a crowd. Weights, margin, floor and every tolerance are RankingConfig data;
no number that decides anything is written in this file (AGENT-16). A feature with nothing to compare (no amount said)
scores 0, which can only lower a score, never raise one.
"""

import hashlib
import json
from datetime import UTC, datetime, timedelta
from fractions import Fraction

from noema_model.understanding.resolution.config import RankingConfig
from noema_model.understanding.resolution.matcher import RecipientMatcher
from noema_model.understanding.resolution.types import (
    FEATURES,
    Candidate,
    Context,
    Resolution,
    ResolutionState,
    ScoredCandidate,
    Target,
    Why,
)

_ZERO = Fraction(0)
_ONE = Fraction(1)
_MICROSECOND = timedelta(microseconds=1)
_EPOCH = datetime(1970, 1, 1, tzinfo=UTC)


def fraction_text(value: Fraction) -> str:
    return f"{value.numerator}/{value.denominator}"


def _ramp(distance: Fraction, tolerance: Fraction) -> Fraction:
    """1 at distance 0, falling in a straight line to 0 at the tolerance; 0 beyond it and when there is no tolerance."""
    if distance == 0:
        return _ONE
    if tolerance <= 0 or distance >= tolerance:
        return _ZERO
    return _ONE - distance / tolerance


def amount_feature(target: Target, candidate: Candidate, config: RankingConfig) -> Fraction:
    if target.amount_minor is None or target.currency is None or target.currency != candidate.currency:
        return _ZERO
    wanted = Fraction(target.amount_minor)
    return _ramp(abs(Fraction(candidate.amount_minor) - wanted), config.amount_tolerance_ratio * wanted)


def temporal_feature(target: Target, candidate: Candidate, config: RankingConfig) -> Fraction:
    if target.window is None:
        return _ZERO
    start, end = target.window
    if start <= candidate.occurred_at <= end:
        return _ONE
    gap = start - candidate.occurred_at if candidate.occurred_at < start else candidate.occurred_at - end
    return _ramp(Fraction(gap // _MICROSECOND, 1_000_000), Fraction(config.time_tolerance_seconds))


def score_candidates(
    candidates: tuple[Candidate, ...], target: Target, context: Context, config: RankingConfig, matcher: RecipientMatcher
) -> tuple[ScoredCandidate, ...]:
    scored = []
    for candidate in candidates:
        features = {
            "amount": amount_feature(target, candidate, config),
            "recipient": matcher.score(target.counterparty_surface, candidate.counterparty_display, phonetic=context.phonetic),
            "temporal": temporal_feature(target, candidate, config),
            "relationship": _ONE if candidate.beneficiary_alias_match is True else _ZERO,
            "type_direction": _ONE if target.expected_direction is not None and target.expected_direction == candidate.direction else _ZERO,
        }
        total = sum((config.weights[name] * features[name] for name in FEATURES), _ZERO)
        scored.append(ScoredCandidate(candidate, features, total))
    # Presentation order only (REQ-AMB-004: by score, then by time); it never breaks a tie for resolution.
    return tuple(sorted(scored, key=lambda s: (-s.score, -((s.candidate.occurred_at - _EPOCH) // _MICROSECOND), s.candidate.candidate_id)))


def rank(
    candidates: tuple[Candidate, ...],
    target: Target,
    context: Context,
    config: RankingConfig,
    matcher: RecipientMatcher,
    *,
    candidate_set_id: str,
    candidate_set_version: int,
) -> Resolution:
    if len(candidates) > config.max_raw_candidates:
        raise ValueError("more candidates than the bounded read allows")
    if len({c.candidate_id for c in candidates}) != len(candidates):
        raise ValueError("duplicate candidate ids")
    ranked = score_candidates(candidates, target, context, config, matcher)
    top = ranked[0].score if ranked else _ZERO
    second = ranked[1].score if len(ranked) > 1 else _ZERO
    margin = top - second
    above_floor = sum(1 for s in ranked if s.score >= config.absolute_floor)

    why: list[Why] = []
    if not ranked:
        state = ResolutionState.NO_CANDIDATES
    else:
        if len(ranked) > 1 and top == second:
            why.append(Why.TIE_AT_TOP)
        if margin < config.auto_resolve_margin:
            why.append(Why.MARGIN_NOT_MET)
        if top < config.absolute_floor:
            why.append(Why.BELOW_FLOOR)
        if context.material_flag_present:
            why.append(Why.MATERIAL_FLAG)
        if context.key_slot_low_asr:
            why.append(Why.LOW_ASR_CONFIDENCE)
        if above_floor > config.max_candidates_before_escalate:
            why.append(Why.TOO_MANY_ABOVE_FLOOR)
        if Why.TOO_MANY_ABOVE_FLOOR in why:
            state = ResolutionState.TOO_MANY_CANDIDATES
        elif why:
            state = ResolutionState.AMBIGUOUS
        else:
            state = ResolutionState.AUTO_RESOLVED

    resolved = ranked[0].candidate.candidate_id if state is ResolutionState.AUTO_RESOLVED else None
    return Resolution(
        state=state,
        ranked=ranked,
        resolved_candidate_id=resolved,
        top_score=top,
        second_score=second,
        margin=margin,
        above_floor_count=above_floor,
        why_not=tuple(why),
        candidate_set_id=candidate_set_id,
        candidate_set_version=candidate_set_version,
        content_hash=_content_hash(ranked, target, context, config, matcher, candidate_set_id, candidate_set_version),
    )


def _content_hash(
    ranked: tuple[ScoredCandidate, ...],
    target: Target,
    context: Context,
    config: RankingConfig,
    matcher: RecipientMatcher,
    set_id: str,
    version: int,
) -> str:
    """A digest of everything the result depends on, so a replay can prove it recomputed the same set."""
    body = {
        "set": [set_id, version],
        "config": config.digest,
        "matcher": matcher.digest,
        "target": [
            target.amount_minor, target.currency,
            [t.isoformat() for t in target.window] if target.window else None,
            target.counterparty_surface, target.expected_direction,
        ],
        "context": [context.material_flag_present, context.key_slot_low_asr, context.phonetic],
        "ranked": [
            [
                s.candidate.candidate_id, s.candidate.resource_ref, s.candidate.amount_minor, s.candidate.currency,
                s.candidate.occurred_at.isoformat(), s.candidate.counterparty_display, s.candidate.direction,
                s.candidate.beneficiary_alias_match, {k: fraction_text(v) for k, v in s.features.items()},
                fraction_text(s.score),
            ]
            for s in ranked
        ],
    }
    return hashlib.sha256(json.dumps(body, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def candidates_resolved_payload(resolution: Resolution) -> dict[str, object]:
    """The CANDIDATES_RESOLVED audit payload: ids and scores only, no names, amounts or account digits (REQ-AMB-002)."""
    return {
        "candidate_set_id": resolution.candidate_set_id,
        "candidate_set_version": resolution.candidate_set_version,
        "state": resolution.state.value,
        "why_not": [w.value for w in resolution.why_not],
        "margin": fraction_text(resolution.margin),
        "candidates": [
            {
                "candidate_id": s.candidate.candidate_id,
                "score": fraction_text(s.score),
                "features": {k: fraction_text(v) for k, v in s.features.items()},
            }
            for s in resolution.ranked
        ],
        "content_hash": resolution.content_hash,
    }
