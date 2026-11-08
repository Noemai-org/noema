"""BEN-16, ruling on noema#346: the case-write oracles follow the pack and are held, not counted in n, until the pack owner states what `permit_case_writes` means.

The module reads the rule and its condition from the pack at run time; it does not interpret the condition (Architect, 8 Nov 2026). Written before `held.py` and seen failing. SYNTHETIC; generated layer only (INV-22).
"""
import copy
import json
import unittest
from functools import lru_cache

from benchmark.generator.generate import PACK, Suite, generate
from benchmark.generator.held import HOLDS, held_cases

RULE = "fin.pol.default.permit_case_writes"


@lru_cache(maxsize=None)
def suite() -> Suite:
    return generate(20261020)


def pack() -> dict:
    return json.loads(PACK.read_text(encoding="utf-8"))


class Hold(unittest.TestCase):
    def test_every_case_expecting_a_case_write_is_held_on_346_with_the_pack_s_own_condition(self) -> None:
        held = held_cases(suite().cases, pack())
        expecting = {cid for cid, c in suite().cases.items() if any(w["capability"] in {"fin.cap.create_escalation_case", "fin.cap.record_investigation_evidence"} for w in c["expected"]["writes_expected"])}
        self.assertTrue(expecting)
        self.assertEqual(expecting, set(held))
        for entry in held.values():
            self.assertEqual({"held_on": "noema#346", "rule_id": RULE, "condition": "subject_resource.status == 'RESOLVED'"}, entry)

    def test_a_case_with_no_write_governed_by_the_rule_is_not_held(self) -> None:
        held = held_cases(suite().cases, pack())
        reads_only = [cid for cid, c in suite().cases.items() if not c["expected"]["writes_expected"]]
        self.assertTrue(reads_only)
        self.assertEqual(set(), set(reads_only) & set(held))

    def test_the_condition_is_read_from_the_pack_not_hard_coded(self) -> None:
        changed = copy.deepcopy(pack())
        for rule in changed["policy"]["rules"]:
            if rule["rule_id"] == RULE:
                rule["condition"] = "subject_resource.status != 'RESOLVED'"
        held = held_cases(suite().cases, changed)
        self.assertTrue(held)
        self.assertEqual({"subject_resource.status != 'RESOLVED'"}, {e["condition"] for e in held.values()})

    def test_the_hold_follows_the_rule_s_applies_to(self) -> None:
        changed = copy.deepcopy(pack())
        for rule in changed["policy"]["rules"]:
            if rule["rule_id"] == RULE:
                rule["applies_to"] = ["fin.cap.record_investigation_evidence"]
        only_evidence = held_cases(suite().cases, changed)
        self.assertLess(len(only_evidence), len(held_cases(suite().cases, pack())) + 1)
        for cid in only_evidence:
            self.assertTrue(any(w["capability"] == "fin.cap.record_investigation_evidence" for w in suite().cases[cid]["expected"]["writes_expected"]))

    def test_a_pack_without_the_rule_holds_nothing(self) -> None:
        changed = copy.deepcopy(pack())
        changed["policy"]["rules"] = [r for r in changed["policy"]["rules"] if r["rule_id"] != RULE]
        self.assertEqual({}, held_cases(suite().cases, changed))

    def test_a_trivially_true_condition_holds_nothing(self) -> None:
        changed = copy.deepcopy(pack())
        for rule in changed["policy"]["rules"]:
            if rule["rule_id"] == RULE:
                rule["condition"] = "true"
        self.assertEqual({}, held_cases(suite().cases, changed))

    def test_the_registry_names_the_issue_that_lifts_the_hold(self) -> None:
        self.assertEqual({RULE: "noema#346"}, dict(HOLDS))


if __name__ == "__main__":
    unittest.main()
