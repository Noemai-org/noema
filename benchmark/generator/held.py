"""Cases whose case-write oracle is held until the pack owner states what a pack rule means (ruling on noema#346, 8 Nov 2026).

The Architect's ruling: oracles derive from the pack; nobody here interprets `fin.pol.default.permit_case_writes` (`subject_resource.status == 'RESOLVED'`). So this module reads the rule,
its `applies_to` and its condition from the pack at run time, and marks every case that expects a write the rule governs as held. A held case is reported and is NOT counted in n. When EL
states the meaning (and fixes the pack if it is wrong), the entry is removed from `HOLDS`, or the pack changes and the oracles are regenerated from it; a pack defect found this way is
recorded as a finding, not hidden. Generated layer only; reads no oracle or held-out material (INV-22).
"""
from typing import Any, Mapping

Json = dict[str, Any]
# rule id -> the issue that lifts the hold
HOLDS: tuple[tuple[str, str], ...] = (("fin.pol.default.permit_case_writes", "noema#346"),)
TRIVIAL = {"", "true"}


def held_cases(cases: Mapping[str, Json], pack: Mapping[str, Any]) -> dict[str, Json]:
    """case id -> {held_on, rule_id, condition} for every case that expects a write governed by a held rule with a real condition in this pack."""
    rules = {r["rule_id"]: r for r in pack.get("policy", {}).get("rules", [])}
    out: dict[str, Json] = {}
    for rule_id, issue in HOLDS:
        rule = rules.get(rule_id)
        if rule is None or str(rule.get("condition", "")).strip() in TRIVIAL:
            continue
        governed = set(rule.get("applies_to", []))
        for case_id, case in cases.items():
            if any(w["capability"] in governed for w in case["expected"]["writes_expected"]):
                out.setdefault(case_id, {"held_on": issue, "rule_id": rule_id, "condition": rule["condition"]})
    return out
