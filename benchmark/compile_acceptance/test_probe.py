"""A772 item 4 (interim; amends A605; UND-16; INV-22, REQ-TST-006, AGENT-13, AGENT-16): the labelled probe. BO's 28 labelled phrasings (`remedy_labelled.v1.json`, a development set: neither the goldens nor the
held-out set) are run once through the compile service, and the gate's answer for each is scored against its label: a false assertion (an explicit ask asserted for a phrasing that is not one), a false
negative (an explicit ask the gate did not assert) and a borderline item are reported APART. Run 9 is allowed only if no false assertion stands on the 14 non-explicit phrasings, borderline ones included.
The probe decides nothing and sets no threshold. The backend here is a scripted stand-in (SYNTHETIC)."""
import contextlib
import io
import json
import tempfile
import unittest
from collections.abc import Mapping
from fractions import Fraction
from pathlib import Path
from typing import Any

from benchmark.bakeoff.budget import BudgetGuard
from benchmark.compile_acceptance.__main__ import main
from benchmark.compile_acceptance.evidence import format_choice
from benchmark.compile_acceptance.probe import ProbeCase, cases, parse_gate, score
from benchmark.compile_acceptance.recording import Recorder
from benchmark.compile_acceptance.remedy_labelled import load
from benchmark.compile_acceptance.runner import CompileFailed

LABELLED = Path(__file__).resolve().parent / "remedy_labelled.v1.json"
KEY = "sk-test-not-a-real-key-0000"
THRESHOLD = Fraction(9000, 10000)


def gate_line(case_id: str, yes: bool, p_yes: float) -> str:
    return format_choice(case_id, "requested_remedy_gate", "yes" if yes else "no", {"yes": p_yes}, THRESHOLD)


class Cases(unittest.TestCase):
    def test_there_is_one_case_per_labelled_item_with_its_own_text_and_a_distinct_correlation_id(self) -> None:
        items, built = load(LABELLED), cases()
        self.assertEqual([c.item.item_id for c in built], [i.item_id for i in items])
        self.assertEqual(len(built), 28)
        self.assertEqual({c.canonical["text"] for c in built}, {i.text for i in items})
        self.assertEqual(len({c.canonical["correlation_id"] for c in built}), 28)
        self.assertEqual(len({c.canonical["request_id"] for c in built}), 28)

    def test_a_case_carries_no_expectation_and_no_label(self) -> None:
        for c in cases():
            self.assertEqual(set(c.canonical), {"schema_version", "request_id", "correlation_id", "tenant_id", "session_id", "principal_ref", "channel", "mode", "text", "received_at"})

    def test_the_counts_are_the_declared_ones(self) -> None:
        items = load(LABELLED)
        self.assertEqual(sum(1 for i in items if not i.explicit_ask), 14)
        self.assertEqual(sum(1 for i in items if i.explicit_ask), 14)


class Parse(unittest.TestCase):
    def test_a_gate_line_is_read_as_the_answer_the_probability_of_yes_and_the_threshold(self) -> None:
        self.assertEqual(parse_gate(gate_line("RL-001", True, 0.95)), ("RL-001", "yes", Fraction(9500, 10000), THRESHOLD))
        self.assertEqual(parse_gate(gate_line("RL-002", False, 0.1)), ("RL-002", "no", Fraction(1000, 10000), THRESHOLD))

    def test_a_line_that_is_not_a_gate_line_is_not_read(self) -> None:
        self.assertIsNone(parse_gate(format_choice("RL-001", "requested_remedy", None, {"none_or_other": 0.9}, None)))
        self.assertIsNone(parse_gate("not a line"))


