"""The live run's orchestration (benchmark/decisions/2026-10-27-model-selection.md), with a fake decider that answers from the oracle.

The fake stands in for the model service only at the Decider door; the budget guard, the pins, the gates and the reports are the real
ones. No live call exists here (SYNTHETIC)."""
import json
import tempfile
import unittest
from pathlib import Path
from typing import Any, Callable

from benchmark.bakeoff import gates
from benchmark.bakeoff.budget import BudgetGuard
from benchmark.bakeoff.cases import BUCKETS, TASKS, generate
from benchmark.bakeoff.live import Diagnostics
from benchmark.bakeoff.live_run import (
    MODELS, PILOT_PER_CELL, PreflightError, frozen_block, main, pilot_cases, pilot_markdown, pilot_summary, preflight, run_phase,
)
from benchmark.bakeoff.run import Answer, DeciderError

Json = dict[str, Any]
SECRET = "sk-test-not-a-real-key"
CASES = generate(1, 20)
FROZEN = frozen_block()
TEMPLATE = (FROZEN["prompt_template"]["version"], FROZEN["prompt_template"]["digest"])
ORACLE: dict[tuple[str, str], str] = {}
for _case in CASES:
    for _text in [_case["input"]["text"]] + [p["text"] for p in _case["perturbations"]]:
        ORACLE[(_text, _case["question"]["id"])] = _case["expected"]


class FakeDecider:
    """Answers from the oracle, except where told to be wrong, to fail or to cost more."""

    def __init__(self, label: str, guard: BudgetGuard, *, wrong_for: set[str] | None = None, cost: int = 10, stop_after: int | None = None,
                 reported: str = "reported-id", drift: bool = False) -> None:
        self.provider = label
        self._guard = guard
        self.wrong_for = wrong_for or set()
        self.cost = cost
        self.stop_after = stop_after
        self.reported_model_id = reported
        self.drifted = drift
        self.diagnostics = Diagnostics()
        self.calls = 0

    def ready(self) -> None:
        self.diagnostics.calls.append({"model_latency_ms": 1, "tokens_in": 50, "tokens_out": 10, "cost_micro": 7, "projected_micro": 9})

    def model_info(self) -> Json:
        return {"provider": "openai", "name": self.provider, "version": self.reported_model_id}

    def decide(self, question: Json, text: str, context: Json | None) -> Answer:
        if self.stop_after is not None and self.calls >= self.stop_after:
            self._guard.stopped = True
            raise DeciderError("budget")
        self.calls += 1
        expected = ORACLE[(text, question["id"])]
        selected = expected
        if text in self.wrong_for:
            selected = next(o for o in question["options"] if o != expected)
        self.diagnostics.calls.append({"model_latency_ms": 2, "tokens_in": 100, "tokens_out": 30, "cost_micro": self.cost, "projected_micro": 99})
        self._guard.settle(self.cost, self.cost)
        return Answer(selected, 0.9, self.cost)


class Factory:
    def __init__(self, options: dict[str, Json] | None = None) -> None:
        self.options = options or {}
        self.made: dict[str, FakeDecider] = {}

    def __call__(self, label: str, path: str, guard: BudgetGuard) -> FakeDecider:
        self.made[label] = FakeDecider(label, guard, **self.options.get(label, {}))
        return self.made[label]


def phase(name: str, out: Path, environ: dict[str, str] | None = None, prior: int = 0, models: dict[str, Json] | None = None) -> tuple[int, Factory]:
    f = Factory(models)
    code = run_phase(name, prior, out, {"OPENAI_API_KEY": SECRET} if environ is None else environ, f, TEMPLATE)
    return code, f


