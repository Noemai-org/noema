"""The timeline event kinds the runner can drive, and when each is driven (the case schema: "the runner validates the kinds it can drive").

Driven here, through an interface that exists: `session_start` (a marker), `clock_advance:<ISO 8601 duration>` (mock-case's control API),
`confirm:ACCEPT|DECLINE` (PERD §39.2 `POST /v1/confirmations/{id}`), `kill_switch:on|off` (PERD §39.4 admin API) and, from A667, `session_resume:same_channel` (the
customer returns: the session provider is asked for the principal's token again and the later turns go to the same conversation with it) and the two webhook
kinds `webhook:settlement_updated` and `webhook:reversal_posted`, which are driven only when the environment has a webhook sender (`WebhookSender`, env.py). Every
other kind needs an interface that is not built or not in this runner's remit (process control, provider control, the policy publish API, any other webhook) and
makes the case an ERROR, listed by kind. A kind is never skipped, and a kind that sits at the start of the timeline, where only a kill switch and a clock advance
are driven, is an error too."""
import re
from typing import Any

Json = dict[str, Any]
_DURATION = re.compile(r"^P(T(\d+H)?(\d+M)?(\d+S)?|\d+D(T(\d+H)?(\d+M)?(\d+S)?)?)$")
_NOW = {"+00:00", "+00:00:00"}
WEBHOOKS = frozenset({"settlement_updated", "reversal_posted"})  # A667: the two webhook events the runner delivers; destination_credited, forged_signature and replayed are not driven
RESUMES = frozenset({"same_channel"})


def driven(event: str, *, webhooks: bool = False) -> bool:
    """Whether the runner can drive `event`. A webhook kind is driven only when `webhooks` says the environment has a sender for it."""
    kind, _, argument = event.partition(":")
    if kind == "session_start":
        return True
    if kind == "clock_advance":
        return bool(_DURATION.match(argument)) and argument not in ("PT", "P")
    if kind == "confirm":
        return argument in ("ACCEPT", "DECLINE")
    if kind == "kill_switch":
        return argument in ("on", "off")
    if kind == "session_resume":
        return argument in RESUMES
    if kind == "webhook":
        return webhooks and argument in WEBHOOKS
    return False


START_KINDS = frozenset({"session_start", "kill_switch", "clock_advance"})


def misplaced_at_start(case: Json) -> list[str]:
    """The kinds of events that sit at the start of the timeline where nothing but a kill switch and a clock advance is driven (the marker `session_start` aside). They are an error, never dropped (A667)."""
    return sorted({e["event"].partition(":")[0] for e in case["events_timeline"] if e["at"] in _NOW and e["event"].partition(":")[0] not in START_KINDS})


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
