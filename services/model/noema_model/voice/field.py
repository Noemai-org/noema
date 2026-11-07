"""Analysis of the field sessions for the real-conditions voice gate (VOI-14, REQ-VOI-005; PERD §45.4, SoT §37.5).

The gate's numbers are arguments taken from PERD §45.4 by the caller; none is written here (AGENT-16). A session counts only
with both server timestamps. One missing either is a failure that stays in the denominator as an infinite latency: a run
cannot pass by dropping its failures. An unannotated session counts as incorrect.

The report carries what a published number needs (n, the method and an interval). The interval is distribution-free, from
order statistics of the binomial, so it makes no assumption about the latency distribution; with a sample too small to bound
the percentile from above, the upper bound is infinite rather than a guess.

Records hold references and times only: no audio and no transcript (AGENT-13). A session with no consent reference is
refused, since its audio could not be stored (REQ-VOI-004).
"""

import math
from collections.abc import Sequence
from dataclasses import dataclass
from enum import StrEnum

from noema_model.voice.latency import nearest_rank_percentile

_PERCENTILE = 0.95
_CONFIDENCE = 0.95
_PIDGIN_LOCALE = "pcm"
_METHOD = "nearest-rank P95; distribution-free 95% interval from order statistics"


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


def _latency_ms(session: FieldSession) -> float:
    if session.eos_server_ms is None or session.admitted_server_ms is None:
        return math.inf
    if session.admitted_server_ms < session.eos_server_ms:
        raise ValueError("admission before end of speech: a clock error, not a latency")
    return float(session.admitted_server_ms - session.eos_server_ms)


def analyse(
    sessions: Sequence[FieldSession],
    *,
    target_ms: int,
    min_sessions: int,
    min_pidgin_share: float,
    correctness_floor: float,
) -> FieldAnalysis:
    if target_ms < 1 or min_sessions < 1:
        raise ValueError("target and minimum sessions must be positive")
    for share in (min_pidgin_share, correctness_floor):
        if not (math.isfinite(share) and 0.0 <= share <= 1.0):
            raise ValueError("shares must be in [0, 1]")
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
    correct_rate = sum(1 for s in sessions if s.correct is True) / n

    reasons: list[str] = []
    if n < min_sessions:
        reasons.append("too few sessions")
    if pidgin_share < min_pidgin_share:
        reasons.append("pidgin share below the minimum")
    if p95 > target_ms:
        reasons.append("p95 over the target")
    if correct_rate < correctness_floor:
        reasons.append("correctness below the floor")
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
        method=_METHOD,
        confidence=_CONFIDENCE,
    )
