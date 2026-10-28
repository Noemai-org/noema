"""The reconstruction body the runner reads (PERD §39.4 `GET /v1/reconstructions/{correlation_id}`): contracts/schemas/reconstruction.v3.json (its events are record-event.v3 events; DEC-2026-10-27-actor-and-payload-hash).

The PERD fixes the content and gives no field names; the names in the contract are the founders' (confirmed 21 Oct 2026). PROVISIONAL stays True until core serves a
real body (a test forces it to flip once one is recorded in benchmark/runner/recorded/). The body is validated against the contract first: an item with a float, a
missing record event ID or a basis other than the three is an error, never a quiet partial.

Mapping to the case result's `observed` block:
  disposition      <- the last DECISION item's `decision_outcome` (in Record order); with no DECISION item but a clarification asked, CLARIFY (the clarification
                      is the outcome, as in the contract's `clarification_only` fixture); with neither, an error
  classification   <- the last OBSERVED_STATE item's `classification`, else null
  resolved_target  <- the last RESOLUTION item's `resolved_target`, else null
  dispatches       <- ACTION items: `capability_id`, `grant_state`, and the target
  verdicts         <- VERIFICATION items: `capability_id`, `verdict`, and the target
  claims           <- EXPLANATION items: `text` and `bound_fact_status`
  disclosed_refs   <- the union of the EXPLANATION items' `disclosed_refs`
  audit_events     <- the distinct `event_type`s of `events`, first seen first
  clarification_asked <- an AMBIGUITY item's `clarification_asked`, or CLARIFICATION_REQUESTED in the Record
  escalation_opened   <- an ACHIEVED verdict on the escalation capability
The target of an ACTION or VERIFICATION item is its optional `target` fact when it has one, otherwise the resolved target of the case: the contract's action facts carry
no target, and every write the POC makes is on the one transaction the case resolved. If neither exists the item is an error. Provider cost is not in the Record and not in
this response: `cost_usd_micros` here is always None. Cost is read from the model service's bench cost files by `cost.py`, never from this body, and never estimated."""
from dataclasses import dataclass
from typing import Any

from benchmark.contracts import Contracts

Json = dict[str, Any]
PROVISIONAL = True
ESCALATION_CAPABILITY = "fin.cap.create_escalation_case"
_contracts = Contracts()


class ReconstructionError(Exception):
    pass


@dataclass(frozen=True)
class Parsed:
    observed: Json
    record_events: list[Json]
    cost_usd_micros: int | None  # always None: cost is not in the reconstruction (see cost.py)


def parse(body: Any) -> Parsed:
    if not isinstance(body, dict):
        raise ReconstructionError("the reconstruction is not an object")
    problems = _contracts.errors("reconstruction.v3", body)
    if problems:
        raise ReconstructionError(f"the reconstruction breaks its contract: {problems[0]}")
    if not body["chain"]["verified"]:
        raise ReconstructionError("the Record chain did not verify")
    events = body["events"]
    order = {e["event_id"]: i for i, e in enumerate(events)}
    section = {s["kind"]: sorted(s["items"], key=lambda item: order.get(item["record_event_id"], len(order))) for s in body["sections"]}
    types = list(dict.fromkeys(e["event_type"] for e in events))

    asked = "CLARIFICATION_REQUESTED" in types or any(i["facts"].get("clarification_asked") is True for i in section["AMBIGUITY"])
    if not section["DECISION"] and not asked:
        raise ReconstructionError("the reconstruction has no decision and no clarification")
    resolved = section["RESOLUTION"][-1]["facts"].get("resolved_target") if section["RESOLUTION"] else None

    def target_of(facts: Json) -> str:
        found = facts.get("target") if isinstance(facts.get("target"), str) else resolved
        if not isinstance(found, str):
            raise ReconstructionError("an action or verdict has no target: the item names none and the case resolved none")
        return found

    def need(facts: Json, key: str) -> Any:
        if key not in facts:
            raise ReconstructionError(f"an item lacks the fact {key}")
        return facts[key]

    dispatches = [{"capability": need(i["facts"], "capability_id"), "target": target_of(i["facts"]), "grant": i["facts"]["grant_state"]} for i in section["ACTION"]]
    verdicts = [{"capability": need(i["facts"], "capability_id"), "target": target_of(i["facts"]), "verdict": need(i["facts"], "verdict")} for i in section["VERIFICATION"]]
    refs = list(dict.fromkeys(ref for i in section["EXPLANATION"] for ref in i["facts"].get("disclosed_refs", [])))
    classification = section["OBSERVED_STATE"][-1]["facts"].get("classification") if section["OBSERVED_STATE"] else None
    observed: Json = {
        "disposition": section["DECISION"][-1]["facts"]["decision_outcome"] if section["DECISION"] else "CLARIFY",
        "classification": classification,
        "resolved_target": resolved,
        "clarification_asked": asked,
        "escalation_opened": any(v["capability"] == ESCALATION_CAPABILITY and v["verdict"] == "ACHIEVED" for v in verdicts),
        "dispatches": dispatches, "verdicts": verdicts, "audit_events": types,
        "claims": [{"text": i["facts"]["text"], "bound_fact_status": i["facts"]["bound_fact_status"]} for i in section["EXPLANATION"]],
        "disclosed_refs": refs,
    }
    return Parsed(observed, events, None)
