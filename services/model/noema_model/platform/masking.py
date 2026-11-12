"""The longest digit run that may appear unmasked, read from the pack's masking-limits.v1 document (SEC-03, REQ-SEC-003, AGENT-13, AGENT-16).

Implements: REQ-SEC-003, REQ-OBS-002.

May import: the standard library.

Must never: hold a number of its own, or return a value for a document that is not exactly the contract.

The other service reads the same file, so both mask at the same length (ruling A154 as corrected, relayed, not yet committed). There is
no default. The value is the longest run left alone; a longer run is masked to its last four digits.
"""

import json
import re
from dataclasses import dataclass
from pathlib import Path

KEPT_DIGITS = 4  # how many digits a mask keeps
CONTRACT_VALUE = 4  # the one value masking-limits.v1 admits (its const; A154 as corrected caps every outward surface at four)
_PACK_ID = re.compile(r"\A[a-z][a-z0-9]*\Z")


class MaskingLimitsError(ValueError):
    """The document is not exactly masking-limits.v1. The message never carries the document."""


def _whole_number(value: object) -> bool:
    return type(value) is int and value == CONTRACT_VALUE


@dataclass(frozen=True, slots=True)
class MaskingLimits:
    max_unmasked_run: int

    def __post_init__(self) -> None:
        if not _whole_number(self.max_unmasked_run):
            raise MaskingLimitsError("max_unmasked_digit_run is not the contract's value")

    @property
    def min_masked_run(self) -> int:
        return self.max_unmasked_run + 1

    @property
    def kept_digits(self) -> int:
        return KEPT_DIGITS


_FIELDS = frozenset({"schema_version", "pack_id", "label", "max_unmasked_digit_run"})


def load_limits(text: str) -> MaskingLimits:
    """Read one masking-limits.v1 document; anything else is a MaskingLimitsError."""
    try:
        doc = json.loads(text)
    except ValueError as exc:  # json.JSONDecodeError, including trailing data after one document
        raise MaskingLimitsError("cannot read the document") from exc
    if not isinstance(doc, dict) or set(doc) != _FIELDS:
        raise MaskingLimitsError("the document does not have exactly the contract's fields")
    if doc["schema_version"] != "1.0":
        raise MaskingLimitsError("schema_version is not 1.0")
    if not isinstance(doc["pack_id"], str) or not _PACK_ID.match(doc["pack_id"]):
        raise MaskingLimitsError("pack_id does not match the contract")
    if doc["label"] not in ("SYNTHETIC", "VALIDATED"):
        raise MaskingLimitsError("label is not allowed")
    return MaskingLimits(doc["max_unmasked_digit_run"])


def load_limits_file(path: Path) -> MaskingLimits:
    """Read the document at path (the pack's masking_limits.json)."""
    try:
        text = path.read_text(encoding="utf-8")
    except OSError as exc:
        raise MaskingLimitsError("cannot read the file") from exc
    return load_limits(text)
