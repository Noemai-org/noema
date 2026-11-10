"""Field-gate analysis tests (VOI-14, REQ-VOI-005, PERD §45.4, SoT §37.5).

The gate numbers are arguments taken from PERD §45.4 and the Founder's decision F5 by the caller, and `analyse` refuses
any that is weaker than those decisions (Security Reviewer note and C2 P2 on noema#230). The measuring function has no
verdict and no floors, so small samples and the interval maths can be tested on their own. All sessions are synthetic.
"""

import math
from fractions import Fraction
from typing import Any

import pytest

from noema_model.voice.field import (
    F5_MIN_CORRECTNESS,
    F5_MIN_CORRECTNESS_LOWER,
    F5_MIN_SESSIONS_PER_LOCALE,
    PERD_MAX_P95_MS,
    FIELD_MIN_SESSIONS,
    F5_MIN_LOCALE_CORRECTNESS,
    PERD_MIN_PIDGIN_SHARE,
    FieldAnalysis,
    FieldSession,
    GateVerdict,
    analyse,
    measure,
)

TARGET_MS = 2500
MIN_SESSIONS = 240  # Founder decision on noema#349, 10 Nov 2026: 120 Pidgin plus 30 in each of the other four locales
LOCALE_FLOOR = 0.80  # the same decision: each locale's own rate at least 80% (24 of 30)
MIN_PIDGIN = 0.5
FLOOR = 0.9  # F5 (Founder, 8 Nov 2026): Intent Object correctness at least 90%
LOWER_FLOOR = 0.80  # F5: the lower end of the 95% interval at least 80%
PER_LOCALE = 30  # F5: at least 30 sessions per locale
LOCALES = ("pcm", "yo")  # most tests use two locales; the real gate lists all five
FIVE = ("en-NG", "pcm", "yo", "ha", "ig")


def session(i: int, elapsed: int | None = 1500, locale: str = "pcm", correct: bool | None = True,
            consent: str | None = "c-1") -> FieldSession:
    eos = 10_000 * i
    return FieldSession(f"s{i}", locale, eos if elapsed is not None else None,
                        eos + elapsed if elapsed is not None else None, correct, consent)


def run_gate(sessions: list[FieldSession], **over: Any) -> FieldAnalysis:
    args: dict[str, Any] = dict(target_ms=TARGET_MS, min_sessions=MIN_SESSIONS, min_pidgin_share=MIN_PIDGIN, correctness_floor=FLOOR,
                                correctness_lower_floor=LOWER_FLOOR, required_locales=LOCALES, min_sessions_per_locale=PER_LOCALE,
                                per_locale_correctness_floor=LOCALE_FLOOR)
    args.update(over)
    return analyse(sessions, **args)


def good(n: int = 240) -> list[FieldSession]:
    return [session(i, 1000 + i, "pcm" if i % 2 == 0 else "yo") for i in range(n)]


def plan(counts: dict[str, int], wrong: int = 0, wrong_in: dict[str, int] | None = None) -> list[FieldSession]:
    """`wrong` marks the first sessions overall incorrect; `wrong_in` marks that many sessions of a locale incorrect."""
    sessions: list[FieldSession] = []
    left = dict(wrong_in or {})
    for locale, count in counts.items():
        for _ in range(count):
            n = len(sessions)
            miss = left.get(locale, 0) > 0
            if miss:
                left[locale] -= 1
            sessions.append(session(n, 1000 + n, locale, correct=not (n < wrong or miss)))
    return sessions


# 120 Pidgin plus 30 in each of the other four locales: the smallest plan that meets both "30 per locale" and "50% Pidgin".
SMALLEST = {"en-NG": 30, "pcm": 120, "yo": 30, "ha": 30, "ig": 30}


# ---- the conditions ------------------------------------------------------------------------------------------------------


