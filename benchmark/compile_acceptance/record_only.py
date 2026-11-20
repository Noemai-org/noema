"""The record-only pass of the compile acceptance run (UND-16; A361, A372, the Architect's answer 5, relayed; REQ-INT-001, REQ-BEN-004; INV-22, AGENT-13, AGENT-25).

After the golden examples the run takes the inputs of BO's `benchmark/replay/g1_inputs.json` (five generated cases: utterance, channel, locale mix) through the same compile pipeline and records every call with the
same recorder, so UND-15 can later make replay fixtures of them. Inputs only: nothing here compares an answer with an expectation or invents one (INV-22), and the run's verdict never depends on what the model answers.

The file is read from a path and trusted for nothing it does not prove. It must be a regular file (no symlink) of at most 1 MiB, strict JSON (no duplicate key, no NaN), of exactly the closed shape BO's exporter writes
(`benchmark/replay/g1_inputs.py`): a key beyond that shape, an expectation or an oracle reference included, is a refusal even if the digest matches, and the file's own `inputs_sha256` is recomputed by the exporter's
rule (the document without that member, sorted keys, indent 2, not ASCII-escaped) and must equal the stored one. A refusal carries one fixed code (MISSING, SIZE, NOT_JSON, SHAPE, DIGEST) and never a piece of the file,
because an utterance must not travel in a message (AGENT-13). Every input is SYNTHETIC (AGENT-25)."""
import hashlib
import hmac
import json
import re
import unicodedata
from dataclasses import dataclass
from pathlib import Path
from collections.abc import Mapping
from typing import Any, NoReturn

from benchmark.compile_acceptance.golden import RECEIVED_AT

ROOT = Path(__file__).resolve().parents[2]
DEFAULT_PATH = ROOT / "benchmark" / "replay" / "g1_inputs.json"
DEFAULT_FIXTURE = ROOT / "benchmark" / "replay" / "g1_canonical_inputs.json"
MAX_BYTES = 1 << 20
MAX_CASES = 20
MAX_TEXT = 2_000  # canonical-input.v2: "Unicode NFC, trimmed, 1 to 2000 characters"
_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
_CASE_ID = re.compile(r"BEN-[A-Z]{2,6}-[0-9]{4}")
_LOCALE = re.compile(r"[a-z]{2,3}(?:-[A-Za-z0-9]{2,8})*")
_TOP = frozenset({"label", "suite", "cases", "inputs_sha256"})
_SUITE = frozenset({"generator", "generator_version", "manifest_sha256", "seed", "tree_sha256"})
_CASE = frozenset({"case_id", "case_sha256", "channel", "locale_mix", "utterances"})
_UTTERANCE = frozenset({"turn", "text"})


class RecordOnlyRefused(Exception):
    """The inputs file is not one this run may use. `code` is the whole message."""

    def __init__(self, code: str) -> None:
        super().__init__(code)
        self.code = code


@dataclass(frozen=True, slots=True)
class RecordOnlyInput:
    case_id: str
    channel: str
    locale_mix: tuple[str, ...]
    canonical_locale: str
    text: str
    canonical: Mapping[str, Any]
    candidates: tuple[Mapping[str, Any], ...]


def _refuse(code: str) -> NoReturn:
    raise RecordOnlyRefused(code)


