"""What the environment can apply of a case's fault plan (REQ-ENV-004).

The generator writes a provisional plan in the shape of PERD §37.1. mock-case applies only a plan it can apply exactly (see its README), so
the faults aimed at `mock-case` are translated into mock-case's own format, and everything else is listed as UNSUPPORTED: the source is not
reachable from this build (Tushe), or mock-case does not produce the effect. An unsupported fault is never dropped: the case is an ERROR
(REQ-BEN-001) rather than a run that quietly injected less than it claims. Every plan this module builds is loaded into the real mock-case
by `benchmark.oracle_client.live_check`."""
from dataclasses import dataclass, field
from typing import Any

Json = dict[str, Any]
CLOCK_START = "2026-10-01T09:40:00+01:00"  # the start of the PERD §37.1 example, SYNTHETIC; every case's simulated clock starts here
MOCK_CASE = "mock-case"
MOCK_CASE_EFFECTS = frozenset({"LATENCY", "TIMEOUT_BEFORE_COMMIT", "TIMEOUT_AFTER_COMMIT", "DROP_RESPONSE", "HTTP_5XX", "HTTP_429", "PAGINATE",
                               "CONCURRENT_MODIFICATION"})
OPERATIONS = frozenset({"create_case", "find_cases", "get_case", "route_case", "create_complaint", "find_complaints", "get_complaint", "append_evidence",
                        "find_evidence", "get_evidence"})
OPERATION_NAME = {"route": "route_case"}
MATCH_KEYS = frozenset({"nth_call", "tenant_id", "principal_id", "transaction_ref", "idempotency_key", "case_id"})


@dataclass
class Split:
    mock_case: Json | None
    unsupported: list[str] = field(default_factory=list)


def _seconds_past_grant(ttl: int | None) -> int | None:
    return None if ttl is None else ttl + 1


def _translate(fault: Json, ttl: int | None) -> tuple[Json | None, str | None]:
    effect, params = fault["effect"], dict(fault["params"])
    operation = OPERATION_NAME.get(fault["operation"], fault["operation"])
    where = f"{fault['target']}.{fault['operation']} {effect}"
    if operation not in OPERATIONS:
        return None, f"{where}: mock-case has no operation {fault['operation']}"
    if set(fault["match"]) - MATCH_KEYS:
        return None, f"{where}: mock-case cannot match on {sorted(set(fault['match']) - MATCH_KEYS)}"
    out: Json = {}
    if effect in ("LATENCY", "TIMEOUT_BEFORE_COMMIT", "TIMEOUT_AFTER_COMMIT"):
        if params.pop("delay", None) == "past_grant_ttl":
            seconds = _seconds_past_grant(ttl)
            if seconds is None:
                return None, f"{where}: a delay past the grant's expiry needs --grant-ttl-seconds"
            out["delay_ms"] = seconds * 1000
        if "delay_ms" in params:
            out["delay_ms"] = params.pop("delay_ms")
    elif effect == "HTTP_5XX":
        if "committed" in params:
            out["after_commit"] = params.pop("committed")
        if "status" in params:
            out["status"] = params.pop("status")
    elif effect == "HTTP_429":
        if params.pop("retry_after", None) == "beyond_grant_expiry":
            seconds = _seconds_past_grant(ttl)
            if seconds is None:
                return None, f"{where}: a Retry-After beyond the grant's expiry needs --grant-ttl-seconds"
            out["retry_after_seconds"] = seconds
        if "retry_after_seconds" in params:
            out["retry_after_seconds"] = params.pop("retry_after_seconds")
    elif effect == "PAGINATE":
        if "page_size" in params:
            out["page_size"] = params.pop("page_size")
    if params:
        return None, f"{where}: parameters mock-case does not take: {sorted(params)}"
    return {"target": MOCK_CASE, "operation": operation, "match": dict(fault["match"]), "effect": effect, "params": out}, None


def needs_clock(case: Json) -> bool:
    return any(e["event"].startswith("clock_advance:") for e in case["events_timeline"])


def split_plan(case: Json, plan: Json | None, *, grant_ttl_seconds: int | None) -> Split:
    unsupported: list[str] = []
    faults: list[Json] = []
    for fault in (plan or {}).get("faults", []):
        if fault["target"] != MOCK_CASE:
            unsupported.append(f"{fault['target']}.{fault['operation']} {fault['effect']}: no access to {fault['target']} from this build")
        elif fault["effect"] not in MOCK_CASE_EFFECTS:
            unsupported.append(f"{fault['target']}.{fault['operation']} {fault['effect']}: mock-case does not produce this effect")
        else:
            translated, reason = _translate(fault, grant_ttl_seconds)
            if translated is None:
                unsupported.append(reason or "untranslatable")
            else:
                faults.append(translated)
    clock = needs_clock(case)
    if not faults and not clock:
        return Split(None, unsupported)
    plan_id = (plan or {}).get("plan_id") or "fp_" + case["case_id"].lower().replace("-", "_")
    doc: Json = {"fault_plan_id": plan_id, "scenario_id": case["case_id"], "seed": case["generator"]["seed"], "deterministic": True, "source": "MOCK_CASE",
                 "faults": faults}
    if clock:
        doc["clock"] = {"start": CLOCK_START}
    return Split(doc, unsupported)
