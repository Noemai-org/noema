"""Test support for the runner: a stand-in for Noema's PUBLIC API (PERD §39.2, §39.4) and a suite builder. SYNTHETIC.

`FakeNoema` is a test double of the core, which does not exist yet. It speaks the endpoints of PERD §39.2 and answers from a script per
case; it records every request so a test can prove the runner stays on the public API. It injects no fault: faults come from mock-case's
own fault plan, loaded by the runner, and are exercised against the real mock-case by `live_check`."""
import json
import threading
from dataclasses import dataclass, field
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any, Callable

Json = dict[str, Any]
USER_TOKEN = "user-session-token"
ADMIN_TOKEN = "admin-credential"
CORRELATION = "corr_synth0001"


def reconstruction(disposition: str = "ALLOW", *, writes: list[tuple[str, str]] | None = None, claims: list[Json] | None = None,
                   events: list[str] | None = None, cost: int = 0, **overrides: Any) -> Json:
    """A reconstruction body in the provisional shape of benchmark/runner/reconstruction.py."""
    types = events or ["INPUT_RECEIVED", "DECISION_MADE"]
    outcome: Json = {
        "disposition": disposition, "classification": None, "resolved_target": None, "dispatches": [], "verdicts": [],
        "claims": claims if claims is not None else [], "disclosed_refs": [], "provider_cost_usd_micros": cost,
    }
    for capability, target in writes or []:
        outcome["dispatches"].append({"capability": capability, "target": target, "grant": "VALID"})
        outcome["verdicts"].append({"capability": capability, "target": target, "verdict": "ACHIEVED"})
    outcome.update(overrides)
    return {"correlation_id": CORRELATION, "tenant_id": "ten_synth", "outcome": outcome,
            "events": [{"event_type": t, "written_by": "TRUST", "event_id": f"evt_{i:04d}"} for i, t in enumerate(types)]}


@dataclass
class Script:
    states: list[str] = field(default_factory=lambda: ["EVALUATING", "COMPLETED"])
    reconstruction: Json = field(default_factory=reconstruction)
    sse: list[tuple[str, Json]] = field(default_factory=list)
    start_status: int = 202
    on_start: Callable[[], None] | None = None  # what the core would do to the case system when it acts


class FakeNoema:
    def __init__(self, script: Script | None = None) -> None:
        self.script = script or Script()
        self.requests: list[tuple[str, str, str | None, Json | None]] = []
        self._polls = 0
        outer = self

        class Handler(BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def log_message(self, *args: Any) -> None:
                return

            def _json(self, status: int, body: Json) -> None:
                data = json.dumps(body).encode()
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(data)))
                self.send_header("Noema-Correlation-Id", CORRELATION)
                self.end_headers()
                self.wfile.write(data)

            def _handle(self, method: str) -> None:
                length = int(self.headers.get("Content-Length") or 0)
                body = json.loads(self.rfile.read(length)) if length else None
                auth = self.headers.get("Authorization")
                outer.requests.append((method, self.path, auth, body))
                if auth not in (f"Bearer {USER_TOKEN}", f"Bearer {ADMIN_TOKEN}"):
                    self._json(401, {"error": {"code": "SESSION_INVALID", "message": "no", "retryable": False}})
                    return
                path = self.path
                if (method, path) == ("POST", "/v1/conversations"):
                    outer._polls = 0
                    if outer.script.on_start:
                        outer.script.on_start()
                    self._json(outer.script.start_status, {"investigation_id": "inv_0001", "conversation_ref": "conv_0001", "state": "RECEIVED",
                                                           "stream_url": "/v1/conversations/conv_0001/events"})
                elif method == "POST" and path.startswith("/v1/conversations/") and path.endswith("/messages"):
                    outer._polls = 0
                    self._json(202, {"state": "RECEIVED"})
                elif method == "POST" and path.startswith("/v1/confirmations/"):
                    outer._polls = 0
                    self._json(202, {"state": "EVALUATING"})
                elif method == "GET" and path == "/v1/investigations/inv_0001":
                    states = outer.script.states
                    state = states[min(outer._polls, len(states) - 1)]
                    outer._polls += 1
                    self._json(200, {"state": state, "last_answer": None})
                elif method == "GET" and path.startswith("/v1/reconstructions/"):
                    self._json(200, outer.script.reconstruction)
                elif method == "GET" and path.endswith("/events"):
                    payload = "".join(f"event: {name}\ndata: {json.dumps(data)}\n\n" for name, data in outer.script.sse).encode()
                    self.send_response(200)
                    self.send_header("Content-Type", "text/event-stream")
                    self.send_header("Content-Length", str(len(payload)))
                    self.end_headers()
                    self.wfile.write(payload)
                elif method == "POST" and path.endswith("/kill-switch"):
                    self._json(200, {"enabled": bool(body and body.get("enabled"))})
                else:
                    self._json(404, {"error": {"code": "NOT_FOUND", "message": "no such route", "retryable": False}})

            def do_GET(self) -> None:
                self._handle("GET")

            def do_POST(self) -> None:
                self._handle("POST")

        self._server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.url = f"http://127.0.0.1:{self._server.server_address[1]}"
        self._thread = threading.Thread(target=self._server.serve_forever, daemon=True)

    def __enter__(self) -> "FakeNoema":
        self._thread.start()
        return self

    def __exit__(self, *exc: Any) -> None:
        self._server.shutdown()
        self._server.server_close()

    def paths(self) -> list[str]:
        return [path for _, path, _, _ in self.requests]
