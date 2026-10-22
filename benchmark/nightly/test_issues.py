"""The nightly files one regression issue per case that was run and not correctly handled, de-duplicated by case ID (T4 item 11)."""
import json
import tempfile
import unittest
from pathlib import Path
from typing import Any

from benchmark.nightly.__main__ import main
from benchmark.nightly.issues import LABEL, GhCli, file_issues, marker, plan_issues

Json = dict[str, Any]


def card(*failures: Json) -> Json:
    return {"run_id": "r1", "failures": list(failures)}


def failure(case_id: str, severity: str = "S1", reasons: list[str] | None = None, gates: list[str] | None = None) -> Json:
    return {"case_id": case_id, "severity": severity, "reasons": reasons or ["wrong disposition"], "gates": gates or []}


def result(case_id: str, status: str = "COMPLETED", code: str = "FAULT_UNSUPPORTED") -> Json:
    return {"case_id": case_id, "status": status, "error": None if status == "COMPLETED" else {"code": code, "message": "x"}}


class FakeGh:
    def __init__(self, open_issues: list[Json] | None = None) -> None:
        self.open = open_issues or []
        self.created: list[tuple[str, str, list[str]]] = []
        self.labels_ensured: list[str] = []

    def list_open(self) -> list[Json]:
        return self.open

    def ensure_labels(self, labels: list[str]) -> None:
        self.labels_ensured += labels

    def create(self, title: str, body: str, labels: list[str]) -> int:
        self.created.append((title, body, labels))
        return 100 + len(self.created)


class Plan(unittest.TestCase):
    def test_one_issue_per_failing_case(self) -> None:
        p = plan_issues(card(failure("BEN-AMB-0001"), failure("BEN-CLEAN-0002", "S0", gates=["UNSAFE_CONSEQUENTIAL_ACTION"])),
                        [result("BEN-AMB-0001"), result("BEN-CLEAN-0002")], [], run_id="r1", commit_sha="a" * 40, run_url="https://example.invalid/run/1")
        self.assertEqual([n.case_id for n in p.new], ["BEN-CLEAN-0002", "BEN-AMB-0001"], "most severe first")
        s0 = p.new[0]
        self.assertIn(marker("BEN-CLEAN-0002"), s0.body)
        self.assertIn("BEN-CLEAN-0002", s0.title)
        self.assertEqual(sorted(s0.labels), sorted([LABEL, "severity:S0", "tier-0"]))
        self.assertIn("UNSAFE_CONSEQUENTIAL_ACTION", s0.body)
        self.assertIn("a" * 40, s0.body)
        self.assertNotIn("tier-0", p.new[1].labels)

    def test_a_case_that_already_has_an_open_issue_is_not_filed_again(self) -> None:
        open_issues = [{"number": 7, "title": "x", "body": f"text {marker('BEN-AMB-0001')} more"}]
        p = plan_issues(card(failure("BEN-AMB-0001"), failure("BEN-AMB-0002")), [result("BEN-AMB-0001"), result("BEN-AMB-0002")], open_issues,
                        run_id="r1", commit_sha=None, run_url=None)
        self.assertEqual([n.case_id for n in p.new], ["BEN-AMB-0002"])
        self.assertEqual(p.already_open, {"BEN-AMB-0001": 7})

    def test_the_title_also_identifies_an_open_issue(self) -> None:
        """An issue whose marker was edited away is still found by its case id in the title."""
        open_issues = [{"number": 9, "title": "[benchmark] BEN-STATE-0003: not correctly handled (S1)", "body": ""}]
        p = plan_issues(card(failure("BEN-STATE-0003")), [result("BEN-STATE-0003")], open_issues, run_id="r1", commit_sha=None, run_url=None)
        self.assertEqual(p.new, [])
        self.assertEqual(p.already_open, {"BEN-STATE-0003": 9})

    def test_a_case_that_could_not_run_is_not_a_regression(self) -> None:
        p = plan_issues(card(failure("BEN-AMB-0001"), failure("BEN-AMB-0002")), [result("BEN-AMB-0001", "ERROR", "EVENT_KIND_UNSUPPORTED"), result("BEN-AMB-0002")], [],
                        run_id="r1", commit_sha=None, run_url=None)
        self.assertEqual([n.case_id for n in p.new], ["BEN-AMB-0002"])
        self.assertEqual(p.could_not_run, {"EVENT_KIND_UNSUPPORTED": 1})

    def test_a_failure_with_no_result_is_not_filed(self) -> None:
        """A case with no recorded result has nothing to reproduce."""
        p = plan_issues(card(failure("BEN-AMB-0009")), [], [], run_id="r1", commit_sha=None, run_url=None)
        self.assertEqual(p.new, [])

    def test_no_failures_no_issues(self) -> None:
        p = plan_issues(card(), [result("BEN-AMB-0001")], [], run_id="r1", commit_sha=None, run_url=None)
        self.assertEqual((p.new, p.already_open), ([], {}))

    def test_the_body_has_how_to_reproduce(self) -> None:
        p = plan_issues(card(failure("BEN-AMB-0001")), [result("BEN-AMB-0001")], [], run_id="r1", commit_sha=None, run_url=None)
        self.assertIn("--only BEN-AMB-0001", p.new[0].body)


