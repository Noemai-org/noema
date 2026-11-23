"""US-06 (A503; BO needs it): reconstruction.v3 carries `disposition_decision_id` in the facts of a DECISION item, typed as a DecisionId and optional, so the reconstruction shows which
ESCALATE disposition an escalation action decision links (PERD 23.1, REQ-TRU-010, REQ-AUD-003). An additive minor change in place of v3 (AGENT-15); v1 and v2 are not edited."""
import json
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SCHEMAS = ROOT / "contracts" / "schemas"
DECISION_ID = "https://contracts.noema.invalid/common.v1.json#/$defs/DecisionId"


def decision_facts(name):
    schema = json.loads((SCHEMAS / name).read_text())
    for clause in schema["$defs"]["Section"]["allOf"]:
        if clause["if"]["properties"]["kind"].get("const") == "DECISION":
            return clause["then"]["properties"]["items"]["items"]["properties"]["facts"]
    raise AssertionError("no DECISION clause")


class DispositionFact(unittest.TestCase):
    def test_v3_types_the_disposition_as_a_decision_id(self):
        facts = decision_facts("reconstruction.v3.json")
        self.assertEqual(facts["properties"].get("disposition_decision_id"), {"$ref": DECISION_ID})

    def test_it_is_optional(self):
        facts = decision_facts("reconstruction.v3.json")
        self.assertNotIn("disposition_decision_id", facts.get("required", []))
        self.assertIn("decision_outcome", facts["required"])

    def test_v1_and_v2_are_not_edited(self):
        for name in ("reconstruction.v1.json", "reconstruction.v2.json"):
            self.assertNotIn("disposition_decision_id", decision_facts(name)["properties"], name)


if __name__ == "__main__":
    unittest.main()
