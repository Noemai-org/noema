"""`python -m benchmark.replay`: exit codes and files (BEN-05). Tests first, seen failing. SYNTHETIC."""
import io
import json
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from typing import Any

from benchmark.contracts import Contracts, pretty_bytes
from benchmark.replay.__main__ import main
from benchmark.replay.test_compare import REGISTRY, blocked_pair, pair, run_manifest, suite_manifest

Json = dict[str, Any]


def write_run(root: Path, name: str, manifest: Json, results: dict[str, Json]) -> Path:
    run = root / name
    (run / "results").mkdir(parents=True)
    (run / "run-manifest.json").write_bytes(pretty_bytes(manifest))
    for case_id, r in results.items():
        (run / "results" / f"{case_id}.json").write_bytes(pretty_bytes(r))
    return run


def write_suite(root: Path) -> Path:
    suite = root / "suite"
    suite.mkdir()
    (suite / "manifest.json").write_bytes(pretty_bytes(suite_manifest()))
    return suite


def invoke(*argv: str) -> tuple[int, str, str]:
    out, err = io.StringIO(), io.StringIO()
    with redirect_stdout(out), redirect_stderr(err):
        code = main(list(argv))
    return code, out.getvalue(), err.getvalue()


class Cli(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)
        self.addCleanup(self._tmp.cleanup)

    def args(self, a: Path, b: Path, *extra: str) -> tuple[str, ...]:
        return ("--suite", str(self.root / "suite"), "--first", str(a), "--second", str(b), "--out", str(self.root / "out"), *extra)

    def runs(self, manifest_b: Json | None = None, changed: dict[str, Json] | None = None, blocked: bool = False) -> tuple[Path, Path]:
        write_suite(self.root)
        pa, pb = blocked_pair() if blocked else pair()
        pb.update(changed or {})
        return write_run(self.root, "a", run_manifest("run_a"), pa), write_run(self.root, "b", manifest_b or run_manifest("run_b"), pb)

    def test_identical_runs_exit_0_and_write_both_files(self) -> None:
        a, b = self.runs()
        code, out, _ = invoke(*self.args(a, b))
        self.assertEqual(0, code)
        report = json.loads((self.root / "out" / "replay.json").read_text())
        self.assertEqual("IDENTICAL", report["verdict"])
        md = (self.root / "out" / "replay.md").read_text()
        for needle in ("run_a", "run_b", "20261020", "IDENTICAL", "BEN-TMO-0001", "does not hold the Trust decision payload"):
            self.assertIn(needle, md)
        self.assertIn("IDENTICAL", out)

    def test_the_report_files_are_byte_identical_on_a_second_invocation(self) -> None:
        a, b = self.runs()
        invoke(*self.args(a, b))
        first = [(self.root / "out" / n).read_bytes() for n in ("replay.json", "replay.md")]
        invoke(*self.args(a, b))
        self.assertEqual(first, [(self.root / "out" / n).read_bytes() for n in ("replay.json", "replay.md")])

    def test_a_difference_exits_1_and_the_report_says_where(self) -> None:
        from benchmark.replay.test_compare import result
        a, b = self.runs(changed={"BEN-TMO-0001": result("BEN-TMO-0001", "run_b", verdict="UNKNOWN")})
        code, _, _ = invoke(*self.args(a, b))
        self.assertEqual(1, code)
        self.assertIn("verdicts", (self.root / "out" / "replay.md").read_text())

    def test_a_not_run_run_exits_3_and_writes_nothing(self) -> None:
        a, b = self.runs(manifest_b=run_manifest("run_b", status="NOT_RUN"))
        code, out, _ = invoke(*self.args(a, b))
        self.assertEqual(3, code)
        self.assertIn("not run", out.lower())
        self.assertFalse((self.root / "out").exists())

    def test_a_different_commit_exits_2_and_writes_nothing(self) -> None:
        a, b = self.runs(manifest_b=run_manifest("run_b", commit="e" * 40))
        code, _, err = invoke(*self.args(a, b))
        self.assertEqual(2, code)
        self.assertIn("commit", err)
        self.assertFalse((self.root / "out").exists())

    def test_a_result_that_breaks_its_contract_exits_2(self) -> None:
        a, b = self.runs()
        broken = json.loads((b / "results" / "BEN-TMO-0001.json").read_text())
        broken["status"] = "MAYBE"
        (b / "results" / "BEN-TMO-0001.json").write_bytes(pretty_bytes(broken))
        code, _, err = invoke(*self.args(a, b))
        self.assertEqual(2, code)
        self.assertIn("BEN-TMO-0001", err)

    def test_an_argument_naming_the_held_out_set_is_refused(self) -> None:
        a, b = self.runs()
        code, _, err = invoke(*self.args(a, b, "--story", "benchmark-heldout"))
        self.assertEqual(2, code)
        self.assertIn("benchmark-heldout", err)

    def registry(self, entries: dict[str, Json]) -> str:
        path = self.root / "open_exclusions.json"
        path.write_bytes(pretty_bytes({"exclusions": entries}))
        return str(path)

    def test_the_named_open_exclusions_are_applied_and_recorded_in_the_report(self) -> None:
        a, b = self.runs(blocked=True)
        reg = self.registry(REGISTRY)
        self.assertEqual(0, invoke(*self.args(a, b, "--exclude-mutated", "--open-exclusions", reg))[0])
        report = json.loads((self.root / "out" / "replay.json").read_text())
        self.assertEqual("NAMED_OPEN_EXCLUSIONS_REMOVED", report["selection"])
        md = (self.root / "out" / "replay.md").read_text()
        self.assertIn("NAMED_OPEN_EXCLUSIONS_REMOVED", md)
        self.assertIn("covers 2 of 3 cases", md)
        self.assertIn("Open exclusions", md)
        self.assertIn("IDENTICAL on 2 of 3 cases", md)
        self.assertIn("first run ERROR FAULT_UNSUPPORTED", md)
        self.assertIn("BEN-CLEAN-1007", md)
        self.assertIn("FAULT:INJECTION_PAYLOAD", md)

    def test_the_packaged_registry_names_ben_clean_1007_and_the_tushe_held_fault(self) -> None:
        packaged = json.loads((Path(__file__).parent / "open_exclusions.json").read_text())
        entry = packaged["exclusions"]["BEN-CLEAN-1007"]
        self.assertEqual("Tushe", entry["held_by"])
        self.assertIn("INJECTION_PAYLOAD", entry["reason"])
        self.assertEqual(["BEN-CLEAN-1007"], sorted(packaged["exclusions"]))

    def test_a_malformed_registry_exits_2(self) -> None:
        a, b = self.runs()
        bad = self.root / "bad.json"
        bad.write_text("{}")
        code, _, err = invoke(*self.args(a, b, "--exclude-mutated", "--open-exclusions", str(bad)))
        self.assertEqual(2, code)
        self.assertIn("exclusions", err)

    def test_all_compares_the_whole_suite_and_conflicts_with_story(self) -> None:
        a, b = self.runs()
        code, _, _ = invoke(*self.args(a, b, "--all"))
        self.assertIn(code, (0, 1))
        report = json.loads((self.root / "out" / "replay.json").read_text())
        self.assertEqual("ALL_CASES_OF_THE_SUITE", report["selection"])
        self.assertEqual(4, report["case_coverage"]["of"])
        md = (self.root / "out" / "replay.md").read_text()
        self.assertIn("every case of the suite", md)
        self.assertNotIn("cases that cover the stories", md)
        code, _, err = invoke(*self.args(a, b, "--all", "--story", "US-01"))
        self.assertEqual(2, code)
        self.assertIn("--all", err)

    def test_stories_default_to_us_01_and_us_06(self) -> None:
        a, b = self.runs()
        invoke(*self.args(a, b))
        self.assertEqual(["US-01", "US-06"], json.loads((self.root / "out" / "replay.json").read_text())["stories"])

    def test_the_documents_the_tests_use_are_valid_contracts(self) -> None:
        from benchmark.replay.test_compare import result
        contracts = Contracts()
        self.assertEqual([], contracts.errors("benchmark-run-manifest", run_manifest("run_a")))
        for disposition in ("ALLOW", "ESCALATE", "REJECT"):
            self.assertEqual([], contracts.errors("benchmark-case-result.v2", result("BEN-TMO-0001", "run_a", disposition=disposition)))
        self.assertEqual([], contracts.errors("benchmark-case-result.v2", result("BEN-TMO-0001", "run_a", verdict="UNKNOWN", grant="SPENT")))
        self.assertEqual([], contracts.errors("benchmark-suite-manifest", suite_manifest()))


if __name__ == "__main__":
    unittest.main()
