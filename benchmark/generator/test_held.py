"""BEN-16, ruling on noema#346: the case-write oracles follow the pack; a case that contradicts it is held and not counted in n.

The module reads the rule and its condition from the pack at run time; the meaning is the pack owner's (EL, noema#346). Written before the change to `held.py` and seen failing. SYNTHETIC; generated layer only (INV-22).
"""
import copy
import json
import unittest
from functools import lru_cache
from typing import Any

from benchmark.generator.generate import PACK, Suite, generate
from benchmark.generator.held import HOLDS, PackRuleError, held_cases

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

    def test_the_four_slot_statuses_map_to_the_oracle_target_as_the_contracts_define(self) -> None:
        # intent-slot.v1: "a slot is RESOLVED only with a resolved_ref" (INV-06, REQ-AMB-005). benchmark-oracle.v1: correct_target is "the resource the principal truly means, or null when no
        # single resource is the right answer". So RESOLVED <-> a target; AMBIGUOUS, UNRESOLVED and ABSENT <-> null. Codex on 3eefbf2: the mapping is tested in all four states.
        cid = next(c for c, k in suite().cases.items() if k["expected"]["writes_expected"])
        for status, target_present in (("RESOLVED", True), ("AMBIGUOUS", False), ("UNRESOLVED", False), ("ABSENT", False)):
            oracle = copy.deepcopy(oracles())
            oracle[cid]["correct_target"] = suite().oracles[cid]["correct_target"] if target_present else None
            held = held_cases(suite().cases, oracle, pack())
            self.assertEqual(not target_present, cid in held, status)

    def test_a_missing_rule_fails_closed_not_silently_clean(self) -> None:
        # Codex P1: a broken or incomplete pack must never read as "no case is held".
        changed = copy.deepcopy(pack())
        changed["policy"]["rules"] = [r for r in changed["policy"]["rules"] if r["rule_id"] != RULE]
        with self.assertRaises(PackRuleError):
            held_cases(suite().cases, oracles(), changed)
        with self.assertRaises(PackRuleError):
            held_cases(suite().cases, oracles(), {})

    def test_a_malformed_rule_fails_closed(self) -> None:
        for field, bad in (("condition", None), ("condition", ""), ("condition", 7), ("applies_to", None), ("applies_to", []), ("applies_to", "fin.cap.create_escalation_case"), ("applies_to", [3])):
            changed = copy.deepcopy(pack())
            for rule in changed["policy"]["rules"]:
                if rule["rule_id"] == RULE:
                    rule[field] = bad
            with self.assertRaises(PackRuleError, msg=(field, bad)):
                held_cases(suite().cases, oracles(), changed)

    def test_a_condition_other_than_the_adopted_one_is_unsupported_and_fails_closed(self) -> None:
        # The mapping above is adopted for exactly this condition (SoT 43.8). A pack that changes it (even to a trivial `true`) needs the meaning re-adopted, not a silent pass.
        for condition in ("true", "subject_resource.status != 'RESOLVED'", "subject_resource.status == 'AMBIGUOUS'"):
            changed = copy.deepcopy(pack())
            for rule in changed["policy"]["rules"]:
                if rule["rule_id"] == RULE:
                    rule["condition"] = condition
            with self.assertRaises(PackRuleError, msg=condition):
                held_cases(suite().cases, oracles(), changed)

    def test_whitespace_in_the_adopted_condition_is_not_a_different_condition(self) -> None:
        changed = copy.deepcopy(pack())
        for rule in changed["policy"]["rules"]:
            if rule["rule_id"] == RULE:
                rule["condition"] = "subject_resource.status   ==   'RESOLVED'"
        self.assertEqual({}, held_cases(suite().cases, oracles(), changed))

    def test_the_hold_follows_the_rule_s_applies_to(self) -> None:
        changed = copy.deepcopy(pack())
        for rule in changed["policy"]["rules"]:
            if rule["rule_id"] == RULE:
                rule["applies_to"] = ["fin.cap.record_investigation_evidence"]
        bad = copy.deepcopy(oracles())
        for cid in bad:
            bad[cid]["correct_target"] = None
        for cid in held_cases(suite().cases, bad, changed):
            self.assertTrue(any(w["capability"] == "fin.cap.record_investigation_evidence" for w in suite().cases[cid]["expected"]["writes_expected"]))

    def test_the_registry_names_the_issue(self) -> None:
        self.assertEqual({RULE: "noema#346"}, dict(HOLDS))


if __name__ == "__main__":
    unittest.main()
