"""The mandatory redactor: masks account data to the last four digits before any log line or provider call.

Implements: REQ-SEC-003, REQ-OBS-002.

May import: the standard library.

Must never: pass a secret, a full account number or raw audio through unmasked (AGENT-13).

A run of digits is what `digit_runs` finds: the model service has one definition of it (digits of every script, joined by any number of separators, as the Record's guard in the
core reads them), and this module masks exactly those runs. A run of `min_digits` or more is an account-number candidate and is masked to its last four digits. A comma, a plus sign or
a letter ends a run, so an amount written "20,000,000" is not masked. A shorter run (an amount, a phone-number fragment) is left alone, because masking an
amount would destroy the slot the customer is asking about. `min_digits` comes from configuration (AGENT-16): a
change moves what reaches a provider, so it goes through a decision record.
"""

import re
import unicodedata
from collections.abc import Mapping, Sequence
from typing import Any

from noema_model.platform.masking import MaskingLimits
from noema_model.platform.redaction.digit_runs import digit_runs

_SECRET_KEY = re.compile(r"(api[_-]?key|authorization|auth|token|secret|password|passwd|credential|bearer)", re.I)
REDACTED = "[REDACTED]"
KEPT_DIGITS = 4


class Redactor:
    def __init__(self, min_digits: int) -> None:
        if min_digits <= KEPT_DIGITS:
            raise ValueError("min_digits must exceed the number of digits kept")
        self._min_digits = min_digits

    @classmethod
    def from_limits(cls, limits: MaskingLimits) -> "Redactor":
        """A redactor that masks the first run longer than the pack's masking-limits.v1 value (the one source the core reads too)."""
        return cls(limits.min_masked_run)

    def mask_account_numbers(self, text: str) -> str:
        """Replace every account-number candidate with asterisks plus its last four digits."""

        parts: list[str] = []
        position = 0
        for start, end, count in digit_runs(text):
            if count < self._min_digits:
                continue
            digits = [char for char in text[start:end] if unicodedata.category(char) == "Nd"]
            parts.append(text[position:start])
            parts.append("*" * (count - KEPT_DIGITS) + "".join(digits[-KEPT_DIGITS:]))
            position = end
        parts.append(text[position:])
        return "".join(parts)

    def redact(self, value: Any) -> Any:
        """Redact a JSON-like value for a log line: secrets by key name, account numbers by pattern."""
        if isinstance(value, str):
            return self.mask_account_numbers(value)
        if isinstance(value, Mapping):
            return {
                str(key): REDACTED if _SECRET_KEY.search(str(key)) else self.redact(item)
                for key, item in value.items()
            }
        if isinstance(value, Sequence) and not isinstance(value, (bytes, bytearray)):
            return [self.redact(item) for item in value]
        return value
