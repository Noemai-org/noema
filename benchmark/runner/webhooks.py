"""The runner's WebhookSender (A667, A682): mock-case's bench control routes deliver a signed webhook to the relay. SYNTHETIC.

The runner invents no event body. Tushe's event names and body members are not stated in any governing document (status/ENV-05.md, "Not stated", item 1), so the object for each kind comes from
a file the caller gives (`--webhook-payloads`). The message id is `<run id>:<case id>:<kind>` (A758), so the same run, case and kind give the same id and the relay stores a repeat once, and another run never meets an earlier run's id with a different body."""
import json
import re
from pathlib import Path
from typing import Any

from .control import MockCaseControl
from .drivers import WEBHOOKS

Json = dict[str, Any]


_ID_PART = re.compile(r"[A-Za-z0-9_.-]+")  # mock-case accepts `[A-Za-z0-9_.:-]{1,128}` for a whole id; the colon is the separator here, so no part holds one
MAX_ID = 128


class WebhookNotDelivered(Exception):
    """The delivery did not happen, or the relay did not accept it. The runner turns this into the case's ERROR."""


def read_payloads(path: Path) -> dict[str, Json]:
    """A JSON object of kind to object, for the kinds the runner drives only. An unknown kind, an empty object or a non-object is refused, never guessed."""
    data = json.loads(Path(path).read_text(encoding="utf-8"))
    if not isinstance(data, dict) or not data:
        raise ValueError("the webhook payload file must be a non-empty JSON object of kind to object")
    for kind, body in data.items():
        if kind not in WEBHOOKS:
            raise ValueError(f"the webhook payload file names a kind the runner does not drive: {kind}")
        if not isinstance(body, dict) or not body:
            raise ValueError(f"the payload for {kind} must be a non-empty JSON object")
    return data


class MockCaseWebhooks:
    def __init__(self, control: MockCaseControl, payloads: dict[str, Json], run_id: str) -> None:
        if _ID_PART.fullmatch(run_id) is None:
            raise ValueError("the run id must be letters, digits, underscore, dot or hyphen, for a webhook message id")
        self._control, self._payloads, self._run_id = control, payloads, run_id

    def message_id(self, case_id: str, event: str) -> str:
        parts = (self._run_id, case_id, event)
        if not all(_ID_PART.fullmatch(part) for part in parts) or len(":".join(parts)) > MAX_ID:
            raise WebhookNotDelivered("the run id, case id and kind do not make a valid webhook message id")
        return ":".join(parts)

    def _accepted(self, answer: Json) -> None:
        if answer.get("accepted") is not True:
            raise WebhookNotDelivered(f"the relay did not accept the delivery (status {answer.get('relay_status')})")

    def deliver(self, case_id: str, event: str) -> None:
        payload = self._payloads.get(event)
        if payload is None:
            raise WebhookNotDelivered(f"no payload was given for {event}")
        self._accepted(self._control.deliver_webhook(self.message_id(case_id, event), payload))

    def replay(self, case_id: str, event: str) -> None:
        self._accepted(self._control.replay_webhook(self.message_id(case_id, event)))
