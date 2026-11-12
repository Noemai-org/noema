"""intent-object.v2 (ruling A163, relayed): what JSON Schema cannot state, checked over the schemas and fixtures.

v2 is v1 with `schema_version` major 2 and a looser `principal.asserted_by` pattern (a hyphen after the first letter), so the verified issuer
`noema-poc-idp` is a valid value. This file checks that v1 was not edited, that nothing else differs between the two, that a v1 document is not a valid v2
document and a hyphenated issuer is not a valid v1 document, and that every valid v1 fixture has a v2 twin that validates.
"""
import copy
import hashlib
import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from validate import FIXTURES, SCHEMAS as SCHEMA_DIR, load_schemas, make_registry, validator_for  # noqa: E402

SCHEMAS = load_schemas()
REGISTRY = make_registry(SCHEMAS)
V1_SHA256 = "472e89bab3602dad03d4cb4ac95840a6b1cc0bec8a796f28fad0067c1d22b66d"
ISSUER = "noema-poc-idp"


def check(doc, version):
    return list(validator_for(version, SCHEMAS, REGISTRY).iter_errors(doc))


def sot_example(contract):
    return json.loads((FIXTURES / "valid" / contract / "sot_example.json").read_text())


class IntentObjectV2(unittest.TestCase):
    def test_v1_was_not_edited(self):
        self.assertEqual(hashlib.sha256((SCHEMA_DIR / "intent-object.v1.json").read_bytes()).hexdigest(), V1_SHA256)

    def test_only_the_version_and_the_asserted_by_pattern_differ_from_v1(self):
        a, b = copy.deepcopy(SCHEMAS["intent-object.v1.json"]), copy.deepcopy(SCHEMAS["intent-object.v2.json"])
        for d in (a, b):
            d.pop("$id"), d.pop("description")
            d["properties"].pop("schema_version")
            d["properties"]["principal"]["properties"].pop("asserted_by")
        self.assertEqual(a, b, "v2 must differ from v1 only in schema_version and principal.asserted_by (and its id and description)")
        p1 = SCHEMAS["intent-object.v1.json"]["properties"]["principal"]["properties"]["asserted_by"]["pattern"]
        p2 = SCHEMAS["intent-object.v2.json"]["properties"]["principal"]["properties"]["asserted_by"]["pattern"]
        self.assertEqual((p1, p2), ("^[a-z][a-z0-9_]*$", "^[a-z][a-z0-9_-]*$"))

    def test_a_v1_document_is_not_a_valid_v2_document(self):
        doc = sot_example("intent-object")
        self.assertEqual(doc["schema_version"], "1.0")
        self.assertEqual(check(doc, "intent-object.v1"), [])
        self.assertNotEqual(check(doc, "intent-object.v2"), [], "a v1 document must not validate as v2")

    def test_the_hyphenated_issuer_is_valid_in_v2_and_refused_by_v1(self):
        doc = sot_example("intent-object.v2")
        doc["principal"]["asserted_by"] = ISSUER
        self.assertEqual(check(doc, "intent-object.v2"), [])
        old = copy.deepcopy(doc)
        old["schema_version"] = "1.0"
        self.assertNotEqual(check(old, "intent-object.v1"), [], "v1 must still refuse a hyphen in asserted_by")

    def test_the_pattern_is_anchored_over_the_whole_string(self):
        for bad in (ISSUER + "\n", "-" + ISSUER, ISSUER.upper(), "noema poc", "", "9noema", "noema.poc"):
            doc = sot_example("intent-object.v2")
            doc["principal"]["asserted_by"] = bad
            self.assertNotEqual(check(doc, "intent-object.v2"), [], repr(bad))

    def test_every_valid_v1_fixture_has_a_v2_twin_that_validates(self):
        v1 = {p.name for p in (FIXTURES / "valid" / "intent-object").glob("*.json")}
        v2 = {p.name for p in (FIXTURES / "valid" / "intent-object.v2").glob("*.json")}
        self.assertEqual(v1 - v2, set(), "a v1 fixture has no v2 twin")
        for name in sorted(v2):
            doc = json.loads((FIXTURES / "valid" / "intent-object.v2" / name).read_text())
            self.assertTrue(doc["schema_version"].startswith("2."), name)
            self.assertEqual(check(doc, "intent-object.v2"), [], name)


if __name__ == "__main__":
    unittest.main()
