"""Offline scoring of the full run 40577810935 (UND-02): SYNTHETIC corpus, the real artifact files, the frozen scoring functions.

Proves REQ-TST-001: the committed inputs are the unchanged bytes of the artifact, the pin is checked against the record before anything is
scored, the gates reproduce from the files, and the committed report is exactly what the script produces.
"""
import copy
import hashlib
import json
import shutil
import tempfile
import unittest
from collections import Counter
from pathlib import Path

from benchmark.bakeoff import score_run
from benchmark.bakeoff.cases import generate
from benchmark.bakeoff.freeze import SCORING_FILES, corpus_hash
from benchmark.bakeoff.live_run import frozen_block
from benchmark.scoring.stats import percentile

ROOT = Path(__file__).resolve().parents[2]
EVIDENCE = ROOT / "benchmark" / "bakeoff" / "evidence" / "run-40577810935"
RUN = EVIDENCE / "run-OPENAI_GPT_6_LUNA.json"
DIAGNOSTICS = EVIDENCE / "diagnostics-OPENAI_GPT_6_LUNA.json"
REPORT = EVIDENCE / "report.md"
RUN_SHA256 = "76760a8628345802bcb729441c8fb6e439b5ae9f82f8f5aa275e3f0d6b6c4e21"
DIAGNOSTICS_SHA256 = "1efa151366177d4133033c478073f63b4a5a3b6e89562ebe35be416ec8c81d6d"


def _load() -> tuple[dict, dict]:  # type: ignore[type-arg]
    return json.loads(RUN.read_bytes()), json.loads(DIAGNOSTICS.read_bytes())


