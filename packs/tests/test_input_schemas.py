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
    "evidence_packet_hash", "decision_id", "confirmation_id",
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
                # Security Reviewer (noema#417): the conformance test sends an extra field and requires a refusal, for every never-supplied name.
                for bad in sorted(DERIVED_OR_IDENTITY):
                    with self.subTest(extra=bad), self.assertRaises(jsonschema.ValidationError):
                        jsonschema.validate({**example, bad: "x"}, s)

    # Definition of done 3b (A131): every input is bounded and validated and fails closed. Every string has a maxLength no larger than the body bound the mock
    # case already enforces (1 MiB, synthetic/mock_case/internal/api/server.go maxBody); a timestamp is the committed common.v1 Timestamp pattern (JSON Schema
    # "format" is not enforced by default, so it is not a validation). The cap is a sanity bound; a tighter one is a pack decision (AGENT-16).
    BODY_BOUND = 1 << 20
    TIMESTAMP = json.loads((PACKS.parent / "contracts" / "schemas" / "common.v1.json").read_text(encoding="utf-8"))["$defs"]["Timestamp"]["pattern"]

    def schema_of(self, capability_id):
        c = next(c for c in capabilities(False) if c["capability_id"] == capability_id)
        return json.loads((PACKS / c["input_schema"]).read_text(encoding="utf-8"))

    def test_every_string_field_is_bounded(self):
        for c in capabilities(False):
            s = json.loads((PACKS / c["input_schema"]).read_text(encoding="utf-8"))
            for name, prop in s["properties"].items():
                with self.subTest(c["capability_id"], field=name):
                    self.assertEqual(prop.get("type"), "string")
                    self.assertTrue(prop.get("minLength", 0) >= 1 or "pattern" in prop, "an empty value is refused")
                    self.assertIn("maxLength", prop, "an unbounded string is accepted")
                    self.assertLessEqual(prop["maxLength"], self.BODY_BOUND)

    def test_a_timestamp_field_is_the_committed_pattern_not_an_unenforced_format(self):
        s = self.schema_of("fin.cap.lookup_transaction_history")
        for name in ("from", "to"):
            self.assertEqual(s["properties"][name].get("pattern"), self.TIMESTAMP, name)
        ok = {"account_ref": "acc_1", "from": "2026-11-10T09:00:00Z", "to": "2026-11-10T10:00:00.123Z"}
        jsonschema.validate(ok, s)
        for bad in ("yesterday", "2026-11-10", "2026-11-10T09:00:00+01:00", "2026-11-10T09:00:00", "\uff12026-11-10T09:00:00Z", "2026-11-10T09:00:00Z\n", "", "9" * 100):
            for field in ("from", "to"):
                with self.subTest(field=field, value=bad[:20]), self.assertRaises(jsonschema.ValidationError):
                    jsonschema.validate({**ok, field: bad}, s)

    def test_a_value_over_the_bound_or_empty_or_of_the_wrong_type_is_refused(self):
        s = self.schema_of("fin.cap.record_investigation_evidence")
        base = {"case_id": "case_1", "payload": "note"}
        jsonschema.validate(base, s)
        for field, bad in (("payload", "x" * (self.BODY_BOUND + 1)), ("payload", ""), ("payload", 0), ("payload", None), ("payload", {"a": 1}),
                           ("case_id", "c" * 129), ("case_id", ""), ("case_id", 2**63 - 1), ("case_id", -1), ("case_id", None)):
            with self.subTest(field=field, value=str(bad)[:12]), self.assertRaises(jsonschema.ValidationError):
                jsonschema.validate({**base, field: bad}, s)
        for missing in ("case_id", "payload"):
            with self.subTest(missing=missing), self.assertRaises(jsonschema.ValidationError):
                jsonschema.validate({k: v for k, v in base.items() if k != missing}, s)


if __name__ == "__main__":
    unittest.main()
