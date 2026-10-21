"""Bake-off metrics (T4 item 9a): accuracy, expected calibration error and reliability, perturbation flip rate, latency and cost."""
import unittest
from typing import Any

from hypothesis import given, settings, strategies as st

from benchmark.bakeoff.cases import BUCKETS, TASKS, generate
from benchmark.bakeoff.metrics import BINS, ece_and_bins, summarise
from benchmark.bakeoff.testdata import make_run
from benchmark.testing import examples

CASES = generate(seed=5, per_cell=8)


def cell(summary: dict[str, Any], task: str, bucket: str) -> dict[str, Any]:
    return summary[task][bucket]  # type: ignore[no-any-return]


class EceTest(unittest.TestCase):
    def test_a_perfect_and_confident_model_has_zero_error(self) -> None:
        self.assertEqual(ece_and_bins([(1.0, True)] * 10)[0], 0.0)

    def test_a_confidently_wrong_model_has_error_one(self) -> None:
        self.assertEqual(ece_and_bins([(1.0, False)] * 10)[0], 1.0)

    def test_a_calibrated_model_has_zero_error(self) -> None:
        sample = [(0.8, True)] * 8 + [(0.8, False)] * 2
        self.assertAlmostEqual(ece_and_bins(sample)[0] or 0.0, 0.0, places=6)

    def test_known_mixture(self) -> None:
        # bin 0.9..1.0: 4 samples, confidence 0.95, accuracy 0.5 -> |0.5-0.95| = 0.45 weighted 4/6;
        # bin 0.5..0.6: 2 samples, confidence 0.55, accuracy 1.0 -> 0.45 weighted 2/6
        sample = [(0.95, True), (0.95, True), (0.95, False), (0.95, False), (0.55, True), (0.55, True)]
        self.assertAlmostEqual(ece_and_bins(sample)[0] or 0.0, 0.45, places=6)

    def test_no_samples_has_no_error(self) -> None:
        ece, bins = ece_and_bins([])
        self.assertIsNone(ece)
        self.assertEqual(len(bins), BINS)
        self.assertTrue(all(b["n"] == 0 for b in bins))

    def test_a_confidence_of_exactly_one_lands_in_the_last_bin(self) -> None:
        bins = ece_and_bins([(1.0, True)])[1]
        self.assertEqual([b["n"] for b in bins], [0] * (BINS - 1) + [1])

    @settings(max_examples=examples(80), deadline=None)
    @given(st.lists(st.tuples(st.integers(0, 1000).map(lambda v: v / 1000), st.booleans()), max_size=200))
    def test_bounds_and_counts(self, sample: list[tuple[float, bool]]) -> None:
        ece, bins = ece_and_bins(sample)
        self.assertEqual(sum(b["n"] for b in bins), len(sample))
        if sample:
            self.assertIsNotNone(ece)
            self.assertGreaterEqual(ece or 0.0, 0.0)
            self.assertLessEqual(ece or 0.0, 1.0)
        for b in bins:
            if b["n"]:
                self.assertGreaterEqual(b["mean_confidence"], b["lo"] - 1e-9)
                self.assertLessEqual(b["mean_confidence"], b["hi"] + 1e-9)


