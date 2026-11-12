"""The role-to-capability map is pack data, derived from the pack and never edited by hand (Architect's ruling A3 and EL's instruction of 8 Nov 2026, noema#341).

The Benchmark Owner's arm view reads `permitted_capabilities` from it (SR F-14: identical for every case of a role). The map lists only roles of A1 and never
a prohibited capability, because a prohibited capability has no adapter and no callable path (BD-08, REQ-CAP-002, INV-05), even though the pack names the
roles in its `roles_allowed`.
Run: python -m unittest discover -s packs/tests"""
import json
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent.parent
sys.path.insert(0, str(ROOT / "tools"))
import derive_role_capabilities as derive  # noqa: E402

A1_ROLES = ["approver", "audit", "customer", "ops"]
PACK = json.loads((ROOT / "packs" / "fintech_v1.json").read_text())
MAP = json.loads((ROOT / "packs" / "fintech" / "role_capabilities.json").read_text())


class RoleCapabilityMap(unittest.TestCase):
    def test_the_file_is_exactly_what_the_pack_derives(self):
        self.assertEqual(MAP, derive.derive(PACK), "regenerate with: python3 tools/derive_role_capabilities.py --write")

    def test_every_role_is_an_a1_role_and_all_four_are_present(self):
        self.assertEqual(sorted(MAP["roles"]), A1_ROLES)

    def test_no_prohibited_capability_is_in_any_role(self):
        prohibited = {c["capability_id"] for c in PACK["capabilities"] if c.get("prohibited")}
        self.assertTrue(prohibited)
        self.assertEqual(sorted(MAP["prohibited"]), sorted(prohibited))
        for role, caps in MAP["roles"].items():
            self.assertFalse(set(caps) & prohibited, f"{role} lists a prohibited capability")

    def test_every_listed_capability_exists_and_names_that_role(self):
        by_id = {c["capability_id"]: c for c in PACK["capabilities"]}
        for role, caps in MAP["roles"].items():
            for cap in caps:
                self.assertIn(role, by_id[cap]["authority"]["roles_allowed"], f"{cap} does not allow {role}")

    def test_the_lists_are_sorted_and_without_duplicates(self):
        for caps in MAP["roles"].values():
            self.assertEqual(caps, sorted(set(caps)))

    def test_a_role_in_the_pack_that_is_not_a1_stops_the_derivation(self):
        bad = json.loads(json.dumps(PACK))
        bad["capabilities"][0]["authority"]["roles_allowed"].append("ops_agent")
        with self.assertRaises(ValueError):
            derive.derive(bad)


if __name__ == "__main__":
    unittest.main()
