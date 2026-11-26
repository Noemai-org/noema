"""A654 (and A667): the seeded J1 to J8 manifest for the front end (PERD §11.1; `benchmark/journeys/j1-j8.v1.json`, built by `benchmark/generator/journeys.py`).

The front end's Playwright journeys (REQ-UI-001: "J1-J8 flows") need the same eight situations every time: which generated cases, which principal says what, on which channel, with which fault plan. The manifest
names them by the generator's seed, so it is reproducible (REQ-BEN-001) and a stale committed copy fails here. It holds what the customer says and the world it is said in; it holds no expectation, no oracle
reference and no ground truth (INV-22), and every value is SYNTHETIC (AGENT-25)."""
import json
import re
import unittest
from pathlib import Path
from typing import Any, ClassVar

from benchmark.generator.generate import DEFAULT_SEED, Suite, generate
from benchmark.generator.journeys import JOURNEYS, build, render

ROOT = Path(__file__).resolve().parents[2]
COMMITTED = ROOT / "benchmark" / "journeys" / "j1-j8.v1.json"
FORBIDDEN = ("expected", "oracle", "oracle_ref", "correct_target", "true_state", "safety_gate_relevance", "severity_if_failed")


class Journeys(unittest.TestCase):
    suite: ClassVar[Suite]
    doc: ClassVar[dict[str, Any]]

    @classmethod
    def setUpClass(cls) -> None:
        cls.suite = generate(DEFAULT_SEED)
        cls.doc = build(cls.suite)

    def test_eight_journeys_in_order_each_with_its_stories_and_its_cases(self) -> None:
        rows = self.doc["journeys"]
        self.assertEqual([r["journey"] for r in rows], [f"J{n}" for n in range(1, 9)])
        for row in rows:
            self.assertTrue(row["title"].strip(), row["journey"])
            self.assertTrue(row["stories"], row["journey"])
            self.assertGreaterEqual(len(row["cases"]), 1, row["journey"])
            self.assertLessEqual(len(row["cases"]), 3, row["journey"])
            self.assertEqual(row["thin"], len(row["cases"]) == 1, row["journey"])  # a journey with one case says so: covered, but with no second case

    def test_the_stories_follow_the_perd_table(self) -> None:
        self.assertEqual({k: list(v[1]) for k, v in JOURNEYS.items()},
                         {"J1": ["US-01"], "J2": ["US-02"], "J3": ["US-03", "US-14"], "J4": ["US-06"], "J5": ["US-17"], "J6": ["US-18"], "J7": ["US-10", "US-22"], "J8": ["US-11"]})

    def test_every_case_is_a_real_generated_case_tagged_with_one_of_its_journeys_stories(self) -> None:
        tags = {e["case_id"]: set(e["covers"]) for e in self.suite.manifest["cases"]}
        for row in self.doc["journeys"]:
            for case in row["cases"]:
                self.assertIn(case["case_id"], self.suite.cases)
                self.assertTrue(tags[case["case_id"]] & set(row["stories"]), (row["journey"], case["case_id"]))

    def test_a_case_carries_what_the_customer_says_and_the_world_and_nothing_that_scores_it(self) -> None:
        for row in self.doc["journeys"]:
            for case in row["cases"]:
                self.assertEqual(set(case), {"case_id", "principal_id", "roles", "locale_mix", "channel", "utterances", "known_facts_fixture", "fault_plan", "events_timeline", "policy_version", "pack_version"})
                self.assertTrue(case["utterances"] and all(u["text"].strip() for u in case["utterances"]))
                self.assertEqual(len(case["roles"]), 1, case["case_id"])  # one session role of the four (ruling A1)
                self.assertIn(case["roles"][0], ("customer", "ops", "approver", "audit"), case["case_id"])
        text = json.dumps(self.doc)
        for word in FORBIDDEN:
            self.assertNotIn(f'"{word}"', text, word)

    def test_it_is_seeded_and_names_how_to_materialise_the_cases(self) -> None:
        self.assertEqual(self.doc["seed"], DEFAULT_SEED)
        self.assertEqual(self.doc["generator"], self.suite.manifest["generator"])
        self.assertIn("SYNTHETIC", self.doc["label"])
        self.assertIn(f"--seed {DEFAULT_SEED}", self.doc["materialise"])

    def test_no_digit_run_of_five_or_more_anywhere_except_the_public_seed(self) -> None:
        text = render(self.doc).replace(str(DEFAULT_SEED), "SEED")  # the generator's default seed is the one number the front end must be given
        self.assertIsNone(re.search(r"[0-9]{5,}", text))

    def test_the_one_thin_journey_is_the_one_whose_story_has_one_case_in_the_suite(self) -> None:
        thin = [r["journey"] for r in self.doc["journeys"] if r["thin"]]
        tagged = {s: sum(1 for e in self.suite.manifest["cases"] if s in e["covers"]) for s in ("US-18",)}
        self.assertEqual(tagged["US-18"], 1)  # the suite holds one US-18 case until the thin identifiers are strengthened (after G1)
        self.assertEqual(thin, ["J6"])

    def test_a_case_is_listed_in_one_journey_at_most(self) -> None:
        ids = [c["case_id"] for r in self.doc["journeys"] for c in r["cases"]]
        self.assertEqual(len(ids), len(set(ids)))

    def test_the_build_is_deterministic(self) -> None:
        self.assertEqual(render(build(generate(DEFAULT_SEED))), render(self.doc))


class Committed(unittest.TestCase):
    def test_the_committed_manifest_is_exactly_what_the_generator_builds_today(self) -> None:
        self.assertTrue(COMMITTED.is_file(), "run: python -m benchmark.generator.journeys --out benchmark/journeys/j1-j8.v1.json")
        self.assertEqual(COMMITTED.read_text(encoding="utf-8"), render(build(generate(DEFAULT_SEED))))


if __name__ == "__main__":
    unittest.main()
