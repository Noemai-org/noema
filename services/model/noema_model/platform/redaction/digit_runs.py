"""The one definition of a run of digits in the model service (REQ-SEC-003, REQ-INT-002; INV-02; AGENT-13; rulings A194 and A231, interim).

Implements: REQ-SEC-003.

May import: the standard library.

Must never: hold a number of its own (the longest run a string may hold is the pack's masking limit, `noema_model.platform.masking`), read a clock or a file, or log a string it was given.

A run is digits (any Unicode decimal digit, category Nd, so every script) joined by any number of separators. The separators are those of the Record's guard, `separators` of the
Record guard package (noema#536; noema#600 at its head `75beb587`): every kind of space (Z, the vertical tab and the next-line character, and whitespace as Python reads it), every
dash (Pd) and bracket (Ps, Pe), every format character (Cf: soft hyphen, zero-width characters, joiners) and combining mark (M), the dot, hyphen, slash, colon and underscore, the minus sign U+2212, the middle dot
U+00B7, the katakana middle dot U+30FB and the full-width dot, slash, colon and low line. It also takes what the guard on `main` and the contracts' public-API pattern take (the dashes U+2010 to U+2015, the
round brackets), so it is never looser than any of them: a string the Record would refuse is a string this module counts as long. A comma, a plus sign, a letter, a multiplication sign and a double vertical line end a run
(`20,000,000` is groups of three).

Python's `isspace` also takes U+001C to U+001F; since A308 the guard's class has those four too (the file, group, record and unit separators), so the two agree, and the shared vectors of
`contracts/fixtures/digit_run_vectors.json` hold one vector for each. If the guard changes its `separators`, this set follows it on the same day.
"""

import unicodedata

_EXTRA_SEPARATORS = frozenset(".-/_:−·・．／：＿\x0b\x85") | {chr(code) for code in range(0x2010, 0x2016)}
_SEPARATOR_CATEGORIES = frozenset({"Zs", "Zl", "Zp", "Cf", "Mn", "Mc", "Me", "Pd", "Ps", "Pe"})


def is_digit_separator(char: str) -> bool:
    """True for a character that joins the digits either side of it into one run."""
    return char.isspace() or char in _EXTRA_SEPARATORS or unicodedata.category(char) in _SEPARATOR_CATEGORIES


def digit_runs(text: str) -> list[tuple[int, int, int]]:
    """Every run of `text` as `(start, end, digits)`: the half-open span from its first digit to its last digit, and the number of digits in it.
    Separators before the first digit or after the last digit of a run belong to no run."""
    runs: list[tuple[int, int, int]] = []
    start = last = -1
    count = 0
    for index, char in enumerate(text):
        if unicodedata.category(char) == "Nd":
            if count == 0:
                start = index
            last = index
            count += 1
        elif count and not is_digit_separator(char):
            runs.append((start, last + 1, count))
            count = 0
    if count:
        runs.append((start, last + 1, count))
    return runs


def longest_digit_run(text: str) -> int:
    """The most digits any one run of `text` holds (0 when there is none)."""
    return max((digits for _, _, digits in digit_runs(text)), default=0)
