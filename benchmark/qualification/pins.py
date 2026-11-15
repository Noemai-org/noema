"""What counts as a pinned model or speech version, and how a date is written (UND-13; REQ-INT-006, AGENT-28). Shared by the qualification record and the drift run record.

The pinned rule is the model service's own (`services/model/noema_model/harness/config.py`, `check_pinned`), so a record or run can never name a version the service would refuse:
the version matches `^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`, contains a digit, and contains none of the floating words as a substring, in any case. Keep the two in step; a golden
test in the model service compares them."""
import datetime
import re
from typing import Any

KINDS = ("llm", "speech")
FLOATING_WORDS = ("latest", "default", "auto", "stable", "preview", "current", "newest")
_PIN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$")
_DATE = re.compile(r"^[0-9]{4}-[0-9]{2}-[0-9]{2}$")


def is_pinned(version: Any) -> bool:
    """A pinned version names one snapshot: not an alias, not a floating tag, and it carries a digit."""
    if not isinstance(version, str) or not _PIN.fullmatch(version) or not any(c.isdigit() for c in version):
        return False
    lowered = version.lower()
    return not any(word in lowered for word in FLOATING_WORDS)


def parse_date(value: Any) -> datetime.date:
    """`YYYY-MM-DD` and nothing else: no week dates, no compact form, no time, no unpadded fields. Raises ValueError."""
    if not isinstance(value, str) or not _DATE.fullmatch(value):
        raise ValueError(f"a date must be written YYYY-MM-DD, not {value!r}")
    return datetime.date.fromisoformat(value)
