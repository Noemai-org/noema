#!/usr/bin/env python3
"""Reference construction of the v3 Record hash chain (PERD v1.17 §32.1) and the generator of everything derived from it.

A reference check, not production code. Standard library only; written from the text of PERD v1.17 §32.1 and
contracts/schemas/record-event.v3.json, not from the Go code, so the two cannot agree by sharing a mistake.

  python3 -I tools/record_chain_ref_v3.py --check    regenerate in memory and compare with the files; exit 1 on any difference
  python3 -I tools/record_chain_ref_v3.py --write    rewrite the generated files

Generated (and only here): the `record_event_chain_vectors_v3` family of contracts/fixtures/golden_vectors.json, the valid and
invalid fixtures of contracts/fixtures/{valid,invalid}/record-event.v3/, and the reconstruction.v3 fixtures (the v1 fixtures with their
events replaced by a real v3 chain). Every hash is computed, none is typed in. The `payload_hash_vectors` family is NOT generated here:
its expected values come from an independent implementation (tools/payload_hash_independent.js) and are only checked by this script.
v1 and v2 files are never touched; tools/record_chain_ref.py still generates the v2 files.

PERD v1.17 §32.1: hash = SHA-256(prev_hash || JCS(event without hash)); the genesis event of each tenant chain carries
prev_hash = SHA-256("noema:genesis:" || tenant_id); payload_hash = SHA-256(JCS(payload)), integers only.
"""
import copy
import hashlib
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))  # -I drops the script directory from sys.path
import jcs_ref  # noqa: E402
from jcs_ref import jcs, sha256_text  # noqa: E402

ROOT = Path(__file__).resolve().parent.parent
FIXTURES = ROOT / "contracts" / "fixtures"
GOLDEN = FIXTURES / "golden_vectors.json"
SCHEMA_VERSION = "3.0"
EMPTY = "sha256:" + hashlib.sha256(b"{}").hexdigest()

# PERD v1.17 §32.1. Emitters: the identities of the §41.1 table, and the §34.1 logical services whose adapters carry a per-adapter
# identity (REQ-ENV-001). Principal kinds: §7, REQ-SEC-001, REQ-AUD-003, §38.2, §41.1; the Founder separated regulator from auditor.
EMITTERS = ["core-edge", "core-trust", "core-execution", "core-outcome", "core-record", "core-evidence", "core-workflows", "model-service",
            "benchmark-runner", "admin", "mock-ledger", "mock-switch", "mock-destination", "mock-customer", "mock-policy", "mock-case", "mock-clock"]
KINDS = ["customer", "operator", "approver", "auditor", "regulator", "admin"]
VOID_REASONS = ["INTENT_SUPERSEDED", "STATE_CHANGED", "POLICY_SUPERSEDED", "SIGNATURE_INVALID", "EXPIRED", "BINDING_MISMATCH", "WRITES_DISABLED",
                "REVALIDATION_UNAVAILABLE", "REJECTED_STALE_VERSION"]


def genesis_hash(tenant_id: str) -> str:
    return sha256_text("noema:genesis:" + tenant_id)


def payload_hash(payload) -> str:
    """SHA-256(JCS(payload)); an event that refers to no object has the payload {}."""
    return sha256_text(jcs({} if payload is None else payload))


def event_hash(prev_hash: str, body: dict) -> str:
    assert "hash" not in body
    return sha256_text(prev_hash + jcs(body))


