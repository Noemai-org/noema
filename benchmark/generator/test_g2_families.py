"""BEN-12 (A771, A637): the G2 family case fixtures. For each of the fifteen user stories G2 names, the generated cases tagged with it and what a run needs to load each (its facts fixture, fault plan and timeline), built from the
generated suite of one seed, so the G2 table (`benchmark/scoring/story_table.py`) has its cases named before the run. No expectation, no oracle, no ground truth (INV-22). SYNTHETIC (AGENT-25). Seen failing first: no module."""
import json
import unittest
from pathlib import Path

from benchmark.generator.g2_families import CASE_MEMBERS, G2_STORIES, build, render
from benchmark.generator.generate import DEFAULT_SEED, generate
from benchmark.scoring.story_table import G2_STORIES as TABLE_STORIES

ROOT = Path(__file__).resolve().parents[2]
COMMITTED = ROOT / "benchmark" / "g2" / "g2-families.v1.json"
FORBIDDEN = ("expected", "oracle", "ground_truth", "verdict", "severity", "must_not", "reference")
_cache: dict[str, object] = {}


def built() -> dict[str, object]:
    if "doc" not in _cache:
        _cache["doc"] = build(generate(DEFAULT_SEED))
    return _cache["doc"]  # type: ignore[return-value]


class Families(unittest.TestCase):
    def test_the_stories_are_exactly_the_fifteen_the_g2_table_names(self) -> None:
        self.assertEqual(G2_STORIES, TABLE_STORIES)
        self.assertEqual([r["story"] for r in built()["stories"]], list(TABLE_STORIES))  # type: ignore[index, union-attr]

    def test_every_g2_story_has_at_least_one_generated_case_and_each_case_carries_its_story_tag(self) -> None:
        suite = generate(DEFAULT_SEED)
        tags = {e["case_id"]: set(e["covers"]) for e in suite.manifest["cases"]}
        for row in built()["stories"]:  # type: ignore[union-attr]
            self.assertTrue(row["cases"], row["story"])
            for case in row["cases"]:
                self.assertIn(row["story"], tags[case["case_id"]], case["case_id"])

    def test_a_case_holds_what_a_run_loads_and_nothing_that_scores_it_inv22(self) -> None:
        for row in built()["stories"]:  # type: ignore[union-attr]
            for case in row["cases"]:
                self.assertEqual(tuple(sorted(case)), tuple(sorted(CASE_MEMBERS)), case["case_id"])
        text = render(built()).lower()  # type: ignore[arg-type]
        for word in FORBIDDEN:
            self.assertNotIn(f'"{word}"', text, word)

    def test_the_cases_of_a_story_are_listed_in_id_order_without_repeats(self) -> None:
        for row in built()["stories"]:  # type: ignore[union-attr]
            ids = [c["case_id"] for c in row["cases"]]
            self.assertEqual(ids, sorted(set(ids)), row["story"])

    def test_the_same_seed_gives_the_same_bytes_and_the_committed_copy_is_not_stale(self) -> None:
        self.assertEqual(render(built()), render(build(generate(DEFAULT_SEED))))  # type: ignore[arg-type]
        self.assertEqual(COMMITTED.read_text(encoding="utf-8"), render(built()))  # type: ignore[arg-type]
        self.assertTrue(json.loads(COMMITTED.read_text(encoding="utf-8"))["label"].startswith("SYNTHETIC"))


if __name__ == "__main__":
    unittest.main()
