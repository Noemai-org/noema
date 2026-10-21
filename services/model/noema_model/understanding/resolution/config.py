"""Ranking configuration: the weights, the margin, the floor and the tolerances, read from pack data, never from code.

Numbers are read as exact fractions from strings ("0.35", "7/20") or integers; a JSON float is refused, because a float
would make the same file mean different things on different machines (INV-21). Nothing is defaulted. The adopted values
(REQ-AMB-003: weights 0.35, 0.25, 0.20, 0.10, 0.10; margin 0.35; floor 0.70; at most 4 above the floor) live in the pack,
and only a benchmark run plus a founder decision changes them (AGENT-16).
"""

import hashlib
import json
import re
from collections.abc import Mapping
from dataclasses import dataclass
from decimal import Decimal
from fractions import Fraction
from typing import Any

from noema_model.platform.config import ConfigError, reject_unknown, require_int, require_mapping
from noema_model.understanding.resolution.types import FEATURES


_NUMBER = re.compile(r"^(?:[0-9]{1,18}(?:\.[0-9]{1,18})?|[0-9]{1,18}/[1-9][0-9]{0,17})$")


def exact_number(value: Any, where: str) -> Fraction:
    """A non-negative decimal or fraction as a string, or a non-negative integer. Never a float."""
    if isinstance(value, int) and not isinstance(value, bool) and value >= 0:
        return Fraction(value)
    if not isinstance(value, str) or not _NUMBER.match(value):
        raise ConfigError(f"{where}: write the number as a string such as \"0.35\" or \"7/20\", or as an integer")
    return Fraction(value) if "/" in value else Fraction(Decimal(value))


@dataclass(frozen=True, slots=True)
class RankingConfig:
    weights: Mapping[str, Fraction]
    auto_resolve_margin: Fraction
    absolute_floor: Fraction
    max_candidates_before_escalate: int
    max_raw_candidates: int
    amount_tolerance_ratio: Fraction
    time_tolerance_seconds: int
    digest: str

    @classmethod
    def from_mapping(cls, raw: Any) -> "RankingConfig":
        src = require_mapping(raw, "ranking")
        reject_unknown(
            src,
            frozenset({
                "weights", "auto_resolve_margin", "absolute_floor", "max_candidates_before_escalate",
                "max_raw_candidates", "amount_tolerance_ratio", "time_tolerance_seconds",
            }),
            "ranking",
        )
        weights_src = require_mapping(src.get("weights"), "ranking.weights")
        reject_unknown(weights_src, frozenset(FEATURES), "ranking.weights")
        missing = [name for name in FEATURES if name not in weights_src]
        if missing:
            raise ConfigError(f"ranking.weights: missing {missing}")  # a pack without every weight fails at load
        weights = {name: exact_number(weights_src[name], f"ranking.weights.{name}") for name in FEATURES}
        if any(w < 0 for w in weights.values()) or sum(weights.values()) != 1:
            raise ConfigError("ranking.weights: each weight is at least 0 and together they are exactly 1")
        margin = exact_number(src.get("auto_resolve_margin"), "ranking.auto_resolve_margin")
        floor = exact_number(src.get("absolute_floor"), "ranking.absolute_floor")
        tolerance = exact_number(src.get("amount_tolerance_ratio"), "ranking.amount_tolerance_ratio")
        if not (0 <= margin <= 1 and 0 <= floor <= 1 and 0 <= tolerance <= 1):
            raise ConfigError("ranking: margin, floor and tolerance lie in [0, 1]")
        max_above = require_int(src, "max_candidates_before_escalate", "ranking", minimum=1)
        max_raw = require_int(src, "max_raw_candidates", "ranking", minimum=1)
        time_tolerance = require_int(src, "time_tolerance_seconds", "ranking", minimum=0)
        body = {
            "weights": {k: f"{v.numerator}/{v.denominator}" for k, v in weights.items()},
            "margin": f"{margin.numerator}/{margin.denominator}",
            "floor": f"{floor.numerator}/{floor.denominator}",
            "max_above_floor": max_above,
            "max_raw": max_raw,
            "tolerance": f"{tolerance.numerator}/{tolerance.denominator}",
            "time_tolerance_seconds": time_tolerance,
        }
        digest = hashlib.sha256(json.dumps(body, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
        return cls(
            weights=weights,
            auto_resolve_margin=margin,
            absolute_floor=floor,
            max_candidates_before_escalate=max_above,
            max_raw_candidates=max_raw,
            amount_tolerance_ratio=tolerance,
            time_tolerance_seconds=time_tolerance,
            digest=digest,
        )