def test_a_run_that_meets_every_condition_passes() -> None:
    result = run_gate(good())
    assert result.verdict is GateVerdict.PASS and result.reasons == ()
    assert result.n == 240 and result.n_failed == 0 and result.pidgin_share == 0.5


def test_too_few_sessions_cannot_pass_however_fast() -> None:
    result = run_gate(good(239))
    assert result.verdict is GateVerdict.FAIL and "too few sessions" in result.reasons


def test_too_little_pidgin_fails() -> None:
    sessions = [session(i, 1200, "pcm" if i < 119 else "yo") for i in range(240)]
    result = run_gate(sessions)
    assert result.verdict is GateVerdict.FAIL and "pidgin share below the minimum" in result.reasons


def test_a_slow_p95_fails_and_names_the_p95() -> None:
    sessions = [session(i, 1000, "pcm") for i in range(225)] + [session(300 + i, 3000, "pcm") for i in range(15)]
    result = run_gate(sessions)
    assert result.p95_ms == 3000.0
    assert result.verdict is GateVerdict.FAIL and "p95 over the target" in result.reasons


def test_twelve_slow_sessions_in_240_leave_p95_on_a_fast_one() -> None:
    sessions = [session(i, 1000, "pcm") for i in range(228)] + [session(300 + i, 3000, "pcm") for i in range(12)]
    assert run_gate(sessions).p95_ms == 1000.0  # rank 228 of 240


def test_a_p95_exactly_at_the_target_passes_and_one_millisecond_over_fails() -> None:
    at = run_gate([session(i, TARGET_MS, "pcm" if i % 2 == 0 else "yo") for i in range(240)])
    assert "p95 over the target" not in at.reasons and at.verdict is GateVerdict.PASS
    over = run_gate([session(i, TARGET_MS + 1, "pcm" if i % 2 == 0 else "yo") for i in range(240)])
    assert "p95 over the target" in over.reasons


def test_a_session_with_a_missing_server_timestamp_is_a_failure_in_the_denominator() -> None:
    sessions = [session(i, 1000, "pcm") for i in range(225)] + [session(300 + i, None, "pcm") for i in range(15)]
    result = run_gate(sessions)
    assert result.n == 240 and result.n_failed == 15
    assert result.p95_ms == math.inf and result.verdict is GateVerdict.FAIL


def test_correctness_is_against_the_annotation_and_an_unannotated_session_is_incorrect() -> None:
    sessions = [session(i, 1000, "pcm", True) for i in range(192)] + [session(300 + i, 1000, "pcm", None) for i in range(48)]
    result = run_gate(sessions)
    assert result.correct_rate == 0.8
    assert result.verdict is GateVerdict.FAIL and "correctness below the floor" in result.reasons


def test_exactly_ninety_percent_of_240_meets_the_floor_and_one_fewer_does_not() -> None:
    def run_of(correct: int) -> list[FieldSession]:  # the misses alternate over both locales, so no locale floor is near
        return [session(i, 1000, "pcm" if i % 2 == 0 else "yo", correct=i >= 240 - correct) for i in range(240)]

    at = run_gate(run_of(216))
    assert at.correct_rate == 0.9 and at.correct_ci_low >= LOWER_FLOOR and at.verdict is GateVerdict.PASS, at.reasons
    assert "correctness below the floor" in run_gate(run_of(215)).reasons


def test_every_failing_reason_is_listed_not_only_the_first() -> None:
    result = run_gate([session(i, 5000, "yo", False) for i in range(10)])
    assert set(result.reasons) == {"too few sessions", "pidgin share below the minimum", "p95 over the target",
                                   "correctness below the floor",
                                   "too few sessions in a required locale", "correctness interval below the floor",
                                   "correctness below the floor in a locale"}


# ---- refusals ------------------------------------------------------------------------------------------------------------


def test_a_session_with_no_consent_reference_is_refused() -> None:
    with pytest.raises(ValueError):
        run_gate([session(0, consent=None)])


