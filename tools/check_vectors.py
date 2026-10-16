#!/usr/bin/env python3
"""Reference check for the golden vectors in contracts/fixtures/golden_vectors.json
(DEC-2026-10-16 contract and pack clarifications C1; PERD §21.3 and §27.2).

A check, not production code. Standard library only. The evidence_set_hash and
idempotency_key constructions were written from the text of C1 (steps 1 to 9) and
PERD §27.2, not from the fixture; the other families follow contracts/fixtures/README.md.
It does not replace the Go and Python implementations that task CON-04 requires.

Run: python3 -I tools/check_vectors.py [path/to/golden_vectors.json]
Exit 0 when every vector matches; exit 1 otherwise.
"""
import hashlib
import hmac
import json
import re
import struct
import sys

HASH_RE = re.compile(r"^sha256:[0-9a-f]{64}$")


def lp(text: str) -> bytes:
    """4-byte big-endian length, then the UTF-8 bytes."""
    raw = text.encode("utf-8")
    return struct.pack(">I", len(raw)) + raw


def evidence_set_hash(items, sort_key="evidence_id"):
    """C1. `sort_key` exists only so the wrong-sort check can be shown."""
    seen = set()
    for it in items:
        eid, ph = it["evidence_id"], it["payload_hash"]
        if eid == "":
            raise ValueError("empty evidence_id")
        if eid in seen:
            raise ValueError("duplicate evidence_id: " + eid)
        seen.add(eid)
        if not HASH_RE.match(ph):
            raise ValueError("payload_hash is not sha256:<64 lowercase hex>: " + ph)
    ordered = sorted(items, key=lambda it: it[sort_key].encode("utf-8"))
    body = b"".join(lp(it["evidence_id"]) + lp(it["payload_hash"]) for it in ordered)
    return "sha256:" + hashlib.sha256(body).hexdigest()


def jcs(value) -> str:
    """Canonical JSON (RFC 8785) for the value types the fixtures use: objects, arrays, strings, integers,
    booleans and null. Keys sort by code point, which equals UTF-16 order for every key in the fixtures."""
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False)


def sha256_of(text: str) -> str:
    return "sha256:" + hashlib.sha256(text.encode("utf-8")).hexdigest()


def idempotency_key(tenant_id, principal_id, transaction_id, logical_action,
                    reason_code, policy_scope, logical_resolution_epoch):
    parts = (tenant_id, principal_id, transaction_id, logical_action,
             reason_code, policy_scope, logical_resolution_epoch)
    return "idk_v1_" + hashlib.sha256(b"".join(lp(p) for p in parts)).hexdigest()


ZERO = "sha256:" + "0" * 64
FFFF = "sha256:" + "f" * 64
# Expected values copied from DEC-2026-10-16 C1.
C1 = [
    ("empty_evidence_set", [],
     "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"),
    ("unsorted_evidence_items", [
        {"evidence_id": "evd_03_transaction", "payload_hash": "sha256:" + "cccc333322221111" * 4},
        {"evidence_id": "evd_01_account", "payload_hash": "sha256:" + "aaaa111122223333" * 4},
        {"evidence_id": "evd_02_kyc", "payload_hash": "sha256:" + "bbbb222233334444" * 4}],
     "sha256:85f7407496250253e53b326daa8e96960b8a338eaf8c04782f0d0507a648b48c"),
    ("single_item", [{"evidence_id": "evd_01_account", "payload_hash": ZERO}],
     "sha256:47dfb81eb6a8175a50e80698b3fa944350b08eeb301d48ecb2fa7651cca67f3b"),
    ("sort_key_is_evidence_id", [
        {"evidence_id": "evd_02_switch", "payload_hash": ZERO},
        {"evidence_id": "evd_01_ledger", "payload_hash": FFFF}],
     "sha256:15fd16c042481a7a3a366578ecc90be5eca5e255cd1d8d47ea68b33f97398177"),
]
WRONG_SORT_EXPECTED = "sha256:98bbe9364a2cd31858d6bd63773c9b576d061ad0f04ee4c32aec4df9b0f9b93f"
NEGATIVE = [
    ("duplicate_evidence_id", [{"evidence_id": "evd_01", "payload_hash": ZERO},
                               {"evidence_id": "evd_01", "payload_hash": FFFF}]),
    ("payload_hash_uppercase_hex", [{"evidence_id": "evd_01", "payload_hash": "sha256:" + "A" * 64}]),
    ("payload_hash_no_prefix", [{"evidence_id": "evd_01", "payload_hash": "0" * 64}]),
    ("payload_hash_63_hex", [{"evidence_id": "evd_01", "payload_hash": "sha256:" + "0" * 63}]),
    ("empty_evidence_id", [{"evidence_id": "", "payload_hash": ZERO}]),
]


