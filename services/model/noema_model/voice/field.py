"""Analysis of the field sessions for the real-conditions voice gate (VOI-14, REQ-VOI-005; PERD §45.4, SoT §37.5).

Two functions. `measure` computes the numbers (n, the P95 and its interval, the correctness rate and its exact interval, the
Pidgin share, the sessions per locale) and has no verdict. `analyse` applies the gate. The gate's numbers are still arguments
read by the caller, but each has a floor that is a named constant citing its source, and `analyse` refuses a weaker argument:
a call site cannot loosen the gate (Security Reviewer note and C2 P2 on noema#230). The floors are the Founder's decision F5 of
8 Nov 2026 (Intent Object correctness against the script annotation at least 90%, the lower end of its 95% interval at least
80%, at least 30 sessions in each required locale) and PERD §45.4 (at least 50% in Pidgin, P95 at most 2.5 s), as amended by the Founder's decision on noema#349 of 10 Nov 2026: 240 field
sessions (120 in Pidgin plus 30 in each of the other four locales), the pooled gate (point rate at least 90% and the lower 95% bound
at least 80%), and each locale's own rate at least 80% (24 of 30), reported per locale. REQ-VOI-003's 95% for key-slot accuracy is a different measure and is not computed here. A stricter argument is
accepted and applied. A floor of zero is refused, since it would let a run that is always wrong pass.

A session counts only with both server timestamps. One missing either, or admitted before end of speech (a clock error), is a
failure that stays in the denominator as an infinite latency: a run cannot pass by dropping its failures. An unannotated
session counts as incorrect.

The report carries what a published number needs (n, the method and an interval). The latency interval is distribution-free,
from order statistics of the binomial, so it makes no assumption about the latency distribution; with a sample too small to
bound the percentile from above, the upper bound is infinite, and with one too small to bound it from below the lower bound is
0.0, rather than a guess. The correctness interval is the exact (Clopper-Pearson) binomial interval, which never under-covers.
A latency claim passes only if the upper 95% bound of the P95 is at or below the target (ruling A126b of the Architect sync of
10 Nov 2026; not yet committed in a decision record, so this PR says "do not merge before" it). A correctness claim passes only
if the lower bound of its exact interval is at or above its floor (the F5 lower floor, 80%); the rate floor (90%) also stays.
`notes` says how many sessions had no usable latency.

Records hold references and times only: no audio and no transcript (AGENT-13). A session with no consent reference is refused,
since its audio could not be stored (REQ-VOI-004).
"""

import math
from collections import Counter
from collections.abc import Sequence
from dataclasses import dataclass
from enum import StrEnum

from noema_model.providers.speech.base import Locale
from noema_model.voice.latency import nearest_rank_percentile

F5_MIN_CORRECTNESS = 0.90  # Founder decision F5, 8 Nov 2026
F5_MIN_CORRECTNESS_LOWER = 0.80  # F5: the lower end of the exact 95% interval
F5_MIN_SESSIONS_PER_LOCALE = 30  # F5
F5_MIN_LOCALE_CORRECTNESS = 0.80  # Founder decision on noema#349, 10 Nov 2026: each locale's own rate (24 of 30)
FIELD_MIN_SESSIONS = 240  # the same decision: 120 in Pidgin plus 30 in each of en-NG, Yoruba, Hausa and Igbo (PERD §45.4 said 100)
PERD_MIN_PIDGIN_SHARE = 0.5  # PERD §45.4 / REQ-VOI-005 test plan: at least 50% in Pidgin
PERD_MAX_P95_MS = 2500  # PERD §45.4 / REQ-VOI-005: P95 at most 2.5 s

_PERCENTILE = 0.95
_CONFIDENCE = 0.95
_PIDGIN_LOCALE = Locale.PCM.value
_BISECTION_STEPS = 100
_METHOD = (
    "nearest-rank P95; distribution-free 95% interval from order statistics; "
    "correctness with the exact (Clopper-Pearson) 95% interval"
)


class GateVerdict(StrEnum):
    PASS = "PASS"
    FAIL = "FAIL"


@dataclass(frozen=True, slots=True)
class FieldSession:
    session_ref: str
    locale: str
    eos_server_ms: int | None  # server timestamp of end of speech
    admitted_server_ms: int | None  # server timestamp of Intent Object admitted in core
    correct: bool | None  # against the script annotation; None when not annotated
    consent_ref: str | None


@dataclass(frozen=True, slots=True)
class FieldMeasurements:
    """The numbers, with no verdict and no floors."""

    n: int
    n_failed: int  # sessions with no usable latency, including clock errors
    n_clock_errors: int
    pidgin_share: float
    p95_ms: float
    ci_low_ms: float
    ci_high_ms: float
    correct_rate: float
    correct_ci_low: float
    correct_ci_high: float
    locale_counts: tuple[tuple[str, int], ...]  # sessions per required locale, in the order the caller listed them
    locale_correct_rates: tuple[tuple[str, float], ...]  # correctness per required locale; 0.0 for a locale with no session


