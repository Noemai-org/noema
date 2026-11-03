"""BEN-29: a scored subset of the generated suite, and `python -m benchmark.suites run`: one command per suite that selects, runs through the runner, scores
the subset and records the run (REQ-BEN-004, REQ-TST-005). Written before the code and seen failing. SYNTHETIC. Generated layer only (INV-22)."""
import io
import json
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from typing import Any

from benchmark.contracts import Contracts, pretty_bytes
from benchmark.runner.test_suite import suite_dir
from benchmark.scoring.__main__ import main as score_main
from benchmark.scoring.testdata import clean_result
from benchmark.suites.__main__ import main
from benchmark.suites.subset import subset_suite

Json = dict[str, Any]
CONTRACTS = Contracts()
PACK = Path(__file__).resolve().parents[2] / "packs" / "fintech_v1.json"


def invoke(*argv: str) -> tuple[int, str, str]:
    out, err = io.StringIO(), io.StringIO()
    with redirect_stdout(out), redirect_stderr(err):
        code = main(list(argv))
    return code, out.getvalue(), err.getvalue()


class Subset(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = Path(self._tmp.name)
        manifest = json.loads((suite_dir() / "manifest.json").read_text())
        self.ids = sorted(e["case_id"] for e in manifest["cases"] if "US-06" in e["covers"])
        self.out = self.root / "subset"
        subset_suite(suite_dir(), self.ids, self.out, "us")

    def test_the_subset_manifest_is_a_valid_suite_manifest_that_describes_only_the_chosen_cases(self) -> None:
        manifest = json.loads((self.out / "manifest.json").read_text())
        self.assertEqual([], CONTRACTS.errors("benchmark-suite-manifest", manifest))
        self.assertEqual(self.ids, [e["case_id"] for e in manifest["cases"]])
        self.assertEqual(len(self.ids), sum(manifest["layer_counts"].values()))
        self.assertEqual(len(self.ids), sum(manifest["category_counts"].values()))
        self.assertEqual(len(self.ids), manifest["pidgin_or_code_switched"]["of"])
        self.assertTrue(manifest["suite_id"].endswith("-us"))

    def test_only_the_chosen_cases_and_their_oracles_are_copied(self) -> None:
        self.assertEqual(self.ids, sorted(p.stem for p in (self.out / "cases").glob("*.json")))
        self.assertEqual(len(self.ids), len(list((self.out / "oracles").rglob("*.json"))))

    def test_the_subset_scores_as_a_complete_run_of_exactly_the_chosen_cases(self) -> None:
        results = self.root / "results"
        results.mkdir()
        for case_file in sorted((self.out / "cases").glob("*.json")):
            case = json.loads(case_file.read_text())
            oracle = json.loads((self.out / case["oracle_ref"]).read_text())
            (results / case_file.name).write_bytes(pretty_bytes(clean_result(case, oracle, run_id="run_subset")))
        code = score_main(["--cases", str(self.out / "cases"), "--oracles", str(self.out / "oracles"), "--results", str(results), "--pack", str(PACK),
                           "--manifest", str(self.out / "manifest.json"), "--run-id", "run_subset", "--out", str(self.root / "score")])
        card = json.loads((self.root / "score" / "scorecard.json").read_text())
        self.assertEqual((len(self.ids), True), (card["n_cases"], card["complete"]))
        self.assertIn(code, (0, 1))

    def test_a_case_that_is_not_in_the_suite_is_refused(self) -> None:
        with self.assertRaises(ValueError):
            subset_suite(suite_dir(), ["BEN-NOPE-9999"], self.root / "bad", "us")


class RunCommand(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = Path(self._tmp.name)

    def test_list_prints_the_selected_ids_and_their_digest(self) -> None:
        code, out, _ = invoke("list", "us", "--suite", str(suite_dir()))
        self.assertEqual(0, code)
        self.assertIn("BEN-", out)
        self.assertIn("sha256:", out)

    def test_run_without_an_environment_is_not_run_exit_3_and_records_it_without_faking_a_result(self) -> None:
        out_dir = self.root / "out"
        code, out, _ = invoke("run", "us", "--suite", str(suite_dir()), "--out", str(out_dir), "--run-id", "g3-us-1", "--commit-sha", "a" * 40)
        self.assertEqual(3, code)
        record = json.loads((out_dir / "suite-run.json").read_text())
        self.assertEqual(("us", "g3-us-1", "NOT_RUN", False), (record["suite"], record["run_id"], record["status"], record["reset_done"]))
        self.assertGreaterEqual(record["selected"], 22)
        self.assertRegex(record["selection_digest"], r"^sha256:[0-9a-f]{64}$")
        self.assertEqual("a" * 40, record["commit_sha"])
        self.assertEqual(20261020, record["seed"])
        self.assertFalse((out_dir / "score").exists())
        self.assertIn("not run", out.lower())

    def test_reset_done_is_recorded_only_when_the_caller_says_it_was_done(self) -> None:
        out_dir = self.root / "out2"
        invoke("run", "evil", "--suite", str(suite_dir()), "--out", str(out_dir), "--run-id", "g3-evil-1", "--reset-done")
        self.assertTrue(json.loads((out_dir / "suite-run.json").read_text())["reset_done"])

    def test_an_unknown_suite_exits_2(self) -> None:
        code, _, err = invoke("list", "nope", "--suite", str(suite_dir()))
        self.assertEqual(2, code)
        self.assertIn("nope", err)

    def test_an_argument_naming_the_held_out_set_is_refused(self) -> None:
        code, _, err = invoke("list", "us", "--suite", "/x/benchmark-heldout")
        self.assertEqual(2, code)
        self.assertIn("benchmark-heldout", err)

    def test_the_runner_commands_carry_the_selection_file_and_the_run_id(self) -> None:
        from benchmark.suites.__main__ import runner_command
        cmd = runner_command(Path("S"), Path("OUT"), "rid", Path("OUT/selection.txt"), ["--provider", "x"])
        self.assertEqual(["--only-file", "OUT/selection.txt"], cmd[cmd.index("--only-file"):cmd.index("--only-file") + 2])
        self.assertEqual("rid", cmd[cmd.index("--run-id") + 1])
        self.assertEqual(["--provider", "x"], cmd[-2:])


if __name__ == "__main__":
    unittest.main()
