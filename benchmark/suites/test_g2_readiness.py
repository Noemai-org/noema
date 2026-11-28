"""BEN-12 (A771): which of the G2 stories' cases this build of the runner can run now, and what stops the rest, per story. It reuses the runner's own support check (`benchmark.runner.support.reasons_for`), so it
cannot disagree with `--support` or with a run. A G2 story with no runnable case is named: the G2 table would have no row to pass for it. SYNTHETIC; generated layer only (INV-22). Seen failing first: no module."""
import io
import unittest
from contextlib import redirect_stdout

from benchmark.scoring.story_table import G2_STORIES
from benchmark.runner.suite import load_suite
from benchmark.runner.support import reasons_for
from benchmark.runner.test_suite import suite_dir
from benchmark.suites.g2_readiness import main, readiness, render


class Readiness(unittest.TestCase):
    def setUp(self) -> None:
        self.suite = load_suite(suite_dir())
        self.doc = readiness(self.suite)

    def test_there_is_one_row_per_g2_story_in_the_table_order(self) -> None:
        self.assertEqual([r["story"] for r in self.doc["stories"]], list(G2_STORIES))

    def test_a_cases_runnability_is_exactly_what_the_runner_support_check_says(self) -> None:
        tags = {e["case_id"]: set(e["covers"]) for e in self.suite.manifest["cases"]}
        for row in self.doc["stories"]:
            want = [lc.case["case_id"] for lc in self.suite.cases if row["story"] in tags[lc.case["case_id"]] and not reasons_for(lc)]
            self.assertEqual(row["runnable_cases"], sorted(want), row["story"])
            self.assertEqual(row["runnable"], len(want), row["story"])

    def test_a_story_with_cases_but_none_runnable_is_listed_with_what_blocks_it(self) -> None:
        stuck = [r for r in self.doc["stories"] if r["cases"] and not r["runnable"]]
        self.assertEqual(sorted(self.doc["no_runnable_case"]), sorted(r["story"] for r in stuck))
        for row in stuck:
            self.assertTrue(row["blocked_by"], row["story"])

    def test_a_webhook_sender_unblocks_the_webhook_kinds_and_nothing_else(self) -> None:
        with_sender = readiness(self.suite, webhooks=True)
        for before, after in zip(self.doc["stories"], with_sender["stories"], strict=True):
            self.assertGreaterEqual(after["runnable"], before["runnable"], before["story"])
            self.assertEqual(after["cases"], before["cases"], before["story"])

    def test_the_table_names_every_story_and_the_stuck_ones(self) -> None:
        text = render(self.doc)
        for story in G2_STORIES:
            self.assertIn(story, text)
        for story in self.doc["no_runnable_case"]:
            self.assertIn(story, text.split("no runnable case", 1)[1])

    def test_the_command_prints_the_table_and_exits_zero_unless_all_must_run(self) -> None:
        out = io.StringIO()
        with redirect_stdout(out):
            self.assertEqual(main(["--suite", str(suite_dir())]), 0)
        self.assertIn("| US-02 |", out.getvalue())
        with redirect_stdout(io.StringIO()):
            self.assertEqual(main(["--suite", str(suite_dir()), "--require-all"]), 1 if self.doc["no_runnable_case"] else 0)

    def test_the_held_out_set_is_refused(self) -> None:
        with redirect_stdout(io.StringIO()):
            self.assertEqual(main(["--suite", "/x/benchmark-heldout/suite"]), 2)


if __name__ == "__main__":
    unittest.main()