@dataclass(frozen=True, slots=True)
class FieldAnalysis:
    verdict: GateVerdict
    reasons: tuple[str, ...]
    notes: tuple[str, ...]  # caveats that do not change the verdict
    n: int
    n_failed: int
    n_clock_errors: int
    pidgin_share: float
    p95_ms: float
    ci_low_ms: float
    ci_high_ms: float
    correct_rate: float
    correct_ci_low: float
    correct_ci_high: float
    locale_counts: tuple[tuple[str, int], ...]
    locale_correct_rates: tuple[tuple[str, float], ...]
    method: str
    confidence: float


def _cdf_table(n: int, p: float) -> list[float]:
    """P(X <= k) for k = 0..n, X ~ Binomial(n, p), built in log space so it neither overflows nor costs O(n) per entry."""
    if p <= 0.0:
        return [1.0] * (n + 1)
    if p >= 1.0:
        return [0.0] * n + [1.0]
    log_p, log_q, log_n = math.log(p), math.log1p(-p), math.lgamma(n + 1)
    table: list[float] = []
    total = 0.0
    for i in range(n + 1):
        total += math.exp(log_n - math.lgamma(i + 1) - math.lgamma(n - i + 1) + i * log_p + (n - i) * log_q)
        table.append(min(total, 1.0))
    return table


def _interval(ordered: Sequence[float], fraction: float, confidence: float) -> tuple[float, float]:
    n = len(ordered)
    tail = (1 - confidence) / 2
    cdf = _cdf_table(n, fraction)
    low_rank = 0
    for k in range(1, n + 1):
        if cdf[k - 1] <= tail:
            low_rank = k
    low = ordered[low_rank - 1] if low_rank else 0.0  # no rank qualifies: there is no lower bound to claim
    for k in range(1, n + 1):
        if cdf[k - 1] >= 1 - tail:
            return low, ordered[k - 1]
    return low, math.inf


def clopper_pearson(successes: int, n: int, confidence: float) -> tuple[float, float]:
    """The exact two-sided binomial interval for `successes` of `n`, by bisection on the binomial tail."""
    tail = (1 - confidence) / 2
    low = 0.0
    if successes > 0:  # the p at which P(X >= successes) = tail; that probability rises with p
        lo, hi = 0.0, 1.0
        for _ in range(_BISECTION_STEPS):
            mid = (lo + hi) / 2
            if 1 - _cdf_table(n, mid)[successes - 1] < tail:
                lo = mid
            else:
                hi = mid
        low = (lo + hi) / 2
    high = 1.0
    if successes < n:  # the p at which P(X <= successes) = tail; that probability falls with p
        lo, hi = 0.0, 1.0
        for _ in range(_BISECTION_STEPS):
            mid = (lo + hi) / 2
            if _cdf_table(n, mid)[successes] > tail:
                lo = mid
            else:
                hi = mid
        high = (lo + hi) / 2
    return low, high


def _latency_ms(session: FieldSession) -> tuple[float, bool]:
    """The latency, and whether the session had a clock error. Anything unusable is infinite, never dropped."""
    if session.eos_server_ms is None or session.admitted_server_ms is None:
        return math.inf, False
    if session.admitted_server_ms < session.eos_server_ms:
        return math.inf, True  # admission before end of speech: a clock error, not a latency
    return float(session.admitted_server_ms - session.eos_server_ms), False


def _blank(value: str | None) -> bool:
    return value is None or not value.strip()


def measure(sessions: Sequence[FieldSession], *, required_locales: Sequence[str]) -> FieldMeasurements:
    if not required_locales or len(set(required_locales)) != len(required_locales):
        raise ValueError("required locales must be a non-empty list without repeats")
    if not sessions:
        raise ValueError("no sessions")
    if any(_blank(session.consent_ref) for session in sessions):
        raise ValueError("a session without a consent reference cannot be stored or counted")
    if any(_blank(session.session_ref) for session in sessions):
        raise ValueError("a session without a reference cannot be counted")
    refs = [session.session_ref for session in sessions]
    if len(set(refs)) != len(refs):
        raise ValueError("duplicate session reference")

    results = [_latency_ms(session) for session in sessions]
    latencies = [latency for latency, _ in results]
    n = len(sessions)
    ci_low, ci_high = _interval(sorted(latencies), _PERCENTILE, _CONFIDENCE)
    correct = sum(1 for s in sessions if s.correct is True)
    correct_low, correct_high = clopper_pearson(correct, n, _CONFIDENCE)
    per_locale = Counter(s.locale for s in sessions)
    per_locale_correct = Counter(s.locale for s in sessions if s.correct is True)
    return FieldMeasurements(
        n=n,
        n_failed=sum(1 for value in latencies if math.isinf(value)),
        n_clock_errors=sum(1 for _, clock_error in results if clock_error),
        pidgin_share=sum(1 for s in sessions if s.locale == _PIDGIN_LOCALE) / n,
        p95_ms=nearest_rank_percentile(latencies, _PERCENTILE),
        ci_low_ms=ci_low,
        ci_high_ms=ci_high,
        correct_rate=correct / n,
        correct_ci_low=correct_low,
        correct_ci_high=correct_high,
        locale_counts=tuple((locale, per_locale.get(locale, 0)) for locale in required_locales),
        locale_correct_rates=tuple(
            (locale, per_locale_correct.get(locale, 0) / per_locale[locale] if per_locale.get(locale, 0) else 0.0)
            for locale in required_locales),
    )


