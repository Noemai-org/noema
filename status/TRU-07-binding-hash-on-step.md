# TRU-07 · the Record's confirmation binding evidence hash (A435, A459)

State: draft PR noema#833; red head `dda3189d`; the implementation is a local commit, not pushed. Rulings A435 and A459 (interim, relayed, not yet committed).

# Session 2026-11-20 WAT · TRU-07 binding hash on the step

## Session summary
- What changed (PR links): noema#833. `orchestrator/portadapters/trust`: `SealedFacts` computes, for a request that follows a confirmation, the content digest of the sealed snapshot with `Evidence.contentHash` (new `SealedFactsConfig.Facts`, required); `RecordFacts.ConfirmationBindingEvidenceHash`; `RecorderAdapter.request` hands it to `recorder.Request.ConfirmationBindingEvidenceHash`, which it never set before.
- Why: delta found on main: every ALLOW after a confirmation was refused by the Writer (no evidence value of the binding). The value is per request, from the sealed snapshot, never from the request.
- Risks, and how they are contained: fail closed (unreadable facts, no record ID, malformed value: refused before any transaction). The mismatch refusal is the Writer's existing binding check at the spend (trust/recorder, PostgreSQL, CI only).
- Tests added (REQ-TRU-009; INV-25, INV-17, INV-19): recorder_binding_test.go (7 tests). 9 hand mutants, all killed.
- Regression cases added: none.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL's `wire_trust.go` passes the Reconciler as `SealedFactsConfig.Facts`.
- Open questions (owner): none.
- Files to read first: `recorder_facts.go`, `recorder.go` (request), `evidence.go` (contentHash).
- Do not touch: `trust/**`.
