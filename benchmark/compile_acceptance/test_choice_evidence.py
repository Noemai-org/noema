"""UND-17 (A602, interim; noema#338, compile run 7): the job log, the summary and the run record show, for each golden, the option the model chose, the top five probabilities (the abstention among them) and the
threshold applied, as option ids and numbers only, so that every later run can be read from the job log without the artifact host. The leak scan stays clean: no utterance, no digit run of five or more
(AGENT-13). SYNTHETIC."""
import re
import tempfile
import unittest
from collections.abc import Mapping
from fractions import Fraction
from pathlib import Path
from typing import Any

from benchmark.bakeoff.budget import BudgetGuard
from benchmark.compile_acceptance.__main__ import main
from benchmark.compile_acceptance.evidence import format_choice
from benchmark.compile_acceptance.golden import GOLDEN, Golden
from benchmark.compile_acceptance.recording import Recorder
from benchmark.compile_acceptance.runner import CaseResult, RunResult, render_summary, run_cases
from benchmark.compile_acceptance.test_cli import KEY, Backend, argv
from benchmark.compile_acceptance.test_golden import good
from benchmark.compile_acceptance.test_runner import recorder

DIGIT_RUN = re.compile(r"(?<![A-Za-z0-9])\d(?:[ -]?\d)*(?![A-Za-z0-9])")  # the leak scan's own shape (tools/ci/leakscan.py)
PROBS = {"none_or_other": 0.31, "fin.cap.lookup_transaction": 0.5, "fin.cap.create_escalation_case": 0.1, "fin.cap.log_complaint_reference": 0.05, "fin.cap.a": 0.03, "fin.cap.b": 0.01}


def digit_runs(text: str) -> list[int]:
    return [len(re.sub(r"\D", "", m.group(0))) for m in DIGIT_RUN.finditer(text)]


class Format(unittest.TestCase):
    def test_a_line_names_the_case_the_slot_the_choice_the_top_five_and_the_threshold(self) -> None:
        line = format_choice("G-A", "requested_remedy", "fin.cap.lookup_transaction", PROBS, None)
        self.assertEqual(line, "CHOICE G-A slot=requested_remedy chosen=fin.cap.lookup_transaction top5=fin.cap.lookup_transaction:0.5000,none_or_other:0.3100,fin.cap.create_escalation_case:0.1000,"
                               "fin.cap.log_complaint_reference:0.0500,fin.cap.a:0.0300 threshold=n/a")

    def test_the_abstention_is_a_choice_and_is_named(self) -> None:
        self.assertIn("chosen=none_or_other", format_choice("G-C", "requested_remedy", None, PROBS, None))

    def test_the_top_five_are_the_five_largest_in_order_with_ties_by_id_and_never_more(self) -> None:
        tied = {"b": 0.2, "a": 0.2, "c": 0.2, "d": 0.2, "e": 0.1, "f": 0.1}
        top = format_choice("G-A", "s", "a", tied, None).split("top5=")[1].split(" ")[0].split(",")
        self.assertEqual(top, ["a:0.2000", "b:0.2000", "c:0.2000", "d:0.2000", "e:0.1000"])

    def test_fewer_than_five_options_are_all_shown(self) -> None:
        top = format_choice("G-A", "s", "a", {"a": 0.9, "none_or_other": 0.1}, None).split("top5=")[1].split(" ")[0].split(",")
        self.assertEqual(top, ["a:0.9000", "none_or_other:0.1000"])

    def test_a_threshold_is_printed_as_a_decimal_of_four_places(self) -> None:
        self.assertTrue(format_choice("G-A", "s", "a", {"a": 1.0}, Fraction(9000, 10000)).endswith("threshold=0.9000"))

    def test_no_number_in_a_line_is_a_digit_run_of_five_or_more_whatever_the_probability(self) -> None:
        for p in (0.0, 1.0, 0.99999, 0.00001, 0.12345, 0.5):
            line = format_choice("G-A", "requested_remedy", "a", {"a": p, "none_or_other": 1.0 - p}, Fraction(1, 1))
            self.assertLess(max(digit_runs(line) or [0]), 5, line)
        self.assertIn("a:1.0000", format_choice("G-A", "s", "a", {"a": 0.99999}, None))  # rounded to four places, never five digits

    def test_an_id_that_is_not_in_the_shape_of_a_pack_id_is_never_printed(self) -> None:
        line = format_choice("G-A", "requested_remedy", "Send 5000 to Tunde Adeyemi", {"Send 5000 to Tunde Adeyemi": 0.9, "fin.cap.ok": 0.1}, None)
        self.assertNotIn("Tunde", line)
        self.assertNotIn("5000", line)
        self.assertIn("invalid_id", line)

    def test_a_case_id_that_holds_a_digit_run_of_five_or_more_is_never_printed(self) -> None:
        for bad in ("BEN-12345", "BEN-1-2-3-4-5", "G-A99999", "BEN-CLEAN-0001"[:4] + "0123456789"):
            line = format_choice(bad, "requested_remedy", None, {"none_or_other": 1.0}, None)
            self.assertTrue(line.startswith("CHOICE invalid_id "), line)
        self.assertTrue(format_choice("BEN-CLEAN-0001", "requested_remedy", None, {"none_or_other": 1.0}, None).startswith("CHOICE BEN-CLEAN-0001 "))  # four digits: the example's own id

    def test_a_probability_outside_zero_and_one_is_clamped_not_printed_raw(self) -> None:
        line = format_choice("G-A", "s", "a", {"a": 1.7, "b": -0.2}, None)
        self.assertIn("a:1.0000", line)
        self.assertIn("b:0.0000", line)


