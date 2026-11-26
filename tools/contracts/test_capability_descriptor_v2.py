"""A631, A647 (interim; REQ-CAP-004, REQ-AG-001; AGENT-15): capability-descriptor.v2 declares compensation as one of NONE_REQUIRED, INVERSE_CAPABILITY and ESCALATE_ONLY, and an
INVERSE_CAPABILITY names the inverse capability and the parameter mapping that reaches it. Nothing else changes against v1, and v1 stays as it was. The cross-field rules of the PERD
(by risk class, against the registry, prohibited capabilities) are packcheck's. All data is SYNTHETIC."""
import json
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent.parent
SCHEMAS = ROOT / "contracts" / "schemas"


def load(name: str) -> dict:
    return json.loads((SCHEMAS / name).read_text())


class DescriptorV2(unittest.TestCase):
    def setUp(self):
        self.path = SCHEMAS / "capability-descriptor.v2.json"
        self.assertTrue(self.path.exists(), "capability-descriptor.v2.json does not exist")
        self.v2 = load("capability-descriptor.v2.json")
        self.v1 = load("capability-descriptor.v1.json")

    def test_the_id_is_the_file_name(self):
        self.assertEqual(self.v2["$id"], "https://contracts.noema.invalid/capability-descriptor.v2.json")

    def test_compensation_is_exactly_the_three_values_of_the_perd(self):
        self.assertEqual(self.v2["properties"]["compensation"]["enum"], ["NONE_REQUIRED", "INVERSE_CAPABILITY", "ESCALATE_ONLY"])

    def test_the_inverse_mapping_names_a_capability_and_a_non_empty_parameter_mapping_and_nothing_else(self):
        m = self.v2["properties"]["inverse_mapping"]
        self.assertEqual(sorted(m["required"]), ["capability_id", "parameters"])
        self.assertIs(m["additionalProperties"], False)
        self.assertEqual(m["properties"]["parameters"]["minProperties"], 1)

    def test_the_mapping_is_required_with_an_inverse_capability_and_refused_without_one(self):
        text = json.dumps(self.v2["allOf"])
        self.assertIn("INVERSE_CAPABILITY", text)
        self.assertIn("inverse_mapping", text)

    def test_every_other_member_is_as_in_v1(self):
        keep = {k for k in self.v1["properties"] if k != "compensation"}
        for k in keep:
            self.assertEqual(self.v2["properties"][k], self.v1["properties"][k], k)
        self.assertEqual(sorted(set(self.v2["properties"]) - keep), ["compensation", "inverse_mapping"])
        self.assertEqual(self.v2["required"], self.v1["required"])

    def test_v1_is_untouched(self):
        self.assertEqual(self.v1["properties"]["compensation"], {"type": "string", "minLength": 1})
        self.assertNotIn("inverse_mapping", self.v1["properties"])


if __name__ == "__main__":
    unittest.main()
