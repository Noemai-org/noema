#!/usr/bin/env python3
"""Reference construction of the Record hash chain (PERD §32.1) and the generator of everything derived from it.

A reference check, not production code. Standard library only; written from the text of PERD §32.1 and
contracts/schemas/record-event.v2.json, not from the Go code, so the two cannot agree by sharing a mistake.

  python3 -I tools/record_chain_ref.py --check    regenerate in memory and compare with the files; exit 1 on any difference
  python3 -I tools/record_chain_ref.py --write    rewrite the generated files

Generated (and only here): the `record_event_chain_vectors` family of contracts/fixtures/golden_vectors.json, the
valid and invalid fixtures of contracts/fixtures/{valid,invalid}/record-event.v2/, and the reconstruction.v2 fixtures,
which are the v1 fixtures with their events replaced by a real v2 chain. Every hash is computed, none is typed in.
Nothing here uses a key: a Record event hash is a plain SHA-256 chain. The test-only signing key of
grant_signing_vectors is untouched; no existing golden vector depends on a record event or a void reason.

PERD §32.1: hash = SHA-256(prev_hash || JCS(event without hash)); the genesis event of each tenant chain carries
prev_hash = SHA-256("noema:genesis:" || tenant_id). Hashes are written `sha256:<hex>` (contracts/schemas/common.v1.json).
"""
import copy
import hashlib
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))  # -I drops the script directory from sys.path
import jcs_ref  # noqa: E402

ROOT = Path(__file__).resolve().parent.parent
FIXTURES = ROOT / "contracts" / "fixtures"
GOLDEN = FIXTURES / "golden_vectors.json"
SCHEMA_VERSION = "2.0"
EMPTY = "sha256:" + hashlib.sha256(b"{}").hexdigest()


def jcs(value) -> str:
    """RFC 8785 (tools/jcs_ref.py): members sorted by UTF-16 code unit, integers only. Every v2 payload is ASCII integers and strings."""
    return jcs_ref.jcs(value)


def sha256_text(text: str) -> str:
    return "sha256:" + hashlib.sha256(text.encode("utf-8")).hexdigest()


def genesis_hash(tenant_id: str) -> str:
    return sha256_text("noema:genesis:" + tenant_id)


def payload_hash(payload) -> str:
    """The hash of the referenced object's canonical JSON; an event that refers to no object carries the hash of {}."""
    return EMPTY if payload is None else sha256_text(jcs(payload))


def event_hash(prev_hash: str, body: dict) -> str:
    assert "hash" not in body
    return sha256_text(prev_hash + jcs(body))


def build_chain(tenant_id: str, correlation_id: str, specs: list[dict]) -> list[dict]:
    """specs: event_id, written_by, event_type, object_ref, at, optional actor, kind, reason, payload.
    Returns the events with seq, prev_hash, payload_hash and hash filled in, in order."""
    out, prev = [], genesis_hash(tenant_id)
    for i, s in enumerate(specs):
        body = {"schema_version": SCHEMA_VERSION, "tenant_id": tenant_id, "correlation_id": correlation_id, "event_id": s["event_id"], "seq": i + 1,
                "prev_hash": prev, "written_by": s["written_by"], "actor": s.get("actor", s["written_by"]), "event_type": s["event_type"],
                "object_ref": s["object_ref"], "payload_hash": payload_hash(s.get("payload")), "at": s["at"]}
        for optional in ("kind", "reason"):
            if optional in s:
                body[optional] = s[optional]
        h = event_hash(prev, body)
        out.append({**body, "hash": h})
        prev = h
    return out


VOID_REASONS = ["INTENT_SUPERSEDED", "STATE_CHANGED", "POLICY_SUPERSEDED", "SIGNATURE_INVALID", "EXPIRED", "BINDING_MISMATCH", "WRITES_DISABLED",
                "REVALIDATION_UNAVAILABLE", "REJECTED_STALE_VERSION"]

# ---- golden vectors ----------------------------------------------------------------------------------------------------

