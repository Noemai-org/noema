"""A small client for the tool facade's two sides (synthetic/toolfacade, noema#361): the data side with a session token, the control side with the bench credential. Standard library only."""
import json
import urllib.error
import urllib.request
from typing import Any

Json = dict[str, Any]
TIMEOUT = 10.0


class FacadeError(RuntimeError):
    pass


class FacadeClient:
    def __init__(self, url: str, bench_token: str) -> None:
        self._url = url.rstrip("/")
        self._bench = bench_token

    def _send(self, method: str, path: str, token: str, body: Json | None = None) -> tuple[int, Json]:
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(self._url + path, data=data, method=method, headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
        try:
            with urllib.request.urlopen(req, timeout=TIMEOUT) as res:
                return res.status, json.loads(res.read() or b"{}")
        except urllib.error.HTTPError as err:
            try:
                return err.code, json.loads(err.read() or b"{}")
            except ValueError:
                return err.code, {}
        except (OSError, ValueError) as err:
            raise FacadeError(f"{method} {path}: {err}") from None

    def _control(self, method: str, path: str, body: Json | None = None) -> Json:
        status, doc = self._send(method, path, self._bench, body)
        if status != 200:
            raise FacadeError(f"{method} {path}: HTTP {status}")
        return doc

    def put_clock(self, now: str) -> None:
        self._control("PUT", "/control/v1/clock", {"now": now})

    def put_sessions(self, sessions: list[Json]) -> None:
        self._control("PUT", "/control/v1/sessions", {"sessions": sessions})

    def calls(self) -> list[Json]:
        doc = self._control("GET", "/control/v1/calls")
        calls = doc.get("calls")
        if not isinstance(calls, list):
            raise FacadeError("GET /control/v1/calls: no calls list")
        return calls

    def tools(self, token: str) -> list[str]:
        status, doc = self._send("GET", "/tools", token)
        if status != 200 or not isinstance(doc.get("tools"), list):
            raise FacadeError(f"GET /tools: HTTP {status}")
        return [t["name"] for t in doc["tools"]]

    def call(self, token: str, name: str, arguments: Json) -> int:
        status, _ = self._send("POST", "/tools/" + name, token, arguments)
        return status
