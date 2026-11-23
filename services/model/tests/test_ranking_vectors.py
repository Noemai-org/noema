"""The Python reference ranker against the shared golden vectors (REQ-AMB-003, INV-21; rulings A555, A559, A560, interim, relayed).

The same file, contracts/fixtures/candidate_ranking_vectors.json, is loaded by the Go suite (platform/candidaterank). The Python ranker is exact rational arithmetic (fractions, never a float); the Go
ranker is integer basis points with floor division and is the authority. Every vector is built so its exact score is a whole number of basis points, so both give the same answer; the
expected value in basis points is the floor of the exact one. All data is SYNTHETIC.
"""

import json
import math
import unittest
from datetime import datetime
from fractions import Fraction
from pathlib import Path
from typing import Any, cast

from noema_model.understanding.resolution import Candidate, Context, RankingConfig, RecipientMatcher, Target, rank
from noema_model.understanding.resolution.ranking import amount_feature, temporal_feature

VECTORS = Path(__file__).resolve().parents[3] / "contracts" / "fixtures" / "candidate_ranking_vectors.json"
UNIT = 10_000


def _bp(value: Fraction) -> int:
    return math.floor(value * UNIT)


def _when(text: str) -> datetime:
    return datetime.fromisoformat(text.replace("Z", "+00:00"))


class _StubMatcher:
    """The recipient feature comes from the vector, not from names: the matcher is not part of the shared vectors."""

    digest = "stub"

    def __init__(self, by_display: dict[str, int]) -> None:
        self._by_display = by_display

    def score(self, query: str | None, display: str, *, phonetic: bool) -> Fraction:
        return Fraction(self._by_display[display], UNIT)


def _config(v: dict[str, Any]) -> RankingConfig:
    c = v["config"]
    return RankingConfig.from_mapping(
        {
            "weights": {k: f"{bp}/{UNIT}" for k, bp in c["weights_bp"].items()},
            "auto_resolve_margin": f"{c['margin_bp']}/{UNIT}",
            "absolute_floor": f"{c['floor_bp']}/{UNIT}",
            "max_candidates_before_escalate": c["max_above_floor"],
            "max_raw_candidates": 20,
            "amount_tolerance_ratio": f"{c['amount_tolerance_bp']}/{UNIT}",
            "time_tolerance_seconds": c["time_tolerance_seconds"],
        }
    )


def _candidate(row: dict[str, Any]) -> Candidate:
    return Candidate(
        candidate_id=row["id"], resource_ref="res_" + row["id"], amount_minor=row["amount_minor"], currency=row["currency"],
        occurred_at=_when(row["occurred_at"]), counterparty_display=row["id"], account_last4="1234", direction=row["direction"],
        beneficiary_alias_match=row["alias_match"],
    )


class RankingVectors(unittest.TestCase):
    v: dict[str, Any]
    config: RankingConfig

    @classmethod
    def setUpClass(cls) -> None:
        cls.v = json.loads(VECTORS.read_text(encoding="utf-8"))
        cls.config = _config(cls.v)

    def test_the_vectors_are_synthetic_and_carry_both_thresholds(self) -> None:
        self.assertEqual(self.v["label"], "SYNTHETIC")
        self.assertEqual((self.v["config"]["floor_bp"], self.v["config"]["margin_bp"]), (7000, 3500))

    def test_amount_ramp(self) -> None:
        for case in self.v["amount_ramps"]:
            wanted = case["wanted_minor"]
            target = Target(amount_minor=wanted, currency="NGN" if wanted is not None else None, window=None, counterparty_surface=None, expected_direction=None)
            row = {"id": "cand_x", "amount_minor": case["candidate_minor"], "currency": case.get("candidate_currency", "NGN"),
                   "occurred_at": "2026-11-20T09:30:00Z", "direction": "IN", "alias_match": None}
            with self.subTest(case["name"]):
                self.assertEqual(_bp(amount_feature(target, _candidate(row), self.config)), case["want_bp"])

    def test_temporal_ramp(self) -> None:
        window = (_when("2026-11-20T09:00:00Z"), _when("2026-11-20T10:00:00Z"))
        for case in self.v["temporal_ramps"]:
            target = Target(amount_minor=None, currency=None, window=None if case.get("no_window") else window, counterparty_surface=None, expected_direction=None)
            row = {"id": "cand_x", "amount_minor": 0, "currency": "NGN", "occurred_at": case["occurred_at"], "direction": "IN", "alias_match": None}
            with self.subTest(case["name"]):
                self.assertEqual(_bp(temporal_feature(target, _candidate(row), self.config)), case["want_bp"])

    def test_score_is_the_floored_weighted_sum(self) -> None:
        for case in self.v["scores"]:
            total = sum((self.config.weights[k] * Fraction(bp, UNIT) for k, bp in case["features_bp"].items()), Fraction(0))
            with self.subTest(case["name"]):
                self.assertEqual(_bp(total), case["want_bp"])

    def test_resolution_at_every_threshold(self) -> None:
        for case in self.v["resolutions"]:
            t = case["target"]
            target = Target(
                amount_minor=t["amount_minor"], currency=t["currency"],
                window=(_when(t["window"][0]), _when(t["window"][1])) if t["window"] else None,
                counterparty_surface="x", expected_direction=t["expected_direction"],
            )
            cands = tuple(_candidate(r) for r in case["candidates"])
            matcher = _StubMatcher({r["id"]: r["recipient_bp"] for r in case["candidates"]})
            ctx = Context(material_flag_present=case["context"]["material_flag"], key_slot_low_asr=case["context"]["key_slot_low_asr"], phonetic=False)
            got = rank(cands, target, ctx, self.config, cast(RecipientMatcher, matcher), candidate_set_id="set_x", candidate_set_version=1)
            want = case["want"]
            with self.subTest(case["name"]):
                self.assertEqual(got.state.value, want["state"])
                self.assertEqual([(s.candidate.candidate_id, _bp(s.score)) for s in got.ranked], [(r["id"], r["score_bp"]) for r in want["ranked"]])
                self.assertEqual((_bp(got.top_score), _bp(got.second_score), _bp(got.margin), got.above_floor_count),
                                 (want["top_bp"], want["second_bp"], want["margin_bp"], want["above_floor"]))
                self.assertEqual([w.value for w in got.why_not], want["why_not"])
                self.assertEqual(got.resolved_candidate_id, want["resolved_id"])


if __name__ == "__main__":
    unittest.main()
