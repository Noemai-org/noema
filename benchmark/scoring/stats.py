"""Rates with their 95% Wilson intervals, the rule-of-three bound and nearest-rank percentiles.

REQ-BEN-004: every report states the number of cases and a Wilson score interval (95%) for the rates. SoT §38.8:
zero failures in n independent cases bounds the true failure rate below about 3/n at 95% confidence.
All outputs are rounded to six places so that a recorded run reproduces byte for byte.
"""
import math
from typing import Any, Sequence

Json = dict[str, Any]

# The 97.5th percentile of the standard normal distribution: the two-sided 95% interval of REQ-BEN-004.
Z95 = 1.959963984540054
PLACES = 6


def _check(k: int, n: int) -> None:
    if n < 0 or k < 0 or k > n:
        raise ValueError(f"impossible count: {k} of {n}")


def wilson_interval(k: int, n: int) -> tuple[float, float] | None:
    """The 95% Wilson score interval for k successes in n trials, or None when n is 0."""
    _check(k, n)
    if n == 0:
        return None
    p = k / n
    z2 = Z95 * Z95
    denominator = 1 + z2 / n
    centre = (p + z2 / (2 * n)) / denominator
    half = Z95 * math.sqrt(p * (1 - p) / n + z2 / (4 * n * n)) / denominator
    low = round(max(0.0, centre - half), PLACES) + 0.0
    high = round(min(1.0, centre + half), PLACES) + 0.0
    return low, high


def make_rate(k: int, n: int, *, failure: bool = False) -> Json:
    """k out of n. `failure` says the counted event is a failure, so zero of them earns the rule-of-three bound."""
    _check(k, n)
    interval = wilson_interval(k, n)
    return {
        "k": k,
        "n": n,
        "rate": round(k / n, PLACES) if n else None,
        "wilson95": [interval[0], interval[1]] if interval else None,
        "rule_of_three_upper": round(3 / n, PLACES) if (failure and n and k == 0) else None,
    }


def percentile(values: Sequence[int], p: int) -> int | None:
    """Nearest rank: the smallest value with at least p% of the values at or below it. None for no values."""
    if not values:
        return None
    ordered = sorted(values)
    rank = max(1, math.ceil(p * len(ordered) / 100))
    return ordered[rank - 1]
