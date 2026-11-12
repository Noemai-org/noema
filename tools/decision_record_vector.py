#!/usr/bin/env python3
"""Test vector for the Decision Record v2 signature (rulings A5 and A13), computed independently of the Go code.

The signed bytes are: the ASCII label `noema/decision-record/v2`, one 0x00 byte, then the RFC 8785 (JCS) form of the record with `sig.value` removed
(`sig.key_id` stays). The signature is HMAC-SHA-256 over those bytes, written as lower-case hex. The key below is a TEST KEY, synthetic, and is not a
secret. `python3 -I tools/decision_record_vector.py --write` rewrites contracts/fixtures/decision_record_signing_vector.json; `--check` fails on a difference.
"""
import hashlib
import hmac
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import jcs_ref  # noqa: E402

ROOT = Path(__file__).resolve().parent.parent
OUT = ROOT / "contracts" / "fixtures" / "decision_record_signing_vector.json"
RECORD = ROOT / "contracts" / "fixtures" / "valid" / "decision-record.v2" / "allow.json"
LABEL = b"noema/decision-record/v2"
KEY_ID = "k2"
TEST_KEY = bytes([2]) * 32  # SYNTHETIC test key; the same bytes as k2 in services/core/trust/grants/grants_test.go


def vector() -> dict:
    record = json.loads(RECORD.read_text())
    record["sig"] = {"key_id": KEY_ID}  # the key_id is bound; the value is not part of what is signed
    body = jcs_ref.jcs(record).encode("utf-8")
    signed = LABEL + b"\x00" + body
    sig = hmac.new(TEST_KEY, signed, hashlib.sha256).hexdigest()
    return {
        "description": "Decision Record v2 signature (rulings A5, A13). SYNTHETIC test key. signed_bytes_hex = label || 0x00 || JCS(record without sig.value, key_id kept).",
        "record_fixture": "valid/decision-record.v2/allow.json",
        "key_id": KEY_ID,
        "test_key_hex": TEST_KEY.hex(),
        "label": LABEL.decode(),
        "jcs": body.decode("utf-8"),
        "signed_bytes_hex": signed.hex(),
        "signature_hex": sig,
    }


def main() -> int:
    text = json.dumps(vector(), indent=2, ensure_ascii=False) + "\n"
    if "--write" in sys.argv:
        OUT.write_text(text)
        print(f"wrote {OUT.relative_to(ROOT)}")
        return 0
    if OUT.read_text() != text:
        print("decision_record_signing_vector.json differs: run with --write")
        return 1
    print("decision record signing vector OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
