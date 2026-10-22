"""What this build can run of a suite, and what stops the rest. A blocked case is an ERROR when run, never skipped; this report says so before a run starts."""
from typing import Any

from .drivers import driven
from .faultplan import split_plan
from .suite import LoadedCase, Suite

Json = dict[str, Any]


def reasons_for(lc: LoadedCase) -> set[str]:
    reasons = {f"event: {e['event'] if e['event'].startswith('needs_fault:') else e['event'].split(':')[0]}" for e in lc.case["events_timeline"] if not driven(e["event"])}
    for text in split_plan(lc.case, lc.plan).unsupported:
        target_operation, _, _ = text.partition(":")
        reasons.add(f"fault: {target_operation.split('.')[0]} {target_operation.split(' ')[-1]}")
    return reasons


def report(suite: Suite) -> Json:
    """`runnable` and `blocked` partition the cases. `blocked_by` counts cases per reason, so a case with two reasons is in two entries."""
    per_case = [reasons_for(lc) for lc in suite.cases]
    by_reason: dict[str, int] = {}
    for reasons in per_case:
        for reason in reasons:
            by_reason[reason] = by_reason.get(reason, 0) + 1
    bare = sum(1 for lc in suite.cases if lc.plan is None and all(e["event"] == "session_start:synthetic" for e in lc.case["events_timeline"]))
    return {"total": len(suite.cases), "runnable": sum(1 for r in per_case if not r), "blocked": sum(1 for r in per_case if r), "without_faults_or_events": bare,
            "blocked_by": dict(sorted(by_reason.items(), key=lambda kv: (-kv[1], kv[0])))}
