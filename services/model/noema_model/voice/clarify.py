"""The voice clarification flow (REQ-AMB-007, REQ-VOI-003; PERD §35.3, EC-A11, EC-V02).

A key slot whose speech confidence is below the pack threshold is never acted on: the person is asked to confirm the value
that was heard. After the allowed number of failed turns, text input is offered instead. The flow is language-neutral. It
returns which slot to confirm and the value heard, and the pack's templates render the question in the person's locale.

The threshold and the turn limit are arguments, read from the pack by the caller (AGENT-16). A threshold of 0.0 is refused:
it would let every slot proceed, so a misconfigured pack would fail open (Security Reviewer note on #228).

Scope: this is a decision function only. It does not raise the KEY_SLOT_LOW_ASR_CONFIDENCE classification, write the audit
flag or check that the transcript is final; those belong to the caller that wires it (plan task AMB-04, owner C2), which
takes the final transcript only from voice/pipeline.py's FinalUtterance (EC-V04).
"""

import math
from collections.abc import Sequence
from dataclasses import dataclass
from enum import StrEnum

from noema_model.voice.confidence import is_low_confidence, slot_confidence


class StepKind(StrEnum):
    PROCEED = "PROCEED"
    CONFIRM_SLOT = "CONFIRM_SLOT"
    OFFER_TEXT = "OFFER_TEXT"


@dataclass(frozen=True, slots=True)
class KeySlot:
    name: str
    heard_value: str
    confidence: float


@dataclass(frozen=True, slots=True)
class VoiceStep:
    """`heard_value` is what the provider heard, offered to the person for confirmation; it is not a confirmed value and
    must never be acted on until the person confirms it."""

    kind: StepKind
    slot_name: str | None
    heard_value: str | None


def key_slot_from_tokens(name: str, heard_value: str, token_confidences: Sequence[float | None]) -> KeySlot:
    """A key slot whose confidence is the minimum over its tokens; a missing token confidence counts as low."""
    return KeySlot(name, heard_value, slot_confidence(token_confidences))


def decide_voice_step(key_slots: Sequence[KeySlot], threshold: float, failed_turns: int, max_failed_turns: int) -> VoiceStep:
    """What to do with an utterance's key slots. `key_slots` is in the order the caller wants them confirmed.

    `failed_turns` is counted by the caller for the whole utterance and is not reset per slot: a turn fails when the person
    rejects the offered value, or the slot is still low after they repeat it. Once it reaches `max_failed_turns`, a still-low
    slot offers text instead of asking again."""
    if not key_slots:
        raise ValueError("the caller must name the key slots")
    if not (math.isfinite(threshold) and 0.0 < threshold <= 1.0) or max_failed_turns < 1 or failed_turns < 0:
        raise ValueError("threshold or turn limits out of range")
    for key_slot in key_slots:
        if is_low_confidence(key_slot.confidence, threshold):
            if failed_turns >= max_failed_turns:
                return VoiceStep(StepKind.OFFER_TEXT, None, None)
            return VoiceStep(StepKind.CONFIRM_SLOT, key_slot.name, key_slot.heard_value)
    return VoiceStep(StepKind.PROCEED, None, None)