def build_chain(tenant_id: str, correlation_id: str, specs: list[dict]) -> list[dict]:
    """specs: event_id, written_by, event_type, object_ref, at, optional actor (default: the emitter), kind, reason, payload.
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


# ---- golden vectors: the chain ------------------------------------------------------------------------------------------

VECTOR_INPUTS = [
    ("single_event_empty_payload", "ten_demo", "corr_4c21f9",
     [{"event_id": "recev_2001", "written_by": "core-edge", "actor": "customer:usr_88213", "event_type": "INPUT_RECEIVED", "object_ref": "req_01J9X3",
       "at": "2026-10-01T09:48:00.410Z"}]),
    ("execution_path", "ten_demo", "corr_4c21f9",
     [{"event_id": "recev_2101", "written_by": "core-execution", "event_type": "GRANT_SPENT", "object_ref": "grt_0001", "at": "2026-10-01T09:48:01.000Z",
       "payload": {"grant_id": "grt_0001", "nonce": "n_0001", "idempotency_key": "idk_v1_" + "ab" * 32, "attempt": 1}},
      {"event_id": "recev_2102", "written_by": "core-execution", "event_type": "DISPATCH_ATTEMPTED", "object_ref": "grt_0001", "at": "2026-10-01T09:48:01.001Z",
       "payload": {"idempotency_key": "idk_v1_" + "ab" * 32, "attempt": 1}},
      {"event_id": "recev_2103", "written_by": "core-execution", "event_type": "RECEIPT", "object_ref": "grt_0001",
       "at": "2026-10-01T09:48:01.350Z", "payload": {"receipt_id": "rcp_01", "execution_state": "ACKNOWLEDGED", "attempt": 1}}]),
    ("security_and_void", "ten_other", "corr_9d0e11",
     [{"event_id": "recev_3001", "written_by": "core-execution", "event_type": "SECURITY_EVENT", "kind": "IDEMPOTENCY_COLLISION", "object_ref": "grt_0002",
       "at": "2026-10-01T10:00:00.000Z", "payload": {"idempotency_key": "idk_v1_" + "cd" * 32}},
      {"event_id": "recev_3002", "written_by": "core-execution", "event_type": "GRANT_VOIDED", "reason": "BINDING_MISMATCH", "object_ref": "grt_0002",
       "at": "2026-10-01T10:00:00.002Z", "payload": {"grant_id": "grt_0002", "reason": "BINDING_MISMATCH"}}]),
    ("approver_confirms_control_plane_publishes", "ten_demo", "corr_77aa01",
     [{"event_id": "recev_4001", "written_by": "core-trust", "actor": "approver:usr_synth_014", "event_type": "CONFIRMATION_RECEIVED", "object_ref": "cnf_0001",
       "at": "2026-10-01T11:00:00.000Z", "payload": {"response": "ACCEPT"}},
      {"event_id": "recev_4002", "written_by": "admin", "actor": "admin:usr_admin_01", "event_type": "POLICY_PUBLISHED", "object_ref": "pol_0001",
       "at": "2026-10-01T11:00:05.000Z", "payload": {"policy_id": "pol_escalate", "version": 3}}]),
]


def vectors() -> list[dict]:
    out = []
    for name, tenant, corr, specs in VECTOR_INPUTS:
        events = build_chain(tenant, corr, specs)
        out.append({"name": name, "tenant_id": tenant, "correlation_id": corr, "expected_genesis_hash": genesis_hash(tenant),
                    "events": [{"spec": s, "expected": {"seq": e["seq"], "prev_hash": e["prev_hash"], "actor": e["actor"], "payload_hash": e["payload_hash"],
                                                        "hash": e["hash"]}} for s, e in zip(specs, events)]})
    return out


# ---- record-event.v3 fixtures --------------------------------------------------------------------------------------------

def one(written_by="core-trust", actor=None, event_type="INTENT_ADMITTED", object_ref="int_01J9X3", **extra) -> dict:
    spec = {"event_id": "recev_1001", "written_by": written_by, "event_type": event_type, "object_ref": object_ref, "at": "2026-10-01T09:47:50.100Z", **extra}
    if actor is not None:
        spec["actor"] = actor
    return build_chain("ten_demo", "corr_4c21f9", [spec])[0]


def record_event_fixtures() -> tuple[dict[str, dict], dict[str, dict], dict[str, str]]:
    valid, invalid, expect = {}, {}, {}
    valid["genesis_event"] = one()
    valid["second_event"] = build_chain("ten_demo", "corr_4c21f9", [
        {"event_id": "recev_1001", "written_by": "core-trust", "event_type": "INTENT_ADMITTED", "object_ref": "int_01J9X3", "at": "2026-10-01T09:47:50.100Z"},
        {"event_id": "recev_1002", "written_by": "core-edge", "event_type": "CANDIDATES_RESOLVED", "object_ref": "cns_7A", "at": "2026-10-01T09:47:51.200Z"}])[1]
    for emitter in EMITTERS:
        valid[f"emitter_{emitter.replace('-', '_')}"] = one(written_by=emitter)
    for kind in KINDS:
        valid[f"principal_{kind}"] = one(written_by="admin" if kind == "admin" else "core-trust", actor=f"{kind}:usr_synth_014", payload={"by": kind})
    valid["principal_id_with_dot_and_dash"] = one(actor="customer:a.b-C_9")
    valid["principal_id_64_characters"] = one(actor="operator:" + "a" * 64)
    valid["payload_with_integers"] = one(payload={"n": [0, 1, -7, 9007199254740991, -9007199254740991]})
    valid["security_event"] = one("core-execution", event_type="SECURITY_EVENT", kind="IDEMPOTENCY_COLLISION", object_ref="grt_0001")
    for reason in VOID_REASONS:
        valid[f"grant_voided_{reason.lower()}"] = one("core-execution", event_type="GRANT_VOIDED", reason=reason, object_ref="grt_0001",
                                                      payload={"grant_id": "grt_0001", "reason": reason})
    for reason in ("PROVIDER_ERROR", "TIMEOUT", "REFUSED_REPLY"):
        valid[f"provider_failover_{reason.lower()}"] = one("core-edge", event_type="PROVIDER_FAILOVER", reason=reason)

    void = valid["grant_voided_expired"]

    def bad(name, why, mutate, source=None):
        doc = copy.deepcopy(source or valid["genesis_event"])
        mutate(doc)
        invalid[name], expect[name] = doc, why

    # the void-reason, event-type and shape cases of v2 hold in v3 too
    bad("grant_voided_unknown_reason", "enum", lambda d: d.update(reason="SOMETHING_ELSE"), void)
    bad("grant_voided_reason_lower_case", "enum", lambda d: d.update(reason="expired"), void)
    bad("grant_voided_without_reason", "required", lambda d: d.pop("reason"), void)
    bad("grant_voided_with_failover_reason", "enum", lambda d: d.update(reason="TIMEOUT"), void)
    bad("reason_on_other_event", "not", lambda d: d.update(reason="EXPIRED"))
    bad("security_event_without_kind", "required", lambda d: d.update(event_type="SECURITY_EVENT"))
    bad("kind_on_other_event", "not", lambda d: d.update(kind="CHAIN_BREAK"))
    bad("unknown_event_type", "enum", lambda d: d.update(event_type="NOT_AN_EVENT"))
    bad("schema_version_is_v2", "pattern", lambda d: d.update(schema_version="2.0"))
    bad("schema_version_is_v1", "pattern", lambda d: d.update(schema_version="1.0"))
    bad("seq_missing", "required", lambda d: d.pop("seq"))
    bad("seq_zero", "minimum", lambda d: d.update(seq=0))
    bad("seq_is_a_float", "type", lambda d: d.update(seq=1.0))
    bad("payload_hash_missing", "required", lambda d: d.pop("payload_hash"))
    bad("payload_hash_no_prefix", "pattern", lambda d: d.update(payload_hash="0" * 64))
    bad("payload_hash_upper_case", "pattern", lambda d: d.update(payload_hash="sha256:" + "A" * 64))
    bad("tenant_id_missing", "required", lambda d: d.pop("tenant_id"))
    bad("prev_hash_all_zero_is_the_v1_genesis", "not", lambda d: d.update(prev_hash="sha256:" + "0" * 64))
    bad("prev_hash_missing", "required", lambda d: d.pop("prev_hash"))
    bad("hash_all_zero", "not", lambda d: d.update(hash="sha256:" + "0" * 64))
    bad("free_text_field", "additionalProperties", lambda d: d.update(note="the model said so"))
    bad("id_with_sot_example_prefix", "pattern", lambda d: d.update(event_id="rec_1001"))
    bad("object_ref_is_free_text", "pattern", lambda d: d.update(object_ref="the model said so"))
    # the emitter: a closed set of service identities
    bad("written_by_is_a_power_in_upper_case", "enum", lambda d: d.update(written_by="TRUST", actor="TRUST"))
    bad("written_by_unknown_identity", "enum", lambda d: d.update(written_by="core-understanding", actor="core-understanding"))
    bad("written_by_fault_controller_is_benchmark_only", "enum", lambda d: d.update(written_by="fault-controller", actor="fault-controller"))
    bad("written_by_fixture_loader_is_benchmark_only", "enum", lambda d: d.update(written_by="fixture-loader", actor="fixture-loader"))
    bad("written_by_is_a_principal_kind", "enum", lambda d: d.update(written_by="customer"))
    bad("written_by_missing", "required", lambda d: d.pop("written_by"))
    # the principal: required, a closed kind and an identifier, or the emitter's own identity
    bad("actor_missing", "required", lambda d: d.pop("actor"))
    bad("actor_is_the_v2_power_form", "oneOf", lambda d: d.update(actor="TRUST"))
    bad("actor_is_the_v2_power_with_principal", "oneOf", lambda d: d.update(actor="EXECUTION:usr_88213"))
    bad("actor_kind_service_is_not_a_principal_kind", "oneOf", lambda d: d.update(actor="service:core-trust"))
    bad("actor_unknown_kind", "oneOf", lambda d: d.update(actor="guest:usr_1"))
    bad("actor_kind_upper_case", "oneOf", lambda d: d.update(actor="CUSTOMER:usr_1"))
    bad("actor_empty_id", "oneOf", lambda d: d.update(actor="customer:"))
    bad("actor_id_with_colon", "oneOf", lambda d: d.update(actor="customer:a:b"))
    bad("actor_id_starts_with_dash", "oneOf", lambda d: d.update(actor="customer:-a"))
    bad("actor_id_too_long", "oneOf", lambda d: d.update(actor="customer:" + "a" * 65))
    bad("actor_id_with_space", "oneOf", lambda d: d.update(actor="customer:usr 1"))
    bad("actor_free_text", "oneOf", lambda d: d.update(actor="the model said so"))
    bad("actor_other_service_than_the_emitter", "const", lambda d: d.update(actor="core-execution"))
    bad("actor_unknown_service", "oneOf", lambda d: d.update(actor="core-understanding"))
    bad("actor_null", "oneOf", lambda d: d.update(actor=None))
    return valid, invalid, expect


# ---- reconstruction.v3 fixtures ------------------------------------------------------------------------------------------

V1_WRITER = {"TRUST": "core-trust", "RESOLUTION": "core-evidence", "EXECUTION": "core-execution", "OUTCOME": "core-outcome"}


def convert_reconstruction(doc: dict) -> dict:
    """A v1 reconstruction fixture with its events replaced by a real v3 chain (same event IDs, types, times, objects)."""
    doc = copy.deepcopy(doc)
    events = doc.get("events")
    if not isinstance(events, list):
        return doc
    tenant, corr = doc["tenant_id"], doc["correlation_id"]
    specs = [{"event_id": e["event_id"], "written_by": V1_WRITER[e["written_by"]], "event_type": e["event_type"], "object_ref": e["object_ref"], "at": e["at"]}
             for e in events]
    doc["events"] = build_chain(tenant, corr, specs)
    if "chain" in doc:
        doc["chain"]["head_hash"] = doc["events"][-1]["hash"] if doc["events"] else genesis_hash(tenant)
    return doc


def reconstruction_fixtures() -> tuple[dict[str, dict], dict[str, dict], str]:
    valid = {p.stem: convert_reconstruction(json.loads(p.read_text())) for p in sorted((FIXTURES / "valid" / "reconstruction").glob("*.json"))}
    inv_dir = FIXTURES / "invalid" / "reconstruction"
    invalid = {p.stem: convert_reconstruction(json.loads(p.read_text())) for p in sorted(inv_dir.glob("*.json")) if p.name != "expectations.json"}
    return valid, invalid, (inv_dir / "expectations.json").read_text()


# ---- the payload-hash vectors: checked here, produced by an independent implementation ---------------------------------

PAYLOAD_KEY = "payload_hash_vectors"
REJECTED_KEY = "payload_rejected_vectors"


def check_payload_vectors(golden: dict) -> list[str]:
    """Each recorded vector must agree with this implementation: the same canonical bytes, and the SHA-256 of the recorded bytes.
    The recorded values were produced by tools/payload_hash_independent.js and are never written by this script."""
    problems = []
    for v in golden.get(PAYLOAD_KEY, []):
        payload = json.loads(v["payload_json"])
        mine = jcs(payload)
        if mine != v["input_bytes_utf8"]:
            problems.append(f"{v['name']}: canonical text {mine!r} differs from the recorded {v['input_bytes_utf8']!r}")
        if mine.encode("utf-8").hex() != v["input_bytes_hex"]:
            problems.append(f"{v['name']}: canonical bytes differ from the recorded bytes")
        if hashlib.sha256(bytes.fromhex(v["input_bytes_hex"])).hexdigest() != v["expected_hash"].removeprefix("sha256:"):
            problems.append(f"{v['name']}: the recorded hash is not the SHA-256 of the recorded bytes")
        if payload_hash(payload) != v["expected_hash"]:
            problems.append(f"{v['name']}: payload_hash differs from the recorded hash")
    for v in golden.get(REJECTED_KEY, []):
        try:
            jcs(json.loads(v["payload_json"]))
            problems.append(f"{v['name']}: a payload with a non-integer number must be rejected")
        except jcs_ref.NotCanonical:
            pass
    if not golden.get(PAYLOAD_KEY):
        problems.append("golden_vectors.json has no payload_hash_vectors")
    return problems


# ---- writing and checking ------------------------------------------------------------------------------------------------

def dump(doc) -> str:
    return json.dumps(doc, indent=2, ensure_ascii=False) + "\n"


GOLDEN_KEY = "record_event_chain_vectors_v3"


def outputs() -> dict[Path, str]:
    files: dict[Path, str] = {}
    files[GOLDEN] = jcs_ref.splice_family(GOLDEN.read_text(encoding="utf-8"), GOLDEN_KEY, vectors())
    valid, invalid, expect = record_event_fixtures()
    for name, doc in valid.items():
        files[FIXTURES / "valid" / "record-event.v3" / f"{name}.json"] = dump(doc)
    for name, doc in invalid.items():
        files[FIXTURES / "invalid" / "record-event.v3" / f"{name}.json"] = dump(doc)
    files[FIXTURES / "invalid" / "record-event.v3" / "expectations.json"] = dump({f"{k}.json": v for k, v in sorted(expect.items())})
    rv, ri, rexp = reconstruction_fixtures()
    for name, doc in rv.items():
        files[FIXTURES / "valid" / "reconstruction.v3" / f"{name}.json"] = dump(doc)
    for name, doc in ri.items():
        files[FIXTURES / "invalid" / "reconstruction.v3" / f"{name}.json"] = dump(doc)
    files[FIXTURES / "invalid" / "reconstruction.v3" / "expectations.json"] = rexp
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
    problems = check_payload_vectors(json.loads(GOLDEN.read_text(encoding="utf-8")))
    for p in problems:
        print("payload vector: " + p)
    ok = not stale and not problems
    print("record chain v3 files are current" if ok else f"{len(stale)} file(s) differ, {len(problems)} payload vector problem(s): run --write")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
