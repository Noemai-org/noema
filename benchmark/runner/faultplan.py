"""What the environment can apply of a case's fault plan (REQ-ENV-004).

A case's plan is a fault-plan.v1 document (the generator writes it and `load_suite` validates it). The faults aimed at `mock-case` go to mock-case as a
`MOCK_CASE` plan: the contract already guarantees they are effects mock-case produces, with its operation and parameter names, and mock-case still refuses
at load a plan it cannot apply exactly. Every other fault needs a producer this build cannot reach (Tushe's fault controller, the hostility proxy) and is
listed as UNSUPPORTED: the case is an ERROR (REQ-BEN-001) rather than a run that quietly injected less than it claims. Every plan this module builds is
loaded into the real mock-case by `benchmark.oracle_client.live_check`."""
from dataclasses import dataclass, field
from typing import Any

Json = dict[str, Any]
CLOCK_START = "2026-10-01T09:40:00+01:00"  # the start of the PERD §37.1 example, SYNTHETIC; every case's simulated clock starts here
MOCK_CASE = "mock-case"


@dataclass
class Split:
    mock_case: Json | None
    unsupported: list[str] = field(default_factory=list)


def needs_clock(case: Json) -> bool:
    return any(e["event"].startswith("clock_advance:") for e in case["events_timeline"])


def split_plan(case: Json, plan: Json | None) -> Split:
    unsupported: list[str] = []
    faults: list[Json] = []
    for fault in (plan or {}).get("faults", []):
        if fault["target"] == MOCK_CASE:
            faults.append(fault)
        else:
            unsupported.append(f"{fault['target']}.{fault['operation']} {fault['effect']}: no access to {fault['target']} from this build")
    clock = needs_clock(case)
    if not faults and not clock:
        return Split(None, unsupported)
    doc: Json = {"fault_plan_id": (plan or {}).get("fault_plan_id") or "fp_" + case["case_id"].lower().replace("-", "_"), "scenario_id": case["case_id"],
                 "seed": case["generator"]["seed"], "deterministic": True, "source": "MOCK_CASE", "faults": faults}
    if clock:
        doc["clock"] = {"start": CLOCK_START}
    return Split(doc, unsupported)
