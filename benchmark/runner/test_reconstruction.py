"""The reconstruction body the runner reads: contracts/schemas/reconstruction.v3.json (names confirmed by the founders; PROVISIONAL until core serves a real body)."""
import copy
import json
import unittest
from pathlib import Path
from typing import Any

from benchmark.contracts import ROOT, Contracts
from benchmark.runner import reconstruction
from benchmark.runner.reconstruction import ReconstructionError, parse
from benchmark.runner.testdata import reconstruction as body

FIXTURES = ROOT / "contracts" / "fixtures"
Json = dict[str, Any]


def result_with(observed: Json) -> Json:
    return {"run_id": "r1", "case_id": "BEN-CLEAN-0001", "case_version": 1, "status": "COMPLETED", "error": None, "observed": observed,
            "environment": {"effects": [], "oracle_accesses_by_runtime": 0, "model_governance_changes": 0, "monetary_calls": 0}, "path": "CLEAN",
            "latency_ms": 1, "cost_usd_micros": None, "outcome_digest": "sha256:" + "00" * 32}


class Parse(unittest.TestCase):
    def test_the_contracts_own_fixtures_parse(self) -> None:
        for name in ("full_write_case.json", "clarification_only.json"):
            parsed = parse(json.loads((FIXTURES / "valid" / "reconstruction.v3" / name).read_text()))
            self.assertEqual(Contracts().errors("benchmark-case-result", result_with(parsed.observed)), [], name)

    def test_a_clarification_with_no_decision_is_clarify(self) -> None:
        parsed = parse(json.loads((FIXTURES / "valid" / "reconstruction.v3" / "clarification_only.json").read_text()))
        self.assertEqual((parsed.observed["disposition"], parsed.observed["clarification_asked"]), ("CLARIFY", True))

    def test_a_valid_body_with_no_decision_and_no_clarification_cannot_become_a_result(self) -> None:
        """no_events.json is a valid body (an empty Record); the runner has no disposition to report from it, so it is an error."""
        with self.assertRaises(ReconstructionError):
            parse(json.loads((FIXTURES / "valid" / "reconstruction.v3" / "no_events.json").read_text()))

    def test_the_contracts_invalid_fixtures_are_errors_not_quiet_partials(self) -> None:
        for path in sorted((FIXTURES / "invalid" / "reconstruction.v3").glob("*.json")):
            if path.name == "expectations.json":
                continue
            with self.assertRaises(ReconstructionError, msg=path.name):
                parse(json.loads(path.read_text()))

    def test_the_full_write_case_maps_every_field(self) -> None:
        parsed = parse(json.loads((FIXTURES / "valid" / "reconstruction.v3" / "full_write_case.json").read_text()))
        o = parsed.observed
        self.assertEqual(o["disposition"], "ALLOW")
        self.assertEqual(o["resolved_target"], "fin.transaction:TX88213")
        self.assertEqual(o["dispatches"], [{"capability": "fin.cap.create_escalation_case", "target": "fin.transaction:TX88213", "grant": "VALID"}])
        self.assertEqual(o["verdicts"], [{"capability": "fin.cap.create_escalation_case", "target": "fin.transaction:TX88213", "verdict": "ACHIEVED"}])
        self.assertEqual(o["claims"], [{"text": "A case was opened for your transfer.", "bound_fact_status": "KNOWN"}])
        self.assertEqual(o["disclosed_refs"], ["fin.transaction:TX88213"])
        self.assertEqual(o["audit_events"], ["INTENT_ADMITTED", "CANDIDATES_RESOLVED", "DECISION_MADE", "RECEIPT", "VERDICT"])
        self.assertTrue(o["escalation_opened"])
        self.assertEqual(len(parsed.record_events), 5)

    def test_cost_is_never_read_from_the_reconstruction(self) -> None:
        """No source for provider cost exists yet (the model harness's per-call accounting is not exposed): null, never an estimate."""
        self.assertIsNone(parse(body("ALLOW")).cost_usd_micros)

    def test_flags_come_from_the_record_not_from_assertion(self) -> None:
        asked = parse(body("CLARIFY", events=["DECISION_MADE", "CLARIFICATION_REQUESTED"]))
        self.assertTrue(asked.observed["clarification_asked"])
        self.assertTrue(parse(body("CLARIFY", clarification_asked=True)).observed["clarification_asked"])
        self.assertFalse(parse(body("CLARIFY", clarification_asked=False)).observed["clarification_asked"])
        target = "fin.transaction:TX1001"
        opened = parse(body("ESCALATE", writes=[("fin.cap.create_escalation_case", target)]))
        self.assertTrue(opened.observed["escalation_opened"])
        self.assertFalse(parse(body("ESCALATE", writes=[("fin.cap.create_escalation_case", target)], verdict="UNKNOWN")).observed["escalation_opened"])

    def test_a_dispatch_target_is_the_resolved_target_unless_the_action_names_one(self) -> None:
        a = "fin.transaction:TX1001"
        derived = parse(body("ESCALATE", writes=[("fin.cap.route_to_internal_owner", a)])).observed
        self.assertEqual(derived["dispatches"][0]["target"], a)
        explicit = parse(body("ESCALATE", writes=[("fin.cap.route_to_internal_owner", "fin.transaction:TX2002")], resolved_target=a, explicit_targets=True)).observed
        self.assertEqual(explicit["dispatches"][0]["target"], "fin.transaction:TX2002")

    def test_an_action_with_no_target_is_an_error(self) -> None:
        b = body("ESCALATE", writes=[("fin.cap.create_escalation_case", "fin.transaction:TX1001")])
        for section in b["sections"]:
            if section["kind"] == "RESOLUTION":
                section["items"][0]["facts"]["resolved_target"] = None
        with self.assertRaises(ReconstructionError):
            parse(b)

    def test_the_last_decision_is_the_disposition(self) -> None:
        b = body("CLARIFY", events=["DECISION_MADE", "CLARIFICATION_ANSWERED", "DECISION_MADE"])
        decision = next(s for s in b["sections"] if s["kind"] == "DECISION")
        decision["items"] = [{"record_event_id": "recev_1000", "object_ref": "obj_0000", "basis": "RULE", "facts": {"decision_outcome": "CLARIFY"}},
                             {"record_event_id": "recev_1002", "object_ref": "obj_0002", "basis": "RULE", "facts": {"decision_outcome": "ALLOW"}}]
        self.assertEqual(parse(b).observed["disposition"], "ALLOW")

    def test_no_decision_is_an_error(self) -> None:
        b = body("ALLOW")  # no clarification either
        next(s for s in b["sections"] if s["kind"] == "DECISION")["items"] = []
        with self.assertRaises(ReconstructionError):
            parse(b)

    def test_a_chain_that_did_not_verify_is_an_error(self) -> None:
        b = body("ALLOW")
        b["chain"]["verified"] = False
        with self.assertRaises(ReconstructionError):
            parse(b)

    def test_audit_events_are_distinct_in_first_seen_order(self) -> None:
        self.assertEqual(parse(body("ALLOW", events=["INPUT_RECEIVED", "DECISION_MADE", "DECISION_MADE"])).observed["audit_events"], ["INPUT_RECEIVED", "DECISION_MADE"])

    def test_claims_and_disclosed_refs_come_from_the_explanation(self) -> None:
        parsed = parse(body("ALLOW", claims=[{"text": "one", "bound_fact_status": "KNOWN"}, {"text": "two", "bound_fact_status": None}], disclosed_refs=["fin.transaction:TX1001"]))
        self.assertEqual(parsed.observed["claims"], [{"text": "one", "bound_fact_status": "KNOWN"}, {"text": "two", "bound_fact_status": None}])
        self.assertEqual(parsed.observed["disclosed_refs"], ["fin.transaction:TX1001"])

    def test_the_whole_observed_block_validates(self) -> None:
        parsed = parse(body("ALLOW", claims=[{"text": "completed", "bound_fact_status": "KNOWN"}]))
        self.assertEqual(Contracts().errors("benchmark-case-result", result_with(parsed.observed)), [])

    def test_an_unexpected_shape_is_an_error_never_a_guess(self) -> None:
        broken: list[object] = [{}, {"sections": []}, "text", None, copy.deepcopy(body("ALLOW")) | {"extra": 1}]
        for b in broken:
            with self.assertRaises(ReconstructionError):
                parse(b)

    def test_the_format_is_provisional_until_a_recorded_body_exists(self) -> None:
        """The names are the founders' (reconstruction.v3.json). The flag stays until core serves a real body: add one to benchmark/runner/recorded/ and set PROVISIONAL = False."""
        recorded = [p for p in (Path(__file__).parent / "recorded").glob("*.json")] if (Path(__file__).parent / "recorded").exists() else []
        self.assertEqual(reconstruction.PROVISIONAL, not recorded, "a recorded reconstruction exists: pin the shape and set PROVISIONAL = False")


if __name__ == "__main__":
    unittest.main()
