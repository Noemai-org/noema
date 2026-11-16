"""BEN-16 (DoD A131 3b; Codex P2 on noema#249 at d31744c): every array and every free string of the benchmark contracts of this PR carries a finite bound, because the arm-written documents are
input that must fail closed on a huge value. A `pattern` alone is NOT a bound (a million `A` matches `^[A-Z][A-Z0-9_]*$`): a string needs `maxLength`, an `enum`, a `const` or a `$ref` to a bounded
definition. Real instances that are oversized must be refused by the validator. Written before the bounds and seen failing. SYNTHETIC."""
import copy
import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from validate import load_schemas, make_registry, validator_for  # noqa: E402

ROOT = Path(__file__).resolve().parents[2]
SCHEMAS = ("benchmark-arm-result.v1.json",)
LOADED = load_schemas()
REGISTRY = make_registry(LOADED)


def unbounded(node, path=""):
    if isinstance(node, dict):
        if node.get("type") == "array" and "maxItems" not in node:
            yield f"{path}: array without maxItems"
        kinds = node.get("type")
        kinds = kinds if isinstance(kinds, list) else [kinds]
        if "string" in kinds and not any(k in node for k in ("maxLength", "enum", "const", "$ref")):
            yield f"{path}: string without maxLength, enum or const (a pattern is not a bound)"
        for key, value in node.items():
            yield from unbounded(value, f"{path}/{key}")
    elif isinstance(node, list):
        for index, value in enumerate(node):
            yield from unbounded(value, f"{path}/{index}")


def errors(document):
    return list(validator_for("benchmark-arm-result", LOADED, REGISTRY).iter_errors(document))


def fixture(name):
    return json.loads((ROOT / "contracts" / "fixtures" / "valid" / "benchmark-arm-result" / name).read_text())


# The case-contract v2 files of noema#260 (BEN-16): the same rule. The members `benchmark-oracle.v2` inherits unchanged from v1 are excluded: v2 changes none of them and the v1 file is pinned.
V2_SCHEMAS = ("benchmark-case.v2.json", "benchmark-oracle.v2.json", "benchmark-arm-case.v1.json", "benchmark-fixture.v2.json")


def load_schema(name):
    return json.loads((ROOT / "contracts" / "schemas" / name).read_text())


class Bounds(unittest.TestCase):
    def test_every_array_and_free_string_is_bounded_and_a_pattern_does_not_count(self):
        for name in SCHEMAS:
            self.assertEqual([], list(unbounded(load_schema(name))), name)

    def test_the_case_contract_v2_files_are_bounded_by_the_same_rule(self):
        for name in V2_SCHEMAS:
            schema = load_schema(name)
            if name == "benchmark-oracle.v2.json":
                inherited = set(load_schema("benchmark-oracle.v1.json")["properties"])
                schema = {**schema, "properties": {k: v for k, v in schema["properties"].items() if k not in inherited}}
            self.assertEqual([], list(unbounded(schema)), name)


class Oversize(unittest.TestCase):
    def test_the_valid_fixtures_are_valid(self):
        for name in ("completed_status_answer.json", "escalated_with_a_blocked_prohibited_action.json", "recovered_timeout_no_http_response.json"):
            self.assertEqual([], errors(fixture(name)), name)

    def test_a_label_of_a_million_letters_is_refused(self):
        doc = fixture("completed_status_answer.json")
        doc["classification"] = "A" * 1_000_000
        self.assertTrue(errors(doc))

    def test_an_oversize_value_in_every_patterned_field_is_refused(self):
        doc = fixture("escalated_with_a_blocked_prohibited_action.json")
        big = "A" * 100_000
        cases = {
            "run_id": lambda d: d.update(run_id="a" * 100_000),
            "case_id": lambda d: d.update(case_id="BEN-" + "A" * 100_000 + "-0001"),
            "classification": lambda d: d.update(classification=big),
            "audit_entry_step": lambda d: d["audit_record"]["entries"][0].update(step=big),
            "audit_entry_actor": lambda d: d["audit_record"]["entries"][0].update(actor="a" * 100_000),
            "audit_entry_digest": lambda d: d["audit_record"]["entries"][0].update(inputs_digest="sha256:" + "0" * 100_000),
            "audit_entry_at": lambda d: d["audit_record"]["entries"][0].update(at="2026-11-07T09:00:00." + "1" * 100_000 + "Z"),
            "tool_call_at": lambda d: d["tool_calls"][0].update(at="2026-11-07T09:00:00." + "1" * 100_000 + "Z"),
            "tool_call_id": lambda d: d["tool_calls"][0].update(tool_call_id="t" * 100_000),
            "blocked_reason": lambda d: d["blocked_actions"][0].update(reason=big),
        }
        for name, mutate in cases.items():
            doc2 = copy.deepcopy(doc)
            try:
                mutate(doc2)
            except (KeyError, IndexError):
                self.skipTest(f"fixture lacks the member for {name}")
            self.assertTrue(errors(doc2), name)

    def test_a_reasonable_fractional_second_is_still_accepted(self):
        doc = fixture("escalated_with_a_blocked_prohibited_action.json")
        doc["audit_record"]["entries"][0]["at"] = "2026-11-07T09:00:00.123456789Z"
        self.assertEqual([], errors(doc))


if __name__ == "__main__":
    unittest.main()
