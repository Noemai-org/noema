"""The one definition of a run of digits in the model service (UND-05; REQ-SEC-003; INV-02; AGENT-13). All data is SYNTHETIC.

The Record's guard (`record/payloadguard`, noema#536 and noema#600; A194, A231) joins digits across any number of the separators below, in any script. The assembler uses this
definition to keep a model's words out of the proposal, and the redactor of this package is to use it for what reaches a provider (a follow-up, stated on noema#598). These tests are
written before `noema_model.platform.redaction.digit_runs` exists: they fail with an import error until then.
"""

import pytest

from noema_model.platform.redaction.digit_runs import digit_runs, is_digit_separator, longest_digit_run
from noema_model.understanding.compiler import assemble

# One representative of every kind of separator of the Record guard's `separators` (noema#600, head 75beb587), of the guard on `main` and of the contracts' public-API pattern.
SEPARATORS = {
    "space": " ", "two spaces": "  ", "tab": "\t", "newline": "\n", "vertical tab": "\x0b", "next line": "\x85", "no-break space": " ", "ideographic space": "　",
    "line separator": " ", "paragraph separator": " ", "dot": ".", "hyphen": "-", "slash": "/", "colon": ":", "underscore": "_", "round brackets": ")(",
    "hyphen U+2010": "‐", "en dash": "–", "horizontal bar": "―", "two-em dash": "⸺", "Armenian hyphen": "֊", "minus sign": "−",
    "square brackets": "][", "curly bracket": "{", "corner bracket": "「", "soft hyphen": "­", "zero-width space": "​", "zero-width joiner": "‍", "word joiner": "⁠",
    "byte order mark": "﻿", "combining acute": "́", "combining grapheme joiner": "͏", "middle dot": "·", "katakana middle dot": "・",
    "full-width dot": "．", "full-width slash": "／", "full-width colon": "：", "full-width low line": "＿",
}
# What the guard does not join: a comma ends a run ("20,000,000" is groups of three), and so do a plus sign, a letter, a multiplication sign and a double vertical line.
ENDS_A_RUN = {"comma": ",", "plus": "+", "letter": "a", "multiplication sign": "×", "double vertical line": "‖", "percent": "%"}
DIGITS = {"ASCII": "12345", "Arabic-Indic": "١٢٣٤٥", "full-width": "１２３４５", "Devanagari": "१२३४५"}


@pytest.mark.parametrize("name", sorted(SEPARATORS))
def test_a_separator_of_the_core_joins_the_digits_either_side_of_it(name: str) -> None:
    assert longest_digit_run(f"1234{SEPARATORS[name]}5678") == 8
    assert is_digit_separator(SEPARATORS[name][0])


@pytest.mark.parametrize("name", sorted(ENDS_A_RUN))
def test_a_character_the_core_does_not_join_ends_a_run(name: str) -> None:
    assert longest_digit_run(f"1234{ENDS_A_RUN[name]}5678") == 4
    assert not is_digit_separator(ENDS_A_RUN[name])


@pytest.mark.parametrize("name", sorted(DIGITS))
def test_digits_of_every_script_count(name: str) -> None:
    assert longest_digit_run(DIGITS[name]) == 5
    assert longest_digit_run(DIGITS[name][:2] + " " + DIGITS[name][2:]) == 5


def test_several_separators_in_a_row_are_one_join() -> None:
    assert longest_digit_run("1 - 2 / 3 . 4 _ 5") == 5


def test_a_run_is_counted_in_digits_and_the_longest_wins() -> None:
    assert longest_digit_run("") == 0
    assert longest_digit_run("no digits here") == 0
    assert longest_digit_run("12 and 123 and 1 2") == 3
    assert longest_digit_run("1234 56") == 6


def test_the_spans_are_the_digit_to_digit_extent_of_each_run() -> None:
    text = "card 4111 1111 x 22"
    assert [(text[a:b], n) for a, b, n in digit_runs(text)] == [("4111 1111", 8), ("22", 2)]
    assert digit_runs("no digits") == []
    lone = " -1- "
    assert [(lone[a:b], n) for a, b, n in digit_runs(lone)] == [("1", 1)]  # separators before the first digit and after the last belong to no run


def test_the_assembler_uses_this_definition_and_no_other() -> None:
    assert getattr(assemble, "longest_digit_run") is longest_digit_run  # noqa: B009 - the name is imported, not re-exported
