"""BEN-16, ruling on noema#346: the case-write oracles follow the pack; a case that contradicts it is held and not counted in n.

The module reads the rule and its condition from the pack at run time; the meaning is the pack owner's (EL, noema#346). Written before the change to `held.py` and seen failing. SYNTHETIC; generated layer only (INV-22).
"""
import copy
import json
import unittest
from collections.abc import Callable
from functools import lru_cache
from typing import Any

from benchmark.generator.generate import PACK, Suite, generate
from benchmark.generator.held import HOLDS, PackRuleError, held_cases

RULE = "fin.pol.default.permit_case_writes"
# The generated cases that start with an ambiguous reference and then ask to open a case after an unrelated turn: no turn clarifies the reference, so under the Architect's A116 they may not expect a case write (generator 1.3.3).
UNRELATED = {"BEN-AMB-0016", "BEN-AMB-0034", "BEN-AMB-1006"}


@lru_cache(maxsize=None)
def suite() -> Suite:
    return generate(20261020)


def pack() -> dict[str, Any]:
    out: dict[str, Any] = json.loads(PACK.read_text(encoding="utf-8"))
    return out


def oracles() -> dict[str, Any]:
    return suite().oracles


def with_writes() -> dict[str, Any]:
    """The generated cases with the three UNRELATED cases' former case writes put back (generator 1.3.2), so the hold is still tested on a case that starts ambiguous, expects a write and has no clarifying turn."""
    cases = copy.deepcopy(suite().cases)
    donor = next(c for c in suite().cases.values() if any(w["capability"] == "fin.cap.create_escalation_case" for w in c["expected"]["writes_expected"]))
    for cid in UNRELATED:
        target = oracles()[cid]["correct_target"]
        cases[cid]["expected"]["writes_expected"] = [{**w, "target": target} for w in donor["expected"]["writes_expected"]]
    return cases


