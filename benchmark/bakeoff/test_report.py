"""The comparison and the per-task table (T4 item 9a): LLM against decide() backed by OpenAI Decisions. A comparison, not a decision."""
import unittest
import xml.etree.ElementTree as ET
from typing import Any

from benchmark.bakeoff.cases import BUCKETS, TASKS, generate
from benchmark.bakeoff.report import compare, reliability_svg, render_markdown
from benchmark.bakeoff.testdata import make_run, not_run

CASES = generate(seed=9, per_cell=10)
LLM, DECIDE = "LLM", "DECIDE_OPENAI_DECISIONS"


def odd(case: dict[str, Any]) -> bool:
    return int(case["case_id"].rsplit("-", 1)[1]) % 2 == 1


def llm_run() -> dict[str, Any]:
    return make_run(CASES, LLM, correct=odd, confidence=lambda c, ok: 0.9)


def better_decide() -> dict[str, Any]:
    return make_run(CASES, DECIDE, correct=lambda c: True, confidence=lambda c, ok: 1.0)


class RecommendationTest(unittest.TestCase):
    def test_not_run_decide_is_reported_not_run_for_every_task(self) -> None:
        result = compare(CASES, [llm_run(), not_run(CASES)])
        for task in TASKS:
            self.assertEqual(result["tasks"][task]["recommendation"], "NOT_RUN")
            self.assertIn("no access: no API key", result["tasks"][task]["reason"])
            self.assertIsNone(result["tasks"][task]["buckets"]["ALL"][DECIDE])
            self.assertIsNotNone(result["tasks"][task]["buckets"]["ALL"][LLM])

    def test_both_not_run(self) -> None:
        result = compare(CASES, [not_run(CASES, LLM, "no access: no key"), not_run(CASES)])
        self.assertTrue(all(t["recommendation"] == "NOT_RUN" for t in result["tasks"].values()))

    def test_decide_better_everywhere_is_eligible_for_founder_review(self) -> None:
        result = compare(CASES, [llm_run(), better_decide()])
        for task in TASKS:
            self.assertEqual(result["tasks"][task]["recommendation"], "ELIGIBLE_FOR_FOUNDER_REVIEW", task)
            for bucket in BUCKETS:
                cmp_ = result["tasks"][task]["comparison"][bucket]
                self.assertGreater(cmp_["accuracy_difference"], 0)
                self.assertLessEqual(cmp_["ece_difference"], 0)

    def test_better_accuracy_but_worse_calibration_in_one_bucket_keeps_the_llm(self) -> None:
        def confidence(case: dict[str, Any], ok: bool) -> float:
            return 0.5 if case["language_bucket"] == "pcm" else 1.0

        run = make_run(CASES, DECIDE, correct=lambda c: True, confidence=confidence)
        result = compare(CASES, [make_run(CASES, LLM, correct=odd, confidence=lambda c, ok: 0.5 if ok else 0.5), run])
        # in pcm decide is right 100% of the time at confidence 0.5 (ECE 0.5); the LLM is right 50% at confidence 0.5 (ECE 0)
        self.assertEqual(result["tasks"]["LOCALE"]["recommendation"], "KEEP_LLM")
        self.assertIn("pcm", result["tasks"]["LOCALE"]["reason"])

    def test_equal_accuracy_is_not_better(self) -> None:
        result = compare(CASES, [llm_run(), make_run(CASES, DECIDE, correct=odd, confidence=lambda c, ok: 0.9)])
        self.assertTrue(all(t["recommendation"] == "KEEP_LLM" for t in result["tasks"].values()))

    def test_worse_accuracy_keeps_the_llm(self) -> None:
        result = compare(CASES, [llm_run(), make_run(CASES, DECIDE, correct=lambda c: False, confidence=lambda c, ok: 0.9)])
        self.assertTrue(all(t["recommendation"] == "KEEP_LLM" for t in result["tasks"].values()))

    def test_significance_means_disjoint_wilson_intervals(self) -> None:
        result = compare(CASES, [make_run(CASES, LLM, correct=lambda c: False), better_decide()])
        self.assertTrue(result["tasks"]["LOCALE"]["comparison"]["ALL"]["significant"])
        close = compare(CASES, [llm_run(), make_run(CASES, DECIDE, correct=lambda c: odd(c) or c["case_id"].endswith("2"), confidence=lambda c, ok: 0.9)])
        self.assertFalse(close["tasks"]["LOCALE"]["comparison"]["ALL"]["significant"])

    def test_both_providers_must_be_given_once_each(self) -> None:
        for runs in ([llm_run()], [llm_run(), llm_run()], [better_decide(), better_decide()]):
            with self.assertRaises(ValueError):
                compare(CASES, runs)

    def test_a_run_for_another_case_set_is_refused(self) -> None:
        with self.assertRaises(ValueError):
            compare(generate(seed=10, per_cell=10), [llm_run(), not_run(CASES)])


