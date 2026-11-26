# STM-07 the investigation row stores the verified issuer (PR A of A592; state: in review)

# Session 2026-11-23 · STM-07

## Session summary
- What changed (PR links): noema#925. Migration 0039 adds the NULLABLE column `investigation.asserted_by` with a CHECK of the contract's asserted_by pattern (`^[a-z][a-z0-9_-]*$`, intent-object.v2, the same as `understanding.Session.Valid`). `Store.Start` writes `sess.AssertedBy` (NULL when empty) after refusing a malformed one with `ErrRequest`; `SessionOf` returns it in `RecordedSession.AssertedBy` (empty when NULL). The column is not in the orchestrator's column-limited UPDATE grant (0025), so it never changes after the insert.
- Why: A584 (option b) and A592 (two PRs): INV-12 means what it says only if a session rebuilt from the row names its verified issuer; A514's "AssertedBy stays empty" is amended. A legacy row keeps NULL: no default, no backfill, no invented issuer.
- Risks, and how they are contained: PR A changes no recovery behaviour. Recovery (#882) still leaves `AssertedBy` empty on resume, and `understanding.Session.Valid` still requires a non-empty one, so a recovery-resumed turn still fails closed at the Interpreter port until PR B (the refusal) and EL's writer land. Nothing here makes a row refused; PR B turns that on after A merges.
- Tests added (REQ-STM-001, REQ-SEC-004; INV-12, INV-30): `orchestrator/investigations/issuer_column_test.go` (store writes the verified issuer, NULL when none, malformed refused by store and CHECK, never changes after insert, legacy row keeps NULL, migration carries the contract's pattern).
- Regression cases added: the same file.
- Debt entries: none.
- Conflicts raised: none (the A514 / contract conflict of noema#919 is settled by A584).

## Pre-work for the next agent
- Exact next step (EL): the gateway start (`turnStarter.Start` / the edge session) fills `orchestrator.Session.AssertedBy` from the verified edge session's issuer, never from a request, so `Store.Start` writes it. Then noema#919 (the resolver passes the Session's issuer through) works end to end. PR B (recovery refuses a row without an issuer; C2 builds it after this PR merges) must land with EL's writer, so recovery never refuses rows the running stack just wrote.
- Open questions (owner): none.
- Files to read first: services/core/migrations/0039_investigation_verified_issuer.sql, orchestrator/investigations/store.go (`Start`, `SessionOf`, `checkVerified`).
- Do not touch: migrations 0036 to 0038, recovery's behaviour (PR B).
