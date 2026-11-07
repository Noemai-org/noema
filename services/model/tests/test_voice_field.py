"""Field-gate analysis tests (VOI-14, REQ-VOI-005, PERD §45.4, SoT §37.5).

The gate numbers (100 sessions, 50% Pidgin, P95 2.5 s) are arguments taken from PERD §45.4 by the caller; none is written in
the module. These tests use synthetic sessions only.
"""

import math

import pytest

from noema_model.voice.field import FieldAnalysis, FieldSession, GateVerdict, analyse

TARGET_MS = 2500
MIN_SESSIONS = 100
MIN_PIDGIN = 0.5
FLOOR = 0.9


def session(i: int, elapsed: int | None = 1500, locale: str = "pcm", correct: bool | None = True,
            consent: str | None = "c-1") -> FieldSession:
    eos = 10_000 * i
    return FieldSession(f"s{i}", locale, eos if elapsed is not None else None,
                        eos + elapsed if elapsed is not None else None, correct, consent)


def run_gate(sessions: list[FieldSession], **over: float) -> FieldAnalysis:
    args = dict(target_ms=TARGET_MS, min_sessions=MIN_SESSIONS, min_pidgin_share=MIN_PIDGIN, correctness_floor=FLOOR)
    args.update(over)
    return analyse(sessions, **args)  # type: ignore[arg-type]


def good(n: int = 100) -> list[FieldSession]:
    return [session(i, 1000 + i, "pcm" if i % 2 == 0 else "yo") for i in range(n)]


def test_a_run_that_meets_every_condition_passes() -> None:
    result = run_gate(good())
    assert result.verdict is GateVerdict.PASS and result.reasons == ()
    assert result.n == 100 and result.n_failed == 0 and result.pidgin_share == 0.5


def test_too_few_sessions_cannot_pass_however_fast() -> None:
    result = run_gate(good(99))
    assert result.verdict is GateVerdict.FAIL and "too few sessions" in result.reasons


def test_too_little_pidgin_fails() -> None:
    sessions = [session(i, 1200, "pcm" if i < 49 else "yo") for i in range(100)]
    result = run_gate(sessions)
    assert result.verdict is GateVerdict.FAIL and "pidgin share below the minimum" in result.reasons


def test_a_slow_p95_fails_and_names_the_p95() -> None:
    sessions = [session(i, 1000, "pcm") for i in range(94)] + [session(100 + i, 3000, "pcm") for i in range(6)]
    result = run_gate(sessions)
    assert result.p95_ms == 3000.0
    assert result.verdict is GateVerdict.FAIL and "p95 over the target" in result.reasons


def test_five_slow_sessions_in_a_hundred_leave_p95_on_a_fast_one() -> None:
    sessions = [session(i, 1000, "pcm") for i in range(95)] + [session(100 + i, 3000, "pcm") for i in range(5)]
    assert run_gate(sessions).p95_ms == 1000.0


def test_a_session_with_a_missing_server_timestamp_is_a_failure_in_the_denominator() -> None:
    sessions = [session(i, 1000, "pcm") for i in range(94)] + [session(100 + i, None, "pcm") for i in range(6)]
    result = run_gate(sessions)
    assert result.n == 100 and result.n_failed == 6
    assert result.p95_ms == math.inf and result.verdict is GateVerdict.FAIL


def test_correctness_is_against_the_annotation_and_an_unannotated_session_is_incorrect() -> None:
    sessions = [session(i, 1000, "pcm", True) for i in range(80)] + [session(100 + i, 1000, "pcm", None) for i in range(20)]
    result = run_gate(sessions)
    assert result.correct_rate == 0.8
    assert result.verdict is GateVerdict.FAIL and "correctness below the floor" in result.reasons


def test_every_failing_reason_is_listed_not_only_the_first() -> None:
    result = run_gate([session(i, 5000, "yo", False) for i in range(10)])
    assert set(result.reasons) == {"too few sessions", "pidgin share below the minimum", "p95 over the target",
                                   "correctness below the floor"}


def test_a_session_with_no_consent_reference_is_refused() -> None:
    with pytest.raises(ValueError):
        run_gate([session(0, consent=None)])


def test_an_end_of_speech_after_admission_is_a_clock_error_not_a_negative_latency() -> None:
    bad = FieldSession("s", "pcm", 5000, 4000, True, "c")
    with pytest.raises(ValueError):
        run_gate([bad])


def test_a_duplicate_session_reference_is_refused() -> None:
    with pytest.raises(ValueError):
        run_gate([session(1), session(1)])


def test_no_sessions_are_refused() -> None:
    with pytest.raises(ValueError):
        run_gate([])


@pytest.mark.parametrize("over", [{"target_ms": 0}, {"min_sessions": 0}, {"min_pidgin_share": 1.5}, {"correctness_floor": -0.1},
                                  {"min_pidgin_share": math.nan}])
def test_a_bad_gate_parameter_is_refused(over: dict[str, float]) -> None:
    with pytest.raises(ValueError):
        run_gate(good(), **over)


def test_the_confidence_interval_brackets_the_p95_and_is_open_when_the_sample_is_small() -> None:
    result = run_gate([session(i, 1000 + i * 10, "pcm") for i in range(100)])
    assert result.ci_low_ms <= result.p95_ms <= result.ci_high_ms
    assert 1850.0 <= result.ci_low_ms <= 1950.0  # ranks near 91 of 100
    small = run_gate(good(10))
    assert small.ci_high_ms == math.inf  # ten sessions cannot bound a 95th percentile from above


def test_the_report_states_n_method_and_interval_so_it_can_be_published() -> None:
    result = run_gate(good())
    assert result.method == "nearest-rank P95; distribution-free 95% interval from order statistics"
    assert result.confidence == 0.95