@pytest.mark.parametrize("blank", ["", "   ", "\t"])
def test_an_empty_consent_or_session_reference_is_refused(blank: str) -> None:
    with pytest.raises(ValueError):
        run_gate([session(0, consent=blank)])
    with pytest.raises(ValueError):
        run_gate([FieldSession(blank, "pcm", 0, 1000, True, "c")])


def test_a_duplicate_session_reference_is_refused() -> None:
    with pytest.raises(ValueError):
        run_gate([session(1), session(1)])


def test_no_sessions_are_refused() -> None:
    with pytest.raises(ValueError):
        run_gate([])


def test_an_admission_before_end_of_speech_is_a_counted_failure_with_a_reason_not_a_crash_and_not_dropped() -> None:
    """C2 P3 on #230: a clock error raised for the whole run, and a rerun then tended to drop that session."""
    bad = FieldSession("bad", "pcm", 5000, 4000, True, "c")
    sessions = [bad] + [session(i + 1, 1000, "pcm" if i % 2 == 0 else "yo") for i in range(99)]
    result = run_gate(sessions)
    assert result.n == 100 and result.n_failed == 1 and result.n_clock_errors == 1
    assert "sessions with a clock error" in result.reasons and result.verdict is GateVerdict.FAIL


@pytest.mark.parametrize("over", [{"target_ms": 0}, {"min_sessions": 0}, {"min_pidgin_share": 1.5}, {"correctness_floor": -0.1},
                                  {"min_pidgin_share": math.nan}, {"correctness_lower_floor": -0.1},
                                  {"correctness_lower_floor": 1.1}, {"correctness_lower_floor": math.nan},
                                  {"required_locales": ()}, {"min_sessions_per_locale": 0}])
def test_a_bad_gate_parameter_is_refused(over: dict[str, Any]) -> None:
    with pytest.raises(ValueError):
        run_gate(good(), **over)


@pytest.mark.parametrize("over", [{"correctness_floor": 0.0}, {"correctness_lower_floor": 0.0}, {"min_pidgin_share": 0.0}])
def test_a_floor_of_zero_cannot_be_used_to_pass_a_run_that_is_always_wrong(over: dict[str, Any]) -> None:
    """C2 P2 on #230: floor 0.0 let 100 incorrect sessions in one locale PASS."""
    wrong = [session(i, 1000, "yo", correct=False) for i in range(240)]
    with pytest.raises(ValueError):
        run_gate(wrong, required_locales=("yo",), **over)


def test_the_decided_numbers_are_named_constants_and_the_caller_cannot_loosen_them() -> None:
    """Security Reviewer note on #230: the F5 numbers and the PERD §45.4 gate are floors, not defaults."""
    assert (F5_MIN_CORRECTNESS, F5_MIN_CORRECTNESS_LOWER, F5_MIN_SESSIONS_PER_LOCALE) == (0.90, 0.80, 30)
    assert (FIELD_MIN_SESSIONS, PERD_MIN_PIDGIN_SHARE, PERD_MAX_P95_MS) == (240, 0.5, 2500)
    assert F5_MIN_LOCALE_CORRECTNESS == 0.80
    for weaker in ({"correctness_floor": 0.89}, {"correctness_lower_floor": 0.79}, {"min_sessions_per_locale": 29},
                   {"min_sessions": 239}, {"min_pidgin_share": 0.49}, {"target_ms": 2501},
                   {"per_locale_correctness_floor": 0.79}):
        with pytest.raises(ValueError, match="weaker"):
            run_gate(good(), **weaker)
    stricter = run_gate(good(), correctness_floor=0.95, correctness_lower_floor=0.9, min_sessions_per_locale=40,
                        min_sessions=240, min_pidgin_share=0.6, target_ms=2000)
    assert stricter.verdict is GateVerdict.FAIL  # stricter arguments are accepted and applied (50% Pidgin is under 60%)