ID = r"(?:[a-z][a-z0-9_.]{0,63}|invalid_id)"
DEC = r"[01]\.\d{4}"
LINE = re.compile(rf"CHOICE (?:[A-Z][A-Z0-9-]{{0,31}}|invalid_id) slot={ID} chosen={ID} top5=(?:{ID}:{DEC}(?:,{ID}:{DEC}){{0,4}}|none) threshold=(?:{DEC}|n/a)")
HOSTILE = ["Wetin happen to the 20k wey I send Tunde this morning?", "Send 5000 to Tunde Adeyemi", "usr_evil ten_evil", "0123456789012345", "a b", "a\nb", "a,b:c=d", "x" * 200, "", "é", "A", "9abc",
           "fin.cap.ok", "none_or_other", "a=b top5=c", "threshold=0.9", "CHOICE G-A", "ses_abcdefghijklmnopqrstuvwxyz", "'; DROP", "\u0661\u0662\u0663\u0664\u0665"]


class Property(unittest.TestCase):
    """A612: whatever the option names, the slot, the case id, the chosen option, the probabilities and the threshold are, a line is the fixed words, an id-shaped option or a decimal probability, and nothing else. A string
    that carries a word of the request is either not id-shaped (printed as invalid_id) or never reaches the line."""

    def test_every_line_is_made_only_of_the_fixed_words_id_shaped_options_and_decimals(self) -> None:
        import random
        rng = random.Random(612)
        for _ in range(4000):
            names = [rng.choice(HOSTILE) if rng.random() < 0.7 else f"fin.cap.{rng.choice('abcdef')}" for _ in range(rng.randint(0, 9))]
            probs = {n: rng.choice([0.0, 1.0, rng.random(), rng.uniform(-3, 3), 0.99999, 1e-9, float(rng.randint(0, 5))]) for n in names}
            chosen = rng.choice([None, *HOSTILE, "fin.cap.a"])
            threshold = rng.choice([None, Fraction(rng.randint(0, 20000), 10000), Fraction(9, 10)])
            line = format_choice(rng.choice(["G-A", "BEN-CLEAN-0001", *HOSTILE]), rng.choice(["requested_remedy", *HOSTILE]), chosen, probs, threshold)
            self.assertRegex(line, LINE.pattern)
            self.assertIsNotNone(LINE.fullmatch(line), line)
            self.assertLess(max(digit_runs(line) or [0]), 5, line)

    def test_no_word_of_a_hostile_string_survives_into_a_line(self) -> None:
        for word in ("Tunde", "Adeyemi", "usr_evil", "ten_evil", "DROP", "wey"):
            line = format_choice("G-A", "requested_remedy", None, {f"{word} x": 0.5, "none_or_other": 0.5}, None)
            self.assertNotIn(word, line)


class Script:
    """A compile function that answers every golden with its good proposal and a fixed set of choice lines per case."""

    def __init__(self, fail: tuple[str, ...] = ()) -> None:
        self.calls, self.fail = 0, fail

    async def __call__(self, g: Golden) -> Mapping[str, Any]:
        self.calls += 1
        if g.case_id in self.fail:
            from benchmark.compile_acceptance.runner import CompileFailed
            raise CompileFailed("no usable reply")
        return good(g.case_id)


def lines_for(case_id: str) -> list[str]:
    return [format_choice(case_id, "requested_remedy", None, PROBS, None)]


