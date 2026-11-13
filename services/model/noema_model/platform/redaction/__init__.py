"""The mandatory redactor: masks account data to the last four digits before any log line or provider call.

Implements: REQ-SEC-003, REQ-OBS-002.

May import: the standard library.

Must never: pass a secret, a full account number or raw audio through unmasked (AGENT-13).

A digit run (one separator between digits counts as part of the run: an ASCII space or hyphen, any Unicode space or dash such as a
non-breaking hyphen, an en dash, a no-break or thin space, a full-width hyphen; digits of every script) of `min_digits` or more is an
account-number candidate and is masked to its last four digits. A comma ends a run, so an amount written
"20,000,000" is not masked. A shorter run (an amount, a phone-number fragment) is left alone, because masking an
amount would destroy the slot the customer is asking about. `min_digits` comes from configuration (AGENT-16): a
change moves what reaches a provider, so it goes through a decision record.
"""

import re
from collections.abc import Mapping, Sequence
from typing import Any

from noema_model.platform.masking import MaskingLimits

# One separator between two digits: ASCII space and hyphen, the Unicode spaces (no-break, en quad to hair, narrow no-break, medium mathematical, ideographic, ogham) and the
# Unicode dashes (hyphen to horizontal bar, minus sign, small and full-width hyphen-minus). A comma, a newline or a tab is none of them and ends a run.
_SEPARATOR = " \u00a0\u1680\u2000-\u200a\u202f\u205f\u3000\\-\u2010-\u2015\u2212\ufe58\ufe63\uff0d"
_RUN = re.compile(rf"\d(?:[{_SEPARATOR}]?\d)*")
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
