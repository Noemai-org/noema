"""The record-only pass of the compile acceptance run (UND-16; A361, A372, A439, A460; REQ-INT-001, REQ-BEN-004; INV-22, AGENT-13, AGENT-25).

After the golden examples the run takes five generated cases (BO's `benchmark/replay/g1_inputs.json`) through the same compile pipeline and records every call with the same recorder, so UND-15 can later make
replay fixtures of them. Inputs only: nothing here compares an answer with an expectation or invents one (INV-22), and the run's verdict never depends on what the model answers.

What a request is made of (A439, A460):
- **The canonical input is the core's.** It is what the Interpreter port (`services/core/orchestrator/portadapters/interpreter`) builds from a verified session and the turn. A Go test there builds the five and writes
  `benchmark/replay/g1_canonical_inputs.json`, pinned to BO's `inputs_sha256`. This pass loads that fixture and proves the pin; it builds no canonical input of its own (a Python rebuild is not the core's code, and
  was rejected). The port sends no locale hint, so none is sent: the locale of every case is en-NG (what the UI sends), `locale_mix` stays a tag.
- **The candidates are part of the request.** They come from the world rows of BO's file (the principal's beneficiaries and transactions, an allowlist of fields, no label of the target), as the rows of
  `candidates-response.v1`'s Candidate, newest first. The rule that turns the world rows into candidates is `candidate_rows` below.

The files are read from paths and trusted for nothing they do not prove. Each must be a regular file (no symlink) of at most 1 MiB, strict JSON (no duplicate key, no NaN), of exactly the closed shape BO's exporter and
the Go test write: a key beyond it, an expectation or an oracle reference included, is a refusal even if the digest matches. The inputs file's own `inputs_sha256` is recomputed by the exporter's rule (the document
without that member, sorted keys, indent 2, not ASCII-escaped) and must equal the stored one; the fixture's pin must equal it. A refusal carries one fixed code (MISSING, SIZE, NOT_JSON, SHAPE, DIGEST for the inputs
file; FIXTURE, PIN for the fixture) and never a piece of a file, because an utterance must not travel in a message (AGENT-13). Every input is SYNTHETIC (AGENT-25)."""
import hashlib
import hmac
import json
import re
import unicodedata
from collections.abc import Mapping
from dataclasses import dataclass
from datetime import datetime, timedelta
from pathlib import Path
from typing import Any, NoReturn

from benchmark.compile_acceptance.golden import RECEIVED_AT

ROOT = Path(__file__).resolve().parents[2]
DEFAULT_PATH = ROOT / "benchmark" / "replay" / "g1_inputs.json"
DEFAULT_FIXTURE = ROOT / "benchmark" / "replay" / "g1_canonical_inputs.json"
MAX_BYTES = 1 << 20
MAX_CASES = 20
MAX_ROWS = 20  # candidates-response.v1: at most twenty rows
MAX_TEXT = 2_000  # canonical-input.v2: "Unicode NFC, trimmed, 1 to 2000 characters"
MAX_NAME = 64  # candidates-response.v1: counterparty_display, 1 to 64 characters
CANONICAL_LOCALE = "en-NG"  # A460: the locale of all five cases (what the UI sends)
_DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
_CASE_ID = re.compile(r"BEN-[A-Z]{2,6}-[0-9]{4}")
_LOCALE = re.compile(r"[a-z]{2,3}(?:-[A-Za-z0-9]{2,8})*")
_PRINCIPAL_ID = re.compile(r"usr_[0-9A-Za-z]{2,32}")  # common.v1 PrincipalId
_BENEFICIARY_ID = re.compile(r"ben_[0-9A-Za-z]{2,32}")
_RESOURCE_REF = re.compile(r"[a-z][a-z0-9]*\.[a-z][a-z0-9_]*:[A-Za-z0-9_.-]{1,64}")  # common.v1 ResourceRef
_LAST4 = re.compile(r"[0-9]{4}")
_DIGIT_RUN = re.compile(r"[0-9]{5}")  # counterparty_display: at most four consecutive digits
_UUID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")  # common.v1 ClientRequestId
_CORRELATION = re.compile(r"corr_[0-9A-Za-z]{2,32}")
_PACK_REF = re.compile(r"[a-z][a-z0-9]*@[0-9]+\.[0-9]+\.[0-9]+")
_DIRECTIONS = frozenset({"OUTGOING", "INCOMING"})
_MAX_MINOR = 9_007_199_254_740_991  # money.v1 MinorAmount
_TOP = frozenset({"label", "suite", "cases", "inputs_sha256"})
_TOP_OPTIONAL = frozenset({"canonical_locale_rule"})  # BO's note on how canonical_locale is chosen; text only
_SUITE = frozenset({"generator", "generator_version", "manifest_sha256", "seed", "tree_sha256"})
_CASE = frozenset({"case_id", "case_sha256", "channel", "locale_mix", "canonical_locale", "principal", "candidates", "utterances"})
_PRINCIPAL = frozenset({"principal_id", "roles"})
_WORLD = frozenset({"beneficiaries", "transactions"})
_BENEFICIARY = frozenset({"beneficiary_id", "name", "aliases", "account_last4"})
_TRANSACTION = frozenset({"ref", "direction", "amount_minor", "currency", "age_minutes", "beneficiary"})
_UTTERANCE = frozenset({"turn", "text"})
_FIXTURE_TOP = frozenset({"label", "inputs_sha256", "pack_ref", "cases"})
_FIXTURE_CASE = frozenset({"case_id", "canonical_input"})
_CANONICAL_INPUT = frozenset({"schema_version", "request_id", "correlation_id", "tenant_id", "session_id", "principal_ref", "channel", "mode", "text", "received_at"})  # canonical-input.v2: no hint


