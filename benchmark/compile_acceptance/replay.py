"""UND-15 (REQ-INT-002; REQ-TST-004, AGENT-12; A188, A261): the loader, the labels and the lookup of the recorded-replay provider's recordings.

The recordings are those of one live compile run (UND-16, `recording.py`): `manifest.json` and one `rec-NNNN.json` per provider call. This module reads them, refuses anything that is not exactly what the
recorder wrote, labels each one from its own fields, and serves a recorded reply by the SHA-256 of the request bytes. It sends nothing, imports no network or process module and reads only the one directory
it is given: no symlink, no `..`, no entry the manifest does not name.

A failed call (no status, a 4xx, a 5xx) is evidence of the failure, not of an answer: it is labelled `evidence_only` and is never served (A320; a recording holds no error body). A request with no recording is
refused too: the lookup never guesses and never falls through to a provider. Labels come from the recording's fields and the request's own schema, never from what a reply or an utterance says (A261).
All data is SYNTHETIC (AGENT-25)."""
import copy
import hashlib
import json
import os
import re
import stat
from collections.abc import Mapping
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Final, Literal

from jsonschema import Draft202012Validator  # type: ignore[import-untyped]  # no stubs; adding types-jsonschema needs approval (AGENT-14)
from jsonschema.exceptions import SchemaError  # type: ignore[import-untyped]

from benchmark.compile_acceptance.recording import LABEL

REPLAY_LABEL: Final = "Understanding: recorded replay of live output"  # A261: the evidence of a G1 run on these recordings carries this label
MAX_BYTES: Final = 1024 * 1024
STEPS: Final = ("harness_decide", "pipeline_slot_mentions")  # the request's schema name: the two steps the compile pipeline asks a model for

Reason = Literal["OK", "NO_STATUS", "HTTP_4XX", "HTTP_5XX", "HTTP_OTHER", "FAILURE_LABELLED", "NO_REPLY_BODY", "NOT_COMPLETED", "NO_SINGLE_MESSAGE", "NOT_JSON", "NO_REQUEST_SCHEMA", "UNKNOWN_STEP",
                 "SCHEMA_MISMATCH", "QUESTIONS_MISMATCH"]

