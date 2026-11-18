# TRU-05 follow-up · the grant the real writer stores must load in Execution

State: fixed and tested locally on `prep/tru05-grant-json` (not pushed). Found by the TRU-08 two-version test (status/TRU-08.md, finding 1).

# Session 2026-11-17 18:57 WAT · TRU-05 (grant JSON)

## Session summary
- What changed: `trust/recorder` now stores `decision_grant.json` as the grant document without its `sig` member (`signedPart`); the signature stays in `sig_key_id` and `sig_value`. The retry path (`lookup`, A186) reads the two columns and puts the signature back before it decodes the stored grant (`withSig`), so a retry still returns the grant that was issued, signed. No change to `execution/ledger`, `controller.Grant`, a migration or a contract.
- Why: `Writer.Write` stored the marshalled grant with `sig`; `ledger.Load` decodes strictly (`DisallowUnknownFields`) into `controller.Grant`, which documents its canonical JSON without the signature as the signed part, so `Execute` failed with `ledger: decode grant grt_...: json: unknown field "sig"` for every grant the real Writer issued. No test composed the real Writer with the real Ledger.
- Checked before the change: the only readers of `decision_grant.json` are `ledger.Load` (needs no sig), `ledger` `correlation` (`json->>'correlation_id'`), and `recorder.lookup` (needed the sig; handled). The migration trigger `decision_grant_requires_allow` reads `decision_id`, `tenant_id`, `intent_hash`, `policy_version`, `expires_at` of the document and never `sig`; no other migration or constraint reads `json` of this table. Other tests of `decision_grant` count rows only.
- Risks, and how they are contained: a grant document stored before this change carries `sig` and still cannot be loaded by Execution (none exists outside tests: nothing issued grants through the Writer into a running system; stated, not checked on any deployed database). `withSig` overwrites any `sig` member in a stored document with the columns' values, so an old stored document still decodes on retry.
- Tests added (REQ-TRU-008, REQ-EXE-001; INV-03, INV-17, INV-21): `TestAGrantTheRealWriterIssuedIsLoadedVerifiedAndExecutedByExecution` (real Writer, real `ledger.Load`, real controller E1 to E11 to a receipt; the loaded signature is the issued one; the stored document has no `sig`), `TestARetryOfTheSameInputReturnsTheStoredGrantStillSigned` (`adapters/outcome_e2e/real_grant_test.go`). The first was red on `main` (`f043976b`) for the right reason, commit `9f646abf`.
- Regression cases added: the two tests above.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: on `prep/tru08-twoversion`, once this lands, delete `signlessLoader` from `policy_two_version_test.go` and the line that installs it (it names this fix).
- Open questions (owner): none.
- Files to read first: `trust/recorder/recorder.go` (`signedPart`), `trust/recorder/inputkey.go` (`withSig`).
- Do not touch: `execution/ledger` `Load` (strict decode stays), `controller.Grant`.
