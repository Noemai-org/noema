# Session 2026-11-07 17:00 WAT · REC-08 (slice 1: DB-side verification of Record events, noema#263)

## Session summary
- What changed: failing tests first (commit with the signed-event matrix, red run linked in the PR), then migration 0019, `chain.Appender` signs every event (`Signers` per emitter identity; fails closed without one), `Ring.SignRecordEvent`, the test helper `internal/recordsig`, and every test world gives its appender signers. noema#236's test (a forged unsigned event) is green because the unsigned `record_insert_t` no longer exists.
- Deviations stated: the signature is in columns, the event contract stays v3 (D1's `record-event.v4` needs the A27 edit); the model service and the mocks have no key (OD-P-73 default), so the reconstruct fixture's `INTENT_PROPOSED` is now written by `core-edge` (model output is untrusted data recorded by the core after its schema gate) and its golden is regenerated.
- Not in this slice: no production signer wiring (`cmd/core` is the health-only skeleton); `outcome_record_verdict` (DEBT-08) still takes `p_tenant` as an argument (SR's P1).
- Tests added: REQ-AUD-006, DEBT-03; INV-10, INV-11.
- Conflicts raised: D1 (`sig` member vs columns), stated in the PR body.

## Pre-work for the next agent
- Exact next step: the `core-outcome` verdict signature (OUT); the production signer wiring when `cmd/core` is more than a skeleton.

## Update 2026-11-07 (slice 2: the chain verifier checks every signature)
- Added: `record/verify/signatures.go` (Go-side check with the registry's public keys; `SIGNATURE` and `UNTRUSTED_KEY` findings, which count, stop the tenant and alert like any break); `GRANT SELECT ON signing_key TO record_verify` in 0019 (merge order of 8 Nov; unmerged, so edited in place).
- Tests (REQ-AUD-006, INV-10, INV-11; DEC-2026-11-01 row 13): `record/verify/signatures_test.go`, committed red first (02775d2).
- Stated: a revoked key's later events halt the tenant (the safer reading; whether a revoked-key finding should stop writes is a Founder call). The verifier does not yet flag an event signed after its key's `retired_at` (the database refuses new inserts under a retired key; `at` is the emitter's own clock); not built, not claimed.
- Still open for REC-08: reconstruct (REC-03) reports "consistent, not authenticated" (A40) until it also checks signatures under the tenant's login, which cannot read `signing_key`; needs a decision on how it gets keys.

## Session 2026-11-11 · REC-08 (noema#263): after #181, #168 and (stacked) #229 reached main or were merged in
- What changed (PR links): noema#263, test-only: `platform/db/emitter_provision_test.go` (#181's emitter-login test now calls the signed `record_insert_t` through `signedInsert`, because since this PR the function takes the signature); `adapters/outcome_e2e/crash_test.go` (the crash child signs its Record events: `recordsig.Local` with the label of the parent, and `signingtest.RingAt` with the parent's test name, which `signingtest.Ring` uses as the seed; a new `NOEMA_CRASH_LABEL` variable passes it); `internal/recordsig` gains `Local` (the signers of `For` without touching the database, for a process the test starts); `execution/e2e/evidence_faults_test.go` (two evidence builders were unsigned); `platform/db/migration_order_test.go` is the same upgrade-path change as in #229, #233, #234 and #261. `migrations/README.md` is main's file (A145).
- Why: with #181, #168 and #252 on `main`, five tests of this branch failed (`TestProvisionEmitterLoginBinds...`, `TestCrashMatrix`, two evidence tests of `execution/e2e`, the upgrade-path test). `TestTheListOfDefinersWithoutPgTempPinnedOnlyShrinks` still names `record_insert_t`: it clears when #234 (0016, which empties that list) merges first, as the dependency table on noema#183 requires.
- Tests: red first, locally; green after, `go test -count=1 ./platform/db/ ./adapters/outcome_e2e/ ./execution/e2e/ ./evidence/...`.
- Debt entries: none. Conflicts raised: none.
