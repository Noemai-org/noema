"""UND-15 (REQ-INT-002; REQ-TST-004, AGENT-12; A261; SR's conditions on noema#778, relayed): the reduction of a live run's recordings to committed replay fixtures, and the strict reader of the result.

What is kept and why. A request is kept exactly as recorded and stays keyed by `request_sha256` (the hash of the bytes the harness sent; a replay looks a reply up by it). A reply is REDUCED to what a replay and
the provider adapter read: the one message, the status, the model and the three token counts. The provider's echo of the request, its tools and its opaque reasoning (`encrypted_content`) are dropped, so the
reduced reply parses to the same object as the recorded one (the message text is the same bytes; its hash is kept). A failed call has no reply and stays as it was: it is a negative fixture, evidence of the failure
(A320). Every fixture says SYNTHETIC, the run id, the head SHA the run was dispatched on and the A261 label (AGENT-25).

`reduce_run` writes a directory from a live run's `recordings/`; `load_fixtures` is the reader the tests use: it refuses a directory whose request, reply text, labels or provenance is not what the reduction
wrote. Both read and write local files only; neither opens a connection."""
import argparse
import copy
import hashlib
import json
import re
import sys
from collections.abc import Mapping, Sequence
from pathlib import Path
from typing import Any, Final

from benchmark.compile_acceptance.recording import dump
from benchmark.compile_acceptance.replay import REPLAY_LABEL, RecordingsRefused, RecordingSet, _object, _read, _single_message_text, label_recording, load_recordings

_HEAD: Final = re.compile(r"[0-9a-f]{40}")
_HEX: Final = re.compile(r"[0-9a-f]{64}")
_REPLY_KEYS: Final = frozenset({"object", "status", "model", "output", "usage"})
_USAGE_KEYS: Final = ("input_tokens", "output_tokens", "total_tokens")
_MANIFEST_FIXTURE: Final = frozenset({"label", "synthetic", "run_id", "head_sha", "reduced"})
_DOC_FIXTURE: Final = frozenset({"label", "synthetic", "run_id", "head_sha", "original_file_sha256", "request_canonical_sha256", "reply_message_sha256", "labels"})


def _canonical(value: Any) -> str:
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode("utf-8")).hexdigest()


def _text_hash(reply: Any) -> str | None:
    text = _single_message_text(reply) if isinstance(reply, dict) else None
    return hashlib.sha256(text.encode("utf-8")).hexdigest() if text is not None else None


def reduce_reply(reply: Mapping[str, Any]) -> dict[str, Any]:
    """The reply a replay needs. It invents nothing: a member the reply lacked, or held in another shape, is not in the result. The argument is not changed."""
    out: dict[str, Any] = {}
    for key in ("object", "status", "model"):
        if isinstance(reply.get(key), str):
            out[key] = reply[key]
    items = reply.get("output")
    out["output"] = [copy.deepcopy(i) for i in items if isinstance(i, dict) and i.get("type") == "message"] if isinstance(items, list) else []
    usage = reply.get("usage")
    if isinstance(usage, dict):
        out["usage"] = {k: usage[k] for k in _USAGE_KEYS if isinstance(usage.get(k), int) and not isinstance(usage.get(k), bool)}
    return out


def reduce_run(src: Path, dst: Path, *, head_sha: str) -> None:
    """Write the fixtures of the run in `src` (a `recordings/` directory the loader accepts) into the empty or absent `dst`. Nothing is written when anything is refused."""
    if _HEAD.fullmatch(head_sha) is None:
        raise RecordingsRefused("BAD_HEAD", "head_sha is not a full lowercase SHA-1")
    if dst.exists() and (not dst.is_dir() or any(dst.iterdir())):
        raise RecordingsRefused("DESTINATION_NOT_EMPTY", "the destination holds files")
    recordings = load_recordings(src)
    manifest = _object(_read(src, "manifest.json"), "manifest.json", "MANIFEST_INVALID")
    docs: dict[str, dict[str, Any]] = {}
    for rec in recordings.recordings:
        doc = _object(_read(src, f"{rec.recording_id}.json"), f"{rec.recording_id}.json", "RECORDING_INVALID")
        before = label_recording(doc)
        reduced = {**doc, "reply": reduce_reply(doc["reply"]) if isinstance(doc["reply"], dict) else None}
        if label_recording(reduced) != before or _text_hash(reduced["reply"]) != _text_hash(doc["reply"]):
            raise RecordingsRefused("REDUCTION_CHANGED", f"{rec.recording_id}.json: the reduced reply is not equivalent to the recorded one")
        reduced["fixture"] = {"label": REPLAY_LABEL, "synthetic": True, "run_id": recordings.run_id, "head_sha": head_sha, "original_file_sha256": rec.file_sha256,
                              "request_canonical_sha256": _canonical(doc["request"]), "reply_message_sha256": _text_hash(doc["reply"]),
                              "labels": {"usable_as_fixture": before.usable_as_fixture, "reason": before.reason}}
        docs[f"{rec.recording_id}.json"] = reduced
    manifest["fixture"] = {"label": REPLAY_LABEL, "synthetic": True, "run_id": recordings.run_id, "head_sha": head_sha, "reduced": True}
    dst.mkdir(parents=True, exist_ok=True)
    (dst / "manifest.json").write_text(dump(manifest), encoding="utf-8")
    for name, doc in docs.items():
        (dst / name).write_text(dump(doc), encoding="utf-8")


