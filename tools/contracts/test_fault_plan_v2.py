"""fault-plan.v2 (ENV-15, DEC-2026-10-31 D15, REQ-ENV-005 as amended; AGENT-15): v1 is untouched, v2 is a superset, and the new effects are v2's alone."""
import hashlib
import json
import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent.parent.parent))
from benchmark.contracts import Contracts  # noqa: E402

ROOT = pathlib.Path(__file__).resolve().parent.parent.parent / "contracts"
V1_SHA256 = "8a2ec33b1c60a2e8af37d4a4448c3d03676314142948e78ac48be84e84fc5bba"  # the bytes of fault-plan.v1.json when v2 was added
V2_ONLY = ["proxy_corrupted_payloads.json", "proxy_paginate.json"]


class FaultPlanV2(unittest.TestCase):
    def setUp(self) -> None:
        self.c = Contracts()

    def test_v1_is_byte_for_byte_unchanged(self) -> None:
        self.assertEqual(hashlib.sha256((ROOT / "schemas" / "fault-plan.v1.json").read_bytes()).hexdigest(), V1_SHA256)

    def test_every_v1_valid_fixture_is_a_valid_v2_plan(self) -> None:
        files = sorted((ROOT / "fixtures" / "valid" / "fault-plan").glob("*.json"))
        self.assertTrue(files)
        for f in files:
            doc = json.loads(f.read_text())
            self.assertEqual(self.c.errors("fault-plan.v2", doc), [], f.name)

    def test_v1_rejects_what_only_v2_allows(self) -> None:
        for name in V2_ONLY:
            doc = json.loads((ROOT / "fixtures" / "valid" / "fault-plan.v2" / name).read_text())
            self.assertEqual(self.c.errors("fault-plan.v2", doc), [], name)
            self.assertNotEqual(self.c.errors("fault-plan", doc), [], f"{name} must not validate as v1")

    def test_the_unversioned_name_still_means_v1(self) -> None:
        doc = json.loads((ROOT / "fixtures" / "valid" / "fault-plan.v2" / "proxy_paginate.json").read_text())
        self.assertNotEqual(self.c.errors("fault-plan", doc), [])

    def test_every_family_of_the_amended_requirement_has_an_effect(self) -> None:
        doc = json.loads((ROOT / "fixtures" / "valid" / "fault-plan.v2" / "proxy_corrupted_payloads.json").read_text())
        seen = {(f["effect"], (f.get("params") or {}).get("change")) for f in doc["faults"]}
        for family in [("MALFORMED_RESPONSE", None), ("INVALID_JSON", None), ("WRONG_CONTENT_TYPE", None), ("EMPTY_BODY_SUCCESS", None), ("SCHEMA_DRIFT", "add"), ("SCHEMA_DRIFT", "remove")]:
            self.assertIn(family, seen)


if __name__ == "__main__":
    unittest.main()