class MarkdownTest(unittest.TestCase):
    def setUp(self) -> None:
        self.text = render_markdown(compare(CASES, [llm_run(), not_run(CASES)]))
        self.ran = render_markdown(compare(CASES, [llm_run(), better_decide()]))

    def test_it_says_it_decides_nothing(self) -> None:
        self.assertIn("changes no threshold", self.text)
        self.assertIn("The founders adopt providers", self.text)

    def test_not_run_is_stated_with_the_reason_and_nothing_is_shown_for_it(self) -> None:
        self.assertIn("not run: no access: no API key", self.text)
        self.assertNotIn("DECIDE_OPENAI_DECISIONS accuracy", self.text)

    def test_per_task_recommendation_table_comes_first_and_has_all_six_tasks(self) -> None:
        head = self.ran.split("## ")[2]
        self.assertTrue(head.startswith("Per-task recommendation"))
        for task in TASKS:
            self.assertIn(task, head)

    def test_every_bucket_is_reported_for_every_task(self) -> None:
        for bucket in BUCKETS:
            self.assertGreaterEqual(self.ran.count(f"| {bucket} |"), len(TASKS))

    def test_every_rate_shows_its_n(self) -> None:
        rows = [line for line in self.ran.splitlines() if line.startswith("| ") and "Wilson" in line]
        self.assertTrue(rows)
        for line in rows:
            self.assertRegex(line, r"\d+ of \d+")

    def test_latency_is_labelled_as_lagos_region_only_when_it_is(self) -> None:
        self.assertIn("Lagos-region measurement: yes", self.ran)
        elsewhere = render_markdown(compare(CASES, [make_run(CASES, LLM, region="eu-west"), not_run(CASES)]))
        self.assertIn("Lagos-region measurement: no (runner region eu-west)", elsewhere)

    def test_an_unpinned_model_is_flagged(self) -> None:
        run = make_run(CASES, LLM)
        run["model"]["version"] = None
        self.assertIn("NOT PINNED", render_markdown(compare(CASES, [run, not_run(CASES)])))

    def test_no_verdict_words(self) -> None:
        for word in ("PASS", "winner", "wins", "meets the target", "adopt DECIDE"):
            self.assertNotIn(word, self.ran)

    def test_deterministic(self) -> None:
        self.assertEqual(self.ran, render_markdown(compare(CASES, [llm_run(), better_decide()])))


class SvgTest(unittest.TestCase):
    def test_a_well_formed_diagram_with_a_curve_per_run_provider(self) -> None:
        svg = reliability_svg(compare(CASES, [llm_run(), better_decide()]), "INTENT_NODE")
        root = ET.fromstring(svg)
        self.assertTrue(root.tag.endswith("svg"))
        titles = [e.text for e in root.iter() if e.tag.endswith("title")]
        self.assertIn("INTENT_NODE reliability", titles)
        self.assertEqual(sum(1 for e in root.iter() if e.tag.endswith("polyline") or e.tag.endswith("circle")) > 0, True)
        self.assertNotIn("href", svg)
        self.assertNotIn("<script", svg)

    def test_a_not_run_provider_draws_no_curve(self) -> None:
        both = reliability_svg(compare(CASES, [llm_run(), better_decide()]), "LOCALE")
        one = reliability_svg(compare(CASES, [llm_run(), not_run(CASES)]), "LOCALE")
        self.assertGreater(both.count("<circle"), one.count("<circle"))

    def test_deterministic(self) -> None:
        c = compare(CASES, [llm_run(), better_decide()])
        self.assertEqual(reliability_svg(c, "LOCALE"), reliability_svg(c, "LOCALE"))


if __name__ == "__main__":
    unittest.main()
