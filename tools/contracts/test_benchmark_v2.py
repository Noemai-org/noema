"""benchmark-case-result.v2 and benchmark-scorecard.v2 (BEN-30, REQ-BEN-009; DEC-2026-10-31-proof-depth-and-completeness D5, D6). AGENT-15: the v1 files are not edited,
v2 is v1 plus exactly the additions, and a document is v1 or v2, never both. SYNTHETIC fixtures only."""
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
V1_SHA256 = {
    "benchmark-scorecard.v1.json": "fd5d2c9d55ee841ff9287c03490c7d45eef95ca9b8293164012d13fa57c55461",
    "benchmark-case-result.v1.json": "a20494564748c4e6943ace6df21fbc371456fb25245f5f8289fa9fa8e0e60a87",
}
NEW_DIAGNOSTICS = {"pass_k", "turns_to_resolution", "unnecessary_clarification", "auto_resolve_precision", "definitions"}


def errors(name: str, document: dict) -> list:
    return list(validator_for(name, SCHEMAS, REGISTRY).iter_errors(document))


def fixture(kind: str, folder: str, name: str) -> dict:
    return json.loads((FIXTURES / kind / folder / name).read_text())


class BenchmarkV2(unittest.TestCase):
    def test_v1_files_are_not_edited(self):
        for name, digest in V1_SHA256.items():
            self.assertEqual(hashlib.sha256((ROOT / "contracts" / "schemas" / name).read_bytes()).hexdigest(), digest, name)

    def test_case_result_v2_is_v1_plus_exactly_turns(self):
        v1, v2 = SCHEMAS["benchmark-case-result.v1.json"], SCHEMAS["benchmark-case-result.v2.json"]
        self.assertEqual(set(v2["properties"]) - set(v1["properties"]), {"turns"})
        self.assertEqual(set(v1["properties"]) - set(v2["properties"]), set())
        self.assertEqual(set(v2["required"]) - set(v1["required"]), {"turns"})
        self.assertEqual(v1["$defs"], v2["$defs"])

    def test_scorecard_v2_is_v1_plus_exactly_the_four_diagnostics_and_their_definitions(self):
        v1, v2 = SCHEMAS["benchmark-scorecard.v1.json"], SCHEMAS["benchmark-scorecard.v2.json"]
        old, new = (s["$defs"]["Tier1"]["properties"]["diagnostics"] for s in (v1, v2))
        self.assertEqual(set(new["properties"]) - set(old["properties"]), NEW_DIAGNOSTICS)
        self.assertEqual(set(old["properties"]) - set(new["properties"]), set())
        self.assertEqual(set(new["required"]) - set(old["required"]), NEW_DIAGNOSTICS)
        self.assertEqual(set(v2["$defs"]) - set(v1["$defs"]), {"PassK", "TurnsToResolution", "DiagnosticDefinitions"})
        for kept in ("Rate", "GateRow", "Tier0", "Tier2", "Latency", "Tier3", "Slice"):
            self.assertEqual(v1["$defs"][kept], v2["$defs"][kept], kept)

    def test_pass_k_is_fixed_at_five(self):
        branches = SCHEMAS["benchmark-scorecard.v2.json"]["$defs"]["PassK"]["oneOf"]
        self.assertEqual([b["properties"]["k_required"] for b in branches], [{"const": 5}, {"const": 5}])

    def test_a_v1_document_is_not_a_v2_document_and_the_reverse(self):
        result_v1 = fixture("valid", "benchmark-case-result", "write_verified.json")
        result_v2 = fixture("valid", "benchmark-case-result.v2", "write_verified.json")
        self.assertEqual(errors("benchmark-case-result", result_v1), [])
        self.assertTrue(errors("benchmark-case-result.v2", result_v1))
        self.assertEqual(errors("benchmark-case-result.v2", result_v2), [])
        self.assertTrue(errors("benchmark-case-result", result_v2))
        card_v1 = fixture("valid", "benchmark-scorecard", "two_cases_clean.json")
        card_v2 = fixture("valid", "benchmark-scorecard.v2", "two_cases_clean.json")
        self.assertEqual(errors("benchmark-scorecard", card_v1), [])
        self.assertTrue(errors("benchmark-scorecard.v2", card_v1))
        self.assertEqual(errors("benchmark-scorecard.v2", card_v2), [])
        self.assertTrue(errors("benchmark-scorecard", card_v2))

    def test_every_v1_fixture_has_a_v2_counterpart_with_the_same_verdict(self):
        for folder in ("benchmark-case-result", "benchmark-scorecard"):
            for kind in ("valid", "invalid"):
                v1 = {p.name for p in (FIXTURES / kind / folder).glob("*.json")}
                v2 = {p.name for p in (FIXTURES / kind / f"{folder}.v2").glob("*.json")}
                self.assertEqual(v1 - v2, set(), f"{kind}/{folder}")


if __name__ == "__main__":
    unittest.main()