class Runner(unittest.IsolatedAsyncioTestCase):
    async def test_every_golden_that_ran_carries_its_choice_lines_met_or_not(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            s = Script(fail=("G-B",))
            rec = recorder(Path(tmp))
            result = await run_cases(GOLDEN, s, BudgetGuard(limit_micros=5_000_000), rec, calls=lambda: s.calls, evidence=lines_for)
        self.assertEqual([c.evidence for c in result.cases], [tuple(lines_for(c.case_id)) for c in result.cases])  # the failed one too: what the model said before the failure is the evidence

    async def test_a_case_the_stop_never_reached_carries_no_lines(self) -> None:
        from benchmark.compile_acceptance.test_runner import Script as Spending
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=2_500, spent_micros=0)
            s = Spending(guard, spend=1_000)
            result = await run_cases(GOLDEN, s, guard, recorder(Path(tmp)), calls=lambda: s.calls, evidence=lines_for)
        self.assertEqual([c.status for c in result.cases], ["MET", "MET", "NOT_RUN", "NOT_RUN", "NOT_RUN"])
        self.assertEqual([bool(c.evidence) for c in result.cases], [True, True, False, False, False])

    async def test_a_run_without_an_evidence_function_is_unchanged(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            s = Script()
            rec = recorder(Path(tmp))
            result = await run_cases(GOLDEN, s, BudgetGuard(limit_micros=5_000_000), rec, calls=lambda: s.calls)
        self.assertTrue(all(c.evidence == () for c in result.cases))


class Summary(unittest.TestCase):
    def test_the_summary_lists_the_choice_lines_after_the_table_and_keeps_the_failing_line_first(self) -> None:
        result = RunResult(cases=[CaseResult("G-A", "MISSED", ("requested_remedy: present where none is expected",), tuple(lines_for("G-A"))), CaseResult("G-C", "MET", (), tuple(lines_for("G-C")))],
                           spent_micros=2_000_000, calls=9)
        text = render_summary(result)
        self.assertEqual(text.splitlines()[0], "FAILING: G-A")
        self.assertIn("## Model choices (option ids and probabilities only)", text)
        for case_id in ("G-A", "G-C"):
            self.assertIn(lines_for(case_id)[0], text)
        self.assertLess(max(digit_runs(text) or [0]), 5)

    def test_a_summary_without_choice_lines_has_no_such_section(self) -> None:
        text = render_summary(RunResult(cases=[CaseResult("G-A", "MET")], spent_micros=1, calls=1))
        self.assertNotIn("Model choices", text)


class WithChoices(Backend):
    def choice_lines(self, case_id: str) -> list[str]:
        return lines_for(case_id)


class Command(unittest.TestCase):
    def test_the_command_prints_each_goldens_choice_lines_to_the_job_log_and_writes_them_to_the_summary(self) -> None:
        import contextlib
        import io
        with tempfile.TemporaryDirectory() as tmp:
            built: list[Any] = []

            def factory(dry: bool, env: Mapping[str, str], guard: BudgetGuard, recorder: Recorder, args: Any) -> Backend:
                b = WithChoices(guard, recorder, dry=dry)
                b._costs = []
                built.append(b)
                return b

            out, err = io.StringIO(), io.StringIO()
            with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
                code = main(argv(tmp), {"OPENAI_API_KEY": KEY}, factory)
            log = out.getvalue()
            summary = (Path(tmp) / "out" / "summary.md").read_text(encoding="utf-8")
        self.assertEqual(code, 0)
        for g in GOLDEN:
            self.assertIn(lines_for(g.case_id)[0], log)
            self.assertIn(lines_for(g.case_id)[0], summary)
        self.assertNotIn(KEY, log + summary)

    def test_a_backend_without_choice_lines_prints_none_and_the_command_still_runs(self) -> None:
        import contextlib
        import io
        with tempfile.TemporaryDirectory() as tmp:
            def factory(dry: bool, env: Mapping[str, str], guard: BudgetGuard, recorder: Recorder, args: Any) -> Backend:
                b = Backend(guard, recorder, dry=dry)
                b._costs = []
                return b

            out = io.StringIO()
            with contextlib.redirect_stdout(out), contextlib.redirect_stderr(io.StringIO()):
                code = main(argv(tmp), {"OPENAI_API_KEY": KEY}, factory)
        self.assertEqual(code, 0)
        self.assertNotIn("CHOICE ", out.getvalue())


if __name__ == "__main__":
    unittest.main()
