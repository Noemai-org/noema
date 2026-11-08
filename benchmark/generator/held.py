"""Cases that contradict a pack rule whose meaning the pack owner has stated (noema#346, 8 Nov 2026).

The Architect's ruling: oracles derive from the pack; this module does not interpret `fin.pol.default.permit_case_writes` (`subject_resource.status == 'RESOLVED'`). The pack owner (EL) stated
its meaning: `subject_resource` is the intent slot, and RESOLVED means the person's reference was resolved to exactly one transaction, so a case write is permitted only against an
identified transaction, never against an ambiguous, unresolved or absent one. An oracle with `correct_target` null has no single resolved transaction. A case that expects a write the rule
governs while its oracle has no resolved target contradicts the pack: it is HELD (not counted in n) and reported as a finding, not hidden. The rule, `applies_to` and condition are read from the
pack at run time. Today no generated case is held. Generated layer only; reads no held-out material (INV-22).
"""
from typing import Any, Mapping

Json = dict[str, Any]
# rule id -> the issue that recorded its meaning
HOLDS: tuple[tuple[str, str], ...] = (("fin.pol.default.permit_case_writes", "noema#346"),)
TRIVIAL = {"", "true"}


def held_cases(cases: Mapping[str, Json], oracles: Mapping[str, Json], pack: Mapping[str, Any]) -> dict[str, Json]:
    """case id -> {held_on, rule_id, condition} for every case that expects a governed write although its oracle has no single resolved target."""
    rules = {r["rule_id"]: r for r in pack.get("policy", {}).get("rules", [])}
    out: dict[str, Json] = {}
    for rule_id, issue in HOLDS:
        rule = rules.get(rule_id)
        if rule is None or str(rule.get("condition", "")).strip() in TRIVIAL:
            continue
        governed = set(rule.get("applies_to", []))
        for case_id, case in cases.items():
            expects = any(w["capability"] in governed for w in case["expected"]["writes_expected"])
            if expects and oracles[case_id].get("correct_target") is None:
                out.setdefault(case_id, {"held_on": issue, "rule_id": rule_id, "condition": rule["condition"]})
    return out