def main(path):
    ok = True

    def report(name, got, want):
        nonlocal ok
        good = got == want
        ok &= good
        print(("PASS " if good else "FAIL ") + name + "\n     got  " + str(got) + ("" if good else "\n     want " + str(want)))

    for name, items, want in C1:
        report("C1 " + name, evidence_set_hash(items), want)
    # Wrong sort key: sorting the fourth vector by payload_hash must differ, and give the stated value.
    wrong = evidence_set_hash(C1[3][1], sort_key="payload_hash")
    report("wrong sort key (payload_hash) gives the stated value", wrong, WRONG_SORT_EXPECTED)
    report("wrong sort key differs from the correct hash", wrong != C1[3][2], True)
    # Reordering inputs never changes the hash.
    report("reordering inputs leaves the hash unchanged",
           evidence_set_hash(list(reversed(C1[1][1]))), C1[1][2])
    for name, items in NEGATIVE:
        try:
            evidence_set_hash(items)
            report("negative " + name + " is rejected", "accepted", "rejected")
        except ValueError as e:
            report("negative " + name + " is rejected", "rejected", "rejected")
            print("     reason: " + str(e))
    if path:
        fx = json.load(open(path, encoding="utf-8"))
        for v in fx["jcs_vectors"]:
            report("fixture jcs " + v["name"], (jcs(v["input"]), sha256_of(jcs(v["input"]))),
                   (v["expected_jcs"], v["expected_sha256"]))
        for v in fx["intent_hash_vectors"]:
            report("fixture intent_hash " + v["name"], sha256_of(jcs(v["input"])), v["expected_intent_hash"])
        for v in fx["plan_hash_vectors"]:
            report("fixture plan_hash " + v["name"], sha256_of(jcs(v["input"])), v["expected_plan_hash"])
        for v in fx["evidence_set_hash_vectors"]:
            report("fixture evidence_set_hash " + v["name"], evidence_set_hash(v["input"]), v["expected_evidence_set_hash"])
        for v in fx.get("evidence_set_hash_negative_vectors", []):
            try:
                evidence_set_hash(v["input"])
                report("fixture negative " + v["name"] + " is rejected", "accepted", "rejected")
            except ValueError:
                report("fixture negative " + v["name"] + " is rejected", "rejected", "rejected")
        for v in fx["binding_hash_vectors"]:
            report("fixture binding_hash " + v["name"], (jcs(v["input"]), sha256_of(jcs(v["input"]))),
                   (v["expected_jcs"], v["expected_binding_hash"]))
        for v in fx["idempotency_key_vectors"]:
            report("fixture idempotency " + v["name"], idempotency_key(**v["input"]), v["expected_idempotency_key"])
        for v in fx["grant_signing_vectors"]:
            text = jcs(v["grant"])
            sig = hmac.new(bytes.fromhex(v["secret_hex"]), text.encode("utf-8"), hashlib.sha256).hexdigest()
            report("fixture grant_signing " + v["name"], (text, v["key_id"], sig),
                   (v["expected_jcs"], v["expected_sig"]["key_id"], v["expected_sig"]["value"]))
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1] if len(sys.argv) > 1 else None))
