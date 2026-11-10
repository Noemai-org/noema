"""benchmark-scorecard.v3 (BEN-30; Architect ruling A1 of 10 Nov 2026, A80): v3 is v2 plus exactly the required `exact_upper95` on the shared Rate definition. AGENT-15: the v2 file
is not edited, and a document is v2 or v3, never both. SYNTHETIC fixtures only."""
import hashlib
import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from validate import load_schemas, make_registry, validator_for  # noqa: E402

ROOT = Path(__file__).resolve().parents[2]
FIXTURES = ROOT / "contracts" / "fixtures"
SCHEMAS = load_schemas()
REGISTRY = make_registry(SCHEMAS)
V2_SHA256 = "d60eb012ae6477ee03656eeb3351ccd664d9ecbca03893c282402d6cfebd61c1"


def errors(name: str, document: dict) -> list:
    return list(validator_for(name, SCHEMAS, REGISTRY).iter_errors(document))


def fixture(kind: str, folder: str, name: str) -> dict:
    return json.loads((FIXTURES / kind / folder / name).read_text())


class ScorecardV3(unittest.TestCase):
    def test_the_v2_file_is_not_edited(self):
        self.assertEqual(hashlib.sha256((ROOT / "contracts" / "schemas" / "benchmark-scorecard.v2.json").read_bytes()).hexdigest(), V2_SHA256)

    def test_v3_is_v2_plus_exactly_exact_upper95_on_the_rate(self):
        v2, v3 = SCHEMAS["benchmark-scorecard.v2.json"], SCHEMAS["benchmark-scorecard.v3.json"]
        self.assertEqual(set(v3["$defs"]["Rate"]["properties"]) - set(v2["$defs"]["Rate"]["properties"]), {"exact_upper95"})
        self.assertEqual(set(v3["$defs"]["Rate"]["required"]) - set(v2["$defs"]["Rate"]["required"]), {"exact_upper95"})
        for kept in set(v2["$defs"]) - {"Rate"}:
            self.assertEqual(v2["$defs"][kept], v3["$defs"][kept], kept)
        self.assertEqual(v2["required"], v3["required"])
        self.assertEqual(v2["properties"], v3["properties"])

    def test_the_v2_rule_of_three_field_is_kept_beside_the_exact_bound_not_relabelled(self):
        rate = SCHEMAS["benchmark-scorecard.v3.json"]["$defs"]["Rate"]["properties"]
        self.assertEqual(rate["rule_of_three_upper"], SCHEMAS["benchmark-scorecard.v2.json"]["$defs"]["Rate"]["properties"]["rule_of_three_upper"])
        self.assertIn("exact_upper95", rate)

    def test_a_v2_document_is_not_a_v3_document_and_the_reverse(self):
        v2 = fixture("valid", "benchmark-scorecard.v2", "two_cases_clean.json")
        v3 = fixture("valid", "benchmark-scorecard.v3", "two_cases_clean.json")
        self.assertEqual(errors("benchmark-scorecard.v2", v2), [])
        self.assertEqual(errors("benchmark-scorecard.v3", v3), [])
        self.assertTrue(errors("benchmark-scorecard.v3", v2))
        self.assertTrue(errors("benchmark-scorecard.v2", v3))

    def test_the_exact_bound_is_one_minus_the_twentieth_root_of_five_hundredths_for_a_zero_failure_rate(self):
        for path in sorted((FIXTURES / "valid" / "benchmark-scorecard.v3").glob("*.json")):
            def visit(node):
                if isinstance(node, dict):
                    if "rule_of_three_upper" in node:
                        expected = None if node["rule_of_three_upper"] is None else round(1 - 0.05 ** (1 / node["n"]), 6)
                        self.assertEqual(node["exact_upper95"], expected, (path.name, node))
                    for value in node.values():
                        visit(value)
                elif isinstance(node, list):
                    for value in node:
                        visit(value)
            visit(json.loads(path.read_text()))


if __name__ == "__main__":
    unittest.main()