def _refuse(name: str, what: str) -> RecordingsRefused:
    return RecordingsRefused("FIXTURE_INVALID", f"{name}: {what}")


def _opaque(node: Any) -> bool:
    if isinstance(node, dict):
        return "encrypted_content" in node or any(_opaque(v) for v in node.values())
    if isinstance(node, list):
        return any(_opaque(v) for v in node)
    return False


def _check_provenance(f: Any, keys: frozenset[str], run_id: str, head: str | None, name: str) -> dict[str, Any]:
    if not isinstance(f, dict) or set(f) != keys:
        raise _refuse(name, "the fixture member has not exactly its members")
    if f["label"] != REPLAY_LABEL or f["synthetic"] is not True or f["run_id"] != run_id:
        raise _refuse(name, "the fixture member is not labelled with the A261 label, SYNTHETIC and the run id")
    if not isinstance(f["head_sha"], str) or _HEAD.fullmatch(f["head_sha"]) is None or (head is not None and f["head_sha"] != head):
        raise _refuse(name, "the head SHA is not a full lowercase SHA-1, or not the manifest's")
    return f


def load_fixtures(directory: Path) -> RecordingSet:
    """Load a committed fixture directory, strictly: everything `load_recordings` checks, and that the provenance, the request, the reply's message and the labels are what `reduce_run` wrote."""
    rset = load_recordings(directory)
    manifest = _object(_read(directory, "manifest.json"), "manifest.json", "MANIFEST_INVALID")
    mf = manifest.get("fixture")
    if not isinstance(mf, dict) or mf.get("reduced") is not True:
        raise _refuse("manifest.json", "the manifest is not that of a reduced fixture directory")
    head = str(_check_provenance(mf, _MANIFEST_FIXTURE, rset.run_id, None, "manifest.json")["head_sha"])
    for rec in rset.recordings:
        name = f"{rec.recording_id}.json"
        doc = _object(_read(directory, name), name, "RECORDING_INVALID")
        f = _check_provenance(doc.get("fixture"), _DOC_FIXTURE, rset.run_id, head, name)
        if not isinstance(f["original_file_sha256"], str) or _HEX.fullmatch(f["original_file_sha256"]) is None:
            raise _refuse(name, "original_file_sha256 is not a SHA-256")
        if f["request_canonical_sha256"] != _canonical(doc["request"]):
            raise _refuse(name, "the request is not the one recorded")
        if f["reply_message_sha256"] != _text_hash(doc["reply"]):
            raise _refuse(name, "the reply's message is not the one recorded")
        labels = label_recording(doc)
        if f["labels"] != {"usable_as_fixture": labels.usable_as_fixture, "reason": labels.reason}:
            raise _refuse(name, "the recorded labels are not the labels of the recording")
        reply = doc["reply"]
        if reply is not None:
            usage = reply.get("usage", {})
            if (not set(reply) <= _REPLY_KEYS or not isinstance(reply.get("output"), list) or any(not isinstance(i, dict) or i.get("type") != "message" for i in reply["output"])
                    or not isinstance(usage, dict) or not set(usage) <= set(_USAGE_KEYS) or _opaque(reply)):
                raise _refuse(name, "the reply holds more than the reduction keeps")
    return rset


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="python -m benchmark.compile_acceptance.fixtures", description="Reduce a live run's recordings to committed replay fixtures, or check a fixture directory.")
    sub = parser.add_subparsers(dest="command", required=True)
    red = sub.add_parser("reduce")
    red.add_argument("src", type=Path)
    red.add_argument("dst", type=Path)
    red.add_argument("--head-sha", required=True)
    chk = sub.add_parser("check")
    chk.add_argument("directory", type=Path)
    args = parser.parse_args(argv)
    try:
        if args.command == "reduce":
            reduce_run(args.src, args.dst, head_sha=args.head_sha)
            print(f"reduced {args.src} into {args.dst}")
        else:
            rset = load_fixtures(args.directory)
            print(f"ok: {len(rset.recordings)} recordings, {len(rset.usable())} usable")
    except RecordingsRefused as err:
        print(f"refused: {err}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
