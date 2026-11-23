"""UND-16, run #6 (noema#338): the run names its failing goldens and its failed record-only inputs where a person reads first, so nobody has to open the log.

`summary.md` is written beside `run.json`, in the directory the leak scan reads before anything is uploaded. It holds case ids, statuses and the fixed texts of the problems and details; never an
utterance, a proposal or a digit run (AGENT-13). Its first line is `FAILING: <ids>` (or `FAILING: none`), the one line the workflow's result step quotes. SYNTHETIC."""
import re
import tempfile
import unittest
from pathlib import Path

from benchmark.compile_acceptance.runner import CaseResult, RecordOnlyResult, RunResult, render_summary, write_summary
from benchmark.compile_acceptance.test_cli import Backend, argv, run_main

DIGIT_RUN = re.compile(r"(?<![A-Za-z0-9])\d(?:[ -]?\d)*(?![A-Za-z0-9])")  # the leak scan's own shape (tools/ci/leakscan.py)


def result(**over: object) -> RunResult:
    base = RunResult(
        cases=[CaseResult("G-A", "MISSED", ("requested_remedy: present where none is expected",)), CaseResult("G-B", "ERROR", ("the compile service gave no usable proposal (InputInvalid:NO_TIME_WINDOW)",)),
               CaseResult("G-C", "MET"), CaseResult("G-D", "NOT_RUN", ("the budget stop was reached",)), CaseResult("G-E", "ERROR", ("x",))],
        record_only=[RecordOnlyResult("BEN-CLEAN-0001", "ERROR", "the compile service gave no usable proposal (InputInvalid:NO_TIME_WINDOW)"), RecordOnlyResult("BEN-CLEAN-0014", "RECORDED")],
        spent_micros=2_631_299, stopped=False, calls=48,
    )
    for key, value in over.items():
        setattr(base, key, value)
    return base


class Summary(unittest.TestCase):
    def test_the_first_line_names_every_golden_that_was_not_met_in_order(self) -> None:
        first = render_summary(result()).splitlines()[0]
        self.assertEqual(first, "FAILING: G-A G-B G-D G-E")  # MISSED, ERROR and NOT_RUN all count; MET does not

    def test_a_run_that_met_every_golden_says_none(self) -> None:
        ok = result(cases=[CaseResult(f"G-{x}", "MET") for x in "ABCDE"])
        self.assertEqual(render_summary(ok).splitlines()[0], "FAILING: none")

    def test_every_case_is_listed_with_its_status_and_its_fixed_text(self) -> None:
        text = render_summary(result())
        self.assertIn("| G-A | MISSED | requested_remedy: present where none is expected |", text)
        self.assertIn("| G-B | ERROR | the compile service gave no usable proposal (InputInvalid:NO_TIME_WINDOW) |", text)
        self.assertIn("| G-C | MET |", text)
        self.assertIn("| BEN-CLEAN-0001 | ERROR | the compile service gave no usable proposal (InputInvalid:NO_TIME_WINDOW) |", text)
        self.assertIn("| BEN-CLEAN-0014 | RECORDED |", text)
        self.assertIn("recorded, not judged", text)  # the record-only pass is named apart from the goldens (A361, A372)

    def test_a_failed_record_only_input_is_named_but_is_not_a_failing_golden(self) -> None:
        self.assertNotIn("BEN-CLEAN-0001", render_summary(result()).splitlines()[0])

    def test_a_text_cannot_break_the_table(self) -> None:
        rough = result(cases=[CaseResult("G-A", "MISSED", ("a | b\nc",))])
        lines = [line for line in render_summary(rough).splitlines() if line.startswith("| G-A")]
        self.assertEqual(len(lines), 1)
        self.assertEqual(lines[0].count("|"), 4)

    def test_the_summary_holds_no_digit_run_of_five_or_more_and_never_the_spend(self) -> None:
        text = render_summary(result(spent_micros=987_654_321))
        self.assertNotIn("987654321", text.replace(",", "").replace("_", ""))
        for run in DIGIT_RUN.finditer(text):
            self.assertLess(len(re.sub(r"\D", "", run.group(0))), 5, run.group(0))  # the case ids (BEN-CLEAN-0001) hold four digits at most

    def test_it_is_written_beside_the_run_record(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = write_summary(Path(tmp) / "out", result())
            self.assertEqual(path, Path(tmp) / "out" / "summary.md")
            self.assertEqual(path.read_text(encoding="utf-8"), render_summary(result()))


class Command(unittest.TestCase):
    def test_the_command_writes_the_summary_for_a_run_with_a_miss(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, _, _, _ = run_main(argv(tmp), {"OPENAI_API_KEY": "sk-test-not-a-real-key-0000"}, fail=True)
            self.assertEqual(code, 1)
            text = (Path(tmp) / "out" / "summary.md").read_text(encoding="utf-8")
            self.assertEqual(text.splitlines()[0], "FAILING: G-A G-B G-C G-D G-E")

    def test_a_run_that_met_every_golden_writes_none(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, _, _, _ = run_main(argv(tmp), {"OPENAI_API_KEY": "sk-test-not-a-real-key-0000"})
            self.assertEqual(code, 0)
            self.assertEqual((Path(tmp) / "out" / "summary.md").read_text(encoding="utf-8").splitlines()[0], "FAILING: none")

    def test_a_run_that_was_not_run_writes_no_summary(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, _, _, _ = run_main(argv(tmp), {})
            self.assertEqual(code, 3)
            self.assertFalse((Path(tmp) / "out" / "summary.md").exists())


del Backend  # imported only so the helpers above share one scripted backend with the command tests