VECTOR_INPUTS = [
    ("single_event_empty_payload", "ten_demo", "corr_4c21f9",
     [{"event_id": "recev_2001", "written_by": "UNDERSTANDING", "event_type": "INPUT_RECEIVED", "object_ref": "req_01J9X3", "at": "2026-10-01T09:48:00.410Z"}]),
    ("execution_path", "ten_demo", "corr_4c21f9",
     [{"event_id": "recev_2101", "written_by": "EXECUTION", "event_type": "GRANT_SPENT", "object_ref": "grt_0001", "at": "2026-10-01T09:48:01.000Z",
       "payload": {"grant_id": "grt_0001", "nonce": "n_0001", "idempotency_key": "idk_v1_" + "ab" * 32, "attempt": 1}},
      {"event_id": "recev_2102", "written_by": "EXECUTION", "event_type": "DISPATCH_ATTEMPTED", "object_ref": "grt_0001", "at": "2026-10-01T09:48:01.001Z",
       "payload": {"idempotency_key": "idk_v1_" + "ab" * 32, "attempt": 1}},
      {"event_id": "recev_2103", "written_by": "EXECUTION", "actor": "EXECUTION:usr_88213", "event_type": "RECEIPT", "object_ref": "grt_0001",
       "at": "2026-10-01T09:48:01.350Z", "payload": {"receipt_id": "rcp_01", "execution_state": "ACKNOWLEDGED", "attempt": 1}}]),
    ("security_and_void", "ten_other", "corr_9d0e11",
     [{"event_id": "recev_3001", "written_by": "EXECUTION", "event_type": "SECURITY_EVENT", "kind": "IDEMPOTENCY_COLLISION", "object_ref": "grt_0002",
       "at": "2026-10-01T10:00:00.000Z", "payload": {"idempotency_key": "idk_v1_" + "cd" * 32}},
      {"event_id": "recev_3002", "written_by": "EXECUTION", "event_type": "GRANT_VOIDED", "reason": "BINDING_MISMATCH", "object_ref": "grt_0002",
       "at": "2026-10-01T10:00:00.002Z", "payload": {"grant_id": "grt_0002", "reason": "BINDING_MISMATCH"}}]),
]


def vectors() -> list[dict]:
    out = []
    for name, tenant, corr, specs in VECTOR_INPUTS:
        events = build_chain(tenant, corr, specs)
        payloads = [s.get("payload") for s in specs]
        out.append({"name": name, "tenant_id": tenant, "correlation_id": corr, "expected_genesis_hash": genesis_hash(tenant),
                    "events": [{"spec": s, "expected": {"seq": e["seq"], "prev_hash": e["prev_hash"], "payload_hash": e["payload_hash"], "hash": e["hash"]}}
                               for s, e in zip(specs, events)], "payloads_note": "payload is the object the event refers to; null means the hash of {}" if any(p is None for p in payloads) else ""})
    return out


# ---- record-event.v2 fixtures --------------------------------------------------------------------------------------------

def base_event() -> dict:
    return build_chain("ten_demo", "corr_4c21f9", [{"event_id": "recev_1001", "written_by": "TRUST", "event_type": "INTENT_ADMITTED", "object_ref": "int_01J9X3",
                                                   "at": "2026-10-01T09:47:50.100Z"}])[0]


