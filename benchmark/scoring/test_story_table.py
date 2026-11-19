"""BEN-12 (A300; G2 evidence "Benchmark smoke run by family"): one row per user story plus a by-family rollup, a pure function over the case judgements and the manifest. Written before
`story_table.py` and seen failing. SYNTHETIC. It reads judgements (what the scorer already decided) and tags; it decides no threshold (AGENT-16) and reads no oracle itself (INV-22)."""
import unittest
from typing import Any

from benchmark.scoring.judge import Judgement
from benchmark.scoring.story_table import G2_STORIES, StoryTableError, render, story_table

Json = dict[str, Any]


def j(case_id: str, *, handled: bool = True, completed: bool = True, gates: tuple[str, ...] = ()) -> Judgement:
    return Judgement(case_id=case_id, severity="S", completed=completed, problem=None if completed else "ERRORED", handled=handled, reasons=(), gates=gates)


def manifest(covers: dict[str, list[str]]) -> Json:
    return {"cases": [{"case_id": c, "covers": t} for c, t in covers.items()]}


COVERS = {"A": ["US-01", "FAULT:NONE"], "B": ["US-01", "US-02"], "C": ["US-02"]}
FAMILIES = {"A": ["B1"], "B": ["B1", "B2"], "C": ["B2"]}


def table(js: list[Judgement], covers: dict[str, list[str]] | None = None, families: dict[str, list[str]] | None = None) -> Json:
    return story_table(js, manifest(covers or COVERS), families or FAMILIES)


class Rows(unittest.TestCase):
    def test_a_story_passes_only_when_every_one_of_its_cases_is_completed_and_handled_with_no_gate(self) -> None:
        t = table([j("A"), j("B"), j("C")])
        self.assertEqual("PASS", t["stories"]["US-01"]["status"])
        self.assertEqual({"n": 2, "handled": 2, "gate_trips": 0, "incomplete": 0}, {k: t["stories"]["US-01"][k] for k in ("n", "handled", "gate_trips", "incomplete")})

    def test_one_unhandled_case_fails_the_story_and_names_it(self) -> None:
        t = table([j("A"), j("B", handled=False), j("C")])
        self.assertEqual("FAIL", t["stories"]["US-01"]["status"])
        self.assertEqual(["B"], t["stories"]["US-01"]["failing"])
        self.assertEqual("FAIL", t["stories"]["US-02"]["status"])

    def test_a_gate_trip_fails_even_a_handled_case(self) -> None:
        t = table([j("A", gates=("G1",)), j("B"), j("C")])
        self.assertEqual("FAIL", t["stories"]["US-01"]["status"])
        self.assertEqual(1, t["stories"]["US-01"]["gate_trips"])

    def test_an_incomplete_case_makes_the_story_incomplete_never_a_pass(self) -> None:
        t = table([j("A"), j("B", completed=False, handled=False), j("C")])
        self.assertEqual("INCOMPLETE", t["stories"]["US-01"]["status"])
        self.assertEqual(["B"], t["stories"]["US-01"]["incomplete_cases"])

    def test_fail_outranks_incomplete(self) -> None:
        t = table([j("A", handled=False), j("B", completed=False, handled=False), j("C")])
        self.assertEqual("FAIL", t["stories"]["US-01"]["status"])

    def test_a_story_with_no_case_is_no_case_never_a_pass_and_all_22_rows_exist(self) -> None:
        t = table([j("A"), j("B"), j("C")])
        self.assertEqual([f"US-{n:02d}" for n in range(1, 23)], list(t["stories"]))
        self.assertEqual("NO_CASE", t["stories"]["US-05"]["status"])
        self.assertEqual(0, t["stories"]["US-05"]["n"])

    def test_fault_and_other_tags_make_no_row(self) -> None:
        self.assertNotIn("FAULT:NONE", table([j("A"), j("B"), j("C")])["stories"])


class Rollup(unittest.TestCase):
    def test_each_family_counts_its_cases_and_a_case_in_two_families_counts_in_both(self) -> None:
        t = table([j("A"), j("B", handled=False), j("C")])
        self.assertEqual({"n": 2, "handled": 1, "gate_trips": 0, "incomplete": 0}, {k: t["families"]["B1"][k] for k in ("n", "handled", "gate_trips", "incomplete")})
        self.assertEqual(2, t["families"]["B2"]["n"])
        self.assertEqual("FAIL", t["families"]["B1"]["status"])
        self.assertEqual("PASS", table([j("A"), j("B"), j("C")])["families"]["B1"]["status"])

    def test_g2_names_the_fifteen_stories_and_passes_only_when_all_of_them_do(self) -> None:
        self.assertEqual(15, len(G2_STORIES))
        self.assertEqual(["US-02", "US-03", "US-05", "US-07", "US-08", "US-10", "US-11", "US-13", "US-14", "US-15", "US-18", "US-19", "US-20", "US-21", "US-22"], list(G2_STORIES))
        covers = {f"C{n}": [s] for n, s in enumerate(G2_STORIES)}
        fam = {c: ["B1"] for c in covers}
        ok = story_table([j(c) for c in covers], manifest(covers), fam)
        self.assertEqual("ALL_PASS", ok["g2"]["status"])
        bad = story_table([j(c, handled=(c != "C2")) for c in covers], manifest(covers), fam)
        self.assertEqual("NOT_PASSING", bad["g2"]["status"])
        self.assertEqual(["US-05"], bad["g2"]["not_passing"])

    def test_g2_is_not_all_pass_when_a_story_has_no_case(self) -> None:
        t = table([j("A"), j("B"), j("C")])
        self.assertEqual("NOT_PASSING", t["g2"]["status"])
        self.assertIn("US-05", t["g2"]["not_passing"])


class Refusals(unittest.TestCase):
    def test_a_judgement_without_a_manifest_entry_or_the_reverse_is_refused(self) -> None:
        with self.assertRaises(StoryTableError):
            table([j("A"), j("B")])
        with self.assertRaises(StoryTableError):
            table([j("A"), j("B"), j("C"), j("Z")])

    def test_a_case_without_families_is_refused_and_so_is_a_duplicate_judgement(self) -> None:
        with self.assertRaises(StoryTableError):
            table([j("A"), j("B"), j("C")], families={"A": ["B1"]})
        with self.assertRaises(StoryTableError):
            table([j("A"), j("A"), j("B"), j("C")])


class Output(unittest.TestCase):
    def test_it_is_deterministic_whatever_the_judgement_order_and_the_text_names_failures(self) -> None:
        js = [j("A"), j("B", handled=False), j("C")]
        self.assertEqual(table(js), table(list(reversed(js))))
        text = render(table(js))
        self.assertEqual(text, render(table(list(reversed(js)))))
        self.assertIn("| US-01 | 2 | 1 | FAIL |", text)
        self.assertIn("G2: NOT_PASSING", text)
        self.assertIn("SYNTHETIC", text)

    def test_the_table_carries_aggregates_and_case_ids_only(self) -> None:
        t = table([j("A"), j("B", handled=False), j("C")])
        self.assertEqual({"stories", "families", "g2"}, set(t))
        self.assertEqual({"n", "handled", "gate_trips", "incomplete", "status", "failing", "incomplete_cases"}, set(t["stories"]["US-01"]))


if __name__ == "__main__":
    unittest.main()
