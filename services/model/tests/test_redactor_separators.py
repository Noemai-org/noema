"""SEC-03 (A154 as corrected; REQ-SEC-003, AGENT-13): a formatted account number reaches no provider unmasked, whatever separator or script is used. A digit run is joined across
one separator between digits: an ASCII space or hyphen, and every Unicode space and dash a person or a keyboard can produce (a non-breaking hyphen, an en dash, a no-break
space, a thin space, a full-width hyphen and the rest). A comma still ends a run (an amount is not an account number). Digits of every script count (Unicode decimal digits).
All data is SYNTHETIC."""

import pytest

from noema_model.platform.masking import MaskingLimits
from noema_model.platform.redaction import Redactor

R = Redactor.from_limits(MaskingLimits(4))

# One separator of each kind: Unicode dashes (hyphen, non-breaking hyphen, figure dash, en and em dash, horizontal bar, minus sign, small and full-width hyphen-minus) and Unicode
# spaces (no-break, en quad to hair space, narrow no-break, medium mathematical, ideographic).
DASHES = ["‐", "‑", "‒", "–", "—", "―", "−", "﹘", "﹣", "－"]
SPACES = [" ", " ", " ", " ", " ", " ", " ", " ", "　"]


@pytest.mark.parametrize("sep", DASHES + SPACES + [" ", "-"])
def test_a_card_number_grouped_by_any_separator_is_masked(sep: str) -> None:
    text = f"my card is 4111{sep}1111{sep}1111{sep}1111 please"
    out = R.mask_account_numbers(text)
    assert "4111" + sep + "1111" not in out
    assert out == "my card is ************1111 please"


@pytest.mark.parametrize("sep", DASHES + SPACES)
def test_a_five_digit_run_split_by_a_unicode_separator_is_masked_and_a_separate_four_digit_number_is_not(sep: str) -> None:
    assert R.mask_account_numbers(f"ref 1234{sep}5 and 6789") == "ref *2345 and 6789"


@pytest.mark.parametrize("digits", ["١٢٣٤٥", "１２３４５", "१२३४५"])
def test_a_five_digit_run_of_any_script_is_masked_to_its_last_four(digits: str) -> None:
    out = R.mask_account_numbers(f"pin {digits} now")
    assert digits not in out and out == f"pin *{digits[-4:]} now"


def test_a_mixed_script_and_separator_run_is_one_run() -> None:
    assert R.mask_account_numbers("4111‑1111 ١٢٣٤") == "*" * 8 + "١٢٣٤"


def test_a_comma_and_a_newline_still_end_a_run() -> None:
    assert R.mask_account_numbers("20,000,000") == "20,000,000"
    assert R.mask_account_numbers("1234\n5678") == "1234\n5678"
    assert R.mask_account_numbers("1234\t5678") == "1234\t5678"


def test_two_separators_in_a_row_end_a_run() -> None:
    assert R.mask_account_numbers("1234‑ 5678") == "1234‑ 5678"
