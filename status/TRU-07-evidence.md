# TRU-07 · the Trust intake Evidence adapter (T9)

State: draft PR (noema#740), red head `b8940fcf`; implementation written, not yet pushed. Built by Core Engineer 2 (C2). Ruling A329 (interim, relayed): C2 owns Candidates, Evidence and Reconciler; this file is Evidence only.

# Session 2026-11-18 18:00 WAT · TRU-07 Evidence

## Session summary
- What changed (PR links): noema#740. `orchestrator/portadapters/trust/evidence.go` (`Evidence`, `intake.Evidence`), `evidence_sealed.go` (the sealed-snapshot types and the pgx reader as the tenant's trust_rw login), `evidence_test.go`, `evidence_db_test.go`, `README.md`.
- Why: A329. T9 needs the freshness of the sealed snapshot's items and the fact statuses as the Decision Record keeps them, from what was sealed and verified.
- Risks, and how they are contained: it decides nothing and writes nothing (read-only transaction, rolled back). Every doubt is an error and an empty answer, so T9 ends UNKNOWN. The intent attribution is conservative but not exact (see debt). Tenant isolation is the database's; a test hands tenant B's pool to the adapter under tenant A and reads nothing.
- Tests added (REQ-TRU-001, REQ-TRU-003, REQ-CTX-003, REQ-CTX-004, REQ-NFR-002; INV-11, INV-16, INV-17, INV-19, INV-23; AGENT-13; EC-E16): 33 tests with table cases for integrity (18), items (9), intent (9), required sources (5) and rules (4); a rapid property for limit, age and staleness; six over real PostgreSQL. Seen failing first at `b8940fcf` (all for the right reason: the compile-only stub answers an empty snapshot and no error).
- Regression cases added: none (new feature).
- Debt entries: proposed, for the Steward: the sealed snapshot does not record its intent (`context_snapshot` and the `CONTEXT_SNAPSHOT_SEALED` payload hold the correlation only). Owner C2 with C1; fix: add `intent_id` and `intent_version` to the sealer's input, the table (migration) and the event (contract), then the adapter reads them; due before G3.
- Conflicts raised: none. One gap with the intake status file: it says the adapter sets the intent "from the snapshot's own record", which holds no intent; resolved by the safer reading above (A332).

## Pre-work for the next agent
- Exact next step: EL's `wire_trust.go` builds `SealedSnapshots` over the per-tenant trust pools and a `SourceProfiles` over the pack's trust profile by intent node (not written: `packs/` has no loader for it yet).
- Open questions (owner): the debt entry above (Steward, C1); whether a per-source override of the limit exists in any pack (Architect), as `PROFILE_OVERRIDE` is never produced.
- Files to read first: `trust/intake/ports_t7_t9.go`, `trust/intake/gates_t7_t9.go` (T9 and `classify`), `evidence/snapshot/seal.go`, `migrations/0020_context_snapshot.sql`.
- Do not touch: `trust/**`, `evidence/snapshot` (C2's, sealing).
