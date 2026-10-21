"""Property tests for scoring (REQ-TST-002): purity, Tier 0 monotonicity and 'no Tier 1-3 without Tier 0'."""
import copy
import random
import unittest

from hypothesis import HealthCheck, given, settings, strategies as st

from benchmark.contracts import Contracts
from benchmark.scoring.judge import GATES
from benchmark.scoring.scorecard import score
from benchmark.scoring.testdata import GATE_MUTATIONS, REGISTRY, clean_result, renumber, templates

CONTRACTS = Contracts()
SETTINGS = settings(max_examples=60, deadline=None, suppress_health_check=[HealthCheck.too_slow])


@st.composite
def suites(draw):
    """A clean suite of 1 to 9 cases built from the three templates, with distinct case ids."""
    kinds = draw(st.lists(st.integers(0, 2), min_size=1, max_size=9))
    cases, oracles, results = [], [], []
    for i, kind in enumerate(kinds):
        pair = templates()[kind]
        prefix = pair[0]["case_id"].split("-")[1]
        case, oracle = renumber(pair, f"BEN-{prefix}-{i + 1:04d}")
        cases.append(case)
        oracles.append(oracle)
        results.append(clean_result(case, oracle))
    return cases, oracles, results


def card_of(cases, oracles, results, **kw):
    return score(cases, oracles, results, REGISTRY, run_id="run_prop", **kw)


def gate_count(card, gate):
    return next(g["count"] for g in card["tier0"]["gates"] if g["gate"] == gate)


class ScoringProperties(unittest.TestCase):
    @SETTINGS
    @given(suites())
    def test_a_clean_suite_is_all_zero_and_all_correct(self, suite) -> None:
        card = card_of(*suite)
        self.assertTrue(card["tier0"]["all_zero"])
        self.assertTrue(card["tier1"]["reportable"])
        rate = card["tier1"]["correct_terminal_handling"]
        self.assertEqual(rate["k"], rate["n"])
        self.assertEqual(card["failures"], [])

    @SETTINGS
    @given(suites(), st.integers(0, 10**6))
    def test_order_of_inputs_never_changes_the_score(self, suite, seed) -> None:
        cases, oracles, results = suite
        shuffled = [list(x) for x in (cases, oracles, results)]
        rng = random.Random(seed)
        for part in shuffled:
            rng.shuffle(part)
        self.assertEqual(card_of(*suite), card_of(*shuffled))

    @SETTINGS
    @given(suites(), st.sampled_from(GATES), st.data())
    def test_one_violation_adds_exactly_one_to_its_gate_and_lowers_no_other(self, suite, gate, data) -> None:
        cases, oracles, results = suite
        before = card_of(cases, oracles, results)
        index = data.draw(st.integers(0, len(results) - 1))
        mutated = copy.deepcopy(results)
        GATE_MUTATIONS[gate](mutated[index])
        after = card_of(cases, oracles, mutated)
        self.assertEqual(gate_count(after, gate), gate_count(before, gate) + 1)
        for other in GATES:
            self.assertGreaterEqual(gate_count(after, other), gate_count(before, other))
        self.assertFalse(after["tier0"]["all_zero"])
        self.assertFalse(after["tier1"]["reportable"])
        self.assertLess(after["tier1"]["correct_terminal_handling"]["k"], before["tier1"]["correct_terminal_handling"]["k"])

    @SETTINGS
    @given(suites(), st.lists(st.tuples(st.integers(0, 8), st.sampled_from(GATES)), max_size=6))
    def test_tier_zero_is_never_compensated_by_tier_one(self, suite, violations) -> None:
        cases, oracles, results = suite
        mutated = copy.deepcopy(results)
        for index, gate in violations:
            GATE_MUTATIONS[gate](mutated[index % len(mutated)])
        card = card_of(cases, oracles, mutated)
        self.assertEqual(card["tier1"]["reportable"], card["tier0"]["all_zero"])
        self.assertEqual(card["tier0"]["all_zero"], all(g["count"] == 0 for g in card["tier0"]["gates"]))
        self.assertEqual(CONTRACTS.errors("benchmark-scorecard", card), [])

    @SETTINGS
    @given(suites())
    def test_every_scorecard_always_carries_all_eleven_gates_beside_every_tier(self, suite) -> None:
        card = card_of(*suite)
        self.assertEqual([g["gate"] for g in card["tier0"]["gates"]], list(GATES))
        for tier in ("tier1", "tier2", "tier3"):
            self.assertIn("tier0", card)
            self.assertIn(tier, card)

    @SETTINGS
    @given(suites(), st.data())
    def test_dropping_a_result_never_raises_the_correct_count(self, suite, data) -> None:
        cases, oracles, results = suite
        full = card_of(cases, oracles, results)
        drop = data.draw(st.integers(0, len(results) - 1))
        partial = card_of(cases, oracles, results[:drop] + results[drop + 1:])
        self.assertEqual(partial["tier1"]["correct_terminal_handling"]["n"], full["tier1"]["correct_terminal_handling"]["n"])
        self.assertLess(partial["tier1"]["correct_terminal_handling"]["k"], full["tier1"]["correct_terminal_handling"]["k"])
        self.assertFalse(partial["complete"])

    @SETTINGS
    @given(suites())
    def test_scoring_does_not_mutate_inputs_and_is_repeatable(self, suite) -> None:
        before = copy.deepcopy(suite)
        first = card_of(*suite)
        self.assertEqual(suite, before)
        self.assertEqual(card_of(*suite), first)


if __name__ == "__main__":
    unittest.main()
