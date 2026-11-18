"""SEC-03 (REQ-SEC-003, AGENT-13; noema#598, #468): the redactor masks exactly the runs that `digit_runs` finds, so the model service has one definition of a run of digits and the masking never
trusts a narrower one. Every code point is tried as the separator of a five-digit run. All data is SYNTHETIC."""

import pytest

from noema_model.platform.masking import MaskingLimits
from noema_model.platform.redaction import Redactor
from noema_model.platform.redaction.digit_runs import digit_runs, is_digit_separator

R = Redactor.from_limits(MaskingLimits(4))


def test_every_code_point_that_joins_a_run_for_digit_runs_joins_it_for_the_redactor_and_no_other() -> None:
    wrong_joined: list[str] = []
    wrong_left: list[str] = []
    for code in range(0x110000):
        char = chr(code)
        if char.isdecimal():
            continue
        text = f"1234{char}5"
        masked = R.mask_account_numbers(text) != text
        if masked and not is_digit_separator(char):
            wrong_joined.append(f"U+{code:04X}")
        if not masked and is_digit_separator(char):
            wrong_left.append(f"U+{code:04X}")
    assert not wrong_joined and not wrong_left, (wrong_joined[:10], wrong_left[:10])


HOSTILE = [
    "card 4111 1111 1111 1111 now",
    "card 4111\n1111\n1111\n1111",
    "card 4111 - 1111 - 1111 - 1111",
    "card 4111​1111​1111​1111",
    "card 4111(1111)1111(1111)",
    "card 4111.1111.1111.1111",
    "card 4111·1111·1111·1111",
    "card ４１１１　１１１１　１１１１　１１１１",
    "card ٤١١١ ١١١١ ١١١١ ١١١١",
    "amount 20,000,000 and 1234",
    "two runs 12345 + 67890",
    "short 123 456",
    "",
]


@pytest.mark.parametrize("text", HOSTILE)
def test_the_masked_text_keeps_every_character_outside_a_masked_run_and_four_digits_of_each_masked_run(text: str) -> None:
    out = R.mask_account_numbers(text)
    expected = []
    position = 0
    for start, end, count in digit_runs(text):
        expected.append(text[position:start])
        if count >= 5:
            digits = [c for c in text[start:end] if c.isdecimal()]
            expected.append("*" * (count - 4) + "".join(digits[-4:]))
        else:
            expected.append(text[start:end])
        position = end
    expected.append(text[position:])
    assert out == "".join(expected)


@pytest.mark.parametrize("text", HOSTILE)
def test_no_run_longer_than_the_limit_survives_masking(text: str) -> None:
    out = R.mask_account_numbers(text)
    assert all(count <= 4 for _, _, count in digit_runs(out))
