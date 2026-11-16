# Status · REC-01 append guard (Core Engineer 1) · the Record refuses a long digit run (A184)

Updated 2026-11-13. A184 (the Founder's instruction of 13 Nov, relayed; to be committed by the Steward) under A154: no payload string in the Record holds a run of more than four digits, except typed IDs by contract shape, RFC 3339 timestamps and number-typed fields; a CI scan of every test Record; a test that an `object_ref` is never payload-derived.

## State: built, in review (PR noema#536)

- `record/payloadguard`: the rule (pure, no database). `chain.Append` calls it on the canonical payload before anything is signed or written; a refusal writes nothing and names the JSON path and the run length, never the value.
- `internal/testpg`: every test database is scanned for the same rule, and for an `object_ref` built from a payload string, when its test ends (`ScanRecord`, run before the database is dropped). `AllowUnguardedRecord` exempts one database for a test that writes a long run on purpose.
- `internal/recordsig.AppendUnguarded`: appends like `chain.Append` minus the guard, for the one test that proves the reconstruction masks a long run (`TestServedFactsAreMaskedToTheLastFourDigits`) and for the scan's own tests.
- `inputgateway`: the input's events are named by the correlation ID the core minted, not by `req_` plus the client's request ID (the scan found it in 15 gateway tests). One commit of its own, for the Core Engineer 2's independent read.
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

## Session 2026-11-14 · REC-01 append guard: the Security Reviewer's round on noema#536 (373c4722)
- Summary: the exemptions are structural (a typed ID, a hash, a nonce, a UUID or a signature needs a letter in its body, so a bare run of digits in a typed shape is refused; `/external_ack` and `/target_ref` accept one token of at most 64 characters with no space, exactly as ruling A193 accepts them for the POC (A201: the stricter fix I had written is dropped; DEBT-30 is the safeguard); the algorithm name is exempt only as a key ID at `/key_id`, no longer removed from any string); object keys are held to the same rule and never quoted in an error; combining marks and format characters no longer split a number; numbers are not examined (A184, now stated in the code and a test); `AppendUnguarded` and `testpg` may not be imported by production code (a test, until an arch-lint rule exists). Commits `be2d2dbd` and the one after it, both held (no push until noema#531 merges, A193).
- Not enforced in the database: the guard is in `chain.Appender.Append` only. Stated in `guard.go`; the PR body no longer implies otherwise. Waiting on the Architect: database enforcement as well, or "application only" in the ruling.
- Measured, no cap set: Check of a 318 KB payload (2,000 items) takes about 25 ms (12.6 MB/s) and allocates 4.6 MB. A size cap is a limit; the Architect's call.
- Residual, named: a number written with a letter inside an exempt shape (`ten_4111111111111111a`) is not caught; a bound on the digit run inside an exempt ID would be a threshold.

## Session 2026-11-14 · REC-01 append guard: the push after noema#531 (A193, A201)
- noema#531 merged; main is merged into this branch and the duplicate gateway commit is gone (the gateway files are main's). The two reference paths follow A193 as written; every other structural fix of the Security Reviewer's round stays. Application-only enforcement, no bound inside exempt IDs and no size cap, as A201 rules.
- Pre-work for the next agent: A194 asks for one digit and separator definition in `record/payloadguard`, imported by #457 and by `record/reconstruct/project.go` (my follow-up after this merges), with the limit read from `masking-limits.v1`.

## Session 2026-11-15 · REC-01 · A194: one digit definition in `record/payloadguard`
- What changed: `payloadguard.Mask` is the display mask and `record/reconstruct` delegates to it (its own regexp and `keepDigits` are gone). The separator set is defined once (`baseSeparators`); the guard uses it as it is, the display mask adds an underscore, a bracket and a colon. A property test shows a masked string is never refused by the guard; a drift test ties `Limit` to the pack's `masking-limits.v1`.
- Why: A194 (the one digit and separator definition lives in `record/payloadguard`, imported by `record/reconstruct`).
- **A finding for the Architect, not changed here:** the two definitions were not the same and are **not made the same**, on purpose. Adding an underscore, a bracket and a colon to the guard would refuse legitimate event payloads (an error code such as `VERSION_CONFLICT_409_412` is six digits joined by an underscore) and stop writes; leaving them out of the display mask would serve a number split by an underscore unmasked. So the guard keeps its narrower set and the display mask its longer one, both built from one base set in one file. The consequence the Architect should know: the `events` array is served as stored (A184) and relies on the guard, so a payload string such as `4111_1111_1111_1111` (digits joined only by underscores) is **not** refused at append and would be served unmasked to ops and audit. Payload strings are written by the core, not by a customer, so I have left the guard as it is; if the Architect wants it closed, the guard needs a typed exemption for codes instead of a narrower separator set.
- Behaviour: the existing `project_mask_test.go` cases pass unchanged; the display mask is now slightly stricter for format characters and combining marks that the old list did not name (all Unicode Cf and M), never looser.
- Tests added (REQ-AUD-003, REQ-SEC-003, AGENT-13): `payloadguard/mask_test.go` (the mask cases, the property, the drift test) and `TestTheProjectionMasksWithThePayloadGuardsDefinition`. The events array is already behind the ops and audit gate (`handler.go`, `DenyAll` by default; `session_auth_test.go` covers the refusals).
- Debt entries: none new. Conflicts raised: none; the question above is for the Architect.

## Session 2026-11-16 · REC-01 · A231: one wide separator set, one exemption (noema#600)
- What changed: the guard and the display mask now use one separator set and one `digitRun` (no `displayRun`, no second mode). The set is wide: every space (`\p{Z}`, the vertical tab, the next-line character), every dash (`\p{Pd}`) and the minus sign, every bracket, the dot, slash, colon and underscore with their full-width forms and the middle dots, every format character and combining mark. One exemption (A231): a short UPPER_SNAKE reason code. The stray rapid counterexample file of the first red run is removed.
- **A deviation, for the Architect to confirm:** the bare A231 shape refused a real receipt. The receipt's `raw_status` is `<transport result>[:<raw code>]` (`execution/ledger/complete.go`), e.g. `VERSION_CONFLICT_409_412:409`; `record/chain TestAppendAcceptsWhatTheRuleExempts` ("a code with digits") failed on it, which would have refused the receipt of every version conflict after the write was dispatched. The smallest safe default: the exemption takes up to two colon-separated raw-code parts (one to three digits, or an UPPER_SNAKE word with no digit), at most 9 digits in all. What would invalidate it: a raw code that is not a status or a word (then the guard refuses it and the adapter must be changed), or the Architect's wish for a typed field instead of a string.
- Tests added (REQ-AUD-003, REQ-SEC-003, AGENT-13): every separator of the wide set refused and masked; the exemption's accepted and refused cases (including the raw-code shapes and `X_4111_1111_1111_1111`); a walk over every code point of `unicode.Nd`; the property test's alphabet widened. Two existing assertions in `guard_test.go` (`4111_1111` and `409:412:409` were accepted) changed to refused, as A231 supersedes them. One case of mine in the red push was wrong (`CODE4111_X` holds four digits, within the limit) and is `CODE41111_X`. The Nd walk and the property test pass on the old class too (the class was right): their evidence is a mutant, not a red run.
- Not done: the `masking-limits.v1` value is still a constant checked by a drift test, not read at run time (needs a loader in `record/`, which the layer rules decide); the size cap of the decoded payload (A201); DEBT-30 residue (`ten_4111111111111111a`, the bare `/external_ack` and `/target_ref`). The model-service redactor and the core redactor of noema#457 are separate definitions (A231: the model service conforms, noema#468); #457 does not (my read there).
- Debt entries: none new. Conflicts raised: the raw_status deviation above.

