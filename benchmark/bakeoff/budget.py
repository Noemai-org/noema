"""The budget guard of the model-selection freeze (benchmark/decisions/2026-10-27-model-selection.md): $25 in total across both
models, $2 for the pilot, retries and repeated runs included. The cap is never raised by an agent.

Before each request the guard is asked `authorize(projected)`. If `spent + projected` would pass the limit it raises, the run is
marked stopped, and the request is not sent. Once stopped, nothing more is authorised: a cheaper request does not slip through.
The projection is the worst case for the request (`projected_micros`): every input byte counted as a token, plus the whole
`max_output_tokens` at the output rate. A request whose cost was not reported is settled at its projection.
"""
from dataclasses import dataclass

MICRO = 1_000_000


class BudgetExceeded(Exception):
    """The request would pass the remaining budget; it was not sent."""


def projected_micros(input_bytes: int, max_output_tokens: int, input_micro_per_mtok: int, output_micro_per_mtok: int) -> int:
    """Worst-case cost of one request in micro-dollars, rounded up. A token is never smaller than a byte of the prompt."""
    for value in (input_bytes, max_output_tokens, input_micro_per_mtok, output_micro_per_mtok):
        if isinstance(value, bool) or not isinstance(value, int):
            raise TypeError("projected_micros takes integers")
        if value < 0:
            raise ValueError("projected_micros takes non-negative integers")
    return -(-(input_bytes * input_micro_per_mtok + max_output_tokens * output_micro_per_mtok) // MICRO)


@dataclass
class BudgetGuard:
    limit_micros: int
    spent_micros: int = 0  # spending already made against the same cap, by earlier runs of the phase
    stopped: bool = False

    def __post_init__(self) -> None:
        if self.limit_micros < 0 or self.spent_micros < 0:
            raise ValueError("the budget and the prior spending are non-negative")

    def authorize(self, projected: int) -> None:
        if self.stopped or self.spent_micros + projected > self.limit_micros:
            self.stopped = True
            raise BudgetExceeded(f"spent {self.spent_micros} + projected {projected} would pass the limit {self.limit_micros}")

    def settle(self, actual_micros: int | None, projected_micros: int) -> None:
        self.spent_micros += projected_micros if actual_micros is None else actual_micros
