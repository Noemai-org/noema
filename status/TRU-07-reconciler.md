# Status · TRU-07 intake Reconciler adapter (Core Engineer 2) · A329

Updated 2026-11-18. The Reconciler of the Trust intake, `services/core/orchestrator/portadapters/trust/reconciler/` (package `reconcilerport`), implementing `intake.Reconciler` (T10). Ruling A329 (C2: Candidates, Evidence and Reconciler). The branch label TRU-07 is as assigned; `tasks.csv` TRU-07 is the Confirmation contract (the row is closest to STM-01, RES-01 and RES-02).

## State: draft pull request (red head, then the implementation). Not Done.

## Session 2026-11-18 · TRU-07

### Session summary
- What changed (PR links): noema#739. The adapter, its Store over the tenant's own Trust login, `ReconcilerAt`, the package README and this file.
- Why: T10 has no Reconciler adapter, so the intake cannot be wired (A329).
- Risks, and how they are contained: the adapter writes nothing and calls no model. A drift, a tampered or empty snapshot, another tenant's snapshot, a store failure and a deadline that is missing each fail closed with a fixed-text error. The only fakes in the tests are the snapshot port, the payload port and the pack's routing (data); the rule set and the mapping are the pack's.
- Tests added (REQ-RES-001, REQ-RES-002, REQ-TRU-003, REQ-TRU-007, REQ-CTX-004; INV-11, INV-12, INV-16, INV-17, INV-19, INV-21; AGENT-13, AGENT-16): `reconciler_test.go` (adapter), `store_test.go` (Store over a spy transaction), `store_db_test.go` (`TestDB*`, a real database, CI only).
- Regression cases added: none (no defect).
- Debt entries: none new. The freeze table and the pack routing are not built (see below).
- Conflicts raised: none.

### Silences resolved by the safer reading (A332)
1. `Frozen` is true exactly when the sealed classification is `CONTRADICTORY_STATE` (no freeze table exists).
2. Pack routing comes through a `Pack` port; the adapter names no reason code or capability.
3. Only the classification is compared on re-derivation, not the reconciliation ID (it hashes a clock reading the snapshot does not hold).
4. The re-derivation time is the later of `sealed_at` and the latest `read_at`.
5. The item's freshness limit is the row's `freshness_sla_seconds`.
6. A sealed classification outside the twelve is `ErrBadAnswer`.
7. A store that returns foreign data is `ErrNotFound`; a store that fails is `ErrRead`.

### New setting
- `NOEMA_TRUST_RECONCILE_TIMEOUT_MS` (A330): `Config.Timeout`, no code default; zero or negative refuses.

### Mutants (hand-made, each killed by a named test run, none a compile error)
30 mutants over `reconciler.go` and `store.go`: hash check, frozen never or always, drift check, tenant or ID of the row unchecked, not-found as read error, no deadline, allow-list aliased, reason unchecked, items not compared with the IDs, seal time only, closed set unchecked, no timeout, cause printed, reclassify error as drift, empty set accepted, zero seal time accepted, tenant unbound in each query, read-write transaction, read-committed, no rollback, row tenant unchecked, no UTC, unsorted items, store cause printed, malformed ID queried, fallback login, rows not closed. Two mutants survived the first set (an empty set with its own honest hash, a zero seal time); a test case was added for each and both are now killed. A third (the sorted-ID check) was redundant with the item comparison and the check was removed.

### What was not verified here
- The `TestDB*` tests did not run: this machine has no PostgreSQL server and no pgsodium. They compile and vet; CI runs them. The SQL is checked by the spy tests (tenant binding, isolation level, rollback) and by reading, not by a server.
- No wiring: `wire_trust.go` (EL) constructs the adapter with the Pack and the timeout.

## Pre-work for the next agent
- Exact next step: EL's `wire_trust.go` builds `reconcilerport.New(Config{Snapshots: NewStore(PoolsOf(trustPools)), Classifiers: ReconcilerAt(reconciler.Config{...}), Pack: <the pack's routing>, Timeout: NOEMA_TRUST_RECONCILE_TIMEOUT_MS})`.
- Open questions (owner): the pack's escalation reasons and `allowed_under_freeze` in a loadable form (PCK, RES-02); recording the clock reading in the snapshot so the reconciliation ID can be checked too (contract change, Architect).
- Files to read first: `services/core/orchestrator/portadapters/trust/reconciler/README.md`, `services/core/trust/intake/ports_t10_t12.go`, `services/core/evidence/reconciler/README.md`.
- Do not touch: `evidence/reconciler` and `evidence/snapshot` for this adapter's sake; `intake` ports.
