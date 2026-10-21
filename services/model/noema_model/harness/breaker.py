"""A circuit breaker per provider, driven by the injected clock (REQ-INT-002; PERD §19.1).

Closed until `failure_threshold` consecutive failures, then open for `cooldown_ms`, then half-open: exactly one probe
call is let through; its success closes the breaker and its failure re-opens it. Both numbers are configuration.
"""

from dataclasses import dataclass
from enum import StrEnum


class BreakerState(StrEnum):
    CLOSED = "CLOSED"
    OPEN = "OPEN"
    HALF_OPEN = "HALF_OPEN"


@dataclass
class Breaker:
    failure_threshold: int
    cooldown_ms: int
    state: BreakerState = BreakerState.CLOSED
    failures: int = 0
    opened_at_ms: int = 0
    probe_in_flight: bool = False

    def allow(self, now_ms: int) -> bool:
        if self.state is BreakerState.CLOSED:
            return True
        if self.state is BreakerState.OPEN:
            if now_ms - self.opened_at_ms < self.cooldown_ms:
                return False
            self.state = BreakerState.HALF_OPEN
            self.probe_in_flight = True
            return True
        if self.probe_in_flight:
            return False
        self.probe_in_flight = True
        return True

    def record_success(self) -> None:
        self.state = BreakerState.CLOSED
        self.failures = 0
        self.probe_in_flight = False

    def record_failure(self, now_ms: int) -> None:
        if self.state is BreakerState.HALF_OPEN:
            self._open(now_ms)
            return
        self.failures += 1
        if self.failures >= self.failure_threshold:
            self._open(now_ms)

    def release_probe(self) -> None:
        """The caller was cancelled mid-probe: the provider was not judged, so the next caller may probe."""
        self.probe_in_flight = False

    def _open(self, now_ms: int) -> None:
        self.state = BreakerState.OPEN
        self.opened_at_ms = now_ms
        self.probe_in_flight = False
