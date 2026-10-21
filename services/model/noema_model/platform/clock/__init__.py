"""The one place the model service reads time (INV-21; the py-clock lint exempts only this module).

Implements: REQ-NFR-001 (replay), REQ-OBS-002.

May import: the standard library.

Must never: be bypassed. Anything that needs the wall clock or a duration takes a Clock, so a test can drive it.
"""

import time
from datetime import UTC, datetime
from typing import Protocol


class Clock(Protocol):
    def now_utc(self) -> datetime:
        """The current instant, timezone-aware, in UTC."""
        ...

    def monotonic_ms(self) -> int:
        """A monotonic counter in whole milliseconds, for durations and timeouts only."""
        ...


class SystemClock:
    def now_utc(self) -> datetime:
        return datetime.now(UTC)

    def monotonic_ms(self) -> int:
        return time.monotonic_ns() // 1_000_000
