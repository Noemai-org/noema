"""BEN-30, scorecard v3 (Architect ruling A1 of 10 Nov 2026; Founders' E5): the exact zero-failure bound and the v3 scorecard. Written before `v3.py` and seen failing.
The v2 scorer path is unchanged (A1: build it beside, do not touch v2 before the contract set is approved). SYNTHETIC; generated layer only (INV-22)."""
import copy
import math
import unittest
from typing import Any

from benchmark.contracts import Contracts
from benchmark.scoring.stats import make_rate
from benchmark.scoring.test_report import card_for, fixture
from benchmark.scoring.v3 import exact_zero_upper95, scorecard_v3

Json = dict[str, Any]
CONTRACTS = Contracts()


class ExactBound(unittest.TestCase):
    def test_it_is_one_minus_the_nth_root_of_five_hundredths(self) -> None:
        for n in (1, 6, 40, 300, 1000):
            self.assertEqual(round(1 - 0.05 ** (1 / n), 6), exact_zero_upper95(n))

    def test_it_is_below_the_rule_of_three_and_close_to_it_for_large_n(self) -> None:
        bound = exact_zero_upper95(300)
        one = exact_zero_upper95(1)
        assert bound is not None and one is not None
        self.assertLess(bound, 3 / 300)
        self.assertAlmostEqual(3 / 300, bound, places=3)
        self.assertTrue(math.isclose(one, 0.95, abs_tol=1e-6))

    def test_it_is_undefined_without_cases(self) -> None:
        self.assertIsNone(exact_zero_upper95(0))
        with self.assertRaises(ValueError):
            exact_zero_upper95(-1)


class Card(unittest.TestCase):
    def v2(self) -> Json:
        cases, oracles, results, _ = fixture()
        return card_for(cases, oracles, results)

    def test_the_v3_card_validates_against_the_v3_contract_and_the_v2_card_does_not(self) -> None:
        card = self.v2()
        self.assertEqual([], CONTRACTS.errors("benchmark-scorecard.v2", card))
        v3 = scorecard_v3(card)
        self.assertEqual([], CONTRACTS.errors("benchmark-scorecard.v3", v3))
        self.assertTrue(CONTRACTS.errors("benchmark-scorecard.v3", card))

    def test_every_rate_carries_the_exact_bound_exactly_when_it_carries_the_rule_of_three(self) -> None:
        seen = 0

        def visit(node: Any) -> None:
            nonlocal seen
            if isinstance(node, dict):
                if "rule_of_three_upper" in node:
                    seen += 1
                    if node["rule_of_three_upper"] is None:
                        self.assertIsNone(node["exact_upper95"])
                    else:
                        self.assertEqual(exact_zero_upper95(node["n"]), node["exact_upper95"])
                for value in node.values():
                    visit(value)
            elif isinstance(node, list):
                for value in node:
                    visit(value)
        visit(scorecard_v3(self.v2()))
        self.assertGreater(seen, 10)

    def test_the_v2_fields_are_kept_and_the_input_is_not_changed(self) -> None:
        card = self.v2()
        before = copy.deepcopy(card)
        v3 = scorecard_v3(card)
        self.assertEqual(before, card)
        self.assertEqual(card["tier0"]["gates"][0]["rate"]["rule_of_three_upper"], v3["tier0"]["gates"][0]["rate"]["rule_of_three_upper"])

    def test_a_failure_rate_with_a_failure_has_no_exact_bound(self) -> None:
        rate = make_rate(2, 40, failure=True)
        self.assertIsNone(rate["rule_of_three_upper"])
        card = self.v2()
        card["tier0"]["gates"][0]["rate"] = rate
        self.assertIsNone(scorecard_v3(card)["tier0"]["gates"][0]["rate"]["exact_upper95"])

    def test_a_card_that_is_already_v3_is_refused(self) -> None:
        with self.assertRaises(ValueError):
            scorecard_v3(scorecard_v3(self.v2()))


if __name__ == "__main__":
    unittest.main()
