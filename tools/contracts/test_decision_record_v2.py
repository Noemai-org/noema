"""decision-record.v2 (ruling A5 of 6 Nov 2026): what JSON Schema cannot state, checked over the fixtures.

v2 is v1 with `schema_version` major 2 and a defined `sig`. This file checks that v1 was not edited, that a v1-versioned record is not a valid v2
record, that every valid v1 fixture is a valid v2 fixture once its version is 2.0, and that the signing rule (domain label, newline, JCS form) is
stated in the schema so a reader cannot miss it.
"""
import hashlib
import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from validate import FIXTURES, SCHEMAS as SCHEMA_DIR, load_schemas, make_registry, validator_for  # noqa: E402

SCHEMAS = load_schemas()
REGISTRY = make_registry(SCHEMAS)
V1_SHA256 = "d8a89c75719a08994ee147a5e6cb5d2403723cff02de92489071714eb56d5a18"
LABEL = "noema/decision-record/v2"


def check(doc, version):
    return list(validator_for(version, SCHEMAS, REGISTRY).iter_errors(doc))


class DecisionRecordV2(unittest.TestCase):
    def test_v1_was_not_edited(self):
        self.assertEqual(hashlib.sha256((SCHEMA_DIR / "decision-record.v1.json").read_bytes()).hexdigest(), V1_SHA256)

    def test_a_record_with_major_one_is_not_a_valid_v2_record(self):
        doc = json.loads((FIXTURES / "valid" / "decision-record" / "allow.json").read_text())
        self.assertEqual(doc["schema_version"], "1.0")
        self.assertEqual(check(doc, "decision-record.v1"), [])
        self.assertNotEqual(check(doc, "decision-record.v2"), [], "a v1 record must not validate as v2")
        doc["schema_version"] = "2.0"
        self.assertNotEqual(check(doc, "decision-record.v2"), [], "a v1 record with only its version changed lacks issued_at and expires_at (ruling A13)")
        doc["issued_at"], doc["expires_at"] = doc["evaluated_at"], "2099-01-01T00:00:00Z"
        self.assertEqual(check(doc, "decision-record.v2"), [])

    def test_every_valid_v1_fixture_has_a_v2_twin_that_validates(self):
        v1 = {p.name for p in (FIXTURES / "valid" / "decision-record").glob("*.json")}
        v2 = {p.name for p in (FIXTURES / "valid" / "decision-record.v2").glob("*.json")}
        self.assertEqual(v1, v2)
        for name in sorted(v2):
            doc = json.loads((FIXTURES / "valid" / "decision-record.v2" / name).read_text())
            self.assertTrue(doc["schema_version"].startswith("2."), name)
            self.assertEqual(check(doc, "decision-record.v2"), [], name)

    def test_the_signing_rule_is_stated_in_the_schema(self):
        text = json.dumps(SCHEMAS["decision-record.v2.json"])
        self.assertIn(LABEL, text)
        self.assertIn("HMAC-SHA-256", text)
        self.assertIn("JCS", text)
        self.assertIn("0x00", text, "ruling A13: the separator after the domain label is a zero byte, not a newline")
        self.assertIn("key_id", text, "ruling A13: the key_id is a bound field")

    def test_a_record_missing_a_bound_field_is_invalid(self):
        """Ruling A13: intent_id, correlation_id, policy_version (policy_id@version), issued_at, expires_at and the key_id are bound; a record missing any is invalid."""
        required = set(SCHEMAS["decision-record.v2.json"]["required"])
        for f in ("intent_id", "correlation_id", "policy_version", "issued_at", "expires_at", "sig"):
            self.assertIn(f, required, f)
        base = json.loads((FIXTURES / "valid" / "decision-record.v2" / "allow.json").read_text())
        self.assertEqual(check(base, "decision-record.v2"), [])
        for f in ("intent_id", "correlation_id", "policy_version", "issued_at", "expires_at"):
            doc = {k: v for k, v in base.items() if k != f}
            self.assertNotEqual(check(doc, "decision-record.v2"), [], f"a record without {f} validated")
        doc = json.loads(json.dumps(base))
        del doc["sig"]["key_id"]
        self.assertNotEqual(check(doc, "decision-record.v2"), [], "a record whose sig names no key_id validated")


if __name__ == "__main__":
    unittest.main()