class Score(unittest.TestCase):
    def setUp(self) -> None:
        self.items = load(LABELLED)

    def lines(self, asserted: set[str]) -> dict[str, list[str]]:
        return {i.item_id: [gate_line(i.item_id, i.item_id in asserted, 0.95 if i.item_id in asserted else 0.05)] for i in self.items}

    def test_a_perfect_gate_has_no_false_assertion_and_no_false_negative(self) -> None:
        report = score(self.items, self.lines({i.item_id for i in self.items if i.explicit_ask}))
        self.assertEqual((report.false_positive, report.false_negative, report.borderline_false_positive, report.borderline_false_negative), ((), (), (), ()))
        self.assertTrue(report.no_false_assertion)

    def test_a_clear_non_ask_that_is_asserted_is_a_false_positive_and_a_borderline_one_is_reported_apart(self) -> None:
        clear = next(i for i in self.items if not i.explicit_ask and not i.borderline)
        edge = next(i for i in self.items if not i.explicit_ask and i.borderline)
        report = score(self.items, self.lines({i.item_id for i in self.items if i.explicit_ask} | {clear.item_id, edge.item_id}))
        self.assertEqual(report.false_positive, (clear.item_id,))
        self.assertEqual(report.borderline_false_positive, (edge.item_id,))
        self.assertFalse(report.no_false_assertion)  # the gate to run 9 counts the borderline ones among the 14

    def test_an_explicit_ask_the_gate_did_not_assert_is_a_false_negative_and_never_a_false_positive(self) -> None:
        ask = next(i for i in self.items if i.explicit_ask)
        report = score(self.items, self.lines({i.item_id for i in self.items if i.explicit_ask} - {ask.item_id}))
        self.assertEqual(report.false_negative, (ask.item_id,))
        self.assertEqual(report.false_positive, ())
        self.assertTrue(report.no_false_assertion)

    def test_a_yes_below_the_threshold_is_not_an_assertion(self) -> None:
        item = next(i for i in self.items if not i.explicit_ask and not i.borderline)
        lines = self.lines(set())
        lines[item.item_id] = [gate_line(item.item_id, True, 0.5)]  # the model said yes, in doubt: the gate abstains
        self.assertTrue(score(self.items, lines).no_false_assertion)

    def test_a_no_answer_is_never_an_assertion_whatever_the_probability_printed_beside_it(self) -> None:
        item = next(i for i in self.items if not i.explicit_ask and not i.borderline)
        lines = self.lines(set())
        lines[item.item_id] = [format_choice(item.item_id, "requested_remedy_gate", "no", {"yes": 0.95}, THRESHOLD)]
        self.assertTrue(score(self.items, lines).no_false_assertion)

    def test_one_borderline_non_ask_asserted_alone_is_enough_to_fail_the_condition_of_run_9(self) -> None:
        edge = next(i for i in self.items if not i.explicit_ask and i.borderline)
        report = score(self.items, self.lines({i.item_id for i in self.items if i.explicit_ask} | {edge.item_id}))
        self.assertEqual((report.false_positive, report.borderline_false_positive), ((), (edge.item_id,)))
        self.assertFalse(report.no_false_assertion)

    def test_a_yes_at_exactly_the_threshold_is_an_assertion(self) -> None:
        item = next(i for i in self.items if not i.explicit_ask and not i.borderline)
        lines = self.lines(set())
        lines[item.item_id] = [gate_line(item.item_id, True, 0.9)]
        self.assertEqual(score(self.items, lines).false_positive, (item.item_id,))

    def test_an_item_with_no_gate_line_is_named_apart_and_is_not_a_pass_for_an_explicit_ask(self) -> None:
        ask = next(i for i in self.items if i.explicit_ask)
        lines = self.lines({i.item_id for i in self.items if i.explicit_ask})
        lines[ask.item_id] = []
        report = score(self.items, lines)
        self.assertEqual(report.no_gate, (ask.item_id,))
        self.assertIn(ask.item_id, report.false_negative)

    def test_the_report_names_ids_and_counts_only(self) -> None:
        text = score(self.items, self.lines(set())).render()
        for item in self.items:
            self.assertNotIn(item.text, text)
        self.assertIn("false assertions on the non-explicit phrasings: 0 of 14", text)