class Hold(unittest.TestCase):
    """EL's answer on noema#346 (8 Nov 2026): `subject_resource.status == 'RESOLVED'` is the intent-slot ambiguity gate: a case write needs the reference resolved to exactly one transaction.
    A case that expects a governed write while its oracle has no single resolved target contradicts the pack: it is held (not counted in n) and recorded as a finding."""

    def test_no_generated_case_expects_a_governed_write_without_a_single_resolved_target(self) -> None:
        held = held_cases(suite().cases, oracles(), pack())
        self.assertEqual([], [c for c, h in held.items() if "no single resolved target" in h["reason"]], "a case expects a case write the pack's slot rule would not permit")
        self.assertEqual(set(), set(held), "no generated case is held: the ones that open a case after an unrelated turn expect no case write")
        expecting = [cid for cid, c in suite().cases.items() if any(w["capability"] in {"fin.cap.create_escalation_case", "fin.cap.record_investigation_evidence"} for w in c["expected"]["writes_expected"])]
        self.assertTrue(expecting)
        self.assertTrue(all(oracles()[cid]["correct_target"] is not None for cid in expecting))

    def test_a_case_that_expects_a_case_write_but_has_no_resolved_target_is_held_with_the_pack_s_condition(self) -> None:
        cid = next(c for c, k in suite().cases.items() if k["expected"]["writes_expected"])
        bad = copy.deepcopy(oracles())
        bad[cid]["correct_target"] = None
        held = held_cases(suite().cases, bad, pack())
        self.assertEqual({cid}, set(held))
        self.assertEqual({"held_on": "noema#346", "rule_id": RULE, "condition": "subject_resource.status == 'RESOLVED'"}, {k: v for k, v in held[cid].items() if k != "reason"})
        self.assertIn("no single resolved target", held[cid]["reason"])

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
            self.assertEqual(not target_present, cid in held and "no single resolved target" in held[cid]["reason"], status)

    # Architect's ruling A116 (10 Nov 2026, adopting EL's reading; recorded by docs#148 in DEC-2026-11-10-architect-sync-a113-a122, not merged yet): a reference that STARTS ambiguous (R3, R4) must not expect any case write before the clarifying turn that resolves it.
    # `correct_target` set is not enough: it describes where the conversation ends, not the first turn.
    def _governed_cases(self) -> list[str]:
        governed = {"fin.cap.create_escalation_case", "fin.cap.record_investigation_evidence"}
        return [cid for cid, c in with_writes().items() if any(w["capability"] in governed for w in c["expected"]["writes_expected"])]

    def _starts_ambiguous(self, cid: str) -> bool:
        case, oracle = with_writes()[cid], oracles()[cid]
        return case["category"] == "AMBIGUITY" or bool(oracle["high_risk_ambiguous"]) or bool(case["expected"]["clarification_expected"])

    def test_a_case_that_starts_ambiguous_and_expects_a_case_write_has_a_clarifying_turn_before_it_or_is_held(self) -> None:
        held = held_cases(with_writes(), oracles(), pack())
        ambiguous = [cid for cid in self._governed_cases() if self._starts_ambiguous(cid)]
        self.assertEqual(UNRELATED, set(ambiguous), "with the writes restored, the cases that open a case after an unrelated turn are the ambiguous ones that expect a case write")
        self.assertEqual(UNRELATED, set(held), "and each is held: its second turn does not clarify the reference")
        self.assertEqual(set(), {c for c in self._governed_cases() if c in suite().cases and suite().cases[c]["expected"]["writes_expected"] and self._starts_ambiguous(c)}, "the generated suite has none")

    def test_an_ambiguous_start_with_a_write_and_a_resolved_target_but_no_clarifying_turn_is_held(self) -> None:
        cid = next(c for c in self._governed_cases() if self._starts_ambiguous(c))
        cases = copy.deepcopy(with_writes())
        cases[cid]["utterances"] = cases[cid]["utterances"][:1]
        self.assertIsNotNone(oracles()[cid]["correct_target"])
        held = held_cases(cases, oracles(), pack())
        self.assertIn(cid, held)
        self.assertEqual("noema#346", held[cid]["held_on"])
        self.assertIn("clarifying turn", held[cid]["reason"])

    def test_every_marker_of_an_ambiguous_start_is_honoured(self) -> None:
        cid = next(c for c in self._governed_cases() if self._starts_ambiguous(c))
        markers: list[tuple[str, Callable[[dict[str, Any], dict[str, Any]], object]]] = [
            ("category", lambda c, o: c.update(category="AMBIGUITY")), ("flag", lambda c, o: o.update(high_risk_ambiguous=True)),
            ("clarify", lambda c, o: c["expected"].update(clarification_expected=True))]
        for marker, change in markers:
            cases, oracle = copy.deepcopy(with_writes()), copy.deepcopy(oracles())
            plain = next(c for c in self._governed_cases() if not self._starts_ambiguous(c))
            cases[plain]["utterances"] = cases[plain]["utterances"][:1]
            self.assertNotIn(plain, held_cases(cases, oracle, pack()), "a reference that starts resolved needs no clarifying turn")
            change(cases[plain], oracle[plain])
            self.assertIn(plain, held_cases(cases, oracle, pack()), marker)
        self.assertTrue(cid)

    # Codex P1 on noema#358 (Architect, 10 Nov): a SECOND UTTERANCE IS NOT A CLARIFICATION. The turn that resolves the reference must answer the clarification; an unrelated turn ("Forget that, I want to
    # open a case instead") leaves the reference unresolved, so the governed write would come before any clarifying turn. In a v1 case the writes carry no turn: they follow the last turn, so the order is
    # "a clarifying answer exists at turn 2 or later, before the write". A clarifying answer in turn 1 cannot clarify itself.
    def _case_with_turns(self, *texts: str) -> tuple[str, dict[str, Any]]:
        cid = next(c for c in self._governed_cases() if self._starts_ambiguous(c))
        cases = copy.deepcopy(with_writes())
        cases[cid]["utterances"] = [{"turn": i, "text": t} for i, t in enumerate(texts, start=1)]
        return cid, cases

    def test_an_unrelated_second_utterance_is_not_a_clarification_and_the_case_is_held(self) -> None:
        for second in ("Forget that, I want to open a case instead", "Forget am, I wan open case instead", "Thanks", "ok", "What is my balance?", "Where is the 20k I sent to Amina this morning?"):
            cid, cases = self._case_with_turns("Where is the 20k I sent to Amina this morning?", second)
            held = held_cases(cases, oracles(), pack())
            self.assertIn(cid, held, second)
            self.assertIn("clarifying", held[cid]["reason"])

    def test_a_turn_that_answers_the_clarification_lets_the_write_stand(self) -> None:
        for answer in ("The first one", "The second one", "The one ending 3334", "The one wey end for 3334", "None of these", "E no be any of dem", "Abeg none of these",
                       "No, I made a mistake, it was N2,500 not 10k", "No, I don mistake, na N50,000 no be 5,000 naira"):
            cid, cases = self._case_with_turns("Where is the 20k I sent to Amina this morning?", answer)
            self.assertNotIn(cid, held_cases(cases, oracles(), pack()), answer)

    def test_the_clarifying_answer_comes_before_the_write_a_first_turn_cannot_clarify_itself(self) -> None:
        cid, cases = self._case_with_turns("The first one")
        self.assertIn(cid, held_cases(cases, oracles(), pack()))
        cid, cases = self._case_with_turns("The first one", "Thanks")
        self.assertIn(cid, held_cases(cases, oracles(), pack()), "the only answer-shaped text is turn 1")
        cid, cases = self._case_with_turns("Where is the 20k I sent to Amina?", "Thanks", "The first one")
        self.assertNotIn(cid, held_cases(cases, oracles(), pack()), "a clarifying answer at turn 3 precedes the write, which follows the last turn")

    def test_the_generated_cases_that_open_a_case_after_an_unrelated_turn_expect_no_case_write_and_nothing_is_held(self) -> None:
        held = held_cases(suite().cases, oracles(), pack())
        self.assertEqual(set(), set(held))
        for cid in UNRELATED:
            self.assertEqual([], suite().cases[cid]["expected"]["writes_expected"], cid)
            self.assertIn("any_write", suite().cases[cid]["expected"]["forbidden_actions"], cid)

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
        self.assertEqual(set(), set(held_cases(suite().cases, oracles(), changed)))

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
