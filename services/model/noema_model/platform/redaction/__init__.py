"""The mandatory redactor: masks account data to the last four digits before any log line or provider call.

Implements: REQ-SEC-003, REQ-OBS-002.

May import: the standard library.

Must never: pass a secret, a full account number or raw audio through unmasked (AGENT-13).

A digit run (single spaces or hyphens between digits count as part of the run) of `min_digits` or more is an
account-number candidate and is masked to its last four digits. A comma ends a run, so an amount written
"20,000,000" is not masked. A shorter run (an amount, a phone-number fragment) is left alone, because masking an
amount would destroy the slot the customer is asking about. `min_digits` comes from configuration (AGENT-16): a
change moves what reaches a provider, so it goes through a decision record.
"""

import re
from collections.abc import Mapping, Sequence
from typing import Any

_RUN = re.compile(r"\d(?:[ \-]?\d)*")
_SECRET_KEY = re.compile(r"(api[_-]?key|authorization|auth|token|secret|password|passwd|credential|bearer)", re.I)
REDACTED = "[REDACTED]"
KEPT_DIGITS = 4


class Redactor:
    def __init__(self, min_digits: int) -> None:
        if min_digits <= KEPT_DIGITS:
            raise ValueError("min_digits must exceed the number of digits kept")
        self._min_digits = min_digits

    def mask_account_numbers(self, text: str) -> str:
        """Replace every account-number candidate with asterisks plus its last four digits."""

        def mask(match: re.Match[str]) -> str:
            digits = re.sub(r"\D", "", match.group(0))
            if len(digits) < self._min_digits:
                return match.group(0)
            return "*" * (len(digits) - KEPT_DIGITS) + digits[-KEPT_DIGITS:]

        return _RUN.sub(mask, text)

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
