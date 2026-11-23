"""The runner's own per-request record, `requests.jsonl` (A444, rulings of 20 Nov; noema#730 derives the expected RECORD_ACCESSED count from it).

One JSON object per line with exactly `case_id`, `correlation_id`, `method`, `path`, `status` (an integer) and `kind`: "case" for the calls that drive a case, "reconstruction" for the reads of
the reconstruction route (the audit read that writes a RECORD_ACCESSED line) and "probe" for the evidence probes' refusal checks (written by `benchmark.runner.evidence`). No header, no body and no token
is ever recorded, and `path` is the route TEMPLATE (`/v1/reconstructions/{correlation_id}`), never a concrete identifier: a template carries no digit run for the leak scan to flag, and a concrete path is
refused here so none can reach the file.

A request belongs to the case in progress (`begin`) and takes the case's correlation id when the case ends (`end`): the first request of a case is sent before the core has issued one. A case that
never got a correlation id records nothing (an empty correlation is never written), and a request made outside a case is not recorded. SYNTHETIC."""
import json
from pathlib import Path
from typing import Any

Json = dict[str, Any]
FIELDS = ("case_id", "correlation_id", "method", "path", "status", "kind")
RECONSTRUCTION_ROUTE = "/v1/reconstructions/{correlation_id}"


class RequestRecorder:
    def __init__(self) -> None:
        self._open: tuple[str, list[Json]] | None = None
        self._done: list[Json] = []

    def begin(self, case_id: str) -> None:
        self._open = (case_id, [])

    def record(self, method: str, path: str, status: int) -> None:
        if not isinstance(method, str) or not method or not method.isupper():
            raise ValueError("the method must be an upper-case word")
        if type(status) is not int:
            raise ValueError("the status must be an integer")
        if not path.startswith("/v1/") or "?" in path or any(part and not (part.startswith("{") and part.endswith("}")) and "_" in part and any(c.isdigit() for c in part) for part in path.split("/")):
            raise ValueError("the path must be a route template (/v1/..., identifiers as {name}), never a concrete path")
        if self._open is not None:
            self._open[1].append({"method": method, "path": path, "status": status, "kind": "reconstruction" if path == RECONSTRUCTION_ROUTE else "case"})

    def end(self, case_id: str, correlation_id: str | None) -> None:
        if self._open is None or self._open[0] != case_id:
            return
        pending = self._open[1]
        self._open = None
        if not isinstance(correlation_id, str) or not correlation_id:
            return
        self._done.extend({"case_id": case_id, "correlation_id": correlation_id, **entry} for entry in pending)

    def append(self, path: Path) -> None:
        """Add this recorder's finished entries to an existing record, keeping every line already in it."""
        if not path.is_file():
            raise ValueError(f"{path.name} does not exist: the runner did not write its own request record")
        path.write_bytes(path.read_bytes() + "".join(json.dumps({k: line[k] for k in FIELDS}, sort_keys=True) + "\n" for line in self._done).encode("utf-8"))

    def write(self, path: Path) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes("".join(json.dumps({k: line[k] for k in FIELDS}, sort_keys=True) + "\n" for line in self._done).encode("utf-8"))
