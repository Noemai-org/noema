"""The M-02 fold: the state of a payout at a point in time, folded from its transition log (PLAN-tushe-readonly-fold.md section 3.2; REQ-ENV-002, REQ-BEN-002).

Tushe's addendum says "state at a point in time" is a client-side fold of the transitions up to a timestamp. This function is that fold and nothing else: pure, no clock, no network, no
credential, and no claim about Tushe. The row shape is a PARAMETER: the default names are ASSUMED until a delta file adopts the addendum (AGENTS section 2, item 4), so adopting the real shape
changes one `Shape`, not the logic. Fail closed: only status OK yields a state; every other status is "the oracle could not say", never a pass (as `CounterUnavailable`).

Statuses: OK; NOT_YET_EXISTING (no transition at or before `at`: never an initial state); CHAIN_BROKEN (a gap, a branch, or two different rows with one id); ORDER_AMBIGUOUS (two transitions
at one instant with no sequence to order them, inside the prefix); UNREADABLE (a row or `at` that cannot be read; a row's time is unknown, so any unreadable row fails the whole fold).
Generated layer only; it reads no oracle and no held-out set (INV-22)."""
import datetime
from collections.abc import Mapping
from dataclasses import dataclass
from typing import Any

Json = dict[str, Any]


@dataclass(frozen=True)
class Shape:
    """The member names of a transition row. `seq` is None when the log gives no sequence."""
    id: str = "id"
    at: str = "occurred_at"
    from_state: str = "from_state"
    to_state: str = "to_state"
    seq: str | None = None
    assumed: bool = True


ASSUMED_SHAPE = Shape()


@dataclass(frozen=True)
class Fold:
    status: str
    state: str | None
    used: tuple[str, ...]
    collapsed: int = 0


def _instant(value: Any) -> datetime.datetime | None:
    """An RFC 3339 instant with a zone. A string without a zone is not an instant."""
    if not isinstance(value, str) or not value:
        return None
    try:
        parsed = datetime.datetime.fromisoformat(value.replace("Z", "+00:00") if value.endswith("Z") else value)
    except ValueError:
        return None
    return parsed if parsed.tzinfo is not None and parsed.utcoffset() is not None else None


def _text(value: Any) -> bool:
    return isinstance(value, str) and bool(value.strip())


def _parse(row: Any, shape: Shape) -> tuple[datetime.datetime, int, str, str, str] | None:
    if not isinstance(row, Mapping):
        return None
    when = _instant(row.get(shape.at))
    if when is None or not (_text(row.get(shape.id)) and _text(row.get(shape.from_state)) and _text(row.get(shape.to_state))):
        return None
    seq = 0
    if shape.seq is not None:
        raw = row.get(shape.seq)
        if type(raw) is not int:
            return None
        seq = raw
    return when, seq, str(row[shape.id]), str(row[shape.from_state]), str(row[shape.to_state])


def fold_transitions(rows: Any, at: Any, shape: Shape = ASSUMED_SHAPE) -> Fold:
    unreadable = Fold("UNREADABLE", None, ())
    moment = _instant(at)
    if moment is None or not isinstance(rows, (list, tuple)):
        return unreadable
    # Every row is read first: an unreadable row has an unknown time, so it fails the whole answer wherever it sits.
    read = []
    for row in rows:
        one = _parse(row, shape)
        if one is None:
            return unreadable
        read.append(one)
    by_id: dict[str, list[tuple[datetime.datetime, int, str, str, str]]] = {}
    for one in read:
        by_id.setdefault(one[2], []).append(one)
    parsed: dict[str, tuple[datetime.datetime, int, str, str, str]] = {}
    collapsed = 0
    for ident, found in by_id.items():
        distinct = set(found)
        if len(distinct) > 1:
            # One id, different rows. If every one of them is after the cutoff the id is simply not part of this answer (the prefix property, PLAN 3.2); if any is at or before it,
            # the row inside the prefix is the contested one and there is no state.
            if any(r[0] <= moment for r in distinct):
                return Fold("CHAIN_BROKEN", None, ())
            continue
        parsed[ident] = found[0]
        collapsed += len(found) - 1
    prefix = sorted((p for p in parsed.values() if p[0] <= moment), key=lambda p: (p[0], p[1]))
    if not prefix:
        return Fold("NOT_YET_EXISTING", None, (), collapsed)
    for earlier, later in zip(prefix, prefix[1:]):
        if (earlier[0], earlier[1]) == (later[0], later[1]):
            return Fold("ORDER_AMBIGUOUS", None, (), collapsed)
    for earlier, later in zip(prefix, prefix[1:]):
        if later[3] != earlier[4]:
            return Fold("CHAIN_BROKEN", None, (), collapsed)
    return Fold("OK", prefix[-1][4], tuple(p[2] for p in prefix), collapsed)