def _check_share(name: str, value: float, floor: float) -> None:
    if not (math.isfinite(value) and 0.0 < value <= 1.0):
        raise ValueError(f"{name} must be in (0, 1]; a floor of zero would let any run pass")
    if value < floor:
        raise ValueError(f"{name} {value} is weaker than the decided minimum {floor}")


def analyse(
    sessions: Sequence[FieldSession],
    *,
    target_ms: int,
    min_sessions: int,
    min_pidgin_share: float,
    correctness_floor: float,
    correctness_lower_floor: float,
    required_locales: Sequence[str],
    min_sessions_per_locale: int,
    per_locale_correctness_floor: float,
) -> FieldAnalysis:
    if target_ms < 1 or min_sessions < 1 or min_sessions_per_locale < 1:
        raise ValueError("target and minimum sessions must be positive")
    _check_share("min_pidgin_share", min_pidgin_share, PERD_MIN_PIDGIN_SHARE)
    _check_share("correctness_floor", correctness_floor, F5_MIN_CORRECTNESS)
    _check_share("correctness_lower_floor", correctness_lower_floor, F5_MIN_CORRECTNESS_LOWER)
    _check_share("per_locale_correctness_floor", per_locale_correctness_floor, F5_MIN_LOCALE_CORRECTNESS)
    if target_ms > PERD_MAX_P95_MS:
        raise ValueError(f"target_ms {target_ms} is weaker than the decided maximum {PERD_MAX_P95_MS}")
    if min_sessions < FIELD_MIN_SESSIONS:
        raise ValueError(f"min_sessions {min_sessions} is weaker than the decided minimum {FIELD_MIN_SESSIONS}")
    if min_sessions_per_locale < F5_MIN_SESSIONS_PER_LOCALE:
        raise ValueError(
            f"min_sessions_per_locale {min_sessions_per_locale} is weaker than the decided minimum {F5_MIN_SESSIONS_PER_LOCALE}")
    m = measure(sessions, required_locales=required_locales)

    reasons: list[str] = []
    if m.n < min_sessions:
        reasons.append("too few sessions")
    if any(count < min_sessions_per_locale for _, count in m.locale_counts):
        reasons.append("too few sessions in a required locale")
    if m.pidgin_share < min_pidgin_share:
        reasons.append("pidgin share below the minimum")
    if m.p95_ms > target_ms:
        reasons.append("p95 over the target")
    elif m.ci_high_ms > target_ms:
        reasons.append("p95 upper bound over the target")  # A126b: the evidence must rule out a P95 over the target
    if m.correct_rate < correctness_floor:
        reasons.append("correctness below the floor")
    if m.correct_ci_low < correctness_lower_floor:
        reasons.append("correctness interval below the floor")
    if any(rate < per_locale_correctness_floor for _, rate in m.locale_correct_rates):
        reasons.append("correctness below the floor in a locale")
    if m.n_clock_errors:
        reasons.append("sessions with a clock error")
    notes: list[str] = []
    if m.n_failed:
        notes.append(f"{m.n_failed} of {m.n} sessions have no usable latency and are counted as infinite")
    return FieldAnalysis(
        verdict=GateVerdict.FAIL if reasons else GateVerdict.PASS,
        reasons=tuple(reasons),
        notes=tuple(notes),
        n=m.n,
        n_failed=m.n_failed,
        n_clock_errors=m.n_clock_errors,
        pidgin_share=m.pidgin_share,
        p95_ms=m.p95_ms,
        ci_low_ms=m.ci_low_ms,
        ci_high_ms=m.ci_high_ms,
        correct_rate=m.correct_rate,
        correct_ci_low=m.correct_ci_low,
        correct_ci_high=m.correct_ci_high,
        locale_counts=m.locale_counts,
        locale_correct_rates=m.locale_correct_rates,
        method=_METHOD,
        confidence=_CONFIDENCE,
    )
