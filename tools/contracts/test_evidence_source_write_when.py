"""Checks for the optional `write_when` member of evidence-source.v1 (A364, interim, relayed, not yet committed; REQ-CAP-001, INV-03).

`write_when` is a predicate in the pack's predicate language that says WHEN a capability may write to the source; absent means never. The generated Pydantic models accept an
explicit null for an optional member, so a null cannot be an invalid FIXTURE (the generated-model test would require the model to refuse it): it is held here, against the
schema, which does refuse it. The Go decoder refuses it too (services/core/execution/registry/writewhen_test.go). All values are SYNTHETIC.
"""
import copy
import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from validate import load_schemas, make_registry, validator_for  # noqa: E402

ROOT = Path(__file__).resolve().parents[2]
SCHEMAS = load_schemas()
REGISTRY = make_registry(SCHEMAS)
VALID = ROOT / "contracts" / "fixtures" / "valid" / "evidence-source" / "core_ledger.json"


def errors(doc):
    return list(validator_for("evidence-source", SCHEMAS, REGISTRY).iter_errors(doc))


class WriteWhen(unittest.TestCase):
    def setUp(self):
        self.base = json.loads(VALID.read_text())

    def test_the_member_is_optional_and_a_predicate_string(self):
        self.assertNotIn("write_when", self.base)
        self.assertEqual(errors(self.base), [])
        doc = copy.deepcopy(self.base)
        doc["write_when"] = "subject_resource.status == 'RESOLVED'"
        self.assertEqual(errors(doc), [])

    def test_null_empty_and_every_other_type_is_refused(self):
        for bad in (None, "", True, False, 1, 0, [], ["true"], {}, {"a": "true"}):
            doc = copy.deepcopy(self.base)
            doc["write_when"] = bad
            self.assertTrue(errors(doc), f"write_when {bad!r} is accepted")

    def test_may_write_is_gone(self):
        self.assertNotIn("may_write", SCHEMAS["evidence-source.v1.json"]["properties"])
        doc = copy.deepcopy(self.base)
        doc["may_write"] = True
        self.assertTrue(errors(doc), "may_write is still accepted")

    def test_produced_by_is_gone_from_the_descriptor(self):
        self.assertNotIn("produced_by", SCHEMAS["evidence-descriptor.v1.json"]["properties"])  # A363


if __name__ == "__main__":
    unittest.main()
