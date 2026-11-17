"""UND-16 (noema#546, A198): the command. Exit codes: 0 every example met; 1 a miss, an error or the stop; 2 a bad input; 3 NOT RUN (no key, or no provider call). The backend here is a scripted stand-in for
the provider (SYNTHETIC); the real one is built from the model service and is tested in services/model/tests/test_compile_acceptance_build.py."""
import contextlib
import io
import json
import tempfile
import unittest
from collections.abc import Mapping
from pathlib import Path
from typing import Any

from benchmark.bakeoff.budget import BudgetGuard
from benchmark.compile_acceptance.__main__ import main
from benchmark.compile_acceptance.golden import GOLDEN, Golden
from benchmark.compile_acceptance.recording import Recorder
from benchmark.compile_acceptance.runner import CompileFailed
from benchmark.compile_acceptance.test_golden import good

KEY = "sk-test-not-a-real-key-0000"


class Backend:
    def __init__(self, guard: BudgetGuard, recorder: Recorder, *, dry: bool, fail: bool = False) -> None:
        self.guard, self.recorder, self.dry, self.fail, self._calls = guard, recorder, dry, fail, 0

    async def compile_one(self, g: Golden) -> Mapping[str, Any]:
        if self.dry:
            self._costs.append((g.case_id, 777))
            raise CompileFailed("dry run")
        self.recorder.case_id = g.case_id
        self.recorder.record_call(url="https://h.invalid/v1/responses", request=b'{"input":"x"}', status=200, reply=b'{"model":"m"}', failure=None, projected_micros=10, cost_micros=7)
        self.guard.authorize(10)
        self.guard.settle(7, 10)
        self._calls += 1
        if self.fail:
            raise CompileFailed("no usable reply")
        return good(g.case_id)

    _costs: list[tuple[str, int]] = []

    def calls(self) -> int:
        return self._calls

    def pin(self) -> dict[str, Any]:
        return {"pinned_model_id": "gpt-6-luna"}

    def dry_costs(self) -> list[tuple[str, int]]:
        return list(self._costs)


def run_main(argv: list[str], environ: Mapping[str, str], *, fail: bool = False) -> tuple[int, str, str, list[Backend]]:
    built: list[Backend] = []

    def factory(dry: bool, env: Mapping[str, str], guard: BudgetGuard, recorder: Recorder, args: Any) -> Backend:
        b = Backend(guard, recorder, dry=dry, fail=fail)
        b._costs = []
        built.append(b)
        return b

    out, err = io.StringIO(), io.StringIO()
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
        try:
            code = main(argv, environ, factory)
        except SystemExit as exit_:
            code = int(exit_.code or 0)
    return code, out.getvalue(), err.getvalue(), built


def argv(tmp: str, *extra: str, dry: bool = False) -> list[str]:
    base = ["--pack-ref", "fin@1.0.0", "--provider", "openai", "--hard-stop-usd", "5", "--prior-spent-micros", "326693", "--out", str(Path(tmp) / "out"), "--run-id", "compile-gh1"]
    return (["--dry-run"] if dry else []) + base + list(extra)


class Inputs(unittest.TestCase):
    def test_an_input_that_is_not_valid_is_exit_two_and_builds_nothing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            for bad in (["--hard-stop-usd", "6"], ["--hard-stop-usd", "0"], ["--provider", "gemini"], ["--pack-ref", "fin@1.0"], ["--pack-ref", "Fin@1.0.0"], ["--run-id", "a b"], ["--run-id", "x" * 65], ["--prior-spent-micros", "-1"]):
                code, _, _, built = run_main(argv(tmp) + bad, {"OPENAI_API_KEY": KEY})
                self.assertEqual((code, built), (2, []), bad)

    def test_nothing_may_reach_the_held_out_set(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, _, err, built = run_main(argv(tmp, "--out", str(Path(tmp) / "benchmark-heldout")), {"OPENAI_API_KEY": KEY})
            self.assertEqual((code, built), (2, []))
            code, _, _, built = run_main(argv(tmp), {"OPENAI_API_KEY": KEY, "X": "benchmark-heldout/y"})
            self.assertEqual((code, built), (2, []))


class Running(unittest.TestCase):
    def test_a_run_without_the_key_is_not_run_exit_three_and_nothing_is_built_or_written(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            for env in ({}, {"OPENAI_API_KEY": ""}, {"OPENAI_API_KEY": "  "}):
                code, out, err, built = run_main(argv(tmp), env)
                self.assertEqual((code, built), (3, []), env)
                self.assertIn("not run", (out + err).lower())
            self.assertFalse((Path(tmp) / "out").exists())

    def test_a_run_that_meets_every_example_writes_the_recordings_the_manifest_and_the_run_record(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, out, _, built = run_main(argv(tmp), {"OPENAI_API_KEY": KEY})
            self.assertEqual(code, 0, out)
            root = Path(tmp) / "out"
            self.assertEqual(len(list((root / "recordings").glob("rec-*.json"))), 5)
            manifest = json.loads((root / "recordings" / "manifest.json").read_text())
            self.assertEqual((manifest["calls"], manifest["run_id"], manifest["pin"]), (5, "compile-gh1", {"pinned_model_id": "gpt-6-luna"}))
            run = json.loads((root / "run.json").read_text())
            self.assertEqual((run["exit_code"], run["limits"]["hard_stop_micros"], run["limits"]["prior_spent_micros"]), (0, 5_000_000, 326_693))
            self.assertIn("spent_micros_total=", out)
            self.assertEqual(len(built), 1)

    def test_the_key_is_in_no_file_and_no_line_of_output(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            _, out, err, _ = run_main(argv(tmp), {"OPENAI_API_KEY": KEY})
            self.assertNotIn(KEY, out + err)
            for path in (Path(tmp) / "out").rglob("*"):
                if path.is_file():
                    self.assertNotIn(KEY, path.read_text())

    def test_a_failed_compile_is_exit_one(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, _, _, _ = run_main(argv(tmp), {"OPENAI_API_KEY": KEY}, fail=True)
            self.assertEqual(code, 1)

    def test_a_prior_spend_that_leaves_no_room_stops_before_the_first_call(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, out, _, built = run_main(argv(tmp) + ["--prior-spent-micros", "5000000"], {"OPENAI_API_KEY": KEY})
            self.assertEqual(code, 1)
            self.assertEqual(json.loads((Path(tmp) / "out" / "run.json").read_text())["stopped"], True)
            self.assertEqual({c["status"] for c in json.loads((Path(tmp) / "out" / "run.json").read_text())["cases"]}, {"NOT_RUN"})


class DryRun(unittest.TestCase):
    def test_a_dry_run_needs_no_key_prints_the_cost_of_each_request_and_the_ceiling_and_calls_nothing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, out, _, built = run_main(argv(tmp, dry=True), {})
            self.assertEqual(code, 0, out)
            self.assertEqual(len(built), 1)
            self.assertTrue(built[0].dry)
            self.assertEqual(built[0].calls(), 0)
            for g in GOLDEN:
                self.assertIn(g.case_id, out)
            self.assertIn("777", out)
            self.assertIn("5000000", out)
            self.assertIn("326693", out)
            self.assertFalse((Path(tmp) / "out" / "recordings").exists())

    def test_a_dry_run_never_prints_a_key_even_if_one_is_set(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            _, out, err, _ = run_main(argv(tmp, dry=True), {"OPENAI_API_KEY": KEY})
            self.assertNotIn(KEY, out + err)


if __name__ == "__main__":
    unittest.main()
