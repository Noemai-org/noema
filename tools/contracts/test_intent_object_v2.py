"""intent-object.v2 (ruling A163 of the Architect, 12 Nov 2026; noema#475): what JSON Schema cannot state about the bump, checked over the fixtures.

v2 is v1 with `schema_version` major 2 and one relaxed pattern: `principal.asserted_by` also allows "-", so the issuer the edge verifies (`noema-poc-idp`, ruling A1)
is a legal value. Nothing else changes. This file checks that v1 was not edited, that a v1-versioned object is not a valid v2 object, that every valid v1 fixture
has a v2 twin that validates, that exactly the intended places differ, and the new pattern's edges.
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


def check(doc, version):
    return list(validator_for(version, SCHEMAS, REGISTRY).iter_errors(doc))


def sot_example():
    return json.loads((FIXTURES / "valid" / "intent-object" / "sot_example.json").read_text())


def as_v2(doc):
    doc = copy.deepcopy(doc)
    doc["schema_version"] = "2.0"
    return doc


def diff(a, b, path=""):
    """The set of JSON paths at which two schema documents differ."""
    if isinstance(a, dict) and isinstance(b, dict):
        out = set()
        for k in set(a) | set(b):
            if k not in a or k not in b:
                out.add(f"{path}/{k}")
            else:
                out |= diff(a[k], b[k], f"{path}/{k}")
        return out
    return set() if a == b else {path}


class IntentObjectV2(unittest.TestCase):
    def test_v1_was_not_edited(self):
        self.assertEqual(hashlib.sha256((SCHEMA_DIR / "intent-object.v1.json").read_bytes()).hexdigest(), V1_SHA256)

    def test_an_object_with_major_one_is_not_a_valid_v2_object(self):
        doc = sot_example()
        self.assertEqual(doc["schema_version"], "1.0")
        self.assertEqual(check(doc, "intent-object.v1"), [])
        self.assertNotEqual(check(doc, "intent-object.v2"), [], "a v1 object must not validate as v2")
        self.assertEqual(check(as_v2(doc), "intent-object.v2"), [])

    def test_every_valid_v1_fixture_has_a_v2_twin_that_validates(self):
        v1 = {p.name for p in (FIXTURES / "valid" / "intent-object").glob("*.json")}
        v2 = {p.name for p in (FIXTURES / "valid" / "intent-object.v2").glob("*.json")}
        self.assertTrue(v1 <= v2, f"missing twins: {sorted(v1 - v2)}")
        for name in sorted(v2):
            doc = json.loads((FIXTURES / "valid" / "intent-object.v2" / name).read_text())
            self.assertTrue(doc["schema_version"].startswith("2."), name)
            self.assertEqual(check(doc, "intent-object.v2"), [], name)

    def test_every_invalid_v1_fixture_has_a_v2_twin_that_is_refused(self):
        v1 = {p.name for p in (FIXTURES / "invalid" / "intent-object").glob("*.json") if p.name != "expectations.json"}
        v2 = {p.name for p in (FIXTURES / "invalid" / "intent-object.v2").glob("*.json") if p.name != "expectations.json"}
        self.assertTrue(v1 <= v2, f"missing twins: {sorted(v1 - v2)}")
        for name in sorted(v2):
            doc = json.loads((FIXTURES / "invalid" / "intent-object.v2" / name).read_text())
            self.assertNotEqual(check(doc, "intent-object.v2"), [], name)

    def test_exactly_the_intended_places_differ_from_v1(self):
        v1, v2 = SCHEMAS["intent-object.v1.json"], SCHEMAS["intent-object.v2.json"]
        changed = diff(v1, v2)
        allowed_prefixes = (
            "/$id", "/description", "/properties/schema_version",
            "/properties/principal/properties/asserted_by/pattern",
        )
        stray = sorted(p for p in changed if not p.startswith(allowed_prefixes))
        self.assertEqual(stray, [], f"v2 changes more than ruling A163 allows: {stray}")
        self.assertIn("/properties/principal/properties/asserted_by/pattern", changed)
        self.assertIn("/properties/schema_version", changed)

    def test_the_issuer_the_edge_verifies_is_a_legal_asserted_by_in_v2_and_not_in_v1(self):
        doc = sot_example()
        doc["principal"]["asserted_by"] = "noema-poc-idp"
        self.assertNotEqual(check(doc, "intent-object.v1"), [], "the gap this bump closes: v1 refuses the ruled issuer")
        self.assertEqual(check(as_v2(doc), "intent-object.v2"), [])

    def test_the_new_asserted_by_pattern_keeps_every_other_edge(self):
        for good in ("noema-poc-idp", "noema_poc_idp", "a", "idp-1", "a-b-c", "enterprise_idp"):
            doc = as_v2(sot_example())
            doc["principal"]["asserted_by"] = good
            self.assertEqual(check(doc, "intent-object.v2"), [], good)
        for bad in ("Noema", "-idp", "_idp", "1idp", "idp.example", "idp/x", "idp x", "", "idp\n", "idp-é", "IDP", "idp:x", "idp@x"):
            doc = as_v2(sot_example())
            doc["principal"]["asserted_by"] = bad
            self.assertNotEqual(check(doc, "intent-object.v2"), [], repr(bad))

    def test_principal_type_and_principal_id_are_untouched(self):
        """A163 names asserted_by only. principal_id and principal_type keep their v1 patterns (the principal_id question is still open on noema#475)."""
        v1 = SCHEMAS["intent-object.v1.json"]["properties"]["principal"]["properties"]
        v2 = SCHEMAS["intent-object.v2.json"]["properties"]["principal"]["properties"]
        for k in ("principal_id", "principal_type", "authn_ref"):
            self.assertEqual(v1[k], v2[k], k)


if __name__ == "__main__":
    unittest.main()