def record_event_fixtures() -> tuple[dict[str, dict], dict[str, dict], dict[str, str]]:
    valid, invalid, expect = {}, {}, {}
    valid["genesis_event"] = base_event()
    second = build_chain("ten_demo", "corr_4c21f9", [
        {"event_id": "recev_1001", "written_by": "TRUST", "event_type": "INTENT_ADMITTED", "object_ref": "int_01J9X3", "at": "2026-10-01T09:47:50.100Z"},
        {"event_id": "recev_1002", "written_by": "RESOLUTION", "event_type": "CANDIDATES_RESOLVED", "object_ref": "cns_7A", "at": "2026-10-01T09:47:51.200Z"}])[1]
    valid["second_event"] = second
    valid["actor_with_principal"] = build_chain("ten_demo", "corr_4c21f9", [
        {"event_id": "recev_1003", "written_by": "EXECUTION", "actor": "EXECUTION:usr_88213", "event_type": "RECEIPT", "object_ref": "grt_0001",
         "at": "2026-10-01T09:48:00.000Z", "payload": {"receipt_id": "rcp_01"}}])[0]
    valid["security_event"] = build_chain("ten_demo", "corr_4c21f9", [
        {"event_id": "recev_1004", "written_by": "EXECUTION", "event_type": "SECURITY_EVENT", "kind": "IDEMPOTENCY_COLLISION", "object_ref": "grt_0001",
         "at": "2026-10-01T09:48:00.000Z"}])[0]
    for reason in VOID_REASONS:
        valid[f"grant_voided_{reason.lower()}"] = build_chain("ten_demo", "corr_4c21f9", [
            {"event_id": "recev_1010", "written_by": "EXECUTION", "event_type": "GRANT_VOIDED", "reason": reason, "object_ref": "grt_0001",
             "at": "2026-10-01T09:48:00.000Z", "payload": {"grant_id": "grt_0001", "reason": reason}}])[0]
    for reason in ("PROVIDER_ERROR", "TIMEOUT", "REFUSED_REPLY"):
        valid[f"provider_failover_{reason.lower()}"] = build_chain("ten_demo", "corr_4c21f9", [
            {"event_id": "recev_1020", "written_by": "UNDERSTANDING", "event_type": "PROVIDER_FAILOVER", "reason": reason, "object_ref": "int_01J9X3",
             "at": "2026-10-01T09:48:00.000Z"}])[0]

    void = valid["grant_voided_expired"]

    def bad(name, why, mutate, source=None):
        doc = copy.deepcopy(source or base_event())
        mutate(doc)
        invalid[name], expect[name] = doc, why

    bad("grant_voided_unknown_reason", "enum", lambda d: d.update(reason="SOMETHING_ELSE"), void)
    bad("grant_voided_reason_lower_case", "enum", lambda d: d.update(reason="expired"), void)
    bad("grant_voided_without_reason", "required", lambda d: d.pop("reason"), void)
    bad("grant_voided_with_failover_reason", "enum", lambda d: d.update(reason="TIMEOUT"), void)
    bad("reason_on_other_event", "not", lambda d: d.update(reason="EXPIRED"))
    bad("security_event_without_kind", "required", lambda d: d.update(event_type="SECURITY_EVENT"))
    bad("kind_on_other_event", "not", lambda d: d.update(kind="CHAIN_BREAK"))
    bad("unknown_event_type", "enum", lambda d: d.update(event_type="NOT_AN_EVENT"))
    bad("schema_version_is_v1", "pattern", lambda d: d.update(schema_version="1.0"))
    bad("seq_missing", "required", lambda d: d.pop("seq"))
    bad("seq_zero", "minimum", lambda d: d.update(seq=0))
    bad("seq_is_a_float", "type", lambda d: d.update(seq=1.0))
    bad("payload_hash_missing", "required", lambda d: d.pop("payload_hash"))
    bad("payload_hash_no_prefix", "pattern", lambda d: d.update(payload_hash="0" * 64))
    bad("actor_missing", "required", lambda d: d.pop("actor"))
    bad("actor_lower_case", "pattern", lambda d: d.update(actor="trust"))
    bad("actor_free_text", "pattern", lambda d: d.update(actor="the model said so"))
    bad("tenant_id_missing", "required", lambda d: d.pop("tenant_id"))
    bad("prev_hash_all_zero_is_the_v1_genesis", "not", lambda d: d.update(prev_hash="sha256:" + "0" * 64))
    bad("prev_hash_missing", "required", lambda d: d.pop("prev_hash"))
    bad("hash_all_zero", "not", lambda d: d.update(hash="sha256:" + "0" * 64))
    bad("free_text_field", "additionalProperties", lambda d: d.update(note="the model said so"))
    bad("id_with_sot_example_prefix", "pattern", lambda d: d.update(event_id="rec_1001"))
    bad("object_ref_is_free_text", "pattern", lambda d: d.update(object_ref="the model said so"))
    return valid, invalid, expect


