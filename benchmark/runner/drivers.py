"""The timeline event kinds the runner can drive, and when each is driven (the case schema: "the runner validates the kinds it can drive").

Driven here, through an interface that exists: `session_start` (a marker), `clock_advance:<ISO 8601 duration>` (mock-case's control API),
`confirm:ACCEPT|DECLINE` (PERD §39.2 `POST /v1/confirmations/{id}`) and `kill_switch:on|off` (PERD §39.4 admin API). Every other kind needs an interface
that is not built or not in this runner's remit (webhook relay, process control, provider control, the policy publish and session APIs) and makes the
case an ERROR, listed by kind. A kind is never skipped."""
import re
from typing import Any

Json = dict[str, Any]
_DURATION = re.compile(r"^P(T(\d+H)?(\d+M)?(\d+S)?|\d+D(T(\d+H)?(\d+M)?(\d+S)?)?)$")
_NOW = {"+00:00", "+00:00:00"}


def driven(event: str) -> bool:
    kind, _, argument = event.partition(":")
    if kind == "session_start":
        return True
    if kind == "clock_advance":
        return bool(_DURATION.match(argument)) and argument not in ("PT", "P")
    if kind == "confirm":
        return argument in ("ACCEPT", "DECLINE")
    if kind == "kill_switch":
        return argument in ("on", "off")
    return False


def split_events(case: Json) -> tuple[list[tuple[str, str]], list[tuple[str, str]]]:
    """(before the first input, after it), as (kind, argument), in timeline order. `session_start` is a marker and is dropped. Offsets order the events;
    they are not waited for: the clock is simulated and advanced only by `clock_advance`."""
    before: list[tuple[str, str]] = []
    after: list[tuple[str, str]] = []
    for entry in case["events_timeline"]:
        kind, _, argument = entry["event"].partition(":")
        if kind == "session_start":
            continue
        (before if entry["at"] in _NOW else after).append((kind, argument))
    return before, after
