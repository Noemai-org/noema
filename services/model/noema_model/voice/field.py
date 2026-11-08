"""Analysis of the field sessions for the real-conditions voice gate (VOI-14, REQ-VOI-005; PERD §45.4, SoT §37.5).

The gate's numbers are arguments taken from PERD §45.4 and from the Founder's decision F5 of 8 Nov 2026 by the caller; none is
written here (AGENT-16). F5: Intent Object correctness against the script annotation at least 90%, the lower end of its 95%
interval at least 80%, and at least 30 sessions in each required locale. (REQ-VOI-003's 95% for key-slot accuracy is a different
measure and is not computed here.)

A session counts only with both server timestamps. One missing either is a failure that stays in the denominator as an infinite
latency: a run cannot pass by dropping its failures. An unannotated session counts as incorrect.

The report carries what a published number needs (n, the method and an interval). The latency interval is distribution-free, from
order statistics of the binomial, so it makes no assumption about the latency distribution; with a sample too small to bound the
percentile from above, the upper bound is infinite rather than a guess. The correctness interval is the exact (Clopper-Pearson)
binomial interval, which never under-covers.

Records hold references and times only: no audio and no transcript (AGENT-13). A session with no consent reference is refused,
since its audio could not be stored (REQ-VOI-004).
"""

import math
from collections import Counter
from collections.abc import Sequence
from dataclasses import dataclass
from enum import StrEnum

from noema_model.voice.latency import nearest_rank_percentile

_PERCENTILE = 0.95
_CONFIDENCE = 0.95
_PIDGIN_LOCALE = "pcm"
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
class FieldAnalysis:
    verdict: GateVerdict
    reasons: tuple[str, ...]
    n: int
    n_failed: int
    pidgin_share: float
    p95_ms: float
    ci_low_ms: float
    ci_high_ms: float
    correct_rate: float
    correct_ci_low: float
    correct_ci_high: float
    locale_counts: tuple[tuple[str, int], ...]  # sessions per required locale, in the order the caller listed them
    method: str
    confidence: float


def _binomial_cdf(k: int, n: int, p: float) -> float:
    return sum(math.comb(n, i) * p**i * (1 - p) ** (n - i) for i in range(k + 1))


def _interval(ordered: Sequence[float], fraction: float, confidence: float) -> tuple[float, float]:
    n = len(ordered)
    tail = (1 - confidence) / 2
    low_rank = 1
    for k in range(1, n + 1):
        if _binomial_cdf(k - 1, n, fraction) <= tail:
            low_rank = k
    for k in range(1, n + 1):
        if _binomial_cdf(k - 1, n, fraction) >= 1 - tail:
            return ordered[low_rank - 1], ordered[k - 1]
    return ordered[low_rank - 1], math.inf


def clopper_pearson(successes: int, n: int, confidence: float) -> tuple[float, float]:
    """The exact two-sided binomial interval for `successes` of `n`, by bisection on the binomial tail."""
    tail = (1 - confidence) / 2
    low = 0.0
    if successes > 0:  # the p at which P(X >= successes) = tail; that probability rises with p
        lo, hi = 0.0, 1.0
        for _ in range(_BISECTION_STEPS):
            mid = (lo + hi) / 2
            if 1 - _binomial_cdf(successes - 1, n, mid) < tail:
                lo = mid
            else:
                hi = mid
        low = (lo + hi) / 2
    high = 1.0
    if successes < n:  # the p at which P(X <= successes) = tail; that probability falls with p
        lo, hi = 0.0, 1.0
        for _ in range(_BISECTION_STEPS):
            mid = (lo + hi) / 2
            if _binomial_cdf(successes, n, mid) > tail:
                lo = mid
            else:
                hi = mid
        high = (lo + hi) / 2
    return low, high


def _latency_ms(session: FieldSession) -> float:
    if session.eos_server_ms is None or session.admitted_server_ms is None:
        return math.inf
    if session.admitted_server_ms < session.eos_server_ms:
        raise ValueError("admission before end of speech: a clock error, not a latency")
    return float(session.admitted_server_ms - session.eos_server_ms)


def _check_share(value: float) -> None:
    if not (math.isfinite(value) and 0.0 <= value <= 1.0):
        raise ValueError("shares must be in [0, 1]")


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
) -> FieldAnalysis:
    if target_ms < 1 or min_sessions < 1 or min_sessions_per_locale < 1:
        raise ValueError("target and minimum sessions must be positive")
    for share in (min_pidgin_share, correctness_floor, correctness_lower_floor):
        _check_share(share)
    if not required_locales or len(set(required_locales)) != len(required_locales):
        raise ValueError("required locales must be a non-empty list without repeats")
    if not sessions:
        raise ValueError("no sessions")
    if any(session.consent_ref is None for session in sessions):
        raise ValueError("a session without a consent reference cannot be stored or counted")
    refs = [session.session_ref for session in sessions]
    if len(set(refs)) != len(refs):
        raise ValueError("duplicate session reference")

    latencies = [_latency_ms(session) for session in sessions]
    n = len(sessions)
    p95 = nearest_rank_percentile(latencies, _PERCENTILE)
    ci_low, ci_high = _interval(sorted(latencies), _PERCENTILE, _CONFIDENCE)
    pidgin_share = sum(1 for s in sessions if s.locale == _PIDGIN_LOCALE) / n
    correct = sum(1 for s in sessions if s.correct is True)
    correct_rate = correct / n
    correct_low, correct_high = clopper_pearson(correct, n, _CONFIDENCE)
    per_locale = Counter(s.locale for s in sessions)
    locale_counts = tuple((locale, per_locale.get(locale, 0)) for locale in required_locales)

    reasons: list[str] = []
    if n < min_sessions:
        reasons.append("too few sessions")
    if any(count < min_sessions_per_locale for _, count in locale_counts):
        reasons.append("too few sessions in a required locale")
    if pidgin_share < min_pidgin_share:
        reasons.append("pidgin share below the minimum")
    if p95 > target_ms:
        reasons.append("p95 over the target")
    if correct_rate < correctness_floor:
        reasons.append("correctness below the floor")
    if correct_low < correctness_lower_floor:
        reasons.append("correctness interval below the floor")
    return FieldAnalysis(
        verdict=GateVerdict.FAIL if reasons else GateVerdict.PASS,
        reasons=tuple(reasons),
        n=n,
        n_failed=sum(1 for value in latencies if math.isinf(value)),
        pidgin_share=pidgin_share,
        p95_ms=p95,
        ci_low_ms=ci_low,
        ci_high_ms=ci_high,
        correct_rate=correct_rate,
        correct_ci_low=correct_low,
        correct_ci_high=correct_high,
        locale_counts=locale_counts,
        method=_METHOD,
        confidence=_CONFIDENCE,
    )
