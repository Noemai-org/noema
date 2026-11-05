"""Slot confidence tests (VOI-03, REQ-VOI-003, REQ-AMB-007)."""

import math

import pytest
from hypothesis import given
from hypothesis import strategies as st

from noema_model.voice.confidence import MISSING_CONFIDENCE, is_low_confidence, slot_confidence

THRESHOLD = 0.85  # in a test only; production reads it from the pack


def test_the_slot_takes_the_minimum_of_its_tokens() -> None:
    assert slot_confidence([0.99, 0.91, 0.97]) == 0.91


def test_one_weak_token_makes_the_whole_slot_weak() -> None:
    assert is_low_confidence(slot_confidence([0.99, 0.2, 0.99]), THRESHOLD)


def test_a_missing_token_confidence_counts_as_low() -> None:
    assert slot_confidence([0.99, None, 0.99]) == MISSING_CONFIDENCE
    assert is_low_confidence(slot_confidence([0.99, None]), THRESHOLD)


def test_a_provider_with_no_confidence_at_all_makes_every_slot_low() -> None:
    assert is_low_confidence(slot_confidence([None, None, None]), THRESHOLD)


def test_a_slot_with_no_tokens_is_low_not_certain() -> None:
    assert slot_confidence([]) == MISSING_CONFIDENCE
    assert is_low_confidence(slot_confidence([]), THRESHOLD)


def test_exactly_the_threshold_is_not_low_and_just_under_is() -> None:
    assert not is_low_confidence(0.85, 0.85)
    assert is_low_confidence(math.nextafter(0.85, 0.0), 0.85)


def test_a_single_perfect_token_is_accepted() -> None:
    assert not is_low_confidence(slot_confidence([1.0]), THRESHOLD)


@given(st.lists(st.one_of(st.none(), st.floats(min_value=0.0, max_value=1.0)), min_size=1))
def test_property_never_above_any_token_and_never_invented(tokens: list[float | None]) -> None:
    result = slot_confidence(tokens)
    assert 0.0 <= result <= 1.0
    assert all(result <= (MISSING_CONFIDENCE if value is None else value) for value in tokens)
    assert result in [MISSING_CONFIDENCE if value is None else value for value in tokens]


@given(st.lists(st.floats(min_value=0.0, max_value=1.0), min_size=1), st.floats(min_value=0.0, max_value=1.0))
def test_property_adding_a_missing_token_never_raises_confidence(tokens: list[float], extra: float) -> None:
    assert slot_confidence([*tokens, None]) <= slot_confidence(tokens)
    assert slot_confidence([*tokens, extra]) <= slot_confidence(tokens)


@pytest.mark.parametrize("threshold", [0.0, 1.0])
def test_threshold_edges(threshold: float) -> None:
    assert not is_low_confidence(0.0, 0.0)
    assert is_low_confidence(0.99, 1.0) == (threshold == 1.0)
