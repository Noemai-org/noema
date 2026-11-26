"""The recorded-replay Transport: it answers a request from a committed, SYNTHETIC recording of a real call, and from nothing else.

It stands where `UrllibTransport` stands, so the real adapter and the real harness run unchanged above it (AGENT-12: no mocked
failure mode; only the network is replaced). The key is the SHA-256 of the exact bytes the adapter sends, as the recorder stored
it in the manifest (A261: the parsed request in a recording cannot be turned back into those bytes, so the recorded hash is the
only key). A request with no recording is refused with a typed `ReplayMiss`; nothing falls through to a live call and nothing is
answered by default. A recording of a failed call is evidence, never a fixture (A320), and is refused as `EVIDENCE_ONLY`.

The directory is read once, at construction, from the path given there; it is exactly a manifest and one `rec-NNNN.json` per row
(what `benchmark/compile_acceptance/fixtures.py` writes), regular files only, each at most 1 MiB, strict JSON, labelled SYNTHETIC
(AGENT-25). It imports no network, process, clock or random module and reads no environment, credential header or URL (INV-29).
The strict loader of the benchmark package is the validator of the committed set; this reader checks what it serves and refuses
the rest, and it does not import the benchmark package (the model service never imports it).
"""

import hashlib
import json
import re
import stat
from collections.abc import Mapping
from pathlib import Path
from types import MappingProxyType
from typing import Any

from noema_model.providers.llm.base import HttpResponse, ProviderError, ProviderFailure

MAX_FILE_BYTES = 1 << 20
_KEY = re.compile(r"sha256:[0-9a-f]{64}")
_RECORDING_ID = re.compile(r"rec-[0-9]{4}")
_MANIFEST = "manifest.json"


class ReplayFixturesRefused(Exception):
    """The directory is not what the recorder wrote. `code` is a closed word; the text names a file, never a value from it."""

    def __init__(self, code: str, file: str) -> None:
        super().__init__(f"replay fixtures refused: {code} ({file})")
        self.code = code


class ReplayMiss(ProviderError):
    """A request that was not answered: `UNKNOWN_REQUEST` (no recording), `EVIDENCE_ONLY` (a recorded failure) or `BAD_KEY`.

    It is a ProviderError of kind REJECTED, so a caller that fails closed on a provider error fails closed here too. The text
    holds the code only, never a request or a reply."""

    def __init__(self, code: str) -> None:
        super().__init__(ProviderFailure.REJECTED)
        self.args = (f"replay refused: {code}",)
        self.code = code


def _reject_constant(_name: str) -> Any:
    raise ValueError("a non-finite number")


