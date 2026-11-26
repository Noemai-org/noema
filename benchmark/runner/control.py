"""mock-case's control API (bench credential, bench_net only): reset between cases, load a fault plan, advance the simulated clock."""
from typing import Any

from benchmark.oracle_client.transport import HttpError, JsonClient

Json = dict[str, Any]


class PlanRejected(Exception):
    """mock-case refused the plan when loading it (422 INVALID_FAULT_PLAN): it accepts only a plan it can apply exactly."""


class MockCaseControl:
    def __init__(self, base_url: str, credential: str, *, timeout: float = 10.0) -> None:
        self._http = JsonClient(base_url, token=credential, timeout=timeout)

    def reset(self) -> None:
        self._http.request("POST", "/control/v1/reset", {})

    def load_plan(self, plan: Json) -> None:
        try:
            self._http.request("PUT", "/control/v1/fault-plan", plan)
        except HttpError as error:
            if error.status == 422:
                raise PlanRejected(error.message) from None
            raise

    def clear_plan(self) -> None:
        self._http.request("DELETE", "/control/v1/fault-plan")

    def advance(self, by: str) -> Json:
        return self._http.request("POST", "/control/v1/clock/advance", {"by": by})[0]

    def clock(self) -> Json:
        return self._http.request("GET", "/control/v1/clock")[0]

    def deliver_webhook(self, message_id: str, payload: Json) -> Json:
        """mock-case signs `payload` with its own SYNTHETIC key and posts it to the configured relay (A682). The answer carries the relay's status and `accepted`."""
        return self._http.request("POST", "/control/v1/webhooks/deliver", {"message_id": message_id, "payload": payload})[0]

    def replay_webhook(self, message_id: str) -> Json:
        """The same id and body again, with a fresh timestamp and signature (A682)."""
        return self._http.request("POST", f"/control/v1/webhooks/{message_id}/replay", {})[0]
