"""Per-token to per-slot confidence (VOI-03, REQ-VOI-003, REQ-AMB-007).

A slot's confidence is the minimum over the confidences of the tokens that make it up. A token with no confidence, or a
slot with no tokens at all, counts as 0.0: the provider did not vouch for it, so it is low (PERD REQ-VOI-003 [REC]).
A confidence that is present but not a finite number in [0, 1] is invalid provider data and is refused, never compared:
NaN is neither above nor below any threshold, so comparing it would let a bad value skip the clarification (INV-02).
Whether a slot is low is judged against a threshold the caller reads from the pack (0.85, DEC-2026-10-17); no threshold
is written here (AGENT-16).

This module sees plain floats, never a provider type, so it cannot depend on a vendor (BD-16). Which tokens belong to a
slot is decided by the gateway that aligns the final transcript to the slot text; that alignment is not built here.
"""

import math
from collections.abc import Sequence

MISSING_CONFIDENCE = 0.0


class InvalidConfidence(ValueError):
    """A confidence or threshold was NaN, infinite or outside [0, 1]. The caller treats the utterance as unusable."""


def _valid(value: float) -> bool:
    return not isinstance(value, bool) and math.isfinite(value) and 0.0 <= value <= 1.0


def slot_confidence(token_confidences: Sequence[float | None]) -> float:
    """The minimum token confidence, with a missing confidence, or no tokens, counting as MISSING_CONFIDENCE.
    Raises InvalidConfidence for a present value that is not a finite ratio."""
    if any(value is not None and not _valid(value) for value in token_confidences):
        raise InvalidConfidence("token confidence is not a finite ratio")
    if not token_confidences:
        return MISSING_CONFIDENCE
    return min(MISSING_CONFIDENCE if value is None else value for value in token_confidences)


def is_low_confidence(confidence: float, threshold: float) -> bool:
    """True when a key slot must be confirmed with the user rather than acted on. Exactly at the threshold is acceptable.
    An invalid confidence is low; an invalid threshold is a configuration error and raises InvalidConfidence."""
    if not _valid(threshold):
        raise InvalidConfidence("threshold is not a finite ratio")
    return not _valid(confidence) or confidence < threshold