class RecordOnlyRefused(Exception):
    """The inputs file or its fixture is not one this run may use. `code` is the whole message."""

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
    canonical: Mapping[str, Any]  # the core's canonical input for the case, from the fixture
    candidates: tuple[Mapping[str, Any], ...]  # the rows of candidates-response.v1's Candidate, from the case's world rows


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


def _closed(value: Any, keys: frozenset[str], optional: frozenset[str] = frozenset()) -> dict[str, Any]:
    if not isinstance(value, dict) or not keys <= set(value) <= keys | optional:
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


def _principal(raw: Any) -> str:
    principal = _closed(raw, _PRINCIPAL)
    roles = principal["roles"]
    if not isinstance(principal["principal_id"], str) or _PRINCIPAL_ID.fullmatch(principal["principal_id"]) is None:
        _refuse("SHAPE")
    if not isinstance(roles, list) or not roles or any(not isinstance(r, str) or not r for r in roles):
        _refuse("SHAPE")
    return str(principal["principal_id"])


def _beneficiary(raw: Any) -> tuple[str, dict[str, Any]]:
    b = _closed(raw, _BENEFICIARY)
    aliases, name = b["aliases"], b["name"]
    if not isinstance(b["beneficiary_id"], str) or _BENEFICIARY_ID.fullmatch(b["beneficiary_id"]) is None or not isinstance(aliases, list) or any(not isinstance(a, str) for a in aliases):
        _refuse("SHAPE")
    if not isinstance(b["account_last4"], str) or _LAST4.fullmatch(b["account_last4"]) is None:
        _refuse("SHAPE")
    if not isinstance(name, str) or not 1 <= len(name) <= MAX_NAME or "\n" in name or "\r" in name or _DIGIT_RUN.search(name) is not None:
        _refuse("SHAPE")  # candidates-response.v1: a masked display of one line, no run of five digits
    return str(b["beneficiary_id"]), b


def _transaction(raw: Any, beneficiaries: Mapping[str, Any]) -> dict[str, Any]:
    t = _closed(raw, _TRANSACTION)
    amount, age = t["amount_minor"], t["age_minutes"]
    if not isinstance(t["ref"], str) or _RESOURCE_REF.fullmatch(t["ref"]) is None or t["direction"] not in _DIRECTIONS or t["currency"] != "NGN":
        _refuse("SHAPE")
    if not _is_int(amount) or not 0 <= amount <= _MAX_MINOR or not _is_int(age) or age < 0 or t["beneficiary"] not in beneficiaries:
        _refuse("SHAPE")
    return t


def _world(raw: Any) -> tuple[dict[str, Any], list[dict[str, Any]]]:
    world = _closed(raw, _WORLD)
    bens, txs = world["beneficiaries"], world["transactions"]
    if not isinstance(bens, list) or not isinstance(txs, list) or len(txs) > MAX_ROWS:
        _refuse("SHAPE")
    by_id: dict[str, Any] = {}
    for raw_b in bens:
        bid, b = _beneficiary(raw_b)
        if bid in by_id:
            _refuse("SHAPE")
        by_id[bid] = b
    rows = [_transaction(t, by_id) for t in txs]
    if len({t["ref"] for t in rows}) != len(rows):
        _refuse("SHAPE")
    return by_id, rows


def _instant(minutes_ago: int) -> str:
    return (datetime.strptime(RECEIVED_AT, "%Y-%m-%dT%H:%M:%SZ") - timedelta(minutes=minutes_ago)).strftime("%Y-%m-%dT%H:%M:%SZ")


