"""BEN-06, BEN-07, BEN-08, BEN-09 ("Cases validate; each is linked to its EC or US ID"; REQ-BEN-001): the per-ID coverage of each task's identifiers, read from the generated suite's manifest. Written
before `task_coverage.py` and seen failing. SYNTHETIC cases; no oracle is read (the manifest carries tags and case ids, never an expectation)."""
import io
import unittest
from contextlib import redirect_stdout
from typing import Any

from benchmark.generator.catalogue import EDGE_CASES, SUPPORTING_STORIES, USER_STORIES
from benchmark.generator.generate import generate
from benchmark.generator.task_coverage import TASKS, coverage, main, render, report, selectors

Json = dict[str, Any]


def manifest(covers: dict[str, list[str]]) -> Json:
    """A tiny manifest: each case id with its coverage tags."""
    return {"cases": [{"case_id": cid, "covers": tags} for cid, tags in covers.items()]}


class Selection(unittest.TestCase):
    def test_the_four_tasks_name_the_ids_the_plan_row_names(self) -> None:
        self.assertEqual(["BEN-06", "BEN-07", "BEN-08", "BEN-09"], sorted(TASKS))
        a = selectors("BEN-06")
        self.assertEqual([f"EC-A{n:02d}" for n in range(1, 21)], [i for i in a if i.startswith("EC-")])
        self.assertTrue({"US-02", "US-07", "PUS-01", "PUS-02", "PUS-08"} <= set(a))
        self.assertTrue({"US-03", "US-04", "US-13", "US-14"} <= set(selectors("BEN-07")))
        self.assertEqual(22, len([i for i in selectors("BEN-07") if i.startswith("EC-S")]))
        self.assertTrue({"US-08", "US-18", "US-20"} <= set(selectors("BEN-08")))
        self.assertEqual(11, len([i for i in selectors("BEN-08") if i.startswith("EC-T")]))
        nine = selectors("BEN-09")
        self.assertTrue({"US-05", "US-21", "EC-S19"} <= set(nine))
        self.assertEqual(16, len([i for i in nine if i.startswith("EC-E")]))

    def test_every_selected_id_is_a_real_identifier_of_the_catalogue(self) -> None:
        known = set(USER_STORIES) | set(SUPPORTING_STORIES) | set(EDGE_CASES)
        for task in TASKS:
            self.assertEqual(set(), set(selectors(task)) - known, task)

    def test_an_unknown_task_is_refused(self) -> None:
        with self.assertRaises(ValueError):
            selectors("BEN-99")


class Coverage(unittest.TestCase):
    def test_a_missing_id_is_a_gap_not_a_zero_row_hidden(self) -> None:
        result = coverage(manifest({"BEN-AMB-0001": ["EC-A01", "US-02"]}), "BEN-06")
        self.assertIn("EC-A02", result["gaps"])
        self.assertNotIn("EC-A01", result["gaps"])
        self.assertEqual(["BEN-AMB-0001"], result["rows"]["EC-A01"])

    def test_a_single_case_is_reported_as_thin_depth_not_as_a_gap(self) -> None:
        result = coverage(manifest({"BEN-AMB-0001": ["EC-A01"], "BEN-AMB-0002": ["EC-A01"], "BEN-AMB-0003": ["EC-A02"]}), "BEN-06")
        self.assertIn("EC-A02", result["thin"])
        self.assertNotIn("EC-A01", result["thin"])
        self.assertNotIn("EC-A02", result["gaps"])

    def test_ids_of_other_tasks_do_not_count(self) -> None:
        result = coverage(manifest({"BEN-X-0001": ["EC-E01", "FAULT:LATENCY", "CAP:fin.cap.lookup_account"]}), "BEN-06")
        self.assertEqual(set(selectors("BEN-06")), set(result["gaps"]))

    def test_the_rendering_is_deterministic_and_names_gaps_and_thin_ids(self) -> None:
        m = manifest({"BEN-AMB-0001": ["EC-A01"]})
        self.assertEqual(render(coverage(m, "BEN-06"), "BEN-06"), render(coverage(m, "BEN-06"), "BEN-06"))
        text = render(coverage(m, "BEN-06"), "BEN-06")
        self.assertIn("GAP", text)
        self.assertIn("EC-A02", text)
        self.assertIn("thin", text)


class Exit(unittest.TestCase):
    def test_the_exit_code_is_one_and_the_text_names_the_gap_when_an_id_has_no_case(self) -> None:
        text, code = report(manifest({"BEN-AMB-0001": ["EC-A01"]}), "BEN-06")
        self.assertEqual(1, code)
        self.assertIn("GAP: ", text)

    def test_the_exit_code_is_zero_when_every_id_has_a_case(self) -> None:
        every = {f"BEN-X-{n:04d}": [tag] for n, tag in enumerate(selectors("BEN-08"), start=1)}
        text, code = report(manifest(every), "BEN-08")
        self.assertEqual(0, code)
        self.assertNotIn("GAP", text)


class RealSuite(unittest.TestCase):
    """The real generated suite (seed 20261020): every id of the four tasks has at least one case, and the table says how thin the thinnest are."""

    manifest: dict[str, Any]

    @classmethod
    def setUpClass(cls) -> None:
        cls.manifest = generate(20261020).manifest

    def test_no_id_of_the_four_tasks_is_a_gap(self) -> None:
        for task in TASKS:
            self.assertEqual([], coverage(self.manifest, task)["gaps"], task)

    def test_the_command_exits_zero_when_there_is_no_gap_and_prints_the_table(self) -> None:
        buffer = io.StringIO()
        with redirect_stdout(buffer):
            code = main(["--task", "BEN-08"])
        self.assertEqual(0, code)
        self.assertIn("EC-T01", buffer.getvalue())
        self.assertNotIn("GAP", buffer.getvalue().split("## ")[0] + buffer.getvalue())  # no gap row in a gap-free table


if __name__ == "__main__":
    unittest.main()
