"""From an archetype's faults to a fault-plan.v1 document (PERD §37.1, REQ-ENV-004; contracts/schemas/fault-plan.v1.json).

An archetype states the fault its scenario needs. Whether a producer exists for it is a separate question the contract settles: a fault aimed at
`mock-case` must be an effect mock-case produces, the four state-change effects exist only in Tushe's fault controller, and so on. This module applies
the few rewrites that keep the scenario's meaning, and names the rest. A fault no producer has is never dropped silently: the case carries a
`needs_fault:<EFFECT>.<operation>` timeline marker, which the runner has no driver for, so the case is an ERROR that says what is missing until a
producer is built (the benchmark owner owns the plan; mock-case and Tushe own the producers). An injection into a mock-case field the contract does not allow is such a fault."""
from typing import Any

Json = dict[str, Any]
MOCK_CASE = "mock-case"
# The thirteen effects mock-case produces (synthetic/mock_case README; the fault-plan contract checks them per operation).
MOCK_CASE_EFFECTS = frozenset({
    "LATENCY", "TIMEOUT_BEFORE_COMMIT", "TIMEOUT_AFTER_COMMIT", "DROP_RESPONSE", "HTTP_5XX", "HTTP_429", "PAGINATE", "CONCURRENT_MODIFICATION",
    "NEAR_TIMEOUT_BEFORE", "NEAR_TIMEOUT_AFTER", "HTTP_200_SEMANTIC_PENDING", "IDEMPOTENCY_COLLISION", "INJECTION_PAYLOAD",
})
# mock-case injects instruction-like text only into these fields (fault-plan.v1, last allOf branch).
INJECTABLE = {("get_case", "reason_code"), ("find_cases", "reason_code"), ("get_evidence", "payload"), ("find_evidence", "payload")}
# The four state-change effects exist only in Tushe's fault controller (the contract's reading rule 3).
TUSHE_EFFECTS = frozenset({"SPLIT_STATE", "DEBIT_WITHOUT_CREDIT", "DELAYED_SETTLEMENT", "DELAYED_REVERSAL"})
# Effects that come from Tushe or mock-case, never from the hostility proxy (reading rule 4).
TUSHE_NATIVE = TUSHE_EFFECTS | {"PAGINATE", "POLICY_MUTATION", "HTTP_200_SEMANTIC_PENDING", "IDEMPOTENCY_COLLISION"}
WEBHOOK_EFFECTS = frozenset({"DUPLICATE_WEBHOOK", "DROP_WEBHOOK", "REORDER_EVENTS"})


def normalise(fault: Json) -> tuple[Json | None, str | None]:
    """(the fault as the contract allows it, None) or (None, "<EFFECT>.<operation>") when no producer has it."""
    if fault["target"] != MOCK_CASE:
        return fault, None
    effect = fault["effect"]
    if effect == "INJECTION_PAYLOAD":
        if (fault["operation"], fault["params"].get("field")) in INJECTABLE:
            return fault, None
        return None, f"{effect}.{fault['operation']}"
    if effect in MOCK_CASE_EFFECTS:
        return fault, None
    if effect == "SERVICE_UNAVAILABLE":  # a case system that is down answers 503
        return {**fault, "effect": "HTTP_5XX", "params": {"status": 503}}, None
    if effect in WEBHOOK_EFFECTS:  # webhooks are the enterprise's (Tushe's), not the case system's
        return {**fault, "target": "mock-destination", "operation": "events_feed"}, None
    return None, f"{effect}.{fault['operation']}"


def source_of(faults: list[Json]) -> str:
    """Who produces the plan (contract reading rules 1 to 4)."""
    if any(f["effect"] in TUSHE_NATIVE and f["target"] != MOCK_CASE for f in faults):
        return "TUSHE"
    if all(f["target"] == MOCK_CASE for f in faults):
        return "MOCK_CASE"
    return "PROXY"


def plan_id(case_id: str) -> str:
    return "fp_" + case_id.lower().replace("-", "_")


def plan_document(case_id: str, seed: int, faults: list[Json]) -> tuple[Json | None, list[str]]:
    """(the plan, the faults nobody produces). The plan is None when nothing in it can be produced."""
    produced: list[Json] = []
    missing: list[str] = []
    for fault in faults:
        out, reason = normalise(fault)
        if out is None:
            missing.append(reason or fault["effect"])
        else:
            produced.append(out)
    if not produced:
        return None, missing
    return {"fault_plan_id": plan_id(case_id), "scenario_id": case_id, "seed": seed, "deterministic": True, "source": source_of(produced), "faults": produced}, missing
