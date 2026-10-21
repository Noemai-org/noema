"""The reconstruction format the runner reads. PROVISIONAL: PERD §39.4 says the endpoint returns structured JSON built from Record events
and gives no field names, so these are the smallest the scorer needs (AGENT-02). It changes in one module when a recorded body exists."""
import unittest
from pathlib import Path

from benchmark.contracts import Contracts
from benchmark.runner import reconstruction
from benchmark.runner.reconstruction import ReconstructionError, parse
from benchmark.runner.testdata import reconstruction as body


class Parse(unittest.TestCase):
    def test_observed_is_the_result_blocks_shape(self) -> None:
        obs = parse(body("ESCALATE", writes=[("fin.cap.create_escalation_case", "fin.transaction:TX1001")], events=["INPUT_RECEIVED", "DECISION_MADE", "DECISION_MADE"]))
        self.assertEqual(obs.observed["disposition"], "ESCALATE")
        self.assertEqual(obs.observed["audit_events"], ["INPUT_RECEIVED", "DECISION_MADE"], "distinct, in first-seen order")
        self.assertEqual(obs.cost_usd_micros, 0)
        self.assertEqual(len(obs.record_events), 3)

    def test_flags_are_derived_from_the_record_not_asserted(self) -> None:
        asked = parse(body("CLARIFY", events=["DECISION_MADE", "CLARIFICATION_REQUESTED"]))
        self.assertTrue(asked.observed["clarification_asked"])
        self.assertFalse(asked.observed["escalation_opened"])
        opened = parse(body("ESCALATE", writes=[("fin.cap.create_escalation_case", "fin.transaction:TX1001")]))
        self.assertTrue(opened.observed["escalation_opened"], "an ACHIEVED verdict on the case write")
        failed = body("ESCALATE", writes=[("fin.cap.create_escalation_case", "fin.transaction:TX1001")])
        failed["outcome"]["verdicts"][0]["verdict"] = "UNKNOWN"
        self.assertFalse(parse(failed).observed["escalation_opened"], "a case not verified is not opened")

    def test_the_whole_observed_block_validates(self) -> None:
        obs = parse(body("ALLOW", claims=[{"text": "completed", "bound_fact_status": "KNOWN"}]))
        doc = {"run_id": "r1", "case_id": "BEN-CLEAN-0001", "case_version": 1, "status": "COMPLETED", "error": None, "observed": obs.observed,
               "environment": {"effects": [], "oracle_accesses_by_runtime": 0, "model_governance_changes": 0, "monetary_calls": 0}, "path": "CLEAN",
               "latency_ms": 1, "cost_usd_micros": 0, "outcome_digest": "sha256:" + "00" * 32}
        self.assertEqual(Contracts().errors("benchmark-case-result", doc), [])

    def test_an_unexpected_shape_is_an_error_never_a_guess(self) -> None:
        broken: list[object] = [{}, {"outcome": {}}, {"events": [], "outcome": {"disposition": "ALLOW"}}, "text"]
        for body_ in broken:
            with self.assertRaises(ReconstructionError):
                parse(body_)

    def test_cost_is_an_integer(self) -> None:
        b = body("ALLOW")
        b["outcome"]["provider_cost_usd_micros"] = 1.5
        with self.assertRaises(ReconstructionError):
            parse(b)

    def test_the_format_is_provisional_until_a_recorded_body_exists(self) -> None:
        """Adding a body to benchmark/runner/recorded/ must be matched by flipping PROVISIONAL and pinning the shape."""
        recorded = [p for p in (Path(__file__).parent / "recorded").glob("*.json")] if (Path(__file__).parent / "recorded").exists() else []
        self.assertEqual(reconstruction.PROVISIONAL, not recorded, "a recorded reconstruction exists: pin the shape and set PROVISIONAL = False")


if __name__ == "__main__":
    unittest.main()
