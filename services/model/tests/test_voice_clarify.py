"""Voice clarification flow tests (REQ-AMB-007, REQ-VOI-003, EC-A11, EC-V02, PERD §35.3).

A low-confidence key slot always clarifies by confirming the value; after the allowed number of failed turns the person is
offered text. The flow is language-neutral: it names the slot and the heard value, and the pack's templates render the
question per locale (A8, A18). Thresholds and limits are arguments.
"""

import math

import pytest
from hypothesis import given
from hypothesis import strategies as st

from noema_model.voice.clarify import KeySlot, StepKind, VoiceStep, decide_voice_step, key_slot_from_tokens

THRESHOLD = 0.85
MAX_FAILED = 2


def slot(name: str, confidence: float, heard: str = "20,000") -> KeySlot:
    return KeySlot(name, heard, confidence)


def test_every_key_slot_at_or_above_the_threshold_proceeds() -> None:
    step = decide_voice_step([slot("amount", 0.99), slot("counterparty", 0.85)], THRESHOLD, 0, MAX_FAILED)
    assert step == VoiceStep(StepKind.PROCEED, None, None)


def test_a_low_confidence_key_slot_confirms_the_heard_value_instead_of_proceeding() -> None:
    step = decide_voice_step([slot("amount", 0.5, "20,000"), slot("counterparty", 0.99)], THRESHOLD, 0, MAX_FAILED)
    assert step.kind is StepKind.CONFIRM_SLOT
    assert step.slot_name == "amount" and step.heard_value == "20,000"


def test_with_two_low_slots_the_first_in_the_callers_order_is_confirmed_one_at_a_time() -> None:
    step = decide_voice_step([slot("counterparty", 0.4, "Ada"), slot("amount", 0.3)], THRESHOLD, 0, MAX_FAILED)
    assert step.slot_name == "counterparty"


def test_after_the_allowed_failed_turns_a_still_low_slot_offers_text() -> None:
    step = decide_voice_step([slot("amount", 0.5)], THRESHOLD, MAX_FAILED, MAX_FAILED)
    assert step == VoiceStep(StepKind.OFFER_TEXT, None, None)


def test_one_failed_turn_short_of_the_limit_still_confirms() -> None:
    assert decide_voice_step([slot("amount", 0.5)], THRESHOLD, MAX_FAILED - 1, MAX_FAILED).kind is StepKind.CONFIRM_SLOT


def test_failed_turns_do_not_block_a_call_whose_slots_are_all_confident() -> None:
    assert decide_voice_step([slot("amount", 0.99)], THRESHOLD, 5, MAX_FAILED).kind is StepKind.PROCEED


@pytest.mark.parametrize("bad", [math.nan, math.inf, -0.5, 1.5])
def test_an_invalid_confidence_is_low_and_never_proceeds(bad: float) -> None:
    assert decide_voice_step([slot("amount", bad)], THRESHOLD, 0, MAX_FAILED).kind is StepKind.CONFIRM_SLOT


def test_no_key_slots_at_all_is_refused_not_treated_as_confident() -> None:
    with pytest.raises(ValueError):
        decide_voice_step([], THRESHOLD, 0, MAX_FAILED)


def test_a_key_slot_built_from_its_tokens_takes_the_minimum_and_a_missing_one_is_low() -> None:
    assert key_slot_from_tokens("amount", "20,000", [0.99, 0.91]) == KeySlot("amount", "20,000", 0.91)
    assert key_slot_from_tokens("amount", "20,000", [0.99, None]).confidence == 0.0
    assert key_slot_from_tokens("amount", "20,000", []).confidence == 0.0


def test_a_provider_that_returns_no_confidence_makes_every_key_slot_confirm() -> None:
    slots = [key_slot_from_tokens(name, "x", [None, None]) for name in ("amount", "counterparty", "time")]
    assert decide_voice_step(slots, THRESHOLD, 0, MAX_FAILED).kind is StepKind.CONFIRM_SLOT


@pytest.mark.parametrize("threshold,max_failed", [(math.nan, 2), (-0.1, 2), (1.1, 2), (0.85, 0), (0.85, -1)])
def test_a_bad_threshold_or_limit_is_a_configuration_error(threshold: float, max_failed: int) -> None:
    with pytest.raises(ValueError):
        decide_voice_step([slot("amount", 0.99)], threshold, 0, max_failed)


def test_a_negative_failed_turn_count_is_refused() -> None:
    with pytest.raises(ValueError):
        decide_voice_step([slot("amount", 0.99)], THRESHOLD, -1, MAX_FAILED)


@given(
    st.lists(st.tuples(st.sampled_from(["amount", "counterparty", "time"]),
                       st.one_of(st.floats(allow_nan=True, allow_infinity=True), st.floats(0.0, 1.0))), min_size=1, max_size=3),
    st.integers(0, 5),
)
def test_property_proceed_only_when_every_key_slot_is_a_valid_ratio_at_or_above_the_threshold(
    raw: list[tuple[str, float]], failed: int
) -> None:
    """REQ-AMB-007: acting on a key slot below the threshold must be zero."""
    slots = [KeySlot(name, "v", value) for name, value in raw]
    step = decide_voice_step(slots, THRESHOLD, failed, MAX_FAILED)
    if step.kind is StepKind.PROCEED:
        assert all(0.0 <= s.confidence <= 1.0 and s.confidence >= THRESHOLD for s in slots)