_REF_ID = re.compile(r"[0-9A-Za-z]{2,32}")  # candidates-response.v1: candidate_id is `cand_` and 2 to 32 letters or digits


def _candidate_id(ref: str) -> str:
    """`cand_` and the id part of the resource reference (`fin.transaction:TX7992` gives `cand_TX7992`). An id part that is not 2 to 32 letters or digits, or that holds a run of five digits (a digit run
    over four never travels to a provider, AGENT-13), is not a candidate id: the file is refused, nothing is hashed or invented."""
    part = ref.split(":", 1)[1]
    if _REF_ID.fullmatch(part) is None or _DIGIT_RUN.search(part) is not None:
        _refuse("SHAPE")
    return "cand_" + part


def _rows(beneficiaries: Mapping[str, Any], transactions: list[dict[str, Any]]) -> tuple[dict[str, Any], ...]:
    out = []
    for t in transactions:
        b = beneficiaries[t["beneficiary"]]
        out.append({"candidate_id": _candidate_id(t["ref"]), "resource_ref": t["ref"], "amount_minor": t["amount_minor"], "currency": t["currency"], "occurred_at": _instant(t["age_minutes"]),
                    "counterparty_display": b["name"], "account_last4": b["account_last4"], "direction": t["direction"], "beneficiary_alias_match": len(b["aliases"]) > 0})
    if len({r["candidate_id"] for r in out}) != len(out):
        _refuse("SHAPE")  # two references with one id part
    by_id = sorted(out, key=lambda r: r["candidate_id"])
    return tuple(sorted(by_id, key=lambda r: r["occurred_at"], reverse=True))  # a stable sort: ties stay in id order


def candidate_rows(world: Any) -> tuple[dict[str, Any], ...]:
    """The candidates of a case (A439): the rows of `candidates-response.v1`'s Candidate that BO's world rows of the case stand for. ONE pure, deterministic function: it reads nothing but its argument and the
    goldens' fixed instant, and does not touch its argument. Raises RecordOnlyRefused (SHAPE) for a world that is not exactly BO's shape.
    - `candidate_id`: `cand_` and the id part of the transaction's reference (`fin.transaction:TX7992` gives `cand_TX7992`).
    - `resource_ref`, `amount_minor`, `currency`, `direction`: the transaction's own.
    - `occurred_at`: the fixed instant of the run (the goldens' synthetic afternoon, `RECEIVED_AT`) minus `age_minutes`, as RFC 3339 UTC.
    - `counterparty_display`: the beneficiary's name as the file gives it, the goldens' convention (a synthetic full name such as "Tunde Adeyemi"); the contract's mask is about digits, so a name with a run of five
      digits is refused, one line, at most 64 characters.
    - `account_last4`: the beneficiary's.
    - `beneficiary_alias_match`: true iff the beneficiary has at least one saved alias (the 'relationship' feature of the ranking reads it that way).
    Newest first (`occurred_at` descending), ties by `candidate_id` ascending, at most 20 rows. Nothing says which row is the target."""
    beneficiaries, transactions = _world(world)
    return _rows(beneficiaries, transactions)


def _case(raw: Any) -> tuple[RecordOnlyInput, str]:
    case = _closed(raw, _CASE)
    case_id, channel, locale_mix = case["case_id"], case["channel"], case["locale_mix"]
    _digest(case["case_sha256"])
    if not isinstance(case_id, str) or _CASE_ID.fullmatch(case_id) is None or channel != "text":
        _refuse("SHAPE")
    if not isinstance(locale_mix, list) or not locale_mix or any(not isinstance(x, str) or _LOCALE.fullmatch(x) is None for x in locale_mix):
        _refuse("SHAPE")
    if case["canonical_locale"] != CANONICAL_LOCALE:  # A460: en-NG for all five; null (a mix) or another locale is not a case of this pass
        _refuse("SHAPE")
    principal_id = _principal(case["principal"])
    candidates = candidate_rows(case["candidates"])
    text = _utterance(case["utterances"])
    return RecordOnlyInput(case_id, "text", tuple(locale_mix), CANONICAL_LOCALE, text, {}, candidates), principal_id


def _stored_digest(doc: dict[str, Any]) -> str:
    body = {k: v for k, v in doc.items() if k != "inputs_sha256"}
    return "sha256:" + hashlib.sha256(json.dumps(body, indent=2, sort_keys=True, ensure_ascii=False).encode("utf-8")).hexdigest()


