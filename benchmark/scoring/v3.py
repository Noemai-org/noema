"""benchmark-scorecard.v3 from a v2 scorecard (BEN-30; Architect ruling A1 of 10 Nov 2026, A80; Founders' E5).

v3 is v2 plus `exact_upper95` on every rate: the exact one-sided 95% upper bound 1 - 0.05^(1/n) when no failure was counted in n cases, null otherwise. `rule_of_three_upper`
(3/n) stays beside it unchanged: it is an approximation and is not relabelled. This module sets no threshold and changes no count (AGENT-16). The v2 scorer path is untouched; it is
switched only after the contract set is approved. Generated layer only; reads no held-out material (INV-22)."""
import copy
from collections.abc import Mapping
from typing import Any

from benchmark.scoring.stats import PLACES

Json = dict[str, Any]


def exact_zero_upper95(n: int) -> float | None:
    """The exact one-sided 95% upper bound on a rate when none of n cases failed: 1 - 0.05^(1/n). None for no cases."""
    if n < 0:
        raise ValueError(f"n must not be negative, not {n}")
    return round(1 - 0.05 ** (1 / n), PLACES) if n else None


def _add(node: Any) -> None:
    if isinstance(node, dict):
        if "rule_of_three_upper" in node:
            node["exact_upper95"] = None if node["rule_of_three_upper"] is None else exact_zero_upper95(node["n"])
        for value in list(node.values()):
            _add(value)
    elif isinstance(node, list):
        for value in node:
            _add(value)


def scorecard_v3(card: Mapping[str, Any]) -> Json:
    """A copy of a v2 scorecard with `exact_upper95` on every rate."""
    out: Json = copy.deepcopy(dict(card))
    if _has_exact(out):
        raise ValueError("the scorecard already carries exact_upper95: it is v3")
    _add(out)
    return out


def _has_exact(node: Any) -> bool:
    if isinstance(node, dict):
        return "exact_upper95" in node or any(_has_exact(v) for v in node.values())
    if isinstance(node, list):
        return any(_has_exact(v) for v in node)
    return False
