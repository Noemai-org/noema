# Session 2026-11-23 · UND-04 · the logical resolution epoch increments from the case list (A520, DEBT-54 part 2)

## Session summary
- What changed (PR links): noema#922. Migration `0037_admitted_intent_epoch_path.sql`; `intentstore.Admitted` gains `SubjectRef` and `Save` stores the stated epoch and subject; `understanding.Node.SubjectSlot` (read from the pack) and `understanding.SubjectOf`; `evidence/portread.Reader.NextEpoch` (non-recording read of the case list); the Interpreter's optional `Subject` and `Epochs` ports and `ErrEpoch`.
- Why: A520 (interim), A513, A482. The epoch rule is PERD 28.2 [REC]: it "increments only when the prior logical action's case is verified CLOSED in the case system and a new intent is admitted". Built as: epoch = the highest epoch of a CLOSED case of the subject + 1; 1 when none is closed or the intent has no resolved subject. An unusable case list is unknown, never 1 (a guessed 1 would reuse an earlier epoch's idempotency key).
- Risks, and how they are contained: (1) the epoch port is optional and not wired: nil keeps epoch 1 and no subject, so the G1 path is unchanged. (2) The status word of a closed case is `CLOSED` (PERD 28.2); the adapter's own comment on `CaseRecord.Status` says "OPEN or ROUTED", so whether the live case system ever reports CLOSED is not verified: until it does, nothing increments (safe). (3) Migration 0037 drops 0033's CHECK and adds three; insert-only holds, no grant changes.
- Tests added (REQ-INT-001, REQ-SEC-004, REQ-AUD-001; INV-12, INV-14, INV-19; A520, A482): `intentstore/epoch_increment_test.go` and the reworked `epoch_test.go` (the tests that pinned "only 1" now pin the new rule), `understanding/subject_test.go`, `evidence/portread/epoch_test.go`, `understanding/interpreter/epoch_test.go`.
- Regression cases added: the same files.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step (EL, composition root): an adapter implementing `interpreter.Epochs` over `portread.Reader.NextEpoch` (it must turn the subject ref `<vertical>.<kind>:<id>` into the transaction reference the case list is keyed by, and build the `adapters.Scope` from the session's tenant and principal), and `interpreter.Config.Subject = understanding.SubjectOf(view)` with the same view as the gate. Both or neither.
- Open questions: does the live case system report status CLOSED (Architect/EL)? Is a per-subject uniqueness or monotonicity guard of the epoch wanted in the database (a later migration)?
- Files to read first: `migrations/0037_admitted_intent_epoch_path.sql`, `evidence/portread/epoch.go`.
- Do not touch: `cmd/core` main (EL's).