# ---- what the report says so it can be published ---------------------------------------------------------------------------


def test_the_confidence_interval_is_the_exact_order_statistic_pair() -> None:
    result = measure([session(i, 1000 + i * 10, "pcm" if i % 2 == 0 else "yo") for i in range(100)], required_locales=LOCALES)
    assert result.p95_ms == 1940.0  # rank 95
    assert (result.ci_low_ms, result.ci_high_ms) == (1890.0, 1990.0)  # ranks 90 and 100, verified with exact fractions below


def test_the_upper_bound_is_finite_from_72_sessions_and_infinite_below() -> None:
    finite = measure([session(i, 1000 + i, "pcm") for i in range(72)], required_locales=("pcm",))
    assert math.isfinite(finite.ci_high_ms)  # the exact ranks are pinned against rational arithmetic below
    assert measure([session(i, 1000 + i, "pcm") for i in range(71)], required_locales=("pcm",)).ci_high_ms == math.inf
    assert measure(good(10), required_locales=LOCALES).ci_high_ms == math.inf


def exact_cdf(k: int, n: int, p: Fraction) -> Fraction:
    from math import comb

    return sum((comb(n, i) * p**i * (1 - p) ** (n - i) for i in range(k + 1)), Fraction(0))


@pytest.mark.parametrize("n", [5, 20, 50, 72, 100, 150, 200])
def test_the_ranks_match_an_exact_rational_computation(n: int) -> None:
    p, tail = Fraction(95, 100), Fraction(25, 1000)
    low = [k for k in range(1, n + 1) if exact_cdf(k - 1, n, p) <= tail]
    high = [k for k in range(1, n + 1) if exact_cdf(k - 1, n, p) >= 1 - tail]
    result = measure([session(i, 1000 + i, "pcm") for i in range(n)], required_locales=("pcm",))
    assert result.ci_low_ms == (1000.0 + max(low) - 1 if low else 0.0)
    assert result.ci_high_ms == (1000.0 + min(high) - 1 if high else math.inf)


def test_with_one_session_there_is_no_lower_bound_to_claim() -> None:
    """C2 P3 on #230: for n = 1 the miss probability is 5%, above the 2.5% tail, so the 95% label would be wrong."""
    one = measure([session(0, 1234, "pcm")], required_locales=("pcm",))
    assert one.ci_low_ms == 0.0 and one.ci_high_ms == math.inf


def test_a_very_large_run_neither_crashes_nor_takes_long() -> None:
    """C2 P3 on #230: the binomial terms overflowed for n >= 1030."""
    big = measure([session(i, 1000 + i % 500, "pcm" if i % 2 == 0 else "yo") for i in range(2000)], required_locales=LOCALES)
    assert big.n == 2000 and big.ci_low_ms <= big.p95_ms <= big.ci_high_ms


def test_the_report_states_n_method_and_interval_so_it_can_be_published() -> None:
    result = run_gate(good())
    assert result.method == ("nearest-rank P95; distribution-free 95% interval from order statistics; "
                             "correctness with the exact (Clopper-Pearson) 95% interval")
    assert result.confidence == 0.95


def test_six_failed_sessions_in_240_fail_the_gate_because_the_upper_bound_is_open() -> None:
    """A126b and the Founder's decision on noema#349: a latency claim passes only if the upper 95% bound of the P95 is at or
    below the target. At n = 240 the bound is the 235th value, so six sessions with no server timestamp (infinite) reach it."""
    sessions = [session(i, 1000, "pcm" if i % 2 == 0 else "yo") for i in range(234)] + [
        session(300 + i, None, "pcm" if i % 2 == 0 else "yo") for i in range(6)]
    result = run_gate(sessions)
    assert result.p95_ms == 1000.0 and result.ci_high_ms == math.inf and result.n_failed == 6
    assert result.verdict is GateVerdict.FAIL and "p95 upper bound over the target" in result.reasons
    assert any("6 of 240" in note and "no usable latency" in note for note in result.notes)


