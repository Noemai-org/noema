"""The AT-19 trace join: one payout transition, joined to its event by the trace identifier the two calls share, and checked for the five audit elements (PLAN-tushe-readonly-fold.md
section 3.3; REQ-ENV-002, REQ-ENV-007). AT-19 as the frozen specification words it: "Audit completeness. Inspect a material transition. Actor / request / correlation / state / timestamp
metadata all present."

Pure: no network, no clock, no credential, no claim about Tushe. The member names are a PARAMETER (ASSUMED until a delta file adopts the addendum, AGENTS section 2, item 4). The state and the
timestamp come from the transition row; the actor, the request and the correlation from the event. COMPLETE only if exactly one distinct event matches and all five are present and non-empty.
NO_EVENT, AMBIGUOUS_JOIN, INCOMPLETE (the missing elements are named) and UNREADABLE are never a pass. A duplicate delivery of one message (same message identity, same content, AT-06) is
collapsed first and the collapse is reported, never hidden. Generated layer only (INV-22)."""
from collections.abc import Mapping
from dataclasses import dataclass
from typing import Any

Json = dict[str, Any]


@dataclass(frozen=True)
class TraceShape:
    transition_key: str = "trace_id"
    event_key: str = "trace_id"
    message_id: str = "message_id"
    actor: str = "actor"
    request: str = "request"
    correlation: str = "correlation"
    state: str = "to_state"
    timestamp: str = "occurred_at"
    assumed: bool = True


ASSUMED_SHAPE = TraceShape()


@dataclass(frozen=True)
class Join:
    status: str
    missing: tuple[str, ...] = ()
    collapsed: int = 0


def _present(value: Any) -> bool:
    if value is None:
        return False
    if isinstance(value, str):
        return bool(value.strip())
    if isinstance(value, (list, tuple, dict, set)):
        return bool(value)
    return True


def join_trace(transition: Any, events: Any, shape: TraceShape = ASSUMED_SHAPE) -> Join:
    if not isinstance(transition, Mapping) or not isinstance(events, (list, tuple)) or not all(isinstance(e, Mapping) for e in events):
        return Join("UNREADABLE")
    key = transition.get(shape.transition_key)
    if not isinstance(key, str) or not key.strip():
        return Join("INCOMPLETE", (shape.transition_key,))
    matches = [e for e in events if e.get(shape.event_key) == key]
    if not matches:
        return Join("NO_EVENT")
    distinct: list[Mapping[str, Any]] = []
    collapsed = 0
    for event in matches:
        mid = event.get(shape.message_id)
        twin = next((d for d in distinct if _present(mid) and d.get(shape.message_id) == mid), None)
        if twin is None:
            distinct.append(event)
        elif dict(twin) == dict(event):
            collapsed += 1
        else:
            return Join("AMBIGUOUS_JOIN", (), collapsed)  # one message identity, two contents
    if len(distinct) > 1:
        return Join("AMBIGUOUS_JOIN", (), collapsed)
    event = distinct[0]
    elements = (("actor", event, shape.actor), ("request", event, shape.request), ("correlation", event, shape.correlation),
                ("state", transition, shape.state), ("timestamp", transition, shape.timestamp))
    missing = tuple(member for _, source, member in elements if not _present(source.get(member)))
    return Join("INCOMPLETE", missing, collapsed) if missing else Join("COMPLETE", (), collapsed)
