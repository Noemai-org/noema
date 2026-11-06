"""Voice latency by stage (VOI-16, REQ-VOI-001, REQ-NFR-003, REQ-VOI-005).

Time comes only from an injected clock, never from the wall clock (INV-21). Incomplete sessions count as the worst case, so
a run cannot look faster by dropping its failures.
"""

import math
from collections.abc import Sequence

import pytest

from noema_model.voice.latency import (
    Stage,
    StageTimeline,
    nearest_rank_percentile,
    summarise,
)


class Clock:
    def __init__(self) -> None:
        self.ms = 0

    def __call__(self) -> int:
        return self.ms


def timeline(clock: Clock, offsets: dict[Stage, int]) -> StageTimeline:
    line = StageTimeline(clock)
    base = clock.ms
    for stage in Stage:
        if stage in offsets:
            clock.ms = base + offsets[stage]
            line.mark(stage)
    return line


FULL = {Stage.END_OF_SPEECH: 0, Stage.FINAL_TRANSCRIPT: 600, Stage.INTENT_ADMITTED: 1100, Stage.ANSWER_TEXT: 1500,
        Stage.FIRST_AUDIO: 1800}


def test_stage_durations_are_the_gaps_between_consecutive_stages() -> None:
    line = timeline(Clock(), FULL)
    assert line.durations() == {Stage.FINAL_TRANSCRIPT: 600, Stage.INTENT_ADMITTED: 500, Stage.ANSWER_TEXT: 400,
                                Stage.FIRST_AUDIO: 300}
    assert line.total_ms() == 1800
    assert line.complete


def test_a_stage_marked_twice_is_refused() -> None:
    clock = Clock()
    line = StageTimeline(clock)
    line.mark(Stage.END_OF_SPEECH)
    with pytest.raises(ValueError):
        line.mark(Stage.END_OF_SPEECH)


def test_a_stage_out_of_order_is_refused() -> None:
    line = StageTimeline(Clock())
    line.mark(Stage.END_OF_SPEECH)
    with pytest.raises(ValueError):
        line.mark(Stage.INTENT_ADMITTED)  # the final transcript has not been marked


def test_a_clock_that_goes_backwards_is_refused() -> None:
    clock = Clock()
    clock.ms = 100
    line = StageTimeline(clock)
    line.mark(Stage.END_OF_SPEECH)
    clock.ms = 50
    with pytest.raises(ValueError):
        line.mark(Stage.FINAL_TRANSCRIPT)


def test_a_session_that_stopped_early_is_incomplete_and_has_no_total() -> None:
    line = timeline(Clock(), {Stage.END_OF_SPEECH: 0, Stage.FINAL_TRANSCRIPT: 700})
    assert not line.complete
    assert line.total_ms() is None


def test_nearest_rank_percentile_on_known_values() -> None:
    values = [float(v) for v in range(1, 21)]  # 1..20
    assert nearest_rank_percentile(values, 0.95) == 19.0
    assert nearest_rank_percentile(values, 0.5) == 10.0
    assert nearest_rank_percentile([5.0], 0.95) == 5.0
    assert nearest_rank_percentile(values, 1.0) == 20.0


@pytest.mark.parametrize("fraction", [0.0, -0.1, 1.1, math.nan])
def test_a_percentile_fraction_outside_zero_to_one_is_refused(fraction: float) -> None:
    with pytest.raises(ValueError):
        nearest_rank_percentile([1.0, 2.0], fraction)


def test_a_percentile_of_nothing_is_refused() -> None:
    with pytest.raises(ValueError):
        nearest_rank_percentile([], 0.95)


def sessions(totals: Sequence[int | None]) -> list[StageTimeline]:
    out = []
    for total in totals:
        clock = Clock()
        offsets = {Stage.END_OF_SPEECH: 0}
        if total is not None:
            offsets = {stage: total * i // 4 for i, stage in enumerate(Stage)}
        out.append(timeline(clock, offsets))
    return out


def test_the_summary_meets_the_target_only_when_p95_is_within_it() -> None:
    good = summarise(sessions([1000] * 20), target_ms=1800)
    assert good.n_total == 20 and good.n_complete == 20
    assert good.p95_ms == 1000.0 and good.meets_target is True


def test_a_p95_over_the_target_does_not_meet_it_and_the_slow_sessions_are_listed() -> None:
    summary = summarise(sessions([1000] * 17 + [2500, 2600, 2700]), target_ms=1800)
    assert summary.p95_ms == 2600.0
    assert summary.meets_target is False
    assert [m.total_ms for m in summary.misses] == [2500, 2600, 2700]


def test_incomplete_sessions_count_as_the_worst_case() -> None:
    summary = summarise(sessions([1000] * 17 + [None, None, None]), target_ms=1800)
    assert summary.n_total == 20 and summary.n_complete == 17
    assert summary.p95_ms == math.inf
    assert summary.meets_target is False


def test_one_incomplete_session_in_twenty_does_not_hide_inside_the_tail() -> None:
    summary = summarise(sessions([1000] * 19 + [None]), target_ms=1800)
    assert summary.p95_ms == 1000.0  # rank 19 of 20 is a complete session: exactly 5% may fail before P95 moves
    assert summary.n_incomplete == 1


def test_each_miss_names_its_slowest_stage() -> None:
    clock = Clock()
    slow_transcript = timeline(clock, {Stage.END_OF_SPEECH: 0, Stage.FINAL_TRANSCRIPT: 2000, Stage.INTENT_ADMITTED: 2100,
                                       Stage.ANSWER_TEXT: 2200, Stage.FIRST_AUDIO: 2300})
    summary = summarise([slow_transcript], target_ms=1800)
    assert summary.misses[0].slowest_stage is Stage.FINAL_TRANSCRIPT


def test_the_per_stage_p95_is_reported_for_attribution() -> None:
    summary = summarise(sessions([800] * 20), target_ms=1800)
    assert set(summary.stage_p95_ms) == {Stage.FINAL_TRANSCRIPT, Stage.INTENT_ADMITTED, Stage.ANSWER_TEXT, Stage.FIRST_AUDIO}
    assert summary.stage_p95_ms[Stage.FINAL_TRANSCRIPT] == 200.0


def test_a_summary_of_no_sessions_is_refused_and_the_target_must_be_positive() -> None:
    with pytest.raises(ValueError):
        summarise([], target_ms=1800)
    with pytest.raises(ValueError):
        summarise(sessions([1000]), target_ms=0)
