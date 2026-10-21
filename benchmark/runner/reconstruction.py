"""The reconstruction body the runner reads (PERD §39.4 `GET /v1/reconstructions/{correlation_id}`).

PROVISIONAL. The PERD says the endpoint returns structured JSON built only from Record events and gives no field names, and `/contracts` has the
Record event but no reconstruction. These are the smallest fields the case result needs (AGENT-02):

    {"correlation_id", "tenant_id",
     "events":  [Record events (record-event.v1)],
     "outcome": {"disposition", "classification", "resolved_target", "dispatches": [...], "verdicts": [...], "claims": [...], "disclosed_refs": [...],
                 "provider_cost_usd_micros"}}

`audit_events`, `clarification_asked` and `escalation_opened` are derived from the Record, never asserted by the body. An unexpected shape is a
ReconstructionError and the case an ERROR. This is the one module that changes when a recorded body exists (benchmark/runner/recorded/)."""
from dataclasses import dataclass
from typing import Any

Json = dict[str, Any]
PROVISIONAL = True
ESCALATION_CAPABILITY = "fin.cap.create_escalation_case"
_OUTCOME_KEYS = ("disposition", "classification", "resolved_target", "dispatches", "verdicts", "claims", "disclosed_refs", "provider_cost_usd_micros")


class ReconstructionError(Exception):
    pass


@dataclass(frozen=True)
class Parsed:
    observed: Json
    record_events: list[Json]
    cost_usd_micros: int


def parse(body: Any) -> Parsed:
    if not isinstance(body, dict) or not isinstance(body.get("events"), list) or not isinstance(body.get("outcome"), dict):
        raise ReconstructionError("the reconstruction has no events and outcome")
    outcome = body["outcome"]
    missing = [k for k in _OUTCOME_KEYS if k not in outcome]
    if missing:
        raise ReconstructionError(f"the reconstruction outcome lacks {missing}")
    events = body["events"]
    if not all(isinstance(e, dict) and isinstance(e.get("event_type"), str) for e in events):
        raise ReconstructionError("a reconstruction event has no event_type")
    cost = outcome["provider_cost_usd_micros"]
    if isinstance(cost, bool) or not isinstance(cost, int) or cost < 0:
        raise ReconstructionError("provider_cost_usd_micros is not a non-negative integer")
    types = list(dict.fromkeys(e["event_type"] for e in events))
    observed: Json = {key: outcome[key] for key in _OUTCOME_KEYS if key != "provider_cost_usd_micros"}
    observed["audit_events"] = types
    observed["clarification_asked"] = "CLARIFICATION_REQUESTED" in types
    observed["escalation_opened"] = any(v.get("capability") == ESCALATION_CAPABILITY and v.get("verdict") == "ACHIEVED" for v in outcome["verdicts"])
    return Parsed(observed, events, cost)