class SummariseTest(unittest.TestCase):
    def test_every_task_bucket_and_the_total_are_reported(self) -> None:
        summary = summarise(CASES, make_run(CASES))
        for task in TASKS:
            self.assertEqual(set(summary[task]), {*BUCKETS, "ALL"})
        self.assertEqual(cell(summary, "INTENT_NODE", "ALL")["accuracy"]["n"], 4 * 8)

    def test_accuracy_is_k_over_all_cases_and_has_a_wilson_interval(self) -> None:
        run = make_run(CASES, correct=lambda c: c["case_id"].endswith(("1", "2", "3", "4", "5")))
        c = cell(summarise(CASES, run), "LOCALE", "ALL")
        self.assertEqual(c["accuracy"]["n"], 32)
        self.assertEqual(len(c["accuracy"]["wilson95"]), 2)

    def test_an_unanswered_case_counts_as_wrong_not_as_missing(self) -> None:
        run = make_run(CASES)
        run["results"] = [r for r in run["results"] if r["case_id"] != CASES[0]["case_id"]]
        c = cell(summarise(CASES, run), CASES[0]["task"], "ALL")
        self.assertEqual(c["accuracy"]["n"], 32)
        self.assertEqual(c["accuracy"]["k"], 31)
        self.assertEqual((c["coverage"]["k"], c["coverage"]["n"]), (31, 32))

    def test_an_abstention_is_wrong_unless_it_was_the_truth(self) -> None:
        none_cases = [c for c in CASES if c["expected"] == "none"]
        self.assertTrue(none_cases)
        run = make_run(CASES)
        for r in run["results"]:
            if r["variant"] == "BASE":
                r["selected"] = "none" if r["case_id"] in {c["case_id"] for c in CASES if c["question"]["type"] == "choice" and "none" in c["question"]["options"]} else r["selected"]
        summary = summarise(CASES, run)
        c = cell(summary, "INTENT_NODE", "ALL")
        right = sum(1 for x in CASES if x["task"] == "INTENT_NODE" and x["expected"] == "none")
        self.assertEqual(c["accuracy"]["k"], right)
        self.assertEqual(c["abstained"]["n"], 32)

    def test_ece_uses_answered_cases_and_the_confidence_given(self) -> None:
        sure_and_right = cell(summarise(CASES, make_run(CASES, confidence=lambda c, ok: 1.0)), "LOCALE", "ALL")
        self.assertEqual(sure_and_right["ece"], 0.0)
        sure_and_wrong = cell(summarise(CASES, make_run(CASES, correct=lambda c: False, confidence=lambda c, ok: 1.0)), "LOCALE", "ALL")
        self.assertEqual(sure_and_wrong["ece"], 1.0)
        self.assertEqual(sure_and_wrong["overconfident_wrong"]["k"], 32)

    def test_flip_rate_counts_cases_whose_answer_changes_under_a_meaning_preserving_rewrite(self) -> None:
        none = cell(summarise(CASES, make_run(CASES)), "ENUM_SLOT", "ALL")["flip_rate"]
        self.assertEqual((none["k"], none["n"]), (0, 32))
        some = cell(summarise(CASES, make_run(CASES, flip=lambda c, i: i == 0 and c["case_id"].endswith("3"))), "ENUM_SLOT", "ALL")["flip_rate"]
        self.assertEqual(some["n"], 32)
        self.assertGreater(some["k"], 0)
        self.assertLess(some["k"], some["n"])

    def test_latency_is_measured_on_base_calls_only(self) -> None:
        run = make_run(CASES, latency=lambda c: 100)
        for r in run["results"]:
            if r["variant"] != "BASE":
                r["latency_ms"] = 99999
        lat = cell(summarise(CASES, run), "LOCALE", "ALL")["latency"]
        self.assertEqual((lat["n"], lat["p50_ms"], lat["p95_ms"]), (32, 100, 100))

    def test_cost_per_correct_outcome_includes_every_call(self) -> None:
        run = make_run(CASES, cost=10)
        c = cell(summarise(CASES, run), "LOCALE", "ALL")
        calls = 32 + sum(len(x["perturbations"]) for x in CASES if x["task"] == "LOCALE")
        self.assertEqual(c["cost_usd_micros_total"], calls * 10)
        self.assertEqual(c["cost_usd_micros_per_correct"], calls * 10 // 32)

    def test_cost_per_correct_is_null_when_nothing_was_right(self) -> None:
        c = cell(summarise(CASES, make_run(CASES, correct=lambda c: False)), "LOCALE", "ALL")
        self.assertIsNone(c["cost_usd_micros_per_correct"])

    def test_a_run_for_a_different_case_set_is_refused(self) -> None:
        run = make_run(CASES)
        with self.assertRaises(ValueError):
            summarise(generate(seed=6, per_cell=8), run)

    def test_a_not_run_run_has_no_summary(self) -> None:
        from benchmark.bakeoff.testdata import not_run
        with self.assertRaises(ValueError):
            summarise(CASES, not_run(CASES))

    def test_the_run_and_cases_are_not_mutated(self) -> None:
        import copy
        run = make_run(CASES)
        before = copy.deepcopy((CASES, run))
        summarise(CASES, run)
        self.assertEqual((CASES, run), before)


if __name__ == "__main__":
    unittest.main()
