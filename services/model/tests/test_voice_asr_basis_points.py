"""The ASR confidence of an utterance, in integer basis points, for the Intent Object's `source.asr_confidence` (VOI-03, REQ-VOI-003, REQ-AMB-007; rulings A173, A191, A228).

A228: the voice confidence for the low-ASR rule is the minimum over tokens, in basis points, with a missing token counted as 0. A173: a confidence is floored to integer basis
points, so it never claims more than the provider did. This is the one definition the compiler's assembler (noema#581) and Trust read; it is the same minimum as `slot_confidence`
(VOI-03), expressed the way the contract carries it. All values are SYNTHETIC (AGENT-25).
"""

from fractions import Fraction
from math import floor

import pytest
from hypothesis import given
from hypothesis import strategies as st

from noema_model.voice.confidence import InvalidConfidence, asr_confidence_basis_points, slot_confidence


@pytest.mark.parametrize("tokens,expected", [
    ([0.93, 0.99], 9300),  # the minimum, not the mean
    ([1.0], 10000),
    ([0.0], 0),
    ([0.29], 2900),  # read as its shortest decimal: 0.29 is 2900, not 2899
    ([0.99999], 9999),  # floored, never rounded up
    ([0.00009], 0),
    ([0.85, 0.85], 8500),
    ([0.5, 0.2, 0.9], 2000),
])
def test_the_minimum_token_is_floored_to_basis_points(tokens: list[float], expected: int) -> None:
    assert asr_confidence_basis_points(tokens) == expected


def test_a_missing_token_confidence_counts_as_zero() -> None:
    assert asr_confidence_basis_points([0.99, None, 0.97]) == 0
    assert asr_confidence_basis_points([None]) == 0


def test_no_tokens_at_all_counts_as_zero_never_as_certain() -> None:
    assert asr_confidence_basis_points([]) == 0


@pytest.mark.parametrize("bad", [float("nan"), float("inf"), float("-inf"), -0.01, 1.01, 2.0, True])
def test_a_value_that_is_not_a_finite_ratio_is_refused_never_compared(bad: float) -> None:
    with pytest.raises(InvalidConfidence):
        asr_confidence_basis_points([0.9, bad])


def test_the_result_is_a_plain_integer_in_the_contracts_range() -> None:
    value = asr_confidence_basis_points([0.123456789])
    assert type(value) is int and value == 1234


@given(st.lists(st.one_of(st.none(), st.floats(min_value=0.0, max_value=1.0, allow_nan=False)), min_size=0, max_size=12))
def test_property_it_never_claims_more_than_the_weakest_token(tokens: list[float | None]) -> None:
    value = asr_confidence_basis_points(tokens)
    assert 0 <= value <= 10000
    for token in tokens:
        assert token is not None or value == 0  # a missing token forces zero
        if token is not None:
            assert Fraction(value, 10000) <= Fraction(str(token))  # never above any token's own decimal


@given(st.lists(st.floats(min_value=0.0, max_value=1.0, allow_nan=False), min_size=1, max_size=12))
def test_property_it_is_the_floor_of_the_slot_confidence(tokens: list[float]) -> None:
    assert asr_confidence_basis_points(tokens) == floor(Fraction(str(slot_confidence(tokens))) * 10000)


@given(st.lists(st.floats(min_value=0.0, max_value=1.0, allow_nan=False), min_size=1, max_size=12))
def test_property_adding_a_missing_token_never_raises_it(tokens: list[float]) -> None:
    assert asr_confidence_basis_points([*tokens, None]) <= asr_confidence_basis_points(tokens)
