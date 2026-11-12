"""Cases that contradict a pack rule whose meaning is committed (noema#346; Codex P1s on 3eefbf2).

The Architect's ruling of 8 Nov 2026: oracles derive from the pack and nobody here invents a reading of `fin.pol.default.permit_case_writes` (`subject_resource.status == 'RESOLVED'`). The meaning
is taken from committed sources only: SoT section 43.8 (the capability precondition `subject_resource.status == RESOLVED`), the SoT's `subject_resource` slot (an intent slot with a status), and
`contracts/schemas/intent-slot.v1.json` ("a slot is RESOLVED only with a resolved_ref", INV-06, REQ-AMB-005). So RESOLVED means the person's reference was resolved to exactly one transaction.
On the oracle side, `benchmark-oracle.v1` defines `correct_target` as "the resource the principal truly means, or null when no single resource is the right answer". The mapping is therefore
RESOLVED <-> `correct_target` set; AMBIGUOUS, UNRESOLVED and ABSENT <-> `correct_target` null. A case that expects a write the rule governs while its oracle has no resolved target contradicts
the pack: it is HELD (not counted in n) and reported as a finding, not hidden.

This check is adopted for exactly one condition. If the rule is missing, malformed, or its condition is any other text (even a trivially true one), `held_cases` raises `PackRuleError`
instead of reporting zero held cases: a broken or changed pack must never read as a clean benchmark. Generated layer only; reads no held-out material (INV-22).
"""
import re
from typing import Any, Mapping

Json = dict[str, Any]
# rule id -> the issue that recorded its meaning
HOLDS: tuple[tuple[str, str], ...] = (("fin.pol.default.permit_case_writes", "noema#346"),)
SUPPORTED_CONDITION = "subject_resource.status == 'RESOLVED'"


class PackRuleError(ValueError):
    """The pack's governing rule is absent, malformed or not the one this check was adopted for."""


def _rule(pack: Mapping[str, Any], rule_id: str) -> Json:
    rules = pack.get("policy", {}).get("rules") if isinstance(pack.get("policy"), Mapping) else None
    if not isinstance(rules, list):
        raise PackRuleError("the pack has no policy rules")
    found = [r for r in rules if isinstance(r, Mapping) and r.get("rule_id") == rule_id]
    if len(found) != 1:
        raise PackRuleError(f"the pack must hold exactly one rule {rule_id!r}, found {len(found)}")
    rule = dict(found[0])
    condition, applies = rule.get("condition"), rule.get("applies_to")
    if not isinstance(condition, str) or not condition.strip():
        raise PackRuleError(f"rule {rule_id!r} has no usable condition")
    if not isinstance(applies, list) or not applies or not all(isinstance(a, str) and a for a in applies):
        raise PackRuleError(f"rule {rule_id!r} has no usable applies_to")
    if " ".join(condition.split()) != SUPPORTED_CONDITION:
        raise PackRuleError(f"rule {rule_id!r} has condition {condition!r}; this check is adopted only for {SUPPORTED_CONDITION!r}, so the meaning must be re-adopted before it can be used")
    return rule


# What answers a clarification question in the generated cases (benchmark/generator/lib_clean_ambiguity.py): picking one ("The first one", "The one ending 3334" and the Pidgin forms), saying
# none is right, or correcting the details ("No, I made a mistake"). "Forget that, I want to open a case instead" changes the request and resolves nothing, so it is NOT here (Codex P1 on
# noema#358: a second utterance is not a clarification). A closed list on purpose: an unrecognised turn is not assumed to clarify.
CLARIFYING_ANSWER = re.compile(
    r"^\s*(?:the\s+(?:first|second|third|last)\s+one|the\s+one\s+(?:ending|wey\s+end\s+for|that\s+ends(?:\s+in)?)\s+\S+|none\s+of\s+these|e\s+no\s+be\s+any\s+of\s+dem|abeg\s+none\s+of\s+these"
    r"|no,\s+i\s+(?:made\s+a\s+mistake|don\s+mistake)\b.*)\s*[.!]?\s*$", re.IGNORECASE)


def _has_clarifying_turn(case: Mapping[str, Any]) -> bool:
    """A later turn (turn 2 or after; the first turn cannot clarify itself) answers the clarification. The governed write follows the last turn, so such a turn precedes it."""
    return any(CLARIFYING_ANSWER.match(str(u.get("text", ""))) for u in list(case["utterances"])[1:])


def _starts_ambiguous(case: Mapping[str, Any], oracle: Mapping[str, Any]) -> bool:
    """A reference that starts ambiguous (R3, R4): an ambiguity case, a high-risk ambiguous oracle, or a case that expects a clarification."""
    return case.get("category") == "AMBIGUITY" or bool(oracle.get("high_risk_ambiguous")) or bool(case["expected"].get("clarification_expected"))


def held_cases(cases: Mapping[str, Json], oracles: Mapping[str, Json], pack: Mapping[str, Any]) -> dict[str, Json]:
    """case id -> {held_on, rule_id, condition, reason} for every case that expects a governed write the pack's slot rule would not permit. Raises PackRuleError on an unusable rule.

    Two ways to contradict the rule. (1) The oracle has no single resolved target. (2) Architect's ruling A116 (10 Nov 2026, adopting EL's reading; to be recorded in DEC-2026-11-10-architect-sync-a113-a122 by docs#148, which has not merged, so this check does not rely on A116 as authority until it does): the reference starts
    ambiguous and no later turn ANSWERS the clarification (a second utterance is not a clarification: `CLARIFYING_ANSWER`), so the write would come before the clarifying turn that resolves it; `correct_target` set is not enough, because it
    says where the conversation ends, not what the first turn resolved."""
    out: dict[str, Json] = {}
    for rule_id, issue in HOLDS:
        rule = _rule(pack, rule_id)
        governed = set(rule["applies_to"])
        for case_id, case in cases.items():
            if not any(w["capability"] in governed for w in case["expected"]["writes_expected"]):
                continue
            oracle = oracles[case_id]
            if oracle.get("correct_target") is None:
                reason = "the case expects a governed write but its oracle has no single resolved target"
            elif _starts_ambiguous(case, oracle) and not _has_clarifying_turn(case):
                reason = "the reference starts ambiguous and the case has no clarifying turn before the governed write (Architect's A116, recorded in docs#148)"
            else:
                continue
            out.setdefault(case_id, {"held_on": issue, "rule_id": rule_id, "condition": rule["condition"], "reason": reason})
    return out
