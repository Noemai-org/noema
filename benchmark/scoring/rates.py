"""The scorecard v3 rate: the frozen `stats.make_rate` plus `exact_upper95` (ruling A190, interim; AGENT-15).

`exact_upper95` is the exact (Clopper-Pearson) one-sided 95% upper bound on the true rate of a counted FAILURE: the largest rate u for which k or fewer failures in n independent cases still has
probability ALPHA (0.05). For k = 0 it is 1 - 0.05^(1/n), which SoT section 38.8 approximates by 3/n. It is rounded UP to six places, so a reader never sees the bound understated. A rate that is
not a failure rate carries null, as `rule_of_three_upper` does, and so does a rate of no cases. No threshold is applied here (AGENT-16).

`stats.py` is hashed by the 27 Oct 2026 model-selection freeze, so it is not edited: this module wraps it, and only the scorecard and its diagnostics use the wrapper."""
import math
from typing import Any

from benchmark.scoring import stats

Json = dict[str, Any]

ALPHA = 0.05
PLACES = stats.PLACES


def _cdf(k: int, n: int, p: float) -> float:
    """P(X <= k) for X ~ Binomial(n, p), summed in log space so a large n does not overflow."""
    log_p, log_q = math.log(p), math.log1p(-p)
    log_n = math.lgamma(n + 1)
    total = 0.0
    for i in range(k + 1):
        total += math.exp(log_n - math.lgamma(i + 1) - math.lgamma(n - i + 1) + i * log_p + (n - i) * log_q)
    return min(1.0, total)


def exact_upper95(k: int, n: int) -> float | None:
    """The exact one-sided 95% upper bound on the failure rate after k failures in n cases; None for n = 0."""
    if n < 0 or k < 0 or k > n:
        raise ValueError(f"impossible count: {k} of {n}")
    if n == 0:
        return None
    if k == n:
        return 1.0
    if k == 0:
        upper = 1 - math.pow(ALPHA, 1 / n)
    else:
        low, high = 0.0, 1.0
        for _ in range(200):  # the cdf falls as p rises; the bound is where it crosses ALPHA
            mid = (low + high) / 2
            if _cdf(k, n, mid) > ALPHA:
                low = mid
            else:
                high = mid
        upper = high
    scale = float(10**PLACES)
    return float(min(1.0, math.ceil(round(upper * scale, 3)) / scale))


def make_rate(k: int, n: int, *, failure: bool = False) -> Json:
    """The frozen rate block plus `exact_upper95`, which only a rate of failures carries."""
    block = stats.make_rate(k, n, failure=failure)
    block["exact_upper95"] = exact_upper95(k, n) if failure else None
    return block