class Backend:
    def __init__(self, guard: BudgetGuard, recorder: Recorder, *, asserted: set[str], dry: bool) -> None:
        self.guard, self.recorder, self.asserted, self.dry, self._calls = guard, recorder, asserted, dry, 0
        self._lines: dict[str, list[str]] = {}

    async def compile_probe(self, c: ProbeCase) -> Mapping[str, Any]:
        if self.dry:
            raise CompileFailed("dry run")
        self.recorder.case_id = c.item.item_id
        self.recorder.record_call(url="https://h.invalid/v1/responses", request=b'{"input":"x"}', status=200, reply=b'{"model":"m"}', failure=None, projected_micros=10, cost_micros=7)
        self.guard.authorize(10)
        self.guard.settle(7, 10)
        self._calls += 1
        yes = c.item.item_id in self.asserted
        self._lines[c.item.item_id] = [gate_line(c.item.item_id, yes, 0.95 if yes else 0.05)]
        return {}

    def choice_lines(self, case_id: str) -> list[str]:
        return list(self._lines.get(case_id, []))

    def calls(self) -> int:
        return self._calls

    def pin(self) -> dict[str, Any]:
        return {"pinned_model_id": "gpt-6-luna"}

    def dry_costs(self) -> list[tuple[str, int]]:
        return []


def run_probe_main(extra: list[str], asserted: set[str], environ: Mapping[str, str]) -> tuple[int, str, Path]:
    tmp = Path(tempfile.mkdtemp())

    def factory(dry: bool, env: Mapping[str, str], guard: BudgetGuard, recorder: Recorder, args: Any) -> Any:
        return Backend(guard, recorder, asserted=asserted, dry=dry)

    argv = ["--probe", "--pack-ref", "fin@1.0.0", "--provider", "openai", "--hard-stop-usd", "5", "--prior-spent-micros", "1000", "--out", str(tmp), "--run-id", "probe-test", *extra]
    out, err = io.StringIO(), io.StringIO()
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
        code = main(argv, environ, factory)
    return code, out.getvalue(), tmp


class Command(unittest.TestCase):
    def asks(self) -> set[str]:
        return {i.item_id for i in load(LABELLED) if i.explicit_ask}

    def test_a_clean_probe_runs_each_phrasing_once_writes_the_report_and_exits_zero(self) -> None:
        code, out, tmp = run_probe_main([], self.asks(), {"OPENAI_API_KEY": KEY})
        self.assertEqual(code, 0, out)
        self.assertIn("provider calls 28", out)
        self.assertIn("false assertions on the non-explicit phrasings: 0 of 14", out)
        record = json.loads((tmp / "probe.json").read_text())
        self.assertEqual(len(record["items"]), 28)
        self.assertEqual(record["calls"], 28)
        self.assertTrue((tmp / "probe.md").is_file())

    def test_a_false_assertion_exits_one_and_is_named(self) -> None:
        clear = next(i for i in load(LABELLED) if not i.explicit_ask and not i.borderline)
        code, out, _ = run_probe_main([], self.asks() | {clear.item_id}, {"OPENAI_API_KEY": KEY})
        self.assertEqual(code, 1)
        self.assertIn(clear.item_id, out)

    def test_no_key_is_not_run_and_calls_nothing(self) -> None:
        code, out, _ = run_probe_main([], self.asks(), {})
        self.assertEqual(code, 3)
        self.assertNotIn("provider calls", out)

    def test_the_probe_never_prints_a_phrasing(self) -> None:
        _, out, tmp = run_probe_main([], self.asks(), {"OPENAI_API_KEY": KEY})
        for item in load(LABELLED):
            self.assertNotIn(item.text, out)
            self.assertNotIn(item.text, (tmp / "probe.md").read_text())
            self.assertNotIn(item.text, (tmp / "probe.json").read_text())


if __name__ == "__main__":
    unittest.main()
