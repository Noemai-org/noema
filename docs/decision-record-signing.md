# Decision Record v2: the signed bytes

**Updated 6 Nov 2026 (Engineering Lead).** As-built documentation: it describes what the code does and defines nothing. The authority is the contract `contracts/schemas/decision-record.v2.json`, rulings A5 and A13 of 6 Nov 2026 (the records that carry them are Proposed until committed: docs#117 and the record for A13 to A23), and `services/core/trust/grants/record.go`. If this file and those differ, they win. Everything here is SYNTHETIC; the key is a test key and is not a secret.

## The rule (A13)

A Decision Record v2 is signed with **HMAC-SHA-256 under a grant key**. The bytes that are signed are exactly:

```
label || 0x00 || JCS(record with sig.value removed and sig.key_id kept)
```

- `label` is the ASCII string `noema/decision-record/v2` (24 bytes), with no trailing byte of its own.
- `0x00` is one zero byte. It is not a newline, and not the end of a C string that is hashed.
- `JCS(...)` is the RFC 8785 canonical form, as UTF-8, of the record as a JSON object with `sig.value` removed. `sig.key_id` **stays**, so the key that signed is part of what is signed and a signature cannot be re-attributed to another key. Numbers are integers only (a float is refused before anything is signed).
- The signature is the lower-case hex of the 32-byte HMAC and is stored in `sig.value`; `sig.key_id` names the grant key (`k2` in the vector below).

**Bound fields.** `intent_id`, `correlation_id`, `policy_version` (`policy_id@version`), `issued_at`, `expires_at` and the `key_id`. Every other field of the record is signed too, because the whole record is under the JCS. A record missing a bound field is invalid and `SignRecord` refuses it (`ErrRecordIncomplete`): so is one whose `expires_at` is not after `issued_at`. Only `schema_version` major 2 is signed (`ErrRecordVersion`).

**Checking.** `VerifyRecord` recomputes the bytes from the record as given and compares the HMAC in constant time; any failure is `ErrSignature` and says nothing about which field differed. `VerifyRecordAt(record, now)` verifies first and then refuses the record before `issued_at` and from `expires_at` on (`ErrRecordExpired`), on the simulated clock the caller supplies; a tampered record is always a signature failure, never an expiry. A `key_id` that is not an active key fails; during a rotation two keys are active and either verifies.

**Domain separation.** A grant and a confirmation are signed over the bare JCS bytes. A record signature is over label, `0x00` and JCS, so it cannot be a grant signature on the same bytes, and a grant signature cannot be a record signature (tests: the record signature does not verify over the bare JCS, over a newline separator, over a different label, or without the `key_id`).

**What there is not.** No `decision-record` v1 signature rule exists, so a v1 record is not signed and does not verify (`ErrRecordVersion` on signing; `ErrSignature` on verifying). That is a loud refusal, not a silent skip. No v1 record exists outside fixtures.

## Test vector

Computed by `tools/decision_record_vector.py` with the Python JCS reference (`tools/jcs_ref.py`), independently of the Go code; the Go test `TestTheSignatureMatchesTheIndependentVector` requires the same bytes, and `tools/contracts/test_decision_record_v2.py` fails if the vector file is not current. Record: `contracts/fixtures/valid/decision-record.v2/allow.json`. Key id `k2`, key (test only) `0202020202020202020202020202020202020202020202020202020202020202`.

JCS of the record (sig reduced to its key_id):

```
{"authority":{"business":"PASS","policy":"PASS","technical":"PASS"},"correlation_id":"corr_4c21f9","decision_id":"dec_8R4","evaluated_at":"2026-10-01T09:47:04.210Z","evidence_freshness":[{"age_seconds":42,"effective_limit_seconds":300,"limit_set_by":"TRUST_PROFILE","source":"fin.src.settlement"}],"evidence_snapshot_id":"evs_51A","expires_at":"2026-10-01T09:57:04.210Z","intent_hash":"sha256:9999999999999999999999999999999999999999999999999999999999999999","intent_id":"int_01J9X3","issued_at":"2026-10-01T09:47:04.210Z","outcome":"ALLOW","pack_version":"fintech@1.0.0","policy_version":"fin.pol@3.2","reason_codes":[],"schema_version":"2.0","sig":{"key_id":"k2"},"tenant_id":"ten_demo"}
```

Signed bytes (hex): label, `00`, then the JCS bytes above.

```
6e6f656d612f6465636973696f6e2d7265636f72642f7632007b22617574686f72697479223a7b22627573696e657373223a2250415353222c22706f6c696379223a2250415353222c22746563686e6963616c223a2250415353227d2c22636f7272656c6174696f6e5f6964223a22636f72725f346332316639222c226465636973696f6e5f6964223a226465635f385234222c226576616c75617465645f6174223a22323032362d31302d30315430393a34373a30342e3231305a222c2265766964656e63655f66726573686e657373223a5b7b226167655f7365636f6e6473223a34322c226566666563746976655f6c696d69745f7365636f6e6473223a3330302c226c696d69745f7365745f6279223a2254525553545f50524f46494c45222c22736f75726365223a2266696e2e7372632e736574746c656d656e74227d5d2c2265766964656e63655f736e617073686f745f6964223a226576735f353141222c22657870697265735f6174223a22323032362d31302d30315430393a35373a30342e3231305a222c22696e74656e745f68617368223a227368613235363a39393939393939393939393939393939393939393939393939393939393939393939393939393939393939393939393939393939393939393939393939393939222c22696e74656e745f6964223a22696e745f30314a395833222c226973737565645f6174223a22323032362d31302d30315430393a34373a30342e3231305a222c226f7574636f6d65223a22414c4c4f57222c227061636b5f76657273696f6e223a2266696e7465636840312e302e30222c22706f6c6963795f76657273696f6e223a2266696e2e706f6c40332e32222c22726561736f6e5f636f646573223a5b5d2c22736368656d615f76657273696f6e223a22322e30222c22736967223a7b226b65795f6964223a226b32227d2c2274656e616e745f6964223a2274656e5f64656d6f227d
```

Signature (HMAC-SHA-256, hex):

```
78b204fa7ee7bf8b837761480c73727bc972270046fba877b3afcf15705af9f1
```

To check it without the repository code:

```python
import hmac, hashlib, json
v = json.load(open("contracts/fixtures/decision_record_signing_vector.json"))
signed = bytes.fromhex(v["signed_bytes_hex"])
assert signed.startswith(b"noema/decision-record/v2\x00")
assert hmac.new(bytes.fromhex(v["test_key_hex"]), signed, hashlib.sha256).hexdigest() == v["signature_hex"]
```

## Not covered here

The writer that builds the record from a Decision and persists it with the grants in one transaction is not built (it is blocked on the evidence-freshness fields of `decide.Basis`; see `status/TRU-05.md`). Key loading and `KEY_ROTATED` are not built either.
