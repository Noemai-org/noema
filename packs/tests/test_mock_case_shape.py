"""The pack's `case` entity and mock-case's Case must agree (SoT 15.3 success predicate; PERD 34.4).

The pack is the normative side for field NAMES the predicates read; mock-case must be able to supply every one
of them, with every status it can emit inside the pack's enum. Reconciliation decision: mock-case changed, the
pack did not (see synthetic/mock_case/README.md, "Reconciliation with the pack"). SYNTHETIC."""
import json
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PACK = json.loads((ROOT / "packs" / "fintech_v1.json").read_text())
OPENAPI = json.loads((ROOT / "synthetic" / "mock_case" / "internal" / "api" / "openapi.json").read_text())
MOCK_CASE = OPENAPI["components"]["schemas"]["Case"]
MOCK_CREATE = OPENAPI["components"]["schemas"]["CreateCaseRequest"]

# Adapter mapping: pack field -> mock-case field. The normalising adapter (ENV/ADP tasks) renames; it never invents.
RENAMED = {"id": "case_id", "subject": "transaction_ref"}


def pack_case() -> dict:
    return next(e for e in PACK["evidence_descriptors"] if e["entity"] == "case")


class CaseShape(unittest.TestCase):
    def test_mock_case_supplies_every_field_the_pack_reads(self):
        for field in pack_case()["fields"]:
            name = RENAMED.get(field, field)
            self.assertIn(name, MOCK_CASE["properties"], f"pack reads case.{field}; mock-case has no {name}")
            self.assertIn(name, MOCK_CASE["required"], f"{name} must always be returned")

    def test_intent_and_epoch_are_set_at_creation(self):
        for name in ("created_by_intent", "epoch"):
            self.assertIn(name, MOCK_CREATE["required"])

    def test_every_status_mock_case_can_emit_is_in_the_pack_enum(self):
        pack_values = set(pack_case()["fields"]["status"]["values"])
        mock_values = set(MOCK_CASE["properties"]["status"]["enum"])
        self.assertLessEqual(mock_values, pack_values)
        self.assertIn("ROUTED", mock_values)

    def test_the_pack_enum_keeps_closed_which_mock_case_never_emits(self):
        # CLOSED stays in the pack (SoT 15.3 lifecycle); mock-case has no operation that closes (REQ-ENV-008).
        self.assertIn("CLOSED", pack_case()["fields"]["status"]["values"])
        self.assertNotIn("CLOSED", MOCK_CASE["properties"]["status"]["enum"])

    def test_epoch_is_an_integer_in_both(self):
        self.assertEqual(pack_case()["fields"]["epoch"]["type"], "Integer")
        self.assertEqual(MOCK_CASE["properties"]["epoch"]["type"], "integer")


if __name__ == "__main__":
    unittest.main()