def test_five_failed_sessions_in_240_still_pass_because_the_upper_bound_is_the_235th_value() -> None:
    sessions = [session(i, 1000, "pcm" if i % 2 == 0 else "yo") for i in range(235)] + [
        session(300 + i, None, "pcm" if i % 2 == 0 else "yo") for i in range(5)]
    result = run_gate(sessions)
    assert result.ci_high_ms == 1000.0 and result.verdict is GateVerdict.PASS, result.reasons


def test_the_upper_bound_at_the_target_passes_and_one_millisecond_over_fails() -> None:
    def run_with(top: int) -> list[FieldSession]:  # 234 fast values, then 6 at `top`: the 235th value, the bound, is `top`
        return [session(i, 1000 if i < 234 else top, "pcm" if i % 2 == 0 else "yo") for i in range(240)]

    at = run_gate(run_with(TARGET_MS))
    assert at.ci_high_ms == float(TARGET_MS) and at.verdict is GateVerdict.PASS, at.reasons
    over = run_gate(run_with(TARGET_MS + 1))
    assert over.p95_ms == 1000.0 and over.ci_high_ms == float(TARGET_MS + 1)
    assert over.verdict is GateVerdict.FAIL and "p95 upper bound over the target" in over.reasons


def test_a_run_too_small_to_bound_the_p95_from_above_cannot_pass() -> None:
    """71 sessions or fewer have an infinite upper bound (see the exact-rank test)."""
    result = measure(good(240)[:71], required_locales=LOCALES)
    assert result.ci_high_ms == math.inf


def test_a_point_estimate_over_the_target_is_reported_once_as_over_the_target() -> None:
    result = run_gate([session(i, 5000, "pcm" if i % 2 == 0 else "yo") for i in range(240)])
    assert "p95 over the target" in result.reasons and "p95 upper bound over the target" not in result.reasons


# ---- F5 (Founder, 8 Nov 2026): the five locales ---------------------------------------------------------------------------------


def gate5(sessions: list[FieldSession], **over: Any) -> FieldAnalysis:
    return run_gate(sessions, required_locales=FIVE, **over)


def test_the_smallest_plan_that_meets_both_rules_is_240_sessions() -> None:
    assert sum(SMALLEST.values()) == 240 and SMALLEST["pcm"] / 240 == 0.5
    result = gate5(plan(SMALLEST))
    assert result.verdict is GateVerdict.PASS, result.reasons
    assert result.n == 240 and result.correct_ci_low > 0.95
    assert dict(result.locale_counts) == {"en-NG": 30, "pcm": 120, "yo": 30, "ha": 30, "ig": 30}


def test_150_sessions_cannot_meet_both_the_locale_rule_and_the_pidgin_rule() -> None:
    result = gate5(plan({loc: 30 for loc in FIVE}))
    assert result.n == 150 and result.pidgin_share == 0.2
    assert "pidgin share below the minimum" in result.reasons and result.verdict is GateVerdict.FAIL


def test_195_sessions_do_not_meet_the_pidgin_rule_either() -> None:
    result = gate5(plan({"en-NG": 30, "pcm": 75, "yo": 30, "ha": 30, "ig": 30}))
    assert result.n == 195 and round(result.pidgin_share, 3) == 0.385
    assert "pidgin share below the minimum" in result.reasons


SPREAD_24 = {"en-NG": 3, "pcm": 12, "yo": 3, "ha": 3, "ig": 3}  # 24 misses, 10% in every locale


def test_ninety_percent_over_240_sessions_passes_because_the_lower_end_clears_eighty() -> None:
    result = gate5(plan(SMALLEST, wrong_in=SPREAD_24))  # 216 of 240 correct
    assert result.correct_rate == 0.9
    assert 0.80 <= result.correct_ci_low < 0.9
    assert result.verdict is GateVerdict.PASS, result.reasons