def _no_duplicates(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    out: dict[str, Any] = {}
    for key, value in pairs:
        if key in out:
            raise ValueError("a duplicate key")
        out[key] = value
    return out


def _read_object(path: Path, name: str) -> dict[str, Any]:
    try:
        info = path.lstat()
    except OSError as exc:
        raise ReplayFixturesRefused("UNREADABLE", name) from exc
    if not stat.S_ISREG(info.st_mode):
        raise ReplayFixturesRefused("NOT_A_REGULAR_FILE", name)
    if info.st_size > MAX_FILE_BYTES:
        raise ReplayFixturesRefused("TOO_LARGE", name)
    try:
        with path.open("rb") as handle:
            data = handle.read(MAX_FILE_BYTES + 1)
    except OSError as exc:
        raise ReplayFixturesRefused("UNREADABLE", name) from exc
    if len(data) > MAX_FILE_BYTES:
        raise ReplayFixturesRefused("TOO_LARGE", name)
    try:
        value = json.loads(data.decode("utf-8"), parse_constant=_reject_constant, object_pairs_hook=_no_duplicates)
    except (ValueError, UnicodeDecodeError) as exc:
        raise ReplayFixturesRefused("NOT_STRICT_JSON", name) from exc
    if not isinstance(value, dict):
        raise ReplayFixturesRefused("NOT_AN_OBJECT", name)
    return value


def _reply_bytes(reply: Mapping[str, Any], name: str) -> bytes:
    try:
        return json.dumps(reply, sort_keys=True, separators=(",", ":"), ensure_ascii=False, allow_nan=False).encode("utf-8")
    except (TypeError, ValueError) as exc:
        raise ReplayFixturesRefused("UNSERVABLE_REPLY", name) from exc


class ReplayTransport:
    """Serves recorded replies by the SHA-256 of the request bytes. Immutable once built; it opens no connection."""

    __slots__ = ("_served", "_evidence")

    def __init__(self, directory: Path) -> None:
        root = Path(directory)
        try:
            top = root.lstat()
        except OSError as exc:
            raise ReplayFixturesRefused("NOT_A_DIRECTORY", "directory") from exc
        if not stat.S_ISDIR(top.st_mode):
            raise ReplayFixturesRefused("NOT_A_DIRECTORY", "directory")
        manifest = _read_object(root / _MANIFEST, _MANIFEST)
        fixture = manifest.get("fixture")
        if not isinstance(fixture, dict) or fixture.get("synthetic") is not True:
            raise ReplayFixturesRefused("NOT_LABELLED_SYNTHETIC", _MANIFEST)
        rows = manifest.get("recordings")
        if not isinstance(rows, list) or not rows:
            raise ReplayFixturesRefused("NO_RECORDINGS", _MANIFEST)
        served: dict[str, bytes] = {}
        evidence: set[str] = set()
        ids: set[str] = set()
        for row in rows:
            if not isinstance(row, dict):
                raise ReplayFixturesRefused("BAD_ROW", _MANIFEST)
            rid, key, status, failure = row.get("recording_id"), row.get("request_sha256"), row.get("status"), row.get("failure")
            if not (isinstance(rid, str) and _RECORDING_ID.fullmatch(rid)) or rid in ids:
                raise ReplayFixturesRefused("BAD_ROW", _MANIFEST)
            if not (isinstance(key, str) and _KEY.fullmatch(key)) or key in served or key in evidence:
                raise ReplayFixturesRefused("BAD_KEY", _MANIFEST)
            if not (status is None or (type(status) is int)) or not (failure is None or isinstance(failure, str)):
                raise ReplayFixturesRefused("BAD_ROW", _MANIFEST)
            ids.add(rid)
            name = f"{rid}.json"
            doc = _read_object(root / name, name)
            if doc.get("recording_id") != rid:
                raise ReplayFixturesRefused("ID_MISMATCH", name)
            labels = (doc.get("fixture") or {}).get("labels") if isinstance(doc.get("fixture"), dict) else None
            usable = labels.get("usable_as_fixture") if isinstance(labels, dict) else None
            if not isinstance(usable, bool):
                raise ReplayFixturesRefused("NOT_LABELLED", name)
            if usable:
                reply = doc.get("reply")
                if status != 200 or failure is not None or not isinstance(reply, dict):
                    raise ReplayFixturesRefused("USABLE_WITHOUT_A_REPLY", name)
                served[key] = _reply_bytes(reply, name)
            else:
                evidence.add(key)
        entries = {p.name for p in root.iterdir()}
        if entries != {_MANIFEST} | {f"{rid}.json" for rid in ids}:
            raise ReplayFixturesRefused("UNEXPECTED_ENTRIES", "directory")
        self._served: Mapping[str, bytes] = MappingProxyType(served)
        self._evidence: frozenset[str] = frozenset(evidence)

    def served_keys(self) -> tuple[str, ...]:
        return tuple(sorted(self._served))

    def evidence_only_keys(self) -> tuple[str, ...]:
        return tuple(sorted(self._evidence))

    def reply_for_key(self, key: str) -> bytes:
        if not isinstance(key, str) or _KEY.fullmatch(key) is None:
            raise ReplayMiss("BAD_KEY")
        body = self._served.get(key)
        if body is not None:
            return body
        if key in self._evidence:
            raise ReplayMiss("EVIDENCE_ONLY")
        raise ReplayMiss("UNKNOWN_REQUEST")

    async def post(
        self, url: str, headers: Mapping[str, str], body: bytes, timeout_s: float, max_bytes: int
    ) -> HttpResponse:
        # The url, the headers (the credential among them) and the deadline are not read: there is no call to make.
        if not isinstance(body, bytes):
            raise ReplayMiss("BAD_KEY")
        reply = self.reply_for_key("sha256:" + hashlib.sha256(body).hexdigest())
        if len(reply) > max_bytes:
            raise ProviderError(ProviderFailure.MALFORMED, 200)
        return HttpResponse(status=200, body=reply)