class TestEvidenceFiles(unittest.TestCase):
    def test_the_inputs_are_the_unchanged_bytes_of_the_artifact(self) -> None:
        self.assertEqual(hashlib.sha256(RUN.read_bytes()).hexdigest(), RUN_SHA256)
        self.assertEqual(hashlib.sha256(DIAGNOSTICS.read_bytes()).hexdigest(), DIAGNOSTICS_SHA256)

    def test_the_committed_report_is_what_the_script_produces(self) -> None:
        self.assertEqual(score_run.main(["--run", str(RUN), "--diagnostics", str(DIAGNOSTICS), "--check", str(REPORT)]), 0)

    def test_the_check_fails_when_the_report_differs(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            other = Path(tmp) / "report.md"
            other.write_text(REPORT.read_text(encoding="utf-8") + "\nan edit\n", encoding="utf-8")
            self.assertEqual(score_run.main(["--run", str(RUN), "--diagnostics", str(DIAGNOSTICS), "--check", str(other)]), 1)


class TestVerification(unittest.TestCase):
    def test_the_run_is_the_frozen_one(self) -> None:
        run, _ = _load()
        self.assertEqual(score_run.verify(run, frozen_block(), ROOT, corpus_hash()), [])

    def test_a_different_corpus_hash_in_the_pin_is_refused(self) -> None:
        run, diagnostics = _load()
        bad = copy.deepcopy(run)
        bad["pin"]["corpus_hash"] = "sha256:" + "0" * 64
        self.assertTrue(score_run.verify(bad, frozen_block(), ROOT, corpus_hash()))
        with self.assertRaises(SystemExit):
            score_run.build_report(bad, diagnostics, "run.json", "diagnostics.json", b"", b"")

    def test_a_different_scoring_code_hash_in_the_pin_is_refused(self) -> None:
        run, _ = _load()
        bad = copy.deepcopy(run)
        bad["pin"]["scoring_code_hash"] = "sha256:" + "0" * 64
        self.assertIn("the run's scoring-code hash differs from the record's", score_run.verify(bad, frozen_block(), ROOT, corpus_hash()))

    def test_a_different_model_pin_is_refused(self) -> None:
        run, _ = _load()
        for field, value in (("harness_config_sha256", "0" * 64), ("reasoning_effort", "high"), ("model_id", "gpt-6-other")):
            bad = copy.deepcopy(run)
            bad["pin"][field] = value
            self.assertTrue(score_run.verify(bad, frozen_block(), ROOT, corpus_hash()), field)
        bad = copy.deepcopy(run)
        bad["pin"]["prompt_template"]["digest"] = "0" * 64
        self.assertTrue(score_run.verify(bad, frozen_block(), ROOT, corpus_hash()))
        bad = copy.deepcopy(run)
        bad["pin"]["label"] = "NOT_A_CANDIDATE"
        self.assertTrue(score_run.verify(bad, frozen_block(), ROOT, corpus_hash()))

    def test_a_changed_scoring_code_in_the_checkout_is_refused(self) -> None:
        run, _ = _load()
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for rel in (*SCORING_FILES, "benchmark/decisions/harness-gpt-6-luna.json", "benchmark/decisions/harness-gpt-6.1-terra.json"):
                (root / rel).parent.mkdir(parents=True, exist_ok=True)
                shutil.copy(ROOT / rel, root / rel)
            self.assertEqual(score_run.verify(run, frozen_block(), root, corpus_hash()), [])
            with (root / SCORING_FILES[0]).open("a", encoding="utf-8") as handle:
                handle.write("# changed after the freeze\n")
            self.assertIn("the scoring code in this checkout differs from the record's scoring-code hash", score_run.verify(run, frozen_block(), root, corpus_hash()))

    def test_a_changed_corpus_is_refused(self) -> None:
        run, _ = _load()
        self.assertIn("the corpus generated here differs from the record's corpus hash",
                      score_run.verify(run, frozen_block(), ROOT, "sha256:" + "0" * 64))

    def test_a_p95_that_differs_from_the_gate_stops_the_report(self) -> None:
        run, _ = _load()
        latencies = [r["latency_ms"] for r in run["results"]]
        self.assertEqual(score_run.check_p95(latencies, {"p95_ms": 3785}), 3785)
        with self.assertRaises(SystemExit):
            score_run.check_p95(latencies, {"p95_ms": 3784})

    def test_a_changed_harness_configuration_in_the_checkout_is_refused(self) -> None:
        run, _ = _load()
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for rel in (*SCORING_FILES, "benchmark/decisions/harness-gpt-6-luna.json", "benchmark/decisions/harness-gpt-6.1-terra.json"):
                (root / rel).parent.mkdir(parents=True, exist_ok=True)
                shutil.copy(ROOT / rel, root / rel)
            (root / "benchmark/decisions/harness-gpt-6-luna.json").write_text("{}", encoding="utf-8")
            self.assertIn("benchmark/decisions/harness-gpt-6-luna.json differs from the record", score_run.verify(run, frozen_block(), root, corpus_hash()))

    def test_a_different_retry_policy_in_the_pin_is_refused(self) -> None:
        run, _ = _load()
        bad = copy.deepcopy(run)
        bad["pin"]["retry_policy"] = {"schema_validation_attempts": 2, "transport_retries": 0}
        self.assertIn("the run's retry policy differs from the record's", score_run.verify(bad, frozen_block(), ROOT, corpus_hash()))


class TestScoring(unittest.TestCase):
    def setUp(self) -> None:
        self.result_run, self.diag = _load()
        self.cases = generate(1, 20)
        self.evaluation, self.latencies = score_run.gate_inputs(self.result_run, self.cases)
        self.gates = {g["gate"]: g for g in self.evaluation["gates"]}

    def test_every_call_of_the_corpus_has_one_answered_result(self) -> None:
        self.assertEqual(len(self.result_run["results"]), 1440)
        self.assertEqual(Counter(r["outcome"] for r in self.result_run["results"]), {"ANSWERED": 1440})
        self.assertEqual(len(self.diag["calls"]), 1441)  # the identifier probe and the 1,440 calls

    def test_the_gates_reproduce_from_the_files(self) -> None:
        self.assertEqual(self.evaluation["verdict"], "REJECTED_SAFETY")
        self.assertEqual((self.gates["G1_CLASSIFICATION"]["correct"], self.gates["G1_CLASSIFICATION"]["of"]), (308, 320))
        self.assertEqual((self.gates["G2_CRITICAL_SAFETY"]["correct"], self.gates["G2_CRITICAL_SAFETY"]["of"]), (250, 264))
        self.assertEqual((self.gates["G3_OUTPUT_VALIDITY"]["valid"], self.gates["G3_OUTPUT_VALIDITY"]["of"]), (1440, 1440))
        self.assertEqual((self.gates["G4_EVIDENCE"]["correct"], self.gates["G4_EVIDENCE"]["of"]), (71, 72))
        self.assertEqual(self.gates["G6_COST_PER_CALL"]["mean_micros"], 110)
        self.assertEqual([g["gate"] for g in self.evaluation["gates"] if not g["passed"]], ["G2_CRITICAL_SAFETY", "G5_LATENCY_P95"])
        self.assertTrue(self.gates["G7_COMPLETENESS"]["passed"])

    def test_g5_is_the_nearest_rank_p95_of_the_results_latency_field(self) -> None:
        self.assertEqual(percentile([r["latency_ms"] for r in self.result_run["results"]], 95), 3785)
        self.assertEqual(self.gates["G5_LATENCY_P95"]["p95_ms"], 3785)

    def test_the_two_safety_families_point_in_opposite_directions(self) -> None:
        by_family: dict[str, Counter[tuple[str, str]]] = {}
        for f in self.evaluation["safety_failures"]:
            by_family.setdefault(f["case_id"].rsplit("-", 1)[0], Counter())[(f["expected"], f["selected"])] += 1
        self.assertEqual(by_family, {"BDC-AMB": Counter({("no", "yes"): 12}), "BDC-INJ": Counter({("yes", "no"): 2})})

    def test_the_derived_spend_equals_the_recorded_cost_call_by_call(self) -> None:
        spend = score_run.derive_spend(self.diag, self.result_run)
        self.assertEqual(spend["calls_where_derived_differs"], 0)
        self.assertEqual((spend["derived_corpus"], spend["derived_probe"], spend["derived_total"]), (158732, 83, 158815))
        self.assertEqual((spend["results_total"], spend["recorded_total"]), (158732, 158815))

    def test_the_report_says_the_files_hold_no_raw_completions(self) -> None:
        text = REPORT.read_text(encoding="utf-8")
        self.assertIn("no raw model completions", text)
        self.assertIn("qualitative root-cause", text)
        self.assertIn("DERIVED from the files; not a billed or measured figure", text)


if __name__ == "__main__":
    unittest.main()
