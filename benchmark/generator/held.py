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


def held_cases(cases: Mapping[str, Json], oracles: Mapping[str, Json], pack: Mapping[str, Any]) -> dict[str, Json]:
    """case id -> {held_on, rule_id, condition} for every case that expects a governed write although its oracle has no single resolved target. Raises PackRuleError on an unusable rule."""
    out: dict[str, Json] = {}
    for rule_id, issue in HOLDS:
        rule = _rule(pack, rule_id)
        governed = set(rule["applies_to"])
        for case_id, case in cases.items():
            expects = any(w["capability"] in governed for w in case["expected"]["writes_expected"])
            if expects and oracles[case_id].get("correct_target") is None:
                out.setdefault(case_id, {"held_on": issue, "rule_id": rule_id, "condition": rule["condition"]})
    return out
