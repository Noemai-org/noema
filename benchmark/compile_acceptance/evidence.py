"""UND-17 (A602, interim; noema#338, compile run 7): the line the job log, the summary and the console print for the closed-vocabulary choice of one slot of one example: the option the model chose, the top five
probabilities (the abstention among them) and the threshold applied. Every run can then be read from the job log without the artifact host.

Option ids and numbers only (AGENT-13). An id that is not in the shape of a pack id is never printed (it is named `invalid_id`), so a word of the request cannot ride in an option name. A probability is clamped to
[0, 1] and printed as a decimal of four places (`0.7800`), never as a count of basis points: the leak scan reads a digit run of five or more as a number worth hiding, and 10000 would be one. SYNTHETIC."""
import re
from collections.abc import Mapping
from fractions import Fraction

ABSTAIN = "none_or_other"
TOP = 5
_ID = re.compile(r"[a-z][a-z0-9_.]{0,63}")
_CASE = re.compile(r"[A-Z][A-Z0-9-]{0,31}")  # G-A, BEN-CLEAN-0001: the examples' own ids
_DIGITS = re.compile(r"\d(?:[ -]?\d){4}")  # the leak scan's own reading of a number worth hiding: five digits, even across dashes
INVALID = "invalid_id"


def _id(value: str) -> str:
    return value if _ID.fullmatch(value) else INVALID


def _p(value: float) -> str:
    return f"{min(max(float(value), 0.0), 1.0):.4f}"


def format_choice(case_id: str, slot: str, chosen: str | None, probabilities: Mapping[str, float], threshold: Fraction | None) -> str:
    """`CHOICE <case> slot=<slot> chosen=<option> top5=<option>:<p>,... threshold=<p or n/a>`. `chosen` None is the abstention."""
    shown_case = case_id if _CASE.fullmatch(case_id) and _DIGITS.search(case_id) is None else INVALID
    top = sorted(probabilities.items(), key=lambda item: (-min(max(float(item[1]), 0.0), 1.0), item[0]))[:TOP]
    cells = ",".join(f"{_id(option)}:{_p(p)}" for option, p in top) or "none"  # a question that offered no option prints the fixed word
    limit = "n/a" if threshold is None else _p(float(threshold))
    return f"CHOICE {shown_case} slot={_id(slot)} chosen={_id(ABSTAIN if chosen is None else chosen)} top5={cells} threshold={limit}"
