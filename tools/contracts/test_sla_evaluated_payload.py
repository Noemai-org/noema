"""A681 (interim, relayed, not yet committed; OD-P-116; REQ-TRU-006, REQ-AUD-001; INV-21, AGENT-15): sla-evaluated-payload.v1 is the payload of the SLA_EVALUATED Record event. The
members are the ones the clock evaluation produces (`trust/clocks` `Evaluation.EventPayload`): the clock, its label, the phase, the times in Unix seconds, the elapsed and remaining
seconds, and the routing. Integers, strings and booleans only (PERD 32.1). A clock with no anchor has no started_at, deadline_at or remaining (never assumed); a routing that names an owner
queue is a re-route, and one that does not is not. No member is open. All data is SYNTHETIC."""
import json
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent.parent
PATH = ROOT / "contracts" / "schemas" / "sla-evaluated-payload.v1.json"
RECORD_EVENTS = ROOT / "contracts" / "schemas" / "record-event.v3.json"


class SlaEvaluatedPayloadV1(unittest.TestCase):
    def setUp(self):
        self.assertTrue(PATH.exists(), "contracts/schemas/sla-evaluated-payload.v1.json does not exist (A681)")
        self.schema = json.loads(PATH.read_text())

    def test_the_id_is_the_file_name(self):
        self.assertEqual(self.schema["$id"], "https://contracts.noema.invalid/sla-evaluated-payload.v1.json")

    def test_the_event_type_it_describes_exists_in_the_record_contract(self):
        self.assertIn("SLA_EVALUATED", json.dumps(json.loads(RECORD_EVENTS.read_text())))

    def test_the_members_are_the_eleven_the_clock_evaluation_produces(self):
        self.assertEqual(
            set(self.schema["properties"]),
            {"clock_id", "label", "phase", "started_at", "deadline_at", "elapsed", "remaining", "priority", "owner_queue", "priority_raised", "reroute"},
        )

    def test_no_member_is_open(self):
        self.assertIs(self.schema["additionalProperties"], False)

    def test_the_always_present_members_are_required_and_the_anchored_ones_are_not_listed_there(self):
        self.assertEqual(set(self.schema["required"]), {"clock_id", "label", "phase", "elapsed", "priority", "priority_raised", "reroute"})

    def test_the_phase_is_the_four_of_the_clock_evaluation(self):
        self.assertEqual(self.schema["properties"]["phase"]["enum"], ["NOT_STARTED", "RUNNING", "AT_RISK", "BREACHED"])

    def test_the_priority_is_low_normal_high_and_the_label_is_the_clock_labels(self):
        self.assertEqual(self.schema["properties"]["priority"]["enum"], ["LOW", "NORMAL", "HIGH"])
        self.assertEqual(self.schema["properties"]["label"]["enum"], ["SYNTHETIC", "VALIDATED"])

    def test_every_time_and_duration_is_a_whole_number_with_no_fractions(self):
        for name in ("started_at", "deadline_at", "elapsed", "remaining"):
            member = self.schema["properties"][name]
            self.assertEqual(member["type"], "integer", name)
            self.assertEqual(member["minimum"], 0, name)

    def test_the_two_conditional_rules_are_in_the_schema(self):
        text = json.dumps(self.schema["allOf"])
        self.assertIn("NOT_STARTED", text)
        self.assertIn("owner_queue", text)
        self.assertIn("reroute", text)


if __name__ == "__main__":
    unittest.main()
