"""UND-16 (noema#546, A198): the run loop. Each golden example once, in order, through one compile function; the budget guard stops the run and keeps it stopped; a run that made no provider call is
NOT RUN (exit 3), never a pass. The compile function here is a scripted stand-in for the provider and says so (AGENT-12 exception, noema#412): it proves the loop, not any model. SYNTHETIC."""
import json
import tempfile
import unittest
from collections.abc import Mapping
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

from benchmark.bakeoff.budget import BudgetGuard
from benchmark.compile_acceptance.golden import GOLDEN, Golden
from benchmark.compile_acceptance.recording import Recorder
from benchmark.compile_acceptance.runner import CompileFailed, run_cases, write_run_record
from benchmark.compile_acceptance.test_golden import good


class Script:
    """A scripted compile: it answers with a good proposal unless told otherwise, counts its calls and can spend from the guard like the transport would."""

    def __init__(self, guard: BudgetGuard, *, spend: int = 0, bad: Mapping[str, Any] | None = None, fail: frozenset[str] = frozenset(), bug: frozenset[str] = frozenset()) -> None:
        self.guard, self.spend, self.bad, self.fail, self.bug, self.calls = guard, spend, dict(bad or {}), fail, bug, 0

    async def __call__(self, g: Golden) -> Mapping[str, Any]:
        self.guard.authorize(self.spend)
        self.guard.settle(self.spend, self.spend)
        self.calls += 1
        if g.case_id in self.bug:
            raise RuntimeError("a bug in the compile")
        if g.case_id in self.fail:
            raise CompileFailed("no usable reply")
        return self.bad.get(g.case_id, good(g.case_id))


def recorder(root: Path) -> Recorder:
    return Recorder(root / "out", run_id="r1", provider="openai", model_id="gpt-6-luna", now=lambda: datetime(2026, 11, 17, tzinfo=UTC))


class Loop(unittest.IsolatedAsyncioTestCase):
    async def test_every_example_met_is_exit_zero_with_every_case_recorded_in_order(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=5_000_000, spent_micros=326_693)
            s = Script(guard, spend=1_000)
            result = await run_cases(GOLDEN, s, guard, recorder(Path(tmp)), calls=lambda: s.calls)
            self.assertEqual([(c.case_id, c.status) for c in result.cases], [(g.case_id, "MET") for g in GOLDEN])
            self.assertEqual((result.exit_code, result.calls, result.spent_micros, result.stopped), (0, 5, 326_693 + 5_000, False))

    async def test_a_missed_example_is_exit_one_and_the_others_still_run(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=5_000_000)
            wrong = {"intent_node": "fin.meta.out_of_scope", "slots": {}}
            s = Script(guard, bad={"G-B": wrong})
            result = await run_cases(GOLDEN, s, guard, recorder(Path(tmp)), calls=lambda: s.calls)
            self.assertEqual([c.status for c in result.cases], ["MET", "MISSED", "MET", "MET", "MET"])
            self.assertTrue(result.cases[1].problems)
            self.assertEqual(result.exit_code, 1)

    async def test_a_compile_that_failed_is_an_error_recorded_not_a_pass_and_the_run_goes_on(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=5_000_000)
            s = Script(guard, fail=frozenset({"G-C"}))
            result = await run_cases(GOLDEN, s, guard, recorder(Path(tmp)), calls=lambda: s.calls)
            self.assertEqual([c.status for c in result.cases], ["MET", "MET", "ERROR", "MET", "MET"])
            self.assertEqual(result.exit_code, 1)

    async def test_a_bug_is_not_hidden_as_a_failed_compile(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=5_000_000)
            s = Script(guard, bug=frozenset({"G-A"}))
            with self.assertRaises(RuntimeError):
                await run_cases(GOLDEN, s, guard, recorder(Path(tmp)), calls=lambda: s.calls)

    async def test_the_stop_ends_the_run_and_what_was_not_reached_is_not_run(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=2_500, spent_micros=0)
            s = Script(guard, spend=1_000)
            result = await run_cases(GOLDEN, s, guard, recorder(Path(tmp)), calls=lambda: s.calls)
            self.assertEqual([c.status for c in result.cases], ["MET", "MET", "NOT_RUN", "NOT_RUN", "NOT_RUN"])
            self.assertTrue(result.stopped)
            self.assertEqual((result.exit_code, result.calls), (1, 2))
            self.assertTrue(guard.stopped)

    async def test_a_stopped_guard_refuses_the_next_example_even_if_it_would_be_cheaper(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=1_000)
            guard.stopped = True
            s = Script(guard, spend=0)
            result = await run_cases(GOLDEN, s, guard, recorder(Path(tmp)), calls=lambda: s.calls)
            self.assertEqual({c.status for c in result.cases}, {"NOT_RUN"})
            self.assertEqual(s.calls, 0)

    async def test_a_run_that_made_no_provider_call_is_not_run_and_exit_three_even_if_every_check_is_met(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=5_000_000)
            s = Script(guard)
            result = await run_cases(GOLDEN, s, guard, recorder(Path(tmp)), calls=lambda: 0)
            self.assertEqual(result.exit_code, 3)


class Record(unittest.IsolatedAsyncioTestCase):
    async def test_the_run_record_says_what_was_run_and_holds_no_text_of_an_utterance_or_a_proposal(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            guard = BudgetGuard(limit_micros=5_000_000, spent_micros=326_693)
            s = Script(guard, spend=1_000, fail=frozenset({"G-C"}))
            result = await run_cases(GOLDEN, s, guard, recorder(Path(tmp)), calls=lambda: s.calls)
            path = write_run_record(Path(tmp) / "out", "r1", result, {"hard_stop_micros": 5_000_000, "prior_spent_micros": 326_693})
            text = path.read_text()
            doc = json.loads(text)
            self.assertEqual(path, Path(tmp) / "out" / "run.json")
            self.assertEqual((doc["run_id"], doc["exit_code"], doc["calls"], doc["spent_micros"], doc["stopped"]), ("r1", 1, 5, 331_693, False))
            self.assertEqual([(c["case_id"], c["status"]) for c in doc["cases"]], [(c.case_id, c.status) for c in result.cases])
            self.assertEqual(doc["limits"], {"hard_stop_micros": 5_000_000, "prior_spent_micros": 326_693})
            self.assertEqual(doc["label"], "Understanding: compile acceptance on synthetic golden examples, recorded live output (SYNTHETIC)")
            for g in GOLDEN:
                self.assertNotIn(g.text, text)
            self.assertEqual(text, json.dumps(doc, indent=2, sort_keys=True) + "\n")


if __name__ == "__main__":
    unittest.main()