def test_a_rate_just_under_ninety_percent_fails_the_floor() -> None:
    result = gate5(plan(SMALLEST, wrong_in={**SPREAD_24, "pcm": 13}))  # 215 of 240
    assert "correctness below the floor" in result.reasons and result.verdict is GateVerdict.FAIL


def test_each_locale_needs_a_rate_of_at_least_80_percent_and_24_of_30_is_enough() -> None:
    """Founder decision on noema#349, 10 Nov 2026: a per-locale floor of 80%, reported per locale, on top of the pooled gate."""
    at = gate5(plan(SMALLEST, wrong_in={"yo": 6}))  # yo 24 of 30 = 0.80 exactly; pooled 234 of 240
    assert at.verdict is GateVerdict.PASS, at.reasons
    assert dict(at.locale_correct_rates)["yo"] == 0.8
    under = gate5(plan(SMALLEST, wrong_in={"yo": 7}))  # 23 of 30; pooled 233 of 240 is far above 90%
    assert under.correct_rate > 0.95 and under.correct_ci_low >= LOWER_FLOOR
    assert "correctness below the floor in a locale" in under.reasons and under.verdict is GateVerdict.FAIL
    assert round(dict(under.locale_correct_rates)["yo"], 3) == 0.767


def test_the_per_locale_rates_are_reported_in_the_order_the_caller_listed_the_locales() -> None:
    result = gate5(plan(SMALLEST, wrong_in={"ig": 3}))
    assert [name for name, _ in result.locale_correct_rates] == list(FIVE)
    assert dict(result.locale_correct_rates)["ig"] == 0.9 and dict(result.locale_correct_rates)["pcm"] == 1.0


def test_a_required_locale_with_no_sessions_has_a_rate_of_zero_and_fails_the_locale_floor() -> None:
    result = gate5(plan({**SMALLEST, "ha": 0}))
    assert dict(result.locale_correct_rates)["ha"] == 0.0
    assert "correctness below the floor in a locale" in result.reasons


def test_a_small_run_can_meet_the_rate_and_still_fail_on_the_lower_end_of_the_interval() -> None:
    sessions = [session(i, 1000, "pcm", correct=(i != 0)) for i in range(10)]  # 9 of 10 = 90%
    numbers = measure(sessions, required_locales=("pcm",))
    assert numbers.correct_rate == 0.9 and numbers.correct_ci_low < LOWER_FLOOR
    assert "correctness interval below the floor" in run_gate(sessions, required_locales=("pcm",)).reasons


def test_a_locale_with_fewer_than_thirty_sessions_fails_even_if_the_total_is_large() -> None:
    result = gate5(plan({**SMALLEST, "ig": 29}))
    assert dict(result.locale_counts)["ig"] == 29
    assert "too few sessions in a required locale" in result.reasons and result.verdict is GateVerdict.FAIL


def test_a_required_locale_with_no_session_at_all_fails_and_is_listed_with_zero() -> None:
    result = gate5(plan({**SMALLEST, "ha": 0}))
    assert dict(result.locale_counts)["ha"] == 0
    assert "too few sessions in a required locale" in result.reasons


def test_the_exact_interval_matches_closed_forms() -> None:
    all_correct = measure([session(i, 1000, "pcm") for i in range(10)], required_locales=("pcm",))
    assert all_correct.correct_ci_low == pytest.approx(0.025 ** (1 / 10), abs=1e-6)  # Clopper-Pearson, k = n
    assert all_correct.correct_ci_high == 1.0
    none_correct = measure([session(i, 1000, "pcm", correct=False) for i in range(10)], required_locales=("pcm",))
    assert none_correct.correct_ci_low == 0.0
    assert none_correct.correct_ci_high == pytest.approx(1 - 0.025 ** (1 / 10), abs=1e-6)  # k = 0
