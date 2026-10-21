"""The capability registry comes from the pack, not from a copy (REQ-CAP-002, AGENT-06)."""
import json
import unittest
from pathlib import Path

from benchmark.scoring.registry import registry_from_pack
from benchmark.scoring.testdata import ALLOWED, PROHIBITED

PACK = Path(__file__).resolve().parents[2] / "packs" / "fintech_v1.json"


class RegistryTest(unittest.TestCase):
    def test_fintech_pack_has_four_allowed_writes_and_eight_prohibited(self) -> None:
        registry = registry_from_pack(json.loads(PACK.read_text()))
        self.assertEqual(registry.allowed_writes, ALLOWED)
        self.assertEqual(registry.prohibited, PROHIBITED)

    def test_a_read_is_not_a_write(self) -> None:
        registry = registry_from_pack(json.loads(PACK.read_text()))
        self.assertNotIn("fin.cap.lookup_transaction", registry.allowed_writes)
        self.assertNotIn("fin.cap.lookup_transaction", registry.prohibited)

    def test_a_capability_that_is_both_listed_and_prohibited_is_prohibited_only(self) -> None:
        pack = {"capabilities": [
            {"capability_id": "x.cap.a", "prohibited": True, "side_effect_class": "CREATES_RECORD"},
            {"capability_id": "x.cap.b", "prohibited": False, "side_effect_class": "CREATES_RECORD"},
        ]}
        registry = registry_from_pack(pack)
        self.assertEqual(registry.prohibited, frozenset({"x.cap.a"}))
        self.assertEqual(registry.allowed_writes, frozenset({"x.cap.b"}))

    def test_a_pack_without_capabilities_is_an_error_not_an_empty_registry(self) -> None:
        for pack in ({}, {"capabilities": []}):
            with self.assertRaises(ValueError):
                registry_from_pack(pack)


if __name__ == "__main__":
    unittest.main()
