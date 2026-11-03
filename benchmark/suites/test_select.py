"""BEN-29 (REQ-BEN-004, REQ-TST-005): the named suites are data; selection is a pure function of the suite manifest; an empty cell or a short suite is an error, never a quiet pass.

Written before `select.py` and seen failing. SYNTHETIC. Generated layer only: nothing here reads `benchmark-heldout` (INV-22, REQ-AGT-004)."""
import json
import unittest
from pathlib import Path
from typing import Any

from benchmark.suites.select import SelectionError, load_families, load_suites, select, selection_digest
from benchmark.runner.test_suite import suite_dir

Json = dict[str, Any]
SUITES = Path(__file__).parent / "suites.json"


def entry(case_id: str, covers: list[str], category: str = "CLEAN_UNIQUE") -> Json:
    return {"case_id": case_id, "category": category, "covers": covers, "mutation": None, "base_case_id": None, "sha256": "sha256:" + "a" * 64,
            "suite_layer": "GENERATED", "version": 1}


MANIFEST: Json = {"cases": [
    entry("BEN-CLEAN-0001", ["US-01", "FAULT:NONE"]),
    entry("BEN-TMO-0001", ["US-06", "FAULT:TIMEOUT_AFTER_COMMIT"], "TIMEOUT_UNKNOWN"),
    entry("BEN-INJ-0001", ["US-09", "FAULT:INJECTION_PAYLOAD"], "PROMPT_INJECTION"),
    entry("BEN-AMB-0001", ["US-02", "FAULT:NONE"], "AMBIGUITY"),
]}
FAMILIES: dict[str, list[str]] = {"BEN-CLEAN-0001": ["B1", "B2"], "BEN-TMO-0001": ["B4", "B5"], "BEN-INJ-0001": ["B6"], "BEN-AMB-0001": ["B1"]}


class Selection(unittest.TestCase):
    def test_all_selects_every_case_in_id_order(self) -> None:
        self.assertEqual(["BEN-AMB-0001", "BEN-CLEAN-0001", "BEN-INJ-0001", "BEN-TMO-0001"], select(MANIFEST, {"select": {"all": True}}, "x"))

    def test_covers_any_matches_a_tag(self) -> None:
        self.assertEqual(["BEN-CLEAN-0001", "BEN-TMO-0001"], select(MANIFEST, {"select": {"covers_any": ["US-01", "US-06"]}}, "x"))

    def test_a_fault_prefix_selects_injected_faults_and_not_fault_none(self) -> None:
        got = select(MANIFEST, {"select": {"covers_prefix": "FAULT:", "except_covers": ["FAULT:NONE"]}}, "x")
        self.assertEqual(["BEN-INJ-0001", "BEN-TMO-0001"], got)

    def test_families_and_categories(self) -> None:
        self.assertEqual(["BEN-INJ-0001"], select(MANIFEST, {"select": {"families_any": ["B6"]}}, "x", FAMILIES))
        self.assertEqual(["BEN-AMB-0001"], select(MANIFEST, {"select": {"categories_any": ["AMBIGUITY"]}}, "x"))

    def test_an_empty_cell_is_an_error_naming_the_tag(self) -> None:
        with self.assertRaises(SelectionError) as ctx:
            select(MANIFEST, {"select": {"covers_any": ["US-01", "US-02", "US-03"], "each_must_match": ["US-01", "US-03"]}}, "us")
        self.assertIn("US-03", str(ctx.exception))

    def test_a_short_suite_is_an_error(self) -> None:
        with self.assertRaises(SelectionError) as ctx:
            select(MANIFEST, {"select": {"all": True}, "min_cases": 5}, "full")
        self.assertIn("5", str(ctx.exception))

    def test_a_family_selector_without_the_case_families_is_an_error(self) -> None:
        with self.assertRaises(SelectionError) as ctx:
            select(MANIFEST, {"select": {"families_any": ["B6"]}}, "x")
        self.assertIn("families", str(ctx.exception))

    def test_an_empty_selection_is_an_error(self) -> None:
        with self.assertRaises(SelectionError):
            select(MANIFEST, {"select": {"covers_any": ["US-99"]}}, "x")

    def test_an_unknown_selector_key_is_an_error_not_ignored(self) -> None:
        with self.assertRaises(SelectionError) as ctx:
            select(MANIFEST, {"select": {"covers_anyy": ["US-01"]}}, "x")
        self.assertIn("covers_anyy", str(ctx.exception))

    def test_a_selector_with_no_key_is_an_error(self) -> None:
        with self.assertRaises(SelectionError):
            select(MANIFEST, {"select": {}}, "x")

    def test_the_digest_depends_on_the_ids_only_and_their_order_is_fixed(self) -> None:
        ids = select(MANIFEST, {"select": {"all": True}}, "x")
        self.assertEqual(selection_digest(ids), selection_digest(list(ids)))
        self.assertNotEqual(selection_digest(ids), selection_digest(ids[:-1]))


class NamedSuites(unittest.TestCase):
    """The suites BEN-29 names, against the real generated suite (seed 20261020)."""

    manifest: Json
    families: dict[str, list[str]]
    suites: dict[str, Json]

    @classmethod
    def setUpClass(cls) -> None:
        cls.manifest = json.loads((suite_dir() / "manifest.json").read_text())
        cls.families = load_families(suite_dir())
        cls.suites = load_suites(SUITES)

    def test_the_four_suites_exist(self) -> None:
        self.assertEqual({"us", "chaos", "evil", "full"}, set(self.suites))

    def test_us_covers_every_story_from_us_01_to_us_22(self) -> None:
        ids = select(self.manifest, self.suites["us"], "us")
        covered = {c for e in self.manifest["cases"] if e["case_id"] in set(ids) for c in e["covers"] if c.startswith("US-")}
        self.assertEqual({f"US-{n:02d}" for n in range(1, 23)}, covered)

    def test_full_is_the_whole_generated_suite_of_at_least_320_cases(self) -> None:
        ids = select(self.manifest, self.suites["full"], "full")
        self.assertEqual(len(self.manifest["cases"]), len(ids))
        self.assertGreaterEqual(len(ids), 320)

    def test_chaos_selects_only_cases_with_an_injected_fault_and_every_fault_family_is_in_it(self) -> None:
        ids = set(select(self.manifest, self.suites["chaos"], "chaos"))
        families = {c for e in self.manifest["cases"] for c in e["covers"] if c.startswith("FAULT:") and c != "FAULT:NONE"}
        in_chaos = {c for e in self.manifest["cases"] if e["case_id"] in ids for c in e["covers"] if c.startswith("FAULT:")}
        self.assertTrue(families <= in_chaos)
        self.assertTrue(all(any(c.startswith("FAULT:") and c != "FAULT:NONE" for c in e["covers"]) for e in self.manifest["cases"] if e["case_id"] in ids))

    def test_evil_is_the_b6_security_family(self) -> None:
        ids = set(select(self.manifest, self.suites["evil"], "evil", self.families))
        b6 = {case_id for case_id, families in self.families.items() if "B6" in families}
        self.assertEqual(b6, ids)
        self.assertTrue(ids)


if __name__ == "__main__":
    unittest.main()
