"""A659 (interim, relayed, not yet committed; REQ-RES-005, REQ-RES-001; AGENT-15): recon-rule-set.v1 is the shape of the pack's reconciliation rule set. From version 2 each compared
pair (`incompatible`) and each `single_valued` field declares `convergence_window_seconds`, a whole number of seconds of at least 0 (A708); in version 1 the member is not allowed. The
shape is the loader's (an unknown member is refused). That a window equals the smaller freshness limit of its pair (A641) is packcheck's, not the schema's. All data is SYNTHETIC."""
import json
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent.parent
PATH = ROOT / "contracts" / "schemas" / "recon-rule-set.v1.json"


class ReconRuleSetV1(unittest.TestCase):
    def setUp(self):
        self.assertTrue(PATH.exists(), "contracts/schemas/recon-rule-set.v1.json does not exist (A659)")
        self.schema = json.loads(PATH.read_text())

    def test_the_id_is_the_file_name(self):
        self.assertEqual(self.schema["$id"], "https://contracts.noema.invalid/recon-rule-set.v1.json")

    def test_the_member_is_named_convergence_window_seconds_on_both_sections(self):
        defs = self.schema["$defs"]
        self.assertIn("convergence_window_seconds", defs["IncompatiblePair"]["properties"])
        self.assertIn("convergence_window_seconds", defs["SingleValued"]["properties"])
        self.assertNotIn("window_seconds", json.dumps(self.schema).replace("convergence_window_seconds", "").replace("within_seconds", ""))

    def test_the_window_is_a_whole_number_of_at_least_zero_with_no_ceiling_of_its_own(self):
        w = self.schema["$defs"]["ConvergenceWindowSeconds"]
        self.assertEqual((w["type"], w["minimum"]), ("integer", 0))
        self.assertNotIn("maximum", w)

    def test_it_is_required_from_version_2_and_not_allowed_in_version_1(self):
        then, other = json.dumps(self.schema["then"]), json.dumps(self.schema["else"])
        self.assertIn('"not": {"required": ["convergence_window_seconds"]}', then)
        self.assertIn('"required": ["convergence_window_seconds"]', other)
        self.assertEqual(self.schema["if"]["properties"]["version"]["const"], 1)

    def test_no_member_is_open(self):
        for name in ("IncompatiblePair", "SingleValued", "Derived", "Rule", "FactRef", "FieldSpec", "PendingMarker"):
            self.assertIs(self.schema["$defs"][name]["additionalProperties"], False, name)
        self.assertIs(self.schema["additionalProperties"], False)

    def test_the_twelve_classifications_are_the_enums_and_all_are_required(self):
        self.assertEqual(self.schema["properties"]["certainty"]["minProperties"], 12)
        self.assertIn("TransactionClassification", json.dumps(self.schema["properties"]["certainty"]))


if __name__ == "__main__":
    unittest.main()
