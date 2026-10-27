"""The budget guard of the model-selection freeze ($25 total, pilot $2; benchmark/decisions/2026-10-27-model-selection.md)."""
import unittest

from hypothesis import given, settings, strategies as st

from benchmark.bakeoff.budget import BudgetExceeded, BudgetGuard, projected_micros
from benchmark.testing import examples


class Projection(unittest.TestCase):
    def test_it_is_the_worst_case_input_bytes_plus_max_output_rounded_up(self) -> None:
        # 2,000 bytes at $1/M in and 4,000 tokens at $5/M out: 2,000 + 20,000 micro-dollars
        self.assertEqual(projected_micros(2_000, 4_000, 1_000_000, 5_000_000), 22_000)
        self.assertEqual(projected_micros(1, 1, 100_000, 500_000), 1)  # 0.6 rounds up to 1, never down to 0
        self.assertEqual(projected_micros(0, 0, 100_000, 500_000), 0)

    def test_a_negative_or_float_input_is_refused(self) -> None:
        for args in ((-1, 1, 1, 1), (1, -1, 1, 1), (1, 1, -1, 1), (1, 1, 1, -1)):
            with self.assertRaises(ValueError):
                projected_micros(*args)
        with self.assertRaises(TypeError):
            projected_micros(1.5, 1, 1, 1)  # type: ignore[arg-type]


class Guard(unittest.TestCase):
    def test_a_request_that_fits_is_authorised_and_settled_at_its_actual_cost(self) -> None:
        guard = BudgetGuard(limit_micros=100, spent_micros=10)
        guard.authorize(40)
        guard.settle(actual_micros=25, projected_micros=40)
        self.assertEqual((guard.spent_micros, guard.stopped), (35, False))

    def test_a_request_that_would_exceed_the_remaining_budget_is_not_sent_and_stops_the_run(self) -> None:
        guard = BudgetGuard(limit_micros=100, spent_micros=70)
        with self.assertRaises(BudgetExceeded):
            guard.authorize(31)
        self.assertTrue(guard.stopped)
        self.assertEqual(guard.spent_micros, 70)

    def test_spent_plus_projected_equal_to_the_limit_fits(self) -> None:
        guard = BudgetGuard(limit_micros=100, spent_micros=70)
        guard.authorize(30)

    def test_once_stopped_even_a_cheaper_request_is_refused(self) -> None:
        guard = BudgetGuard(limit_micros=100, spent_micros=70)
        with self.assertRaises(BudgetExceeded):
            guard.authorize(31)
        with self.assertRaises(BudgetExceeded):
            guard.authorize(1)

    def test_a_call_with_no_usage_report_is_counted_at_its_projected_cost(self) -> None:
        guard = BudgetGuard(limit_micros=100)
        guard.authorize(40)
        guard.settle(actual_micros=None, projected_micros=40)
        self.assertEqual(guard.spent_micros, 40)

    def test_an_actual_cost_above_the_projection_is_still_counted(self) -> None:
        guard = BudgetGuard(limit_micros=100)
        guard.authorize(10)
        guard.settle(actual_micros=30, projected_micros=10)
        self.assertEqual(guard.spent_micros, 30)

    def test_prior_spending_counts_against_the_cap(self) -> None:
        guard = BudgetGuard(limit_micros=25_000_000, spent_micros=24_990_000)
        with self.assertRaises(BudgetExceeded):
            guard.authorize(20_000)

    def test_a_prior_spend_above_the_limit_stops_everything(self) -> None:
        guard = BudgetGuard(limit_micros=100, spent_micros=101)
        with self.assertRaises(BudgetExceeded):
            guard.authorize(0)

    def test_bad_construction_is_refused(self) -> None:
        for kwargs in ({"limit_micros": -1}, {"limit_micros": 1, "spent_micros": -1}):
            with self.assertRaises(ValueError):
                BudgetGuard(**kwargs)  # type: ignore[arg-type]

    @settings(max_examples=examples(200), deadline=None)
    @given(st.integers(0, 1000), st.lists(st.tuples(st.integers(0, 300), st.integers(0, 300)), max_size=40))
    def test_the_guard_never_lets_spending_pass_the_limit_when_actual_stays_within_the_projection(self, limit: int, calls: list[tuple[int, int]]) -> None:
        guard = BudgetGuard(limit_micros=limit)
        for projected, actual in calls:
            try:
                guard.authorize(projected)
            except BudgetExceeded:
                break
            guard.settle(actual_micros=min(actual, projected), projected_micros=projected)
            self.assertLessEqual(guard.spent_micros, limit)


if __name__ == "__main__":
    unittest.main()