_ID: Final = re.compile(r"rec-[0-9]{4}")
_HASH: Final = re.compile(r"sha256:[0-9a-f]{64}")
_HEX: Final = re.compile(r"[0-9a-f]{64}")
_DATE: Final = re.compile(r"[0-9]{4}-[0-9]{2}-[0-9]{2}")
_TIME: Final = re.compile(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z")
_MANIFEST_KEYS: Final = frozenset({"run_id", "provider", "model_id", "date", "calls", "pin", "reported_model_ids", "label", "recordings"})
_ROW_KEYS: Final = frozenset({"recording_id", "case_id", "request_sha256", "status", "failure", "cost_micros"})
_PIN_KEYS: Final = frozenset({"harness_config_sha256", "lexicon_sha256", "masking_limits_sha256", "max_output_tokens", "pack_ref", "pack_sha256", "pinned_model_id", "rate_card", "reasoning_effort"})
_RATE_KEYS: Final = frozenset({"as_of", "input_micro_per_mtok", "output_micro_per_mtok"})
_DOC_KEYS: Final = frozenset({"recording_id", "run_id", "case_id", "provider", "model_id", "reported_model_id", "recorded_at", "url_path", "request_sha256", "request", "status", "failure", "reply",
                              "projected_micros", "cost_micros"})


class RecordingsRefused(Exception):
    """The directory is not exactly what the recorder wrote. `code` is one of a closed set; the message is fixed text with a file name, never a value from a recording."""

    def __init__(self, code: str, message: str) -> None:
        super().__init__(f"{code}: {message}")
        self.code = code


class ReplayRefused(Exception):
    """A lookup that was not answered: `UNKNOWN_REQUEST` (no recording), `EVIDENCE_ONLY` (a recording of a failure; `reason` says which) or `BAD_KEY`. The text holds a code and an id, never a reply."""

    def __init__(self, code: str, recording_id: str | None = None, reason: str | None = None) -> None:
        super().__init__(f"{code} {recording_id}" if recording_id else code)
        self.code, self.recording_id, self.reason = code, recording_id, reason


@dataclass(frozen=True, slots=True)
class Labels:
    usable_as_fixture: bool  # true only for HTTP 200, no failure label, a completed reply with one JSON message that meets the request's own schema and answers exactly the questions asked
    reason: Reason
    step: str  # the request's schema name, or "unknown"
    question_ids: tuple[str, ...]  # the questions the request asked (decide step) or the slots its schema names (extraction step)
    case_id: str

    @property
    def evidence_only(self) -> bool:
        return not self.usable_as_fixture


@dataclass(frozen=True, slots=True, eq=False)
class Recording:
    recording_id: str
    request_sha256: str
    file_sha256: str  # SHA-256 of the file's bytes
    labels: Labels
    reply: Mapping[str, Any] | None


@dataclass(frozen=True, slots=True, eq=False)
class RecordingSet:
    run_id: str
    provider: str
    model_id: str
    date: str
    label: str  # the recorder's label of the run
    pin: Mapping[str, Any]
    recordings: tuple[Recording, ...]
    replay_label: str = REPLAY_LABEL

    def usable(self) -> tuple[Recording, ...]:
        return tuple(r for r in self.recordings if r.labels.usable_as_fixture)

    def evidence_only(self) -> tuple[Recording, ...]:
        return tuple(r for r in self.recordings if r.labels.evidence_only)


# ---- labels ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------


def _strict_json(raw: str) -> Any:
    def pairs(items: list[tuple[str, Any]]) -> dict[str, Any]:
        keys = [k for k, _ in items]
        if len(set(keys)) != len(keys):
            raise ValueError("duplicate key")
        return dict(items)

    def constant(_name: str) -> Any:
        raise ValueError("not a JSON number")

    return json.loads(raw, object_pairs_hook=pairs, parse_constant=constant)


def _format(request: Any) -> Mapping[str, Any] | None:
    text = request.get("text") if isinstance(request, dict) else None
    fmt = text.get("format") if isinstance(text, dict) else None
    return fmt if isinstance(fmt, dict) else None


def _local_refs_only(node: Any) -> bool:
    if isinstance(node, dict):
        for key, value in node.items():
            if key in ("$id", "$dynamicRef", "$recursiveRef") or (key == "$ref" and not (isinstance(value, str) and value.startswith("#/"))):
                return False
            if not _local_refs_only(value):
                return False
    elif isinstance(node, list):
        return all(_local_refs_only(v) for v in node)
    return True


def _question_ids(step: str, request: Any, schema: Any) -> tuple[str, ...]:
    if step == "harness_decide":
        text = request.get("input") if isinstance(request, dict) else None
        marker = "QUESTIONS:\n"
        if not isinstance(text, str) or marker not in text:
            return ()
        try:
            questions, _ = json.JSONDecoder().raw_decode(text, text.index(marker) + len(marker))
        except ValueError:
            return ()
        if not isinstance(questions, list) or not all(isinstance(q, dict) and isinstance(q.get("question_id"), str) for q in questions):
            return ()
        return tuple(q["question_id"] for q in questions)
    if step == "pipeline_slot_mentions" and isinstance(schema, dict) and isinstance(schema.get("properties"), dict):
        return tuple(str(k) for k in schema["properties"])
    return ()


def _single_message_text(reply: Mapping[str, Any]) -> str | None:
    output = reply.get("output")
    if not isinstance(output, list):
        return None
    messages = [o for o in output if isinstance(o, dict) and o.get("type") == "message"]
    if len(messages) != 1:
        return None
    content = messages[0].get("content")
    if not isinstance(content, list) or len(content) != 1 or not isinstance(content[0], dict) or content[0].get("type") != "output_text":
        return None
    text = content[0].get("text")
    return text if isinstance(text, str) else None


def label_recording(doc: Mapping[str, Any]) -> Labels:
    """The labels of one recording, from its own fields. Pure: it reads no file and calls nothing."""
    request = doc.get("request")
    fmt = _format(request)
    name = fmt.get("name") if fmt is not None else None
    step = name if isinstance(name, str) and name in STEPS else "unknown"
    schema = fmt.get("schema") if fmt is not None else None
    qids = _question_ids(step, request, schema)
    case_id = doc.get("case_id") if isinstance(doc.get("case_id"), str) else ""

    def done(reason: Reason) -> Labels:
        return Labels(reason == "OK", reason, step, qids, str(case_id))

    status = doc.get("status")
    if status is None:
        return done("NO_STATUS")
    if status != 200:
        return done("HTTP_4XX" if isinstance(status, int) and 400 <= status < 500 else "HTTP_5XX" if isinstance(status, int) and 500 <= status < 600 else "HTTP_OTHER")
    if doc.get("failure") is not None:
        return done("FAILURE_LABELLED")
    reply = doc.get("reply")
    if not isinstance(reply, dict):
        return done("NO_REPLY_BODY")
    if reply.get("status") != "completed":
        return done("NOT_COMPLETED")
    text = _single_message_text(reply)
    if text is None:
        return done("NO_SINGLE_MESSAGE")
    try:
        parsed = _strict_json(text)
    except (ValueError, RecursionError):
        return done("NOT_JSON")
    if not isinstance(schema, dict) or not _local_refs_only(schema):
        return done("NO_REQUEST_SCHEMA")
    try:
        Draft202012Validator.check_schema(schema)
        valid = Draft202012Validator(schema).is_valid(parsed)
    except (SchemaError, RecursionError):
        return done("NO_REQUEST_SCHEMA")
    if step == "unknown":
        return done("UNKNOWN_STEP")
    if not valid:
        return done("SCHEMA_MISMATCH")
    if step == "harness_decide":
        answered = [a["question_id"] for a in parsed["answers"]]
        if not qids or len(set(answered)) != len(answered) or set(answered) != set(qids):
            return done("QUESTIONS_MISMATCH")
    return done("OK")


# ---- the loader -----------------------------------------------------------------------------------------------------------------------------------------------------------------------------


def _is_int(v: Any) -> bool:
    return isinstance(v, int) and not isinstance(v, bool)


def _is_count(v: Any) -> bool:
    return _is_int(v) and v >= 0


def _is_str(v: Any) -> bool:
    return isinstance(v, str) and v != ""


def _read(root: Path, name: str) -> bytes:
    try:
        fd = os.open(root / name, os.O_RDONLY | os.O_NOFOLLOW | getattr(os, "O_CLOEXEC", 0))
    except OSError as err:
        raise RecordingsRefused("UNSAFE_PATH", f"{name} could not be opened without following a link") from err
    if not stat.S_ISREG(os.fstat(fd).st_mode):
        os.close(fd)
        raise RecordingsRefused("UNEXPECTED_ENTRY", f"{name} is not a regular file")
    with os.fdopen(fd, "rb") as fh:
        data = fh.read(MAX_BYTES + 1)
    if len(data) > MAX_BYTES:
        raise RecordingsRefused("TOO_LARGE", f"{name} is over {MAX_BYTES} bytes")
    return data


def _object(data: bytes, name: str, code: str) -> dict[str, Any]:
    try:
        value = _strict_json(data.decode("utf-8"))
    except (UnicodeDecodeError, ValueError, RecursionError) as err:
        raise RecordingsRefused(code, f"{name} is not strict JSON") from err
    if not isinstance(value, dict):
        raise RecordingsRefused(code, f"{name} is not a JSON object")
    return value


def _require(ok: bool, code: str, name: str, what: str) -> None:
    if not ok:
        raise RecordingsRefused(code, f"{name}: {what}")


def _check_pin(pin: Any, model_id: str) -> None:
    _require(isinstance(pin, dict) and set(pin) == _PIN_KEYS, "MANIFEST_INVALID", "manifest.json", "pin has not the recorder's members")
    for key in ("harness_config_sha256", "lexicon_sha256", "masking_limits_sha256", "pack_sha256"):
        _require(isinstance(pin[key], str) and _HEX.fullmatch(pin[key]) is not None, "MANIFEST_INVALID", "manifest.json", f"pin.{key} is not a SHA-256")
    _require(_is_int(pin["max_output_tokens"]) and pin["max_output_tokens"] > 0, "MANIFEST_INVALID", "manifest.json", "pin.max_output_tokens")
    _require(_is_str(pin["pack_ref"]) and _is_str(pin["reasoning_effort"]), "MANIFEST_INVALID", "manifest.json", "pin text members")
    _require(pin["pinned_model_id"] == model_id, "MANIFEST_INVALID", "manifest.json", "pin.pinned_model_id is not the run's model")
    rate = pin["rate_card"]
    _require(isinstance(rate, dict) and set(rate) == _RATE_KEYS, "MANIFEST_INVALID", "manifest.json", "pin.rate_card has not the recorder's members")
    _require(isinstance(rate["as_of"], str) and _DATE.fullmatch(rate["as_of"]) is not None and _is_count(rate["input_micro_per_mtok"]) and _is_count(rate["output_micro_per_mtok"]),
             "MANIFEST_INVALID", "manifest.json", "pin.rate_card values")


def _check_manifest(m: dict[str, Any]) -> list[dict[str, Any]]:
    name = "manifest.json"
    _require(set(m) == _MANIFEST_KEYS, "MANIFEST_INVALID", name, "members are not exactly the recorder's")
    _require(all(_is_str(m[k]) for k in ("run_id", "provider", "model_id")), "MANIFEST_INVALID", name, "run_id, provider and model_id are text")
    _require(isinstance(m["date"], str) and _DATE.fullmatch(m["date"]) is not None, "MANIFEST_INVALID", name, "date")
    _require(m["label"] == LABEL, "MANIFEST_INVALID", name, "label is not the recorder's")
    _require(_is_count(m["calls"]), "MANIFEST_INVALID", name, "calls is not a count")
    ids = m["reported_model_ids"]
    _require(isinstance(ids, list) and all(_is_str(i) for i in ids) and ids == sorted(set(ids)), "MANIFEST_INVALID", name, "reported_model_ids")
    _check_pin(m["pin"], m["model_id"])
    rows = m["recordings"]
    _require(isinstance(rows, list) and all(isinstance(r, dict) for r in rows), "MANIFEST_INVALID", name, "recordings is not a list of objects")
    for row in rows:
        _require(set(row) == _ROW_KEYS, "MANIFEST_INVALID", name, "a row has not the recorder's members")
        _require(isinstance(row["recording_id"], str) and _ID.fullmatch(row["recording_id"]) is not None, "MANIFEST_INVALID", name, "a recording_id is not rec-NNNN")
        _require(_is_str(row["case_id"]) and isinstance(row["request_sha256"], str) and _HASH.fullmatch(row["request_sha256"]) is not None, "MANIFEST_INVALID", name, "a row's case or hash")
        _require((row["status"] is None or _is_int(row["status"])) and (row["failure"] is None or _is_str(row["failure"])) and _is_count(row["cost_micros"]), "MANIFEST_INVALID", name, "a row's values")
    _require(len(rows) > 0, "MANIFEST_INVALID", name, "no recordings: a run of no call is nothing to replay")
    _require(m["calls"] == len(rows), "COUNT_MISMATCH", name, "calls is not the number of recordings")
    _require(len({r["recording_id"] for r in rows}) == len(rows) and len({r["request_sha256"] for r in rows}) == len(rows), "DUPLICATE", name, "a recording or a request is listed twice")
    _require([r["recording_id"] for r in rows] == [f"rec-{n:04d}" for n in range(1, len(rows) + 1)], "MANIFEST_INVALID", name, "the recordings are not rec-0001 to rec-NNNN in order")
    return list(rows)


def _check_doc(doc: dict[str, Any], name: str) -> None:
    bad = "RECORDING_INVALID"
    _require(set(doc) == _DOC_KEYS, bad, name, "members are not exactly the recorder's")
    _require(all(_is_str(doc[k]) for k in ("recording_id", "run_id", "case_id", "provider", "model_id", "url_path")), bad, name, "text members")
    _require(isinstance(doc["recorded_at"], str) and _TIME.fullmatch(doc["recorded_at"]) is not None, bad, name, "recorded_at")
    _require(isinstance(doc["request_sha256"], str) and _HASH.fullmatch(doc["request_sha256"]) is not None, bad, name, "request_sha256")
    _require(isinstance(doc["request"], dict), bad, name, "request is not an object")
    _require(doc["status"] is None or _is_int(doc["status"]), bad, name, "status")
    _require(doc["failure"] is None or _is_str(doc["failure"]), bad, name, "failure")
    _require(_is_count(doc["projected_micros"]) and _is_count(doc["cost_micros"]), bad, name, "cost")
    _require(doc["reply"] is None or isinstance(doc["reply"], dict), bad, name, "reply is not an object")
    _require(doc["reported_model_id"] is None or _is_str(doc["reported_model_id"]), bad, name, "reported_model_id")
    ok = isinstance(doc["status"], int) and 200 <= doc["status"] < 300
    # A320: a response that is not 2xx holds no body.
    _require(ok or (doc["reply"] is None and doc["reported_model_id"] is None), bad, name, "a call that is not 2xx holds a body or a model id")
    model = doc["reply"].get("model") if isinstance(doc["reply"], dict) and isinstance(doc["reply"].get("model"), str) else None
    _require(doc["reported_model_id"] == model, bad, name, "reported_model_id is not the reply's own")


def load_recordings(directory: Path, *, file_sha256: Mapping[str, str] | None = None) -> RecordingSet:
    """Load and validate one run's recordings. `file_sha256` (recording id to the SHA-256 of its file), when given, pins the exact bytes: it must cover exactly the recordings and match every one."""
    if ".." in directory.parts or directory.is_symlink():
        raise RecordingsRefused("UNSAFE_PATH", "the directory is a link or holds a parent step")
    if not directory.is_dir():
        raise RecordingsRefused("NOT_A_DIRECTORY", "not a directory")
    found: set[str] = set()
    with os.scandir(directory) as it:
        for entry in it:
            if entry.is_symlink():
                raise RecordingsRefused("SYMLINK", f"{entry.name} is a link")
            if not entry.is_file(follow_symlinks=False):
                raise RecordingsRefused("UNEXPECTED_ENTRY", f"{entry.name} is not a regular file")
            found.add(entry.name)
    if "manifest.json" not in found:
        raise RecordingsRefused("MANIFEST_INVALID", "manifest.json is missing")
    manifest = _object(_read(directory, "manifest.json"), "manifest.json", "MANIFEST_INVALID")
    rows = _check_manifest(manifest)
    expected = {"manifest.json"} | {f"{r['recording_id']}.json" for r in rows}
    extra, missing = sorted(found - expected), sorted(expected - found)
    if extra:
        raise RecordingsRefused("UNEXPECTED_ENTRY", f"{extra[0]} is not listed in the manifest")
    if missing:
        raise RecordingsRefused("MISSING_RECORDING", f"{missing[0]} is listed in the manifest and absent")
    loaded: list[Recording] = []
    reported: set[str] = set()
    for row in rows:
        name = f"{row['recording_id']}.json"
        data = _read(directory, name)
        doc = _object(data, name, "RECORDING_INVALID")
        _check_doc(doc, name)
        same = (doc["recording_id"] == row["recording_id"] and doc["run_id"] == manifest["run_id"] and doc["provider"] == manifest["provider"] and doc["model_id"] == manifest["model_id"]
                and all(doc[k] == row[k] for k in ("case_id", "request_sha256", "status", "failure", "cost_micros")))
        _require(same, "MISMATCH", name, "does not agree with its row of the manifest")
        if doc["reported_model_id"] is not None:
            reported.add(doc["reported_model_id"])
        loaded.append(Recording(row["recording_id"], doc["request_sha256"], hashlib.sha256(data).hexdigest(), label_recording(doc), copy.deepcopy(doc["reply"])))
    _require(sorted(reported) == manifest["reported_model_ids"], "MISMATCH", "manifest.json", "reported_model_ids are not those of the recordings")
    if file_sha256 is not None:
        _require(set(file_sha256) == {r.recording_id for r in loaded} and all(file_sha256[r.recording_id] == r.file_sha256 for r in loaded), "HASH_MISMATCH", "recordings", "a file is not the pinned one")
    return RecordingSet(manifest["run_id"], manifest["provider"], manifest["model_id"], manifest["date"], manifest["label"], copy.deepcopy(manifest["pin"]), tuple(loaded))


# ---- the lookup -----------------------------------------------------------------------------------------------------------------------------------------------------------------------------


class ReplayStore:
    """Serves the recorded reply of a request by the SHA-256 of its bytes. Immutable once built; it opens no file and no connection."""

    __slots__ = ("_by_key",)

    def __init__(self, recordings: RecordingSet) -> None:
        if not isinstance(recordings, RecordingSet):
            raise TypeError("a ReplayStore is built from a loaded RecordingSet")
        self._by_key: dict[str, Recording] = {r.request_sha256: r for r in recordings.recordings}

    def lookup(self, request_sha256: str) -> dict[str, Any]:
        if not isinstance(request_sha256, str) or _HASH.fullmatch(request_sha256) is None:
            raise ReplayRefused("BAD_KEY")
        rec = self._by_key.get(request_sha256)
        if rec is None:
            raise ReplayRefused("UNKNOWN_REQUEST")
        if not rec.labels.usable_as_fixture or rec.reply is None:
            raise ReplayRefused("EVIDENCE_ONLY", rec.recording_id, rec.labels.reason)
        return copy.deepcopy(dict(rec.reply))

    def lookup_bytes(self, request: bytes) -> dict[str, Any]:
        if not isinstance(request, bytes):
            raise ReplayRefused("BAD_KEY")
        return self.lookup("sha256:" + hashlib.sha256(request).hexdigest())
