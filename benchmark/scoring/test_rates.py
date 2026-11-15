"""Ruling A190 (interim, Founder-approved): scorecard v3 adds `exact_upper95`, the exact one-sided 95% upper bound on a failure rate. Written before `rates.py` and seen failing.

The bound is Clopper-Pearson, one-sided: the largest true failure rate u at which seeing k or fewer failures in n independent cases still has probability 0.05. For zero failures it is
1 - 0.05^(1/n), the statement SoT section 38.8 approximates by 3/n. It is meaningful only on a rate of failures, so a rate that is not one carries null (as `rule_of_three_upper` does).
No threshold is applied here (AGENT-16); `stats.py` is frozen by the 27 Oct model-selection record and is not edited."""
import math
import unittest

from benchmark.scoring import stats
from benchmark.scoring.rates import ALPHA, exact_upper95, make_rate


def cdf(k: int, n: int, p: float) -> float:
    return sum(math.comb(n, i) * p**i * (1 - p) ** (n - i) for i in range(k + 1))


class ExactUpper(unittest.TestCase):
    def test_zero_failures_is_the_closed_form(self) -> None:
        for n in (1, 2, 10, 40, 300, 1000):
            self.assertAlmostEqual(exact_upper95(0, n) or 0.0, 1 - ALPHA ** (1 / n), delta=1e-6, msg=str(n))

    def test_it_is_a_little_below_the_rule_of_three_for_zero_failures(self) -> None:
        for n in (10, 40, 300, 1000):
            self.assertLess(exact_upper95(0, n) or 1.0, 3 / n)
            self.assertGreater(exact_upper95(0, n) or 0.0, 0.85 * 3 / n)

    def test_it_is_the_probability_boundary_for_k_failures(self) -> None:
        for k, n in ((1, 10), (1, 300), (2, 40), (3, 100), (5, 300), (20, 300)):
            u = exact_upper95(k, n)
            assert u is not None
            self.assertAlmostEqual(cdf(k, n, u), ALPHA, delta=1e-5, msg=f"{k}/{n}")  # rounding up by up to 1e-6 moves the cdf by about n * 1e-6 * pmf

    def test_known_reference_values(self) -> None:
        self.assertAlmostEqual(exact_upper95(1, 10) or 0.0, 0.394163, places=5)  # one-sided 95% Clopper-Pearson, 1 of 10
        self.assertAlmostEqual(exact_upper95(0, 300) or 0.0, 0.009937, places=5)

    def test_it_is_at_least_the_observed_rate_and_grows_with_k(self) -> None:
        previous = -1.0
        for k in range(0, 31):
            u = exact_upper95(k, 300)
            assert u is not None
            self.assertGreaterEqual(u, k / 300)
            self.assertGreater(u, previous)
            previous = u

    def test_it_shrinks_as_n_grows_for_the_same_failures(self) -> None:
        self.assertGreater(exact_upper95(0, 100) or 0.0, exact_upper95(0, 300) or 0.0)
        self.assertGreater(exact_upper95(2, 100) or 0.0, exact_upper95(2, 300) or 0.0)

    def test_all_failures_is_one_and_no_cases_is_none(self) -> None:
        self.assertEqual(exact_upper95(5, 5), 1.0)
        self.assertIsNone(exact_upper95(0, 0))

    def test_it_is_rounded_up_to_six_places_so_a_reader_never_sees_it_understated(self) -> None:
        for k, n in ((0, 300), (1, 300), (3, 77), (2, 1000)):
            u = exact_upper95(k, n)
            assert u is not None
            self.assertEqual(u, math.ceil(round(u * 10**6, 3)) / 10**6)
            self.assertLessEqual(cdf(k, n, u), ALPHA + 1e-9)  # never below the true boundary

    def test_impossible_counts_are_refused(self) -> None:
        for k, n in ((-1, 5), (6, 5), (0, -1)):
            with self.assertRaises(ValueError):
                exact_upper95(k, n)


class MakeRate(unittest.TestCase):
    def test_it_is_the_frozen_rate_plus_exactly_one_member(self) -> None:
        for k, n, failure in ((0, 300, True), (3, 300, True), (3, 300, False), (0, 0, True), (0, 0, False)):
            old, new = stats.make_rate(k, n, failure=failure), make_rate(k, n, failure=failure)
            self.assertEqual({key: v for key, v in new.items() if key != "exact_upper95"}, old)
            self.assertEqual(set(new) - set(old), {"exact_upper95"})

    def test_a_failure_rate_carries_the_bound_and_any_other_rate_carries_null(self) -> None:
        self.assertEqual(make_rate(0, 300, failure=True)["exact_upper95"], exact_upper95(0, 300))
        self.assertEqual(make_rate(2, 300, failure=True)["exact_upper95"], exact_upper95(2, 300))
        self.assertIsNone(make_rate(2, 300)["exact_upper95"])
        self.assertIsNone(make_rate(0, 0, failure=True)["exact_upper95"])

    def test_the_frozen_stats_module_is_untouched(self) -> None:
        self.assertNotIn("exact_upper95", stats.make_rate(0, 10, failure=True))


if __name__ == "__main__":
    unittest.main()
