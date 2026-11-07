# Session 2026-11-07 17:00 WAT · REC-08 (slice 1: DB-side verification of Record events, noema#263)

## Session summary
- What changed: failing tests first (commit with the signed-event matrix, red run linked in the PR), then migration 0016, `chain.Appender` signs every event (`Signers` per emitter identity; fails closed without one), `Ring.SignRecordEvent`, the test helper `internal/recordsig`, and every test world gives its appender signers. noema#236's test (a forged unsigned event) is green because the unsigned `record_insert_t` no longer exists.
- Deviations stated: the signature is in columns, the event contract stays v3 (D1's `record-event.v4` needs the A27 edit); the model service and the mocks have no key (OD-P-73 default), so the reconstruct fixture's `INTENT_PROPOSED` is now written by `core-edge` (model output is untrusted data recorded by the core after its schema gate) and its golden is regenerated.
- Not in this slice: the chain verifier (REC-05) does not check signatures yet; no production signer wiring (`cmd/core` is the health-only skeleton); `outcome_record_verdict` (DEBT-08) still takes `p_tenant` as an argument (SR's P1).
- Tests added: REQ-AUD-006, DEBT-03; INV-10, INV-11.
- Conflicts raised: D1 (`sig` member vs columns), stated in the PR body.

## Pre-work for the next agent
- Exact next step: the verifier checks every signature and reports a revoked key's events (REC-08 acceptance); then the `core-outcome` verdict signature (OUT).
