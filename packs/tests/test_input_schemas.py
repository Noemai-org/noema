"""The pack's tool input schemas: one file per permitted capability, none for a prohibited one.

PCK (pack data), answering noema#362 (Integration Lead, ENV-05). REQ-CAP-001, REQ-CAP-002; SoT INV-12, INV-05, BD-08.
Rule agreed on noema#362 (IL's yes to EL's default, 10 Nov 2026): a published tool schema holds only the fields a caller
supplies, with additionalProperties false. It never holds the tenant, the principal, or anything Trust or Execution derives
(epoch, created_by_intent, version tokens, idempotency keys, payload hashes): an arm is never asked for an identity (INV-12).
`input_schema` is a pack-relative path (capability-descriptor.v1), relative to `packs/` like `action_graph_files`.
All data SYNTHETIC.
"""
import json
import unittest
from pathlib import Path

import jsonschema

PACKS = Path(__file__).resolve().parents[1]
PACK = json.loads((PACKS / "fintech_v1.json").read_text(encoding="utf-8"))
DERIVED_OR_IDENTITY = {
    "tenant_id", "tenant", "principal_id", "principal", "actor", "session", "role", "roles",
    "epoch", "logical_resolution_epoch", "created_by_intent", "intent_id", "correlation_id",
    "expected_version_token", "version_token", "idempotency_key", "grant_id", "payload_hash",
    "evidence_packet_hash", "decision_id",
}


def capabilities(prohibited):
    return [c for c in PACK["capabilities"] if bool(c["prohibited"]) is prohibited]


class InputSchemas(unittest.TestCase):
    def test_every_permitted_capability_has_its_schema_file(self):
        caps = capabilities(False)
        self.assertEqual(len(caps), 12, "the pack has 12 permitted capabilities")
        for c in caps:
            with self.subTest(c["capability_id"]):
                path = PACKS / c["input_schema"]
                self.assertTrue(path.is_file(), f"{c['input_schema']} does not exist")

    def test_no_prohibited_capability_has_a_schema_file(self):
        for c in capabilities(True):
            with self.subTest(c["capability_id"]):
                self.assertFalse((PACKS / c["input_schema"]).exists(), "a prohibited capability must offer no tool (BD-08, REQ-CAP-002)")

    def test_each_schema_is_closed_and_asks_only_for_caller_supplied_fields(self):
        for c in capabilities(False):
            with self.subTest(c["capability_id"]):
                s = json.loads((PACKS / c["input_schema"]).read_text(encoding="utf-8"))
                jsonschema.Draft202012Validator.check_schema(s)
                self.assertEqual(s.get("$schema"), "https://json-schema.org/draft/2020-12/schema")
                self.assertEqual(s.get("type"), "object")
                self.assertIs(s.get("additionalProperties"), False)
                props = set(s.get("properties", {}))
                self.assertTrue(props, "a tool takes at least one key")
                self.assertLessEqual(set(s.get("required", [])), props)
                self.assertFalse(props & DERIVED_OR_IDENTITY, f"derived or identity fields offered: {sorted(props & DERIVED_OR_IDENTITY)}")
                self.assertEqual(s.get("title"), c["capability_id"])

    def test_a_schema_refuses_an_identity_or_derived_field(self):
        for c in capabilities(False):
            with self.subTest(c["capability_id"]):
                s = json.loads((PACKS / c["input_schema"]).read_text(encoding="utf-8"))
                example = {k: "x" for k in s.get("required", [])}
                for bad in ("tenant_id", "principal_id", "epoch"):
                    with self.assertRaises(jsonschema.ValidationError):
                        jsonschema.validate({**example, bad: "ten_a"}, s)


if __name__ == "__main__":
    unittest.main()
