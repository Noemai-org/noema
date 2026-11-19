"""The recorder of the compile acceptance run (UND-16; the source of the replay provider's fixtures, UND-15, A261).

One file per provider call: `recordings/rec-NNNN.json`, with the provider, the model pin, the run id, the date, the SHA-256 of the exact request bytes (the key the replay provider looks up), both bodies parsed
so a line of the file is one value, and the cost. The host, the query and every header are never written (the credential travels in a header, INV-29). The files are pretty-printed with sorted keys, so the
leak scan reads one value per line. All data is SYNTHETIC (AGENT-25)."""
import hashlib
import json
import re
from collections.abc import Callable
from datetime import datetime
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

# A359: the failure class of every call, a closed vocabulary. The first five are the model service's `ProviderFailure` names (services/model/noema_model/providers/llm/base.py; a test compares them when
# that package is importable); OK is a 2xx reply that is a JSON object; NO_STATUS is a call with no HTTP status and no failure label the transport named.
FAILURE_CLASSES = ("OK", "REJECTED", "RATE_LIMITED", "TIMEOUT", "UNAVAILABLE", "MALFORMED", "NO_STATUS")
_TRANSPORT_LABELS = frozenset({"REJECTED", "RATE_LIMITED", "TIMEOUT", "UNAVAILABLE", "MALFORMED"})
# The only three members read from a provider's error object (A359), each only when it is a short plain identifier. The pattern is the safest reading of "a param such as text.format.schema survives":
# ASCII letters, digits, underscore, dot and square brackets, starting with a letter, 64 characters at most: no space, quote, newline, hyphen (so no `sk-` key) or slash. Then three more refusals:
# five or more digits anywhere (the leak rule, REQ-SEC-003), a run of more than 32 letters or digits (a token, not a name) and a key prefix.
_ERROR_FIELDS = ("type", "code", "param")
_SAFE_IDENTIFIER = re.compile(r"[A-Za-z][A-Za-z0-9_.\[\]]{0,63}")
_KEY_SHAPE = re.compile(r"(?<![A-Za-z0-9])(?:sk|pk|rk)_|^(?:akia|asia|gh[pousr]_|xox|aiza|eyj)", re.IGNORECASE)
_MAX_ERROR_BODY_BYTES = 1 << 20
_MAX_DIGITS, _MAX_RUN = 4, 32
LABEL = "Understanding: compile acceptance on synthetic golden examples, recorded live output (SYNTHETIC)"


def _body(data: bytes | None) -> Any:
    if data is None:
        return None
    try:
        return json.loads(data)
    except (ValueError, RecursionError):
        return {"text": data.decode("utf-8", errors="replace")}


def _is_json_object(data: bytes | None) -> bool:
    try:
        return data is not None and isinstance(json.loads(data), dict)
    except (ValueError, RecursionError):
        return False


def _identifier(value: Any) -> str | None:
    """`value` when it is a short plain identifier, else None. Never raises."""
    if not isinstance(value, str) or _SAFE_IDENTIFIER.fullmatch(value) is None:
        return None
    if len(re.findall(r"[0-9]", value)) > _MAX_DIGITS or _KEY_SHAPE.search(value) is not None:
        return None
    if any(len(run) > _MAX_RUN for run in re.findall(r"[A-Za-z0-9]+", value)):
        return None
    return value


def error_triple(reply: bytes | None) -> dict[str, str | None]:
    """`type`, `code` and `param` of the error object of a provider's reply, each validated, else None. Nothing else of the reply is read, kept or returned (A359; SR's condition, A320).
    A reply that is missing, huge, not JSON, not an object or without an error object gives three Nones: it never raises."""
    nothing: dict[str, str | None] = dict.fromkeys(_ERROR_FIELDS)
    if reply is None or len(reply) > _MAX_ERROR_BODY_BYTES:
        return nothing
    try:
        parsed = json.loads(reply)
    except (ValueError, RecursionError):
        return nothing
    err = parsed.get("error") if isinstance(parsed, dict) else None
    if not isinstance(err, dict):
        return nothing
    return {k: _identifier(err.get(k)) for k in _ERROR_FIELDS}