def _read_json(path: Path, missing: str, bad: str) -> dict[str, Any]:
    if path.is_symlink() or not path.is_file():
        _refuse(missing)
    if path.stat().st_size > MAX_BYTES:
        _refuse("SIZE")
    try:
        doc = json.loads(path.read_bytes().decode("utf-8"), object_pairs_hook=_no_duplicates, parse_constant=_no_constant)
    except (UnicodeDecodeError, ValueError, RecursionError):
        _refuse(bad)
    if not isinstance(doc, dict):
        _refuse(bad)
    return doc


def _read_fixture(path: Path) -> dict[str, Any]:
    try:
        return _read_json(path, "FIXTURE", "FIXTURE")
    except RecordOnlyRefused:
        _refuse("FIXTURE")  # whatever is wrong with the fixture file is one code: a missing file, a symlink, a size, bad JSON


def _canonical(raw: Any, inp: RecordOnlyInput, principal_id: str) -> dict[str, Any]:
    """One canonical input of the fixture, held to the ten members of canonical-input.v2 and to its case: the text, the principal, the channel."""
    if not isinstance(raw, dict) or set(raw) != _CANONICAL_INPUT:
        _refuse("FIXTURE")
    if raw["schema_version"] != "2.0" or raw["channel"] != "text" or raw["mode"] != "MODE_1" or raw["text"] != inp.text or raw["principal_ref"] != principal_id:
        _refuse("FIXTURE")
    if not isinstance(raw["request_id"], str) or _UUID.fullmatch(raw["request_id"]) is None or not isinstance(raw["correlation_id"], str) or _CORRELATION.fullmatch(raw["correlation_id"]) is None:
        _refuse("FIXTURE")
    for key in ("tenant_id", "session_id", "received_at"):
        if not isinstance(raw[key], str) or not raw[key]:
            _refuse("FIXTURE")
    return dict(raw)


def load_inputs(path: Path, fixture: Path | None = None) -> tuple[RecordOnlyInput, ...]:
    """The inputs of the file, in its order, each with the canonical input of the fixture and its candidate rows. Raises RecordOnlyRefused for anything that is not exactly BO's file and the fixture the Go test
    wrote for it. The fixture is `g1_canonical_inputs.json` beside the inputs file unless a path is given."""
    doc = _read_json(path, "MISSING", "NOT_JSON")
    top = _closed(doc, _TOP, _TOP_OPTIONAL)
    suite = _closed(top["suite"], _SUITE)
    if not isinstance(top["label"], str) or not isinstance(suite["generator"], str) or not isinstance(suite["generator_version"], str) or not _is_int(suite["seed"]):
        _refuse("SHAPE")
    if "canonical_locale_rule" in top and not isinstance(top["canonical_locale_rule"], str):
        _refuse("SHAPE")
    _digest(suite["manifest_sha256"])
    _digest(suite["tree_sha256"])
    stored = _digest(top["inputs_sha256"])
    cases = top["cases"]
    if not isinstance(cases, list) or not 1 <= len(cases) <= MAX_CASES:
        _refuse("SHAPE")
    parsed = [_case(c) for c in cases]
    if len({i.case_id for i, _ in parsed}) != len(parsed):
        _refuse("SHAPE")
    if not hmac.compare_digest(stored, _stored_digest(top)):
        _refuse("DIGEST")
    fx = _read_fixture(fixture if fixture is not None else path.with_name(DEFAULT_FIXTURE.name))
    if set(fx) != _FIXTURE_TOP or not isinstance(fx["label"], str) or not isinstance(fx["pack_ref"], str) or _PACK_REF.fullmatch(fx["pack_ref"]) is None:
        _refuse("FIXTURE")
    pin = fx["inputs_sha256"]
    if not isinstance(pin, str) or _DIGEST.fullmatch(pin) is None:
        _refuse("FIXTURE")
    if not hmac.compare_digest(pin, stored):  # the fixture was built from other inputs than these
        _refuse("PIN")
    fx_cases = fx["cases"]
    if not isinstance(fx_cases, list) or len(fx_cases) != len(parsed):
        _refuse("FIXTURE")
    out: list[RecordOnlyInput] = []
    for (inp, principal_id), raw in zip(parsed, fx_cases, strict=True):
        if not isinstance(raw, dict) or set(raw) != _FIXTURE_CASE or raw["case_id"] != inp.case_id:
            _refuse("FIXTURE")
        canonical = _canonical(raw["canonical_input"], inp, principal_id)
        out.append(RecordOnlyInput(inp.case_id, inp.channel, inp.locale_mix, inp.canonical_locale, inp.text, canonical, inp.candidates))
    return tuple(out)