def _no_duplicates(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    out: dict[str, Any] = {}
    for key, value in pairs:
        if key in out:
            _refuse("NOT_JSON")
        out[key] = value
    return out


def _no_constant(_: str) -> NoReturn:
    _refuse("NOT_JSON")


def _is_int(value: Any) -> bool:
    return isinstance(value, int) and not isinstance(value, bool)


def _closed(value: Any, keys: frozenset[str]) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != keys:
        _refuse("SHAPE")
    return value


def _digest(value: Any) -> str:
    if not isinstance(value, str) or _DIGEST.fullmatch(value) is None:
        _refuse("SHAPE")
    return value


def _utterance(raw: Any) -> str:
    utterances = raw
    if not isinstance(utterances, list) or len(utterances) != 1:  # the compile service takes one utterance; a multi-turn case is not a case of this pass
        _refuse("SHAPE")
    one = _closed(utterances[0], _UTTERANCE)
    text = one["text"]
    if not _is_int(one["turn"]) or not isinstance(text, str) or not text or len(text) > MAX_TEXT or text != text.strip() or not unicodedata.is_normalized("NFC", text):
        _refuse("SHAPE")
    return text


def _case(raw: Any) -> RecordOnlyInput:
    case = _closed(raw, _CASE)
    case_id, channel, locale_mix = case["case_id"], case["channel"], case["locale_mix"]
    _digest(case["case_sha256"])
    if not isinstance(case_id, str) or _CASE_ID.fullmatch(case_id) is None or channel != "text":
        _refuse("SHAPE")
    if not isinstance(locale_mix, list) or not locale_mix or any(not isinstance(x, str) or _LOCALE.fullmatch(x) is None for x in locale_mix):
        _refuse("SHAPE")
    return RecordOnlyInput(case_id, "text", tuple(locale_mix), "", _utterance(case["utterances"]), {}, ())  # UND-16 red: the world rows, the locale and the fixture are not read yet


def _stored_digest(doc: dict[str, Any]) -> str:
    body = {k: v for k, v in doc.items() if k != "inputs_sha256"}
    return "sha256:" + hashlib.sha256(json.dumps(body, indent=2, sort_keys=True, ensure_ascii=False).encode("utf-8")).hexdigest()


def load_inputs(path: Path) -> tuple[RecordOnlyInput, ...]:
    """The inputs of the file, in its order. Raises RecordOnlyRefused for anything that is not exactly BO's file."""
    if path.is_symlink() or not path.is_file():
        _refuse("MISSING")
    if path.stat().st_size > MAX_BYTES:
        _refuse("SIZE")
    try:
        doc = json.loads(path.read_bytes().decode("utf-8"), object_pairs_hook=_no_duplicates, parse_constant=_no_constant)
    except (UnicodeDecodeError, ValueError, RecursionError):
        _refuse("NOT_JSON")
    if not isinstance(doc, dict):
        _refuse("NOT_JSON")
    top = _closed(doc, _TOP)
    suite = _closed(top["suite"], _SUITE)
    if not isinstance(top["label"], str) or not isinstance(suite["generator"], str) or not isinstance(suite["generator_version"], str) or not _is_int(suite["seed"]):
        _refuse("SHAPE")
    _digest(suite["manifest_sha256"])
    _digest(suite["tree_sha256"])
    stored = _digest(top["inputs_sha256"])
    cases = top["cases"]
    if not isinstance(cases, list) or not 1 <= len(cases) <= MAX_CASES:
        _refuse("SHAPE")
    inputs = tuple(_case(c) for c in cases)
    if len({i.case_id for i in inputs}) != len(inputs):
        _refuse("SHAPE")
    if not hmac.compare_digest(stored, _stored_digest(top)):
        _refuse("DIGEST")
    return inputs


def _letters(*parts: str) -> str:
    """Twenty letters derived from the parts: an id that carries no digit, so no id of a request holds a digit run (AGENT-13)."""
    digest = hashlib.sha256(b"".join(len(p.encode()).to_bytes(4, "big") + p.encode() for p in parts)).digest()
    number, out = int.from_bytes(digest, "big"), []
    for _ in range(20):
        number, rest = divmod(number, 26)
        out.append(chr(ord("a") + rest))
    return "".join(out)


def _request_id(case_id: str) -> str:
    h = hashlib.sha256(b"record-only-request:" + case_id.encode()).hexdigest()
    rid = f"{h[:8]}-{h[8:12]}-4{h[13:16]}-{'89ab'[int(h[16], 16) % 4]}{h[17:20]}-{h[20:32]}"
    return rid if re.search(r"[a-f]", rid) else "a" + rid[1:]  # a lower-case UUID that is never all digits


def canonical_input(inp: RecordOnlyInput) -> dict[str, Any]:
    """The `canonical-input.v2` of one input: the goldens' shape with the utterance of the file. The same every time (INV-21): the ids are derived from the case id and the instant is the goldens'. The locale mix is
    not sent as a hint: the pipeline detects the locale itself, as for the goldens."""
    return {
        "schema_version": "2.0", "request_id": _request_id(inp.case_id), "correlation_id": "corr_" + _letters("record-only-correlation", inp.case_id), "tenant_id": "ten_synthetic", "session_id": "ses_synthetic",
        "principal_ref": "usr_synthetic", "channel": "text", "mode": "MODE_1", "text": inp.text, "received_at": RECEIVED_AT,
    }


def candidate_rows(inp: "RecordOnlyInput") -> tuple[dict[str, Any], ...]:
    raise NotImplementedError("UND-16 red: the candidate rows of the world rows (A439) are not written yet")
