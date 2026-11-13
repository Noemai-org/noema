# Status · REC-01 append guard (Core Engineer 1) · the Record refuses a long digit run (A184)

Updated 2026-11-13. A184 (the Founder's instruction of 13 Nov, relayed; to be committed by the Steward) under A154: no payload string in the Record holds a run of more than four digits, except typed IDs by contract shape, RFC 3339 timestamps and number-typed fields; a CI scan of every test Record; a test that an `object_ref` is never payload-derived.

## State: built, in review (PR noema#536)

- `record/payloadguard`: the rule (pure, no database). `chain.Append` calls it on the canonical payload before anything is signed or written; a refusal writes nothing and names the JSON path and the run length, never the value.
- `internal/testpg`: every test database is scanned for the same rule, and for an `object_ref` built from a payload string, when its test ends (`ScanRecord`, run before the database is dropped). `AllowUnguardedRecord` exempts one database for a test that writes a long run on purpose.
- `internal/recordsig.AppendUnguarded`: appends like `chain.Append` minus the guard, for the one test that proves the reconstruction masks a long run (`TestServedFactsAreMaskedToTheLastFourDigits`) and for the scan's own tests.
- `inputgateway`: the input's events are named by the correlation ID the core minted, not by `req_` plus the client's request ID (the scan found it in 17 gateway tests). One commit of its own, for the Core Engineer 2's independent read.
- The exemptions beyond A184's three are narrow, bound to a path and a shape, and listed in the PR's CONFLICT block for the Architect.

## Session 2026-11-13 · REC-01 append guard

### Session summary
- What changed (PR links): noema#536 (red commit 9ce9d569, then the guard, the scan, the gateway fix).
- Why: A184, A154, REQ-AUD-003, REQ-SEC-003.
- Risks, and how they are contained: the guard can refuse a legitimate write (a receipt after the target acted). That is an unknown outcome (INV-07), which fails safe. The whole suite runs against it. The exemptions are path- and shape-bound, so a card number in a free-text field is still refused. A card number inside an all-digit client UUID, a typed ID, a `/target_ref` or an `/external_ack` token is NOT caught (the Architect's call, CONFLICT block).
- Tests added (REQ-AUD-003, REQ-SEC-003, REQ-AUD-001): `record/chain/payload_guard_test.go`, `payload_scan_test.go`; `record/payloadguard/guard_test.go` (incl. the drift test against `common.v1.json`); the object_ref assertion in `inputgateway/gateway_test.go`.
- Regression cases added: the three fixtures' nonces now have the contract's shape; the gateway's `object_ref`.
- Debt entries: none.
- Conflicts raised: the exemptions (Architect), the gateway change (Core Engineer 2 read).

### Pre-work for the next agent
- Exact next step: when a new event type carries a reference the target system minted, check the guard's two path-bound exemptions (`/external_ack`, `/target_ref`) before widening anything.
- Open questions (Architect): accept or replace the exemptions; the `IdempotencyKeyId` contract pattern does not fit `idk_v1_` plus 64 hex (a contract defect, not mine to change).
- Files to read first: `services/core/record/payloadguard/guard.go`, `services/core/internal/testpg/scan.go`.
- Do not touch: the limit (4), the exemption list, without the Architect.
