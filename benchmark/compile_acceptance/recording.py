"""The recorder of the compile acceptance run (UND-16; the source of the replay provider's fixtures, UND-15, A261).

One file per provider call: `recordings/rec-NNNN.json`, with the provider, the model pin, the run id, the date, the SHA-256 of the exact request bytes (the key the replay provider looks up), both bodies parsed
so a line of the file is one value, and the cost. The host, the query and every header are never written (the credential travels in a header, INV-29). The files are pretty-printed with sorted keys, so the
leak scan reads one value per line. All data is SYNTHETIC (AGENT-25)."""
import hashlib
import json
from collections.abc import Callable
from datetime import datetime
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

LABEL = "Understanding: compile acceptance on synthetic golden examples, recorded live output (SYNTHETIC)"


def _body(data: bytes | None) -> Any:
    if data is None:
        return None
    try:
        return json.loads(data)
    except ValueError:
        return {"text": data.decode("utf-8", errors="replace")}


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
        parsed_reply = _body(reply)
        reported = parsed_reply.get("model") if isinstance(parsed_reply, dict) and isinstance(parsed_reply.get("model"), str) else None
        digest = "sha256:" + hashlib.sha256(request).hexdigest()
        doc = {
            "recording_id": rid, "run_id": self._run_id, "case_id": self.case_id, "provider": self._provider, "model_id": self.model_id, "reported_model_id": reported,
            "recorded_at": self._now().strftime("%Y-%m-%dT%H:%M:%SZ"), "url_path": urlsplit(url).path, "request_sha256": digest, "request": _body(request),
            "status": status, "failure": failure, "reply": parsed_reply, "projected_micros": projected_micros, "cost_micros": cost_micros,
        }
        (self._dir / f"{rid}.json").write_text(dump(doc), encoding="utf-8")
        self._rows.append({"recording_id": rid, "case_id": self.case_id, "request_sha256": digest, "status": status, "failure": failure, "cost_micros": cost_micros, "reported_model_id": reported})
        return rid

    def write_manifest(self, pin: dict[str, Any]) -> Path:
        self._dir.mkdir(parents=True, exist_ok=True)
        manifest = {
            "run_id": self._run_id, "provider": self._provider, "model_id": self.model_id, "date": self._now().strftime("%Y-%m-%d"), "calls": len(self._rows), "pin": pin,
            "reported_model_ids": sorted({r["reported_model_id"] for r in self._rows if r["reported_model_id"]}), "label": LABEL,
            "recordings": [{k: r[k] for k in ("recording_id", "case_id", "request_sha256", "status", "failure", "cost_micros")} for r in self._rows],
        }
        path = self._dir / "manifest.json"
        path.write_text(dump(manifest), encoding="utf-8")
        return path
