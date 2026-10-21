"""The public API client (PERD §39.2, plus the §39.4 reconstruction read and kill switch): what a partner app calls, and nothing else (REQ-BEN-002 step 3)."""
import json
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Any, Callable

from benchmark.oracle_client.transport import JsonClient, TransportError

Json = dict[str, Any]
# PERD §28.2 states that move on their own. Any other state is at rest: waiting for the customer, the outside world, or an end.
IN_FLIGHT = frozenset({"RECEIVED", "INTERPRETING", "EVALUATING", "PLANNED", "EXECUTING", "RECOVERING", "ESCALATING", "EXPLAINING"})


class NotSettled(Exception):
    pass


@dataclass(frozen=True)
class Conversation:
    investigation_id: str
    conversation_ref: str
    state: str
    correlation_id: str


class PublicApi:
    def __init__(self, base_url: str, *, timeout: float = 10.0) -> None:
        self.base_url, self.timeout = base_url.rstrip("/"), timeout

    def _client(self, token: str) -> JsonClient:
        return JsonClient(self.base_url, token=token, timeout=self.timeout)

    def start(self, token: str, request_id: str, text: str, locale_hint: str | None = None) -> Conversation:
        body: Json = {"request_id": request_id, "text": text}
        if locale_hint:
            body["locale_hint"] = locale_hint
        data, headers = self._client(token).request("POST", "/v1/conversations", body)
        correlation = {k.lower(): v for k, v in headers.items()}.get("noema-correlation-id", "")
        return Conversation(data["investigation_id"], data["conversation_ref"], data["state"], correlation)

    def message(self, token: str, conversation_ref: str, request_id: str, *, text: str | None = None, option_id: str | None = None,
                clarification_id: str | None = None) -> str:
        body: Json = {"request_id": request_id}
        for key, value in (("text", text), ("option_id", option_id), ("clarification_id", clarification_id)):
            if value is not None:
                body[key] = value
        return str(self._client(token).request("POST", f"/v1/conversations/{conversation_ref}/messages", body)[0]["state"])

    def confirm(self, token: str, confirmation_request_id: str, request_id: str, response: str) -> str:
        body = {"request_id": request_id, "response": response}
        return str(self._client(token).request("POST", f"/v1/confirmations/{confirmation_request_id}", body)[0]["state"])

    def investigation(self, token: str, investigation_id: str) -> Json:
        return self._client(token).request("GET", f"/v1/investigations/{investigation_id}")[0]

    def wait_settled(self, token: str, investigation_id: str, *, timeout_s: float, poll_s: float, sleep: Callable[[float], None] = time.sleep,
                     now: Callable[[], float] = time.monotonic) -> str:
        """Poll the investigation until its state is at rest. A wait that outlasts `timeout_s` is NotSettled, never a guess."""
        started = now()
        while True:
            state = str(self.investigation(token, investigation_id)["state"])
            if state not in IN_FLIGHT:
                return state
            if now() - started >= timeout_s:
                raise NotSettled(f"still {state} after {timeout_s:g}s")
            sleep(poll_s)

    def events(self, token: str, conversation_ref: str, *, timeout_s: float) -> list[tuple[str, Json]]:
        """Read the server-sent events of a conversation until the stream ends or `timeout_s` passes. Returns (event name, data)."""
        request = urllib.request.Request(f"{self.base_url}/v1/conversations/{conversation_ref}/events", headers={"Accept": "text/event-stream",
                                                                                                                  "Authorization": f"Bearer {token}"})
        found: list[tuple[str, Json]] = []
        name, data = "message", []  # type: str, list[str]
        try:
            with urllib.request.urlopen(request, timeout=timeout_s) as response:
                for raw in response:
                    line = raw.decode("utf-8").rstrip("\r\n")
                    if line.startswith("event:"):
                        name = line[6:].strip()
                    elif line.startswith("data:"):
                        data.append(line[5:].strip())
                    elif not line and data:
                        found.append((name, json.loads("\n".join(data))))
                        name, data = "message", []
        except (urllib.error.URLError, TimeoutError, ConnectionError, OSError) as error:
            if not found:
                raise TransportError(f"GET events: {type(error).__name__}") from None
        return found

    @staticmethod
    def confirmation_id(events: list[tuple[str, Json]]) -> str | None:
        for name, data in events:
            if name == "confirmation" and isinstance(data.get("confirmation_request_id"), str):
                return str(data["confirmation_request_id"])
        return None

    def reconstruction(self, token: str, correlation_id: str) -> Json:
        return self._client(token).request("GET", f"/v1/reconstructions/{correlation_id}")[0]

    def kill_switch(self, admin_token: str, tenant_id: str, enabled: bool, reason: str) -> None:
        self._client(admin_token).request("POST", f"/v1/admin/tenants/{tenant_id}/kill-switch", {"enabled": enabled, "reason": reason})