# ---- reconstruction.v2 fixtures ------------------------------------------------------------------------------------------

def convert_reconstruction(doc: dict) -> dict:
    """A v1 reconstruction fixture with its events replaced by a real v2 chain (same event IDs, types, times, objects)."""
    doc = copy.deepcopy(doc)
    events = doc.get("events")
    if not isinstance(events, list):
        return doc
    tenant, corr = doc["tenant_id"], doc["correlation_id"]
    specs = [{"event_id": e["event_id"], "written_by": e["written_by"], "event_type": e["event_type"], "object_ref": e["object_ref"], "at": e["at"]} for e in events]
    doc["events"] = build_chain(tenant, corr, specs)
    if "chain" in doc:
        doc["chain"]["head_hash"] = doc["events"][-1]["hash"] if doc["events"] else genesis_hash(tenant)
    return doc


def reconstruction_fixtures() -> tuple[dict[str, dict], dict[str, dict], str]:
    valid = {p.stem: convert_reconstruction(json.loads(p.read_text())) for p in sorted((FIXTURES / "valid" / "reconstruction").glob("*.json"))}
    inv_dir = FIXTURES / "invalid" / "reconstruction"
    invalid = {p.stem: convert_reconstruction(json.loads(p.read_text())) for p in sorted(inv_dir.glob("*.json")) if p.name != "expectations.json"}
    return valid, invalid, (inv_dir / "expectations.json").read_text()


# ---- writing and checking ------------------------------------------------------------------------------------------------

def dump(doc) -> str:
    return json.dumps(doc, indent=2, ensure_ascii=False) + "\n"


GOLDEN_KEY = "record_event_chain_vectors"


def splice_golden(text: str) -> str:
    """golden_vectors.json with the record_event_chain_vectors family set to the current vectors. Only that family is touched:
    the other families keep their bytes, so a regeneration never reformats a vector somebody else owns."""
    return jcs_ref.splice_family(text, GOLDEN_KEY, vectors())


def outputs() -> dict[Path, str]:
    files: dict[Path, str] = {}
    files[GOLDEN] = splice_golden(GOLDEN.read_text(encoding="utf-8"))
    valid, invalid, expect = record_event_fixtures()
    for name, doc in valid.items():
        files[FIXTURES / "valid" / "record-event.v2" / f"{name}.json"] = dump(doc)
    for name, doc in invalid.items():
        files[FIXTURES / "invalid" / "record-event.v2" / f"{name}.json"] = dump(doc)
    files[FIXTURES / "invalid" / "record-event.v2" / "expectations.json"] = dump({f"{k}.json": v for k, v in sorted(expect.items())})
    rv, ri, rexp = reconstruction_fixtures()
    for name, doc in rv.items():
        files[FIXTURES / "valid" / "reconstruction.v2" / f"{name}.json"] = dump(doc)
    for name, doc in ri.items():
        files[FIXTURES / "invalid" / "reconstruction.v2" / f"{name}.json"] = dump(doc)
    files[FIXTURES / "invalid" / "reconstruction.v2" / "expectations.json"] = rexp
    return files


def main(argv: list[str]) -> int:
    if len(argv) != 1 or argv[0] not in ("--check", "--write"):
        print(__doc__)
        return 2
    files = outputs()
    if argv[0] == "--write":
        for path, text in files.items():
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text, encoding="utf-8")
        print(f"wrote {len(files)} files")
        return 0
    stale = [str(p.relative_to(ROOT)) for p, text in files.items() if not p.exists() or p.read_text(encoding="utf-8") != text]
    for s in stale:
        print("differs: " + s)
    print("record chain files are current" if not stale else f"{len(stale)} file(s) differ: run --write")
    return 1 if stale else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
