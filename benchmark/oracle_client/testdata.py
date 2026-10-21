"""Test support: a stand-in for mock-case's bench listener, serving the shapes of synthetic/mock_case/internal/api/openapi-bench.json
and the store types (`internal/store/truth.go`, `types.go`). SYNTHETIC. A test double for the oracle's HTTP surface only; no fault is mocked."""
import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any, Callable

Json = dict[str, Any]
BENCH_TOKEN = "test-bench-credential"
TX = "fin.transaction:TX1001"


def case(case_id: str = "cas_1", tx: str = TX, version: str = "v1", key: str = "key-1") -> Json:
    return {"case_id": case_id, "reference": f"CASE-{case_id}", "status": "OPEN", "owner_queue": "PAYMENTS_OPERATIONS", "priority": "NORMAL",
            "idempotency_key": key, "version_token": version, "created_at": "2026-10-01T09:40:00Z", "tenant_id": "ten_synth", "principal_id": "usr_synth001",
            "transaction_ref": tx, "reason_code": "OUTCOME_UNKNOWN"}


def attachment(attachment_id: str = "att_1", case_id: str = "cas_1") -> Json:
    return {"attachment_id": attachment_id, "case_id": case_id, "payload_hash": "sha256:" + "ab" * 32, "idempotency_key": f"ev-{attachment_id}",
            "attached_at": "2026-10-01T09:41:00Z", "tenant_id": "ten_synth", "payload": "{}"}


def complaint(ref: str = "cmp_1", case_id: str = "cas_1") -> Json:
    return {"complaint_ref": ref, "case_id": case_id, "idempotency_key": f"cmp-{ref}", "logged_at": "2026-10-01T09:42:00Z",
            "clock_started_at": "2026-10-01T09:42:00Z", "tenant_id": "ten_synth"}


def command(seq: int, operation: str, case_id: str | None = None, *, committed: bool = True, replay: bool = False, status: int = 201) -> Json:
    return {"seq": seq, "at": "2026-10-01T09:40:00Z", "scenario_id": "unscoped", "seed": 0, "fault_plan_id": "fp_none", "operation": operation, "call_no": seq,
            "tenant_id": "ten_synth", "case_id": case_id, "idempotency_key": None, "correlation_id": None, "status_code": status, "error_code": None,
            "committed": committed, "replay": replay, "fault_index": None, "fault_effect": None, "fault_params": None, "response_suppressed": False}


class StubBench:
    """Serves the bench listener from a mutable `state`; records every request as (method, path, authorization)."""

    def __init__(self) -> None:
        self.state: Json = {"clock_source": "SYNTHETIC", "cases": [], "complaints": [], "evidence": [], "counters": {}}
        self.commands: list[Json] = []
        self.requests: list[tuple[str, str, str | None]] = []
        self.plan: Json | None = None
        self.clock_now = "2026-10-01T09:40:00Z"
        self.advanced: list[Json] = []
        self.resets = 0
        self.plan_error: Json | None = None
        outer = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args: Any) -> None:
                return

            def _send(self, status: int, body: Any) -> None:
                data = json.dumps(body).encode()
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

            def _handle(self, method: str) -> None:
                path = self.path.split("?", 1)[0]
                query = self.path.split("?", 1)[1] if "?" in self.path else ""
                auth = self.headers.get("Authorization")
                outer.requests.append((method, path, auth))
                if auth != f"Bearer {BENCH_TOKEN}":
                    self._send(403 if auth else 401, {"error": {"code": "FORBIDDEN" if auth else "UNAUTHENTICATED", "message": "no", "retryable": False}})
                    return
                length = int(self.headers.get("Content-Length") or 0)
                body = json.loads(self.rfile.read(length)) if length else None
                if (method, path) == ("GET", "/truth/v1/state"):
                    self._send(200, outer.state)
                elif (method, path) == ("GET", "/truth/v1/commands"):
                    params = dict(p.split("=", 1) for p in query.split("&") if p)
                    after, limit = int(params.get("after_seq", 0)), int(params.get("limit", 200))
                    rows = [c for c in outer.commands if c["seq"] > after][:limit]
                    self._send(200, {"clock_source": "SYNTHETIC", "commands": rows, "complete": len(rows) < limit})
                elif (method, path) == ("GET", "/truth/v1/digest"):
                    self._send(200, {"clock_source": "SYNTHETIC", "clock_simulated": True, "state_hash": "sha256:" + "11" * 32,
                                     "fault_trace_hash": "sha256:" + "22" * 32, "fault_trace_length": 0})
                elif (method, path) == ("PUT", "/control/v1/fault-plan"):
                    if outer.plan_error:
                        self._send(422, outer.plan_error)
                    else:
                        outer.plan = body
                        self._send(200, {"fault_plan_id": body.get("fault_plan_id"), "loaded": True, "plan": body})
                elif (method, path) == ("DELETE", "/control/v1/fault-plan"):
                    outer.plan = None
                    self._send(200, {"fault_plan_id": "fp_none", "loaded": False})
                elif (method, path) == ("GET", "/control/v1/clock"):
                    self._send(200, {"clock_source": "SYNTHETIC", "simulated": True, "now": outer.clock_now})
                elif (method, path) == ("POST", "/control/v1/clock/advance"):
                    outer.advanced.append(body)
                    self._send(200, {"clock_source": "SYNTHETIC", "simulated": True, "now": outer.clock_now})
                elif (method, path) == ("POST", "/control/v1/reset"):
                    outer.resets += 1
                    outer.state = {"clock_source": "SYNTHETIC", "cases": [], "complaints": [], "evidence": [], "counters": {}}
                    outer.commands = []
                    outer.plan = None
                    self._send(200, {"status": "reset", "clock_source": "SYNTHETIC"})
                else:
                    self._send(404, {"error": {"code": "NOT_FOUND", "message": "no such route", "retryable": False}})

            def do_GET(self) -> None:
                self._handle("GET")

            def do_PUT(self) -> None:
                self._handle("PUT")

            def do_POST(self) -> None:
                self._handle("POST")

            def do_DELETE(self) -> None:
                self._handle("DELETE")

        self._server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.url = f"http://127.0.0.1:{self._server.server_address[1]}"
        self._thread = threading.Thread(target=self._server.serve_forever, daemon=True)

    def __enter__(self) -> "StubBench":
        self._thread.start()
        return self

    def __exit__(self, *exc: Any) -> None:
        self._server.shutdown()
        self._server.server_close()


def refusal_line(identity: str = "runtime", listener: str = "bench", path: str = "/truth/v1/state", status: int = 403) -> str:
    return json.dumps({"time": "2026-10-01T09:40:00Z", "level": "WARN", "msg": "refused", "listener": listener, "identity": identity, "method": "GET",
                       "path": path, "status": status})


__all__ = ["StubBench", "BENCH_TOKEN", "TX", "case", "attachment", "complaint", "command", "refusal_line", "Callable"]
