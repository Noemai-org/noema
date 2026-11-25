"""A676 (interim, HIGH, privacy), REQ-CTX-006, REQ-AMB-002, AGENT-15: candidates-response.v2 carries no name of any kind. A candidate is an opaque counterparty reference (cp_1, cp_2, ...), the amount
as integer minor units plus currency, the time, the last four digits, the direction and an exact recipient match computed in core. The candidates-api-shape lint names the same fields. v1 stays
as it is (its consumers move afterwards), so v1's `counterparty_display` stays allowed by the lint only until they do."""
import json
import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
LINT = ROOT / "tools" / "lint" / "golint" / "subpackages.go"
V1 = json.loads((ROOT / "contracts" / "schemas" / "candidates-response.v1.json").read_text())
V2_PATH = ROOT / "contracts" / "schemas" / "candidates-response.v2.json"
FIXTURES = ROOT / "contracts" / "fixtures" / "valid" / "candidates-response.v2"
NAME_WORDS = ("name", "display", "label", "counterparty_display", "narration", "beneficiary_name")


def v2() -> dict:
    assert V2_PATH.is_file(), "contracts/schemas/candidates-response.v2.json does not exist (A676, AGENT-15)"
    return json.loads(V2_PATH.read_text())


def lint_fields() -> set[str]:
    body = re.search(r"var candidateFields = map\[string\]bool\{(.*?)\n\}", LINT.read_text(), re.S).group(1)
    return set(re.findall(r'"([a-z0-9]+)":\s*true', body))


def norm(name: str) -> str:
    return name.lower().replace("_", "")


class ShapeV2(unittest.TestCase):
    def test_v1_is_not_edited(self):
        self.assertIn("counterparty_display", V1["$defs"]["Candidate"]["properties"])  # a published major is never edited (AGENT-15)

    def test_the_v2_candidate_has_exactly_the_agreed_members_and_no_name(self):
        props = v2()["$defs"]["Candidate"]["properties"]
        self.assertEqual(set(props), {"candidate_id", "resource_ref", "amount_minor", "currency", "occurred_at", "counterparty_ref", "recipient_match", "account_last4", "direction", "beneficiary_alias_match"})
        for name in props:
            self.assertFalse(any(w in name for w in NAME_WORDS), f"{name} could carry a name")
        self.assertNotIn("counterparty_display", props)
        self.assertEqual(set(v2()["$defs"]["Candidate"]["required"]), set(props) - {"beneficiary_alias_match"})
        self.assertIs(v2()["$defs"]["Candidate"]["additionalProperties"], False)

    def test_the_counterparty_reference_is_an_ordinal_of_at_most_twenty(self):
        ref = v2()["$defs"]["Candidate"]["properties"]["counterparty_ref"]
        self.assertEqual(ref["type"], "string")
        rx = re.compile(ref["pattern"])
        for ok in ("cp_1", "cp_9", "cp_10", "cp_20"):
            self.assertTrue(rx.match(ok), ok)
        for bad in ("cp_0", "cp_21", "cp_01", "cp_1\n", "Cp_1", "Ada Obi", "cp_", "", "cp_1x"):
            self.assertFalse(rx.fullmatch(bad) and not bad.endswith("\n"), bad)

    def test_the_recipient_match_is_an_exact_share_not_a_name(self):
        m = v2()["$defs"]["Candidate"]["properties"]["recipient_match"]
        self.assertIs(m["additionalProperties"], False)
        self.assertEqual(set(m["properties"]), {"numerator", "denominator"})
        self.assertEqual(m["properties"]["numerator"]["type"], "integer")
        self.assertEqual(m["properties"]["numerator"]["minimum"], 0)
        self.assertEqual(m["properties"]["denominator"]["type"], "integer")
        self.assertEqual(m["properties"]["denominator"]["minimum"], 1)

    def test_the_lint_allows_the_v2_names_and_only_v1s_display_name_besides(self):
        allowed = lint_fields() - {"candidates"}
        props = {norm(k) for k in v2()["$defs"]["Candidate"]["properties"]}
        self.assertEqual(props | {"counterpartydisplay"}, allowed)

    def test_the_cap_is_twenty_and_the_version_is_two(self):
        self.assertEqual(v2()["properties"]["candidates"]["maxItems"], 20)
        self.assertEqual(v2()["properties"]["schema_version"].get("const"), "2.0")

    def test_every_valid_fixture_is_newest_first_with_references_numbered_in_that_order(self):
        paths = sorted(FIXTURES.glob("*.json"))
        self.assertGreaterEqual(len(paths), 3)
        for path in paths:
            rows = json.loads(path.read_text())["candidates"]
            self.assertEqual([r["occurred_at"] for r in rows], sorted((r["occurred_at"] for r in rows), reverse=True), path.name)
            seen: list[str] = []
            for r in rows:
                if r["counterparty_ref"] not in seen:
                    seen.append(r["counterparty_ref"])
            self.assertEqual(seen, [f"cp_{i + 1}" for i in range(len(seen))], f"{path.name}: references are numbered by first appearance")
            for r in rows:
                n, d = r["recipient_match"]["numerator"], r["recipient_match"]["denominator"]
                self.assertTrue(0 <= n <= d and d >= 1, path.name)

    def test_no_fixture_carries_a_name_member(self):
        for path in sorted(FIXTURES.glob("*.json")):
            for r in json.loads(path.read_text())["candidates"]:
                self.assertFalse({"counterparty_display", "counterparty_name", "name", "display", "label"} & set(r), path.name)


if __name__ == "__main__":
    unittest.main()