class Filing(unittest.TestCase):
    def test_labels_are_ensured_then_each_issue_is_created_once(self) -> None:
        gh = FakeGh()
        p = plan_issues(card(failure("BEN-AMB-0001"), failure("BEN-AMB-0002", "S2")), [result("BEN-AMB-0001"), result("BEN-AMB-0002")], [], run_id="r1", commit_sha=None, run_url=None)
        numbers = file_issues(p, gh, dry_run=False)
        self.assertEqual(len(gh.created), 2)
        self.assertIn(LABEL, gh.labels_ensured)
        self.assertEqual(numbers, {"BEN-AMB-0001": 101, "BEN-AMB-0002": 102})

    def test_a_second_night_with_the_same_failures_files_nothing(self) -> None:
        gh = FakeGh()
        p = plan_issues(card(failure("BEN-AMB-0001")), [result("BEN-AMB-0001")], [], run_id="r1", commit_sha=None, run_url=None)
        file_issues(p, gh, dry_run=False)
        gh.open = [{"number": 101, "title": gh.created[0][0], "body": gh.created[0][1]}]
        again = plan_issues(card(failure("BEN-AMB-0001")), [result("BEN-AMB-0001")], gh.open, run_id="r2", commit_sha=None, run_url=None)
        self.assertEqual(file_issues(again, gh, dry_run=False), {})
        self.assertEqual(len(gh.created), 1)

    def test_a_dry_run_creates_nothing(self) -> None:
        gh = FakeGh()
        p = plan_issues(card(failure("BEN-AMB-0001")), [result("BEN-AMB-0001")], [], run_id="r1", commit_sha=None, run_url=None)
        self.assertEqual(file_issues(p, gh, dry_run=True), {})
        self.assertEqual((gh.created, gh.labels_ensured), ([], []))


class Cli(unittest.TestCase):
    def _files(self, d: Path, status: str = "INCOMPLETE") -> None:
        (d / "results").mkdir()
        (d / "results" / "a.json").write_text(json.dumps(result("BEN-AMB-0001")))
        (d / "scorecard.json").write_text(json.dumps(card(failure("BEN-AMB-0001"))))
        (d / "run-manifest.json").write_text(json.dumps({"run_id": "r1", "status": status, "commit_sha": None}))

    def test_a_dry_run_prints_the_plan(self) -> None:
        import io
        from contextlib import redirect_stdout
        with tempfile.TemporaryDirectory() as d:
            self._files(Path(d))
            out = io.StringIO()
            with redirect_stdout(out):
                code = main(["--scorecard", f"{d}/scorecard.json", "--results", f"{d}/results", "--run-manifest", f"{d}/run-manifest.json", "--dry-run"])
        self.assertEqual(code, 0)
        self.assertIn("would file BEN-AMB-0001", out.getvalue())

    def test_a_run_that_was_not_run_files_nothing_and_says_so(self) -> None:
        import io
        from contextlib import redirect_stdout
        with tempfile.TemporaryDirectory() as d:
            (Path(d) / "run-manifest.json").write_text(json.dumps({"run_id": "r1", "status": "NOT_RUN", "not_run_reason": "no access: x", "commit_sha": None}))
            out = io.StringIO()
            with redirect_stdout(out):
                code = main(["--run-manifest", f"{d}/run-manifest.json", "--dry-run"])
        self.assertEqual(code, 0)
        self.assertIn("not run", out.getvalue())

    def test_the_gh_cli_wrapper_lists_open_issues_by_label(self) -> None:
        calls: list[list[str]] = []

        def runner(args: list[str]) -> str:
            calls.append(args)
            return "[]"

        self.assertEqual(GhCli(runner=runner).list_open(), [])
        self.assertIn(LABEL, calls[0])
        self.assertIn("--state", calls[0])


if __name__ == "__main__":
    unittest.main()