def failure_class(status: int | None, failure: str | None, reply: bytes | None) -> str:
    """The class of a call, from what the recorder has: the label the transport named, else the status (the same mapping as the provider's `failure_for_status`)."""
    if failure in _TRANSPORT_LABELS:
        return str(failure)
    if status is None:
        return "NO_STATUS"
    if 200 <= status < 300:
        return "OK" if _is_json_object(reply) else "MALFORMED"
    if status == 408:
        return "TIMEOUT"
    if status == 429:
        return "RATE_LIMITED"
    return "UNAVAILABLE" if status >= 500 else "REJECTED"


def dump(value: Any) -> str:
    return json.dumps(value, indent=2, sort_keys=True) + "\n"


class Recorder:
    def __init__(self, out: Path, run_id: str, provider: str, model_id: str, now: Callable[[], datetime]) -> None:
        if out.exists() and any(out.iterdir()):
            raise ValueError(f"{out} already holds files: a run writes into an empty directory")
        self._out, self._dir = out, out / "recordings"
        self._run_id, self._provider, self.model_id, self._now = run_id, provider, model_id, now
        self._rows: list[dict[str, Any]] = []
        self.case_id = ""

    def record_call(self, *, url: str, request: bytes, status: int | None, reply: bytes | None, failure: str | None, projected_micros: int, cost_micros: int) -> str:
        self._dir.mkdir(parents=True, exist_ok=True)
        rid = f"rec-{len(self._rows) + 1:04d}"
        # SR's condition on noema#654 (A320): only a 2xx reply body is written. A provider's error body can echo the request, name the account or carry the tail of a key; the status and
        # the failure label say what happened, and replay (UND-15) keys on the request hash, not on an error body. A reply that arrives with no status is not written either.
        parsed_reply = _body(reply) if status is not None and 200 <= status < 300 else None
        reported = parsed_reply.get("model") if isinstance(parsed_reply, dict) and isinstance(parsed_reply.get("model"), str) else None
        # A359 amends the above without weakening it: a non-2xx reply gives only its status and three validated fields of its error object (never the message or the body); the class is for every call.
        error = error_triple(reply) if status is not None and not 200 <= status < 300 else None
        klass = failure_class(status, failure, reply)
        # A359 amends the above without weakening it: a non-2xx reply gives only its status and three validated fields of its error object (never the message or the body); the class is for every call.
        digest = "sha256:" + hashlib.sha256(request).hexdigest()
        doc = {
            "recording_id": rid, "run_id": self._run_id, "case_id": self.case_id, "provider": self._provider, "model_id": self.model_id, "reported_model_id": reported,
            "recorded_at": self._now().strftime("%Y-%m-%dT%H:%M:%SZ"), "url_path": urlsplit(url).path, "request_sha256": digest, "request": _body(request),
            "status": status, "failure": failure, "failure_class": klass, "error": error, "reply": parsed_reply, "projected_micros": projected_micros, "cost_micros": cost_micros,
        }
        (self._dir / f"{rid}.json").write_text(dump(doc), encoding="utf-8")
        self._rows.append({"recording_id": rid, "case_id": self.case_id, "request_sha256": digest, "status": status, "failure": failure, "failure_class": klass, "error": error, "cost_micros": cost_micros, "reported_model_id": reported})
        return rid

    def write_manifest(self, pin: dict[str, Any]) -> Path:
        self._dir.mkdir(parents=True, exist_ok=True)
        manifest = {
            "run_id": self._run_id, "provider": self._provider, "model_id": self.model_id, "date": self._now().strftime("%Y-%m-%d"), "calls": len(self._rows), "pin": pin,
            "failure_classes": {k: sum(1 for r in self._rows if r["failure_class"] == k) for k in sorted(FAILURE_CLASSES) if any(r["failure_class"] == k for r in self._rows)},
            "reported_model_ids": sorted({r["reported_model_id"] for r in self._rows if r["reported_model_id"]}), "label": LABEL,
            "recordings": [{k: r[k] for k in ("recording_id", "case_id", "request_sha256", "status", "failure", "failure_class", "error", "cost_micros")} for r in self._rows],
        }
        path = self._dir / "manifest.json"
        path.write_text(dump(manifest), encoding="utf-8")
        return path
