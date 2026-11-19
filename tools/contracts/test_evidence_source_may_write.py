"""Checks for the optional `may_write` member of evidence-source.v1 (A342, interim, relayed, not yet committed; REQ-CAP-001, INV-03).

`may_write` says whether a capability may write to the source; absent means false. The generated Pydantic models accept an explicit null for an optional member, so a null
cannot be an invalid FIXTURE (the generated-model test would require the model to refuse it): it is held here, against the schema, which does refuse it. The Go decoder refuses
it too (services/core/execution/registry/maywrite_test.go). All values are SYNTHETIC.
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
VALID = sorted((ROOT / "contracts" / "fixtures" / "valid" / "evidence-source").glob("*.json"))


def errors(doc):
    return list(validator_for("evidence-source", SCHEMAS, REGISTRY).iter_errors(doc))


class MayWrite(unittest.TestCase):
    def setUp(self):
        self.assertTrue(VALID, "no valid evidence-source fixture to start from")
        self.base = json.loads(VALID[0].read_text())

    def test_the_member_is_optional_and_a_boolean(self):
        self.assertEqual(errors(self.base), [])
        for good in (True, False):
            doc = copy.deepcopy(self.base)
            doc["may_write"] = good
            self.assertEqual(errors(doc), [], good)

    def test_null_and_every_other_type_is_refused(self):
        for bad in (None, "true", "false", 1, 0, [], [True], {}, {"a": True}):
            doc = copy.deepcopy(self.base)
            doc["may_write"] = bad
            self.assertTrue(errors(doc), f"may_write {bad!r} is accepted")


if __name__ == "__main__":
    unittest.main()