class Pilot(unittest.TestCase):
    def test_the_pilot_is_two_base_cases_per_task_and_bucket_in_id_order(self) -> None:
        chosen = pilot_cases(CASES)
        self.assertEqual(len(chosen), len(TASKS) * len(BUCKETS) * PILOT_PER_CELL)
        self.assertEqual([c["case_id"] for c in chosen], sorted(c["case_id"] for c in chosen))
        for task in TASKS:
            for bucket in BUCKETS:
                cell = sorted(c["case_id"] for c in CASES if c["task"] == task and c["language_bucket"] == bucket)
                self.assertEqual([c["case_id"] for c in chosen if c["task"] == task and c["language_bucket"] == bucket], cell[:2])

    def test_it_runs_both_models_writes_the_files_and_reports_the_estimate(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, _ = phase("pilot", Path(tmp), prior=0)
            self.assertEqual(code, 0)
            names = sorted(p.name for p in Path(tmp).iterdir())
            self.assertEqual(names, sorted(["summary.json", "summary.md"] + [f"{k}-{m[0]}.json" for m in MODELS for k in ("run", "diagnostics")]))
            summary = json.loads((Path(tmp) / "summary.json").read_text())
            for model in summary["models"]:
                self.assertEqual(model["status"], "RAN")
                self.assertEqual(model["cost_mean_micros"], 10)
                self.assertEqual((model["tokens_in_mean"], model["tokens_out_mean"]), (100, 30))  # the probe is not a corpus call
                self.assertEqual(model["full_corpus_estimate_micros"], 10 * 1440)
            self.assertEqual(summary["full_estimate_total_micros"], 2 * 10 * 1440)
            self.assertEqual(summary["remaining_budget_micros"], gates.BUDGET_TOTAL_MICROS - summary["spent_micros"])
            self.assertTrue(summary["fits_in_remaining_budget"])
            calls = sum(1 + len(c["perturbations"]) for c in pilot_cases(CASES))
            self.assertEqual(summary["spent_micros"], 2 * calls * 10)  # the fake settles its own cost; the probe is the fake's, not settled

    def test_the_estimates_use_the_mean_and_the_p95_of_the_calls_not_their_maximum(self) -> None:
        costs = [10, 20, 30, 40, 1000]
        runs = [{"status": "RAN", "model": {}, "results": [{"cost_usd_micros": c, "outcome": "ANSWERED"} for c in costs]}]
        diag = [{"label": "a", "calls": [{"cost_micro": 1, "tokens_in": 1, "tokens_out": 1}] * 6, "projection_exceeded": 0}]
        m = pilot_summary(runs, diag, 100, 10**9)["models"][0]
        self.assertEqual((m["cost_mean_micros"], m["cost_p95_micros"], m["cost_max_micros"]), (220, 1000, 1000))
        self.assertEqual((m["full_corpus_estimate_micros"], m["full_corpus_upper_micros"]), (22_000, 100_000))

    def test_fitting_is_judged_on_the_upper_estimate_not_the_mean(self) -> None:
        costs = [10, 10, 10, 10, 1000]
        runs = [{"status": "RAN", "model": {}, "results": [{"cost_usd_micros": c, "outcome": "ANSWERED"} for c in costs]}]
        diag = [{"label": "a", "calls": [{"cost_micro": 1, "tokens_in": 1, "tokens_out": 1}] * 6, "projection_exceeded": 0}]
        summary = pilot_summary(runs, diag, 100, 50_000)  # the mean estimate (20,800) fits, the P95 estimate (100,000) does not
        self.assertEqual((summary["full_estimate_total_micros"], summary["full_upper_total_micros"]), (20_800, 100_000))
        self.assertFalse(summary["fits_in_remaining_budget"])

    def test_a_pilot_that_cannot_fit_the_full_corpus_says_so(self) -> None:
        runs = [{"status": "RAN", "model": {}, "results": [{"cost_usd_micros": 20_000, "outcome": "ANSWERED"}] * 4}] * 2
        diag = [{"label": "a", "calls": [{"cost_micro": 1, "tokens_in": 1, "tokens_out": 1}] * 5, "projection_exceeded": 0}] * 2
        summary = pilot_summary(runs, diag, 1440, 25_000_000)
        self.assertFalse(summary["fits_in_remaining_budget"])
        self.assertIn("fits: False", pilot_markdown(summary))

    def test_the_pilot_cannot_pass_its_cap(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, f = phase("pilot", Path(tmp), prior=gates.BUDGET_PILOT_MICROS - 5)
            self.assertEqual(code, 0)
            # the guard of the phase starts at the prior spend and carries the $2 cap
            guard = next(iter(f.made.values()))._guard
            self.assertEqual(guard.limit_micros, gates.BUDGET_PILOT_MICROS)
            self.assertGreaterEqual(guard.spent_micros, gates.BUDGET_PILOT_MICROS - 5)

    def test_without_the_key_nothing_runs_and_nothing_is_written(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, _ = phase("pilot", Path(tmp) / "out", environ={})
            self.assertEqual(code, 3)
            self.assertFalse((Path(tmp) / "out").exists())


class Full(unittest.TestCase):
    def test_a_perfect_oracle_follower_passes_every_gate_for_both_models_and_no_winner_is_named(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            code, _ = phase("full", Path(tmp))
            self.assertEqual(code, 0)
            summary = json.loads((Path(tmp) / "summary.json").read_text())
            self.assertEqual([e["verdict"] for e in summary["evaluations"]], ["PASSES_ALL_GATES"] * 2, summary["evaluations"][0]["gates"])
            self.assertIsNone(summary["selection"]["winner"])
            text = (Path(tmp) / "summary.md").read_text()
            for gate in ("G1_CLASSIFICATION", "G2_CRITICAL_SAFETY", "G3_OUTPUT_VALIDITY", "G4_EVIDENCE", "G5_LATENCY_P95", "G6_COST_PER_CALL", "G7_COMPLETENESS"):
                self.assertEqual(text.count(gate), 2, gate)
            self.assertIn("No winner is declared", text)

    def test_one_missed_injection_is_a_safety_failure_listed_apart_and_an_automatic_rejection(self) -> None:
        victim = next(c for c in CASES if c["task"] == "INSTRUCTION_SCREEN")
        with tempfile.TemporaryDirectory() as tmp:
            phase("full", Path(tmp), models={MODELS[0][0]: {"wrong_for": {victim["input"]["text"]}}})
            summary = json.loads((Path(tmp) / "summary.json").read_text())
            first, second = summary["evaluations"]
            self.assertEqual((first["verdict"], second["verdict"]), ("REJECTED_SAFETY", "PASSES_ALL_GATES"))
            self.assertTrue(any(f["case_id"] == victim["case_id"] for f in first["safety_failures"]))
            text = (Path(tmp) / "summary.md").read_text()
            self.assertIn("**Safety failures (", text)
            self.assertEqual(summary["selection"]["models_passing_every_gate"], 1)

    def test_a_run_the_guard_stopped_carries_the_flag_and_has_no_verdict(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            phase("full", Path(tmp), models={MODELS[0][0]: {"stop_after": 100}})
            summary = json.loads((Path(tmp) / "summary.json").read_text())
            run = json.loads((Path(tmp) / f"run-{MODELS[0][0]}.json").read_text())
            self.assertTrue(run["budget_stopped"])
            self.assertEqual(summary["evaluations"][0]["verdict"], "INCOMPLETE")  # not a safety rejection: the calls were never made
            self.assertFalse(next(g for g in summary["evaluations"][0]["gates"] if g["gate"] == "G7_COMPLETENESS")["passed"])

    def test_a_run_whose_identifier_drifted_carries_the_flag_and_has_no_verdict(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            phase("full", Path(tmp), models={MODELS[0][0]: {"drift": True}})
            run = json.loads((Path(tmp) / f"run-{MODELS[0][0]}.json").read_text())
            self.assertTrue(run["pin_drift"])
            self.assertEqual(json.loads((Path(tmp) / "summary.json").read_text())["evaluations"][0]["verdict"], "INCOMPLETE")

    def test_the_run_file_carries_the_pins_the_record_requires(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            phase("full", Path(tmp))
            pin = json.loads((Path(tmp) / f"run-{MODELS[0][0]}.json").read_text())["pin"]
            self.assertEqual((pin["model_id"], pin["reported_model_id"], pin["fetched_on"], pin["reasoning_effort"]),
                             ("gpt-6-luna", "reported-id", "2026-10-27", "medium"))
            self.assertEqual(pin["rate_card"]["source"], "https://developers.openai.com/api/docs/models/gpt-6-luna")
            self.assertEqual((pin["corpus_hash"], pin["scoring_code_hash"], pin["prompt_template"]["digest"]),
                             (FROZEN["corpus"]["corpus_hash"], FROZEN["scoring_code"]["scoring_code_hash"], FROZEN["prompt_template"]["digest"]))

    def test_the_key_appears_in_no_output(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            phase("full", Path(tmp))
            for path in Path(tmp).iterdir():
                self.assertNotIn(SECRET, path.read_text(), path.name)


class Preflight(unittest.TestCase):
    def test_the_frozen_state_passes_and_each_difference_stops_the_run(self) -> None:
        preflight(FROZEN, TEMPLATE[1], TEMPLATE[0])
        with self.assertRaises(PreflightError):
            preflight(FROZEN, "0" * 64, TEMPLATE[0])
        with self.assertRaises(PreflightError):
            preflight(FROZEN, TEMPLATE[1], TEMPLATE[0] + 1)
        changes: list[Callable[[Json], Any]] = [
            lambda f: f["corpus"].update(corpus_hash="sha256:x"), lambda f: f["scoring_code"].update(scoring_code_hash="sha256:x"),
            lambda f: f["harness_configs"].update({k: "0" * 64 for k in f["harness_configs"]})]
        for mutate in changes:
            copy = json.loads(json.dumps(FROZEN))
            mutate(copy)
            with self.assertRaises(PreflightError):
                preflight(copy, TEMPLATE[1], TEMPLATE[0])

    def test_a_preflight_failure_sends_nothing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            f = Factory()
            with self.assertRaises(PreflightError):
                run_phase("full", 0, Path(tmp) / "out", {"OPENAI_API_KEY": SECRET}, f, (1, "0" * 64))
            self.assertEqual(f.made, {})


class Command(unittest.TestCase):
    def test_a_negative_prior_spend_and_the_heldout_name_are_refused(self) -> None:
        with self.assertRaises(SystemExit):
            main(["--phase", "pilot", "--prior-spent-micros", "-1", "--out", "x"], {})
        self.assertEqual(main(["--phase", "pilot", "--prior-spent-micros", "0", "--out", "benchmark-heldout"], {}), 2)
        self.assertEqual(main(["--phase", "pilot", "--prior-spent-micros", "0", "--out", "x"], {"X": "benchmark-heldout"}), 2)

    def test_the_prior_spend_is_required(self) -> None:
        with self.assertRaises(SystemExit):
            main(["--phase", "pilot", "--out", "x"], {})


if __name__ == "__main__":
    unittest.main()
