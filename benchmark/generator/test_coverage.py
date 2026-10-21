"""The coverage report fails the build on any gap (PERD §48.1 "Coverage rule", REQ-BEN-001)."""
import copy
import unittest

from benchmark.generator import catalogue
from benchmark.generator.coverage import PackCapabilities, load_pack_capabilities, report
from benchmark.generator.generate import Suite, generate

SEED = 20261020


class Coverage(unittest.TestCase):
    suite: Suite
    pack: PackCapabilities

    @classmethod
    def setUpClass(cls) -> None:
        cls.suite = generate(SEED)
        cls.pack = load_pack_capabilities()

    def test_default_suite_has_no_gaps(self) -> None:
        result = report(self.suite.manifest, self.pack, self.suite.cases)
        self.assertEqual(result.gaps, [], "\n".join(result.gaps))

    def test_the_rule_names_what_it_checks(self) -> None:
        result = report(self.suite.manifest, self.pack)
        self.assertEqual(len(result.required["US"]), len(catalogue.USER_STORIES))
        self.assertEqual(len(result.required["PUS"]), len(catalogue.SUPPORTING_STORIES))
        self.assertEqual(len(result.required["EC"]), len(catalogue.EDGE_CASES))
        self.assertGreaterEqual(len(result.required["FAULT"]), len(catalogue.FAULT_EFFECTS))
        self.assertEqual(len(result.required["CAP"]), len(self.pack.allowed) + len(self.pack.prohibited))

    def test_removing_a_story_is_a_gap(self) -> None:
        manifest = copy.deepcopy(self.suite.manifest)
        for entry in manifest["cases"]:
            entry["covers"] = [c for c in entry["covers"] if c != "US-09"] or ["US-01"]
        result = report(manifest, self.pack)
        self.assertTrue(any("US-09" in g for g in result.gaps))

    def test_removing_a_capability_is_a_gap(self) -> None:
        manifest = copy.deepcopy(self.suite.manifest)
        for entry in manifest["cases"]:
            entry["covers"] = [c for c in entry["covers"] if c != "CAP:fin.cap.transfer_funds"] or ["US-01"]
        self.assertTrue(any("transfer_funds" in g for g in report(manifest, self.pack).gaps))

    def test_a_short_category_is_a_gap(self) -> None:
        manifest = copy.deepcopy(self.suite.manifest)
        manifest["cases"] = [e for e in manifest["cases"] if e["category"] != "STALE_SESSION"][:]
        manifest["category_counts"]["STALE_SESSION"] = 0
        self.assertTrue(any("STALE_SESSION" in g for g in report(manifest, self.pack).gaps))

    def test_pidgin_share_below_thirty_is_a_gap(self) -> None:
        manifest = copy.deepcopy(self.suite.manifest)
        manifest["pidgin_or_code_switched"] = {"count": 1, "of": len(manifest["cases"])}
        self.assertTrue(any("Pidgin" in g for g in report(manifest, self.pack).gaps))

    def test_pack_capabilities_come_from_the_pack(self) -> None:
        self.assertIsInstance(self.pack, PackCapabilities)
        self.assertIn("fin.cap.transfer_funds", self.pack.prohibited)
        self.assertIn("fin.cap.create_escalation_case", self.pack.allowed)


if __name__ == "__main__":
    unittest.main()
