"""Wilson interval, rule of three and percentiles (REQ-BEN-004; SoT §38.8)."""
import math
import unittest

from hypothesis import given, strategies as st

from benchmark.scoring.stats import make_rate, percentile, wilson_interval


def interval(k: int, n: int) -> tuple[float, float]:
    found = wilson_interval(k, n)
    assert found is not None
    return found


def nearest(values: list[int], p: int) -> int:
    found = percentile(values, p)
    assert found is not None
    return found


class WilsonTest(unittest.TestCase):
    def test_known_values(self) -> None:
        lo, hi = interval(0, 10)
        self.assertEqual(lo, 0.0)
        self.assertAlmostEqual(hi, 0.277533, places=5)  # z^2 / (n + z^2)
        lo, hi = interval(10, 10)
        self.assertAlmostEqual(lo, 0.722467, places=5)
        self.assertEqual(hi, 1.0)
        lo, hi = interval(5, 10)
        self.assertAlmostEqual(lo, 0.236593, places=5)
        self.assertAlmostEqual(hi, 0.763407, places=5)

    def test_empty_has_no_interval(self) -> None:
        self.assertIsNone(wilson_interval(0, 0))

    def test_rejects_impossible_counts(self) -> None:
        for k, n in ((-1, 5), (6, 5), (1, -1)):
            with self.assertRaises(ValueError):
                wilson_interval(k, n)

    @given(st.integers(0, 5000).flatmap(lambda n: st.tuples(st.integers(0, n), st.just(n))))
    def test_interval_contains_the_rate_and_stays_in_bounds(self, kn: tuple[int, int]) -> None:
        k, n = kn
        if n == 0:
            self.assertIsNone(wilson_interval(k, n))
            return
        lo, hi = wilson_interval(k, n) or (math.nan, math.nan)
        self.assertGreaterEqual(lo, 0.0)
        self.assertLessEqual(hi, 1.0)
        self.assertLessEqual(lo, k / n + 1e-6)
        self.assertGreaterEqual(hi, k / n - 1e-6)
        self.assertLessEqual(lo, hi)

    @given(st.integers(1, 2000))
    def test_interval_is_monotone_in_k(self, n: int) -> None:
        previous = (-1.0, -1.0)
        for k in range(0, min(n, 40) + 1):
            current = wilson_interval(k, n) or (0.0, 0.0)
            self.assertGreaterEqual(current[0], previous[0])
            self.assertGreaterEqual(current[1], previous[1])
            previous = current


class RateTest(unittest.TestCase):
    def test_rule_of_three_only_on_a_failure_rate_with_zero_failures(self) -> None:
        self.assertEqual(make_rate(0, 300, failure=True)["rule_of_three_upper"], 0.01)
        self.assertIsNone(make_rate(0, 300, failure=False)["rule_of_three_upper"])
        self.assertIsNone(make_rate(1, 300, failure=True)["rule_of_three_upper"])
        self.assertIsNone(make_rate(0, 0, failure=True)["rule_of_three_upper"])

    def test_zero_failures_in_320_is_below_about_one_percent(self) -> None:
        # PERD §48.6: 3/320 is 0.94%
        self.assertEqual(make_rate(0, 320, failure=True)["rule_of_three_upper"], 0.009375)

    def test_empty_rate_is_null_not_zero(self) -> None:
        self.assertEqual(make_rate(0, 0), {"k": 0, "n": 0, "rate": None, "wilson95": None, "rule_of_three_upper": None})

    def test_rate_rejects_k_above_n(self) -> None:
        with self.assertRaises(ValueError):
            make_rate(3, 2)

    @given(st.integers(0, 1000).flatmap(lambda n: st.tuples(st.integers(0, n), st.just(n))))
    def test_rate_is_k_over_n(self, kn: tuple[int, int]) -> None:
        k, n = kn
        rate = make_rate(k, n)
        self.assertEqual((rate["k"], rate["n"]), (k, n))
        if n:
            self.assertAlmostEqual(rate["rate"], k / n, delta=1e-6)


class PercentileTest(unittest.TestCase):
    def test_nearest_rank(self) -> None:
        values = list(range(1, 101))
        self.assertEqual(percentile(values, 50), 50)
        self.assertEqual(percentile(values, 95), 95)
        self.assertEqual(percentile(values, 99), 99)
        self.assertEqual(percentile([7], 99), 7)
        self.assertIsNone(percentile([], 50))

    @given(st.lists(st.integers(0, 10**6), min_size=1, max_size=200), st.permutations([50, 95, 99]))
    def test_order_independent_and_a_member(self, values: list[int], ps: list[int]) -> None:
        for p in ps:
            self.assertEqual(percentile(values, p), percentile(sorted(values, reverse=True), p))
            self.assertIn(percentile(values, p), values)
        self.assertLessEqual(nearest(values, 50), nearest(values, 95))
        self.assertLessEqual(nearest(values, 95), nearest(values, 99))


if __name__ == "__main__":
    unittest.main()
