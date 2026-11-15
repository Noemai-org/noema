"""Voice latency by stage, from end of speech to the first spoken word (VOI-16, REQ-VOI-001, REQ-NFR-003, REQ-VOI-005).

Time comes only from an injected clock (INV-21), so a measurement run repeats and a test never sleeps. The target is an
argument (AGENT-16).

A session that stopped before the last stage is incomplete, and an incomplete session counts as the worst case in the
percentile. A run therefore cannot meet its target by losing its failures: with 20 sessions one failure still leaves
rank 19 on a complete session, and a second moves the percentile to infinity.
"""

import math
from collections.abc import Callable, Sequence
from dataclasses import dataclass
from enum import IntEnum

_PERCENTILE = 0.95
_ROUNDING_DIGITS = 9


class Stage(IntEnum):
    """In the order they happen; the duration of a stage is the time since the previous one."""

    END_OF_SPEECH = 0
    FINAL_TRANSCRIPT = 1
    INTENT_ADMITTED = 2
    ANSWER_TEXT = 3
    FIRST_AUDIO = 4


class StageTimeline:
    def __init__(self, clock_ms: Callable[[], int]) -> None:
        self._clock_ms = clock_ms
        self._marks: list[int] = []

    def mark(self, stage: Stage) -> None:
        if stage != len(self._marks):
            raise ValueError("stage marked twice or out of order")
        now = self._clock_ms()
        if self._marks and now < self._marks[-1]:
            raise ValueError("the clock went backwards")
        self._marks.append(now)

    @property
    def complete(self) -> bool:
        return len(self._marks) == len(Stage)

    def durations(self) -> dict[Stage, int]:
        return {Stage(i): self._marks[i] - self._marks[i - 1] for i in range(1, len(self._marks))}

    def total_ms(self) -> int | None:
        return self._marks[-1] - self._marks[0] if self.complete else None


def nearest_rank_percentile(values: Sequence[float], fraction: float) -> float:
    if not values:
        raise ValueError("no values")
    if not 0.0 < fraction <= 1.0:
        raise ValueError("fraction must be in (0, 1]")
    ordered = sorted(values)
    rank = math.ceil(round(fraction * len(ordered), _ROUNDING_DIGITS))
    return ordered[rank - 1]


@dataclass(frozen=True, slots=True)
class Miss:
    total_ms: int | None  # None: the session never reached the last stage
    slowest_stage: Stage | None


@dataclass(frozen=True, slots=True)
class LatencySummary:
    n_total: int
    n_complete: int
    p95_ms: float
    meets_target: bool
    stage_p95_ms: dict[Stage, float]  # over complete sessions only
    misses: tuple[Miss, ...]

    @property
    def n_incomplete(self) -> int:
        return self.n_total - self.n_complete


def _slowest(line: StageTimeline) -> Stage | None:
    durations = line.durations()
    return max(durations, key=lambda stage: durations[stage]) if durations else None


def summarise(timelines: Sequence[StageTimeline], *, target_ms: int) -> LatencySummary:
    if not timelines:
        raise ValueError("no sessions")
    if target_ms < 1:
        raise ValueError("the target must be positive")
    totals = [line.total_ms() for line in timelines]
    p95 = nearest_rank_percentile([math.inf if t is None else float(t) for t in totals], _PERCENTILE)
    complete = [line for line in timelines if line.complete]
    stage_p95 = {
        stage: nearest_rank_percentile([float(line.durations()[stage]) for line in complete], _PERCENTILE)
        for stage in list(Stage)[1:]
    } if complete else {}
    misses = tuple(
        Miss(total, _slowest(line))
        for line, total in zip(timelines, totals, strict=True)
        if total is None or total > target_ms
    )
    return LatencySummary(len(timelines), len(complete), p95, p95 <= target_ms, stage_p95, misses)
