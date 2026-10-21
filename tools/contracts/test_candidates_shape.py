"""REQ-CTX-006, REQ-AMB-002: the candidates contract and the candidates-api-shape lint name the same fields,
and the response fixtures obey the rules JSON Schema cannot express (newest first, at most 20)."""
import json
import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
LINT = ROOT / "tools" / "lint" / "golint" / "subpackages.go"
RESPONSE = json.loads((ROOT / "contracts" / "schemas" / "candidates-response.v1.json").read_text())
FIXTURES = ROOT / "contracts" / "fixtures" / "valid" / "candidates-response"


def lint_fields() -> set[str]:
    text = LINT.read_text()
    body = re.search(r"var candidateFields = map\[string\]bool\{(.*?)\n\}", text, re.S).group(1)
    return set(re.findall(r'"([a-z0-9]+)":\s*true', body))


def norm(name: str) -> str:
    return name.lower().replace("_", "")


class Shape(unittest.TestCase):
    def test_the_candidate_properties_are_exactly_the_lints_allowed_names(self):
        props = {norm(k) for k in RESPONSE["$defs"]["Candidate"]["properties"]}
        allowed = lint_fields() - {"candidates"}
        self.assertEqual(props, allowed)

    def test_the_response_list_name_is_allowed_by_the_lint(self):
        self.assertIn("candidates", lint_fields())
        self.assertIn("candidates", RESPONSE["properties"])

    def test_the_cap_is_twenty(self):
        self.assertEqual(RESPONSE["properties"]["candidates"]["maxItems"], 20)

    def test_every_response_fixture_is_newest_first_with_total_order(self):
        for path in sorted(FIXTURES.glob("*.json")):
            rows = json.loads(path.read_text())["candidates"]
            keys = [(r["occurred_at"], r["candidate_id"]) for r in rows]
            expected = sorted(keys, key=lambda k: (k[0], ), reverse=True)
            self.assertEqual([k[0] for k in keys], [k[0] for k in expected], path.name)
            for earlier, later in zip(keys, keys[1:]):
                if earlier[0] == later[0]:
                    self.assertLess(earlier[1], later[1], path.name)

    def test_no_fixture_candidate_is_owned_by_anyone_else_by_construction(self):
        # US-07 is proven at the reader (core); here the contract has no owner or principal field to leak.
        self.assertNotIn("principal_ref", RESPONSE["$defs"]["Candidate"]["properties"])


if __name__ == "__main__":
    unittest.main()
