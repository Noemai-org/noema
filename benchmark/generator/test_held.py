"""BEN-16, ruling on noema#346: the case-write oracles follow the pack; a case that contradicts it is held and not counted in n.

The module reads the rule and its condition from the pack at run time; the meaning is the pack owner's (EL, noema#346). Written before the change to `held.py` and seen failing. SYNTHETIC; generated layer only (INV-22).
"""
import copy
import json
import unittest
from functools import lru_cache
from typing import Any

from benchmark.generator.generate import PACK, Suite, generate
from benchmark.generator.held import HOLDS, held_cases

RULE = "fin.pol.default.permit_case_writes"


@lru_cache(maxsize=None)
def suite() -> Suite:
    return generate(20261020)


def pack() -> dict[str, Any]:
    out: dict[str, Any] = json.loads(PACK.read_text(encoding="utf-8"))
    return out


def oracles() -> dict[str, Any]:
    return suite().oracles


class Hold(unittest.TestCase):
    """EL's answer on noema#346 (8 Nov 2026): `subject_resource.status == 'RESOLVED'` is the intent-slot ambiguity gate: a case write needs the reference resolved to exactly one transaction.
    A case that expects a governed write while its oracle has no single resolved target contradicts the pack: it is held (not counted in n) and recorded as a finding."""

    def test_no_generated_case_expects_a_governed_write_without_a_single_resolved_target(self) -> None:
        held = held_cases(suite().cases, oracles(), pack())
        self.assertEqual({}, held, "a case expects a case write the pack's slot rule would not permit")
        expecting = [cid for cid, c in suite().cases.items() if any(w["capability"] in {"fin.cap.create_escalation_case", "fin.cap.record_investigation_evidence"} for w in c["expected"]["writes_expected"])]
        self.assertTrue(expecting)
        self.assertTrue(all(oracles()[cid]["correct_target"] is not None for cid in expecting))

    def test_a_case_that_expects_a_case_write_but_has_no_resolved_target_is_held_with_the_pack_s_condition(self) -> None:
        cid = next(c for c, k in suite().cases.items() if k["expected"]["writes_expected"])
        bad = copy.deepcopy(oracles())
        bad[cid]["correct_target"] = None
        held = held_cases(suite().cases, bad, pack())
        self.assertEqual({cid: {"held_on": "noema#346", "rule_id": RULE, "condition": "subject_resource.status == 'RESOLVED'"}}, held)

    def test_a_case_with_no_governed_write_is_never_held(self) -> None:
        bad = copy.deepcopy(oracles())
        for cid in bad:
            bad[cid]["correct_target"] = None
        held = held_cases(suite().cases, bad, pack())
        self.assertTrue(held)
        for cid in held:
            self.assertTrue(suite().cases[cid]["expected"]["writes_expected"], cid)

    def test_the_condition_is_read_from_the_pack_not_hard_coded(self) -> None:
        changed = copy.deepcopy(pack())
        for rule in changed["policy"]["rules"]:
            if rule["rule_id"] == RULE:
                rule["condition"] = "subject_resource.status != 'RESOLVED'"
        cid = next(c for c, k in suite().cases.items() if k["expected"]["writes_expected"])
        bad = copy.deepcopy(oracles())
        bad[cid]["correct_target"] = None
        self.assertEqual("subject_resource.status != 'RESOLVED'", held_cases(suite().cases, bad, changed)[cid]["condition"])

    def test_a_pack_without_the_rule_or_with_a_trivial_condition_holds_nothing(self) -> None:
        bad = copy.deepcopy(oracles())
        for cid in bad:
            bad[cid]["correct_target"] = None
        no_rule = copy.deepcopy(pack())
        no_rule["policy"]["rules"] = [r for r in no_rule["policy"]["rules"] if r["rule_id"] != RULE]
        self.assertEqual({}, held_cases(suite().cases, bad, no_rule))
        trivial = copy.deepcopy(pack())
        for rule in trivial["policy"]["rules"]:
            if rule["rule_id"] == RULE:
                rule["condition"] = "true"
        self.assertEqual({}, held_cases(suite().cases, bad, trivial))

    def test_the_registry_names_the_issue(self) -> None:
        self.assertEqual({RULE: "noema#346"}, dict(HOLDS))


if __name__ == "__main__":
    unittest.main()
