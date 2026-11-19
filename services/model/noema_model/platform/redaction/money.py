"""The money exemption of the redactor (A279, A270; shared vectors of A259, `contracts/fixtures/digit_run_vectors.json`).

Implements: REQ-SEC-003.

May import: the standard library.

Must never: read a clock or a file, hold the masking limit (`noema_model.platform.masking` does), or log a string it was given.

The core renders money only from integer minor units. A string is exempt from the digit-run rule only when the WHOLE string is an amount: three capital ASCII letters, one space, an
integer part of one to three digits and then groups of three digits each after a comma, and exactly two decimals or none, with at most nine digits before the decimal point. Ungrouped
digits, more than nine integer digits, another prefix and any other number of decimals are not exempt. The pattern is the Record guard's (`payloadguard.moneyShape` in the core, the
source of truth) and the UI scan's; the shared vector file is what keeps the three together. A comma ends a run, so a grouped amount with no decimals holds no run anyway; the
exemption matters for the dot, which joins the last group to its decimals ("12,345.00" holds the run "345.00" of five digits).
"""

import re

_MONEY = re.compile(r"[A-Z]{3} [0-9]{1,3}(?:,[0-9]{3})*(?:\.[0-9]{2})?")
MAX_MONEY_INTEGER_DIGITS = 9
_PREFIX_LENGTH = len("NGN ")


def is_money(text: str) -> bool:
    """True when the whole of `text` is a money string. `fullmatch` has no end-of-line leniency: "NGN 12,345.00\\n" is not one."""
    if _MONEY.fullmatch(text) is None:
        return False
    integer_part = text[_PREFIX_LENGTH:].partition(".")[0]
    return len(integer_part.replace(",", "")) <= MAX_MONEY_INTEGER_DIGITS
