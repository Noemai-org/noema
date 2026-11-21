# Status · TRU-07 the two sealed-snapshot readers agree (Core Engineer 2) · DEBT-52, ruling A350

Updated 2026-11-19. Tests only: no production code, contract, migration or pack changes. Task TRU-07; the follow-up that ruling A350 gives to C2 for noema#739 (the Reconciler adapter) and noema#740 (the Evidence adapter). All data is SYNTHETIC.

## State: pull request with tests only (not yet opened). Not Done: DEBT-52 stays open until one shared reader exists.

## Session 2026-11-19 · TRU-07

### Session summary
- What changed (PR links): not opened yet. Two new test files and one README line, and this file.
  - `services/core/orchestrator/portadapters/trust/reconciler/readers_agree_db_test.go`: seals one snapshot over five items in a real database and reads it through both readers, the Reconciler's `Store` and the Evidence adapter's `SealedSnapshots`. It is an external test package (`reconcilerport_test`) because production code of one adapter may not import the other and a test may; it sits in this directory because the package already has `TestMain` and no directory may hold test files only.
  - `services/core/orchestrator/portadapters/trust/evidence_intent_db_test.go`: the `selectIntent` contract (`ORDER BY version DESC LIMIT 1` per correlation) in the database.
- Why: the Integration Lead's review of noema#739 and #740 (P2 and P3): the two readers read the same tables with two structs and can drift, and nothing asserted that they agree or that the intent read gives the later version. DEBT-52 asks for one shared reader; until then this is the check that catches a drift. It is a regression lock, not a fix: the readers already agree, and the tests passed when first run.
- Risks, and how they are contained: none in production (tests only). The tests use a throwaway database per test (`testpg`) and, for the cross-tenant cases, give the migrating login a test-only grant and a test-only policy to `evidence_verify` (the nightly login), never a migration.
- Tests added (REQ-CTX-004, REQ-TRU-001, REQ-TRU-003, REQ-SEC-004; INV-11, INV-17, INV-21):
  - `TestDBTheTwoSealedSnapshotReadersAgreeOnOneSnapshot`: the same keys, evidence IDs, evidence set hash and item set (evidence ID, source, status, payload hash, read_at and effective_at to the microsecond, freshness limit), and both equal to the rows that were sealed.
  - `TestDBEachSealedSnapshotReaderReturnsItsOwnColumnsAsSealed`: what only one reader reads: the Reconciler's sealed_at (to the microsecond, UTC), reconciliation, classification, payload_ref, UTC times and bytewise order; the Evidence reader's item correlation and intent.
  - `TestDBTheTwoSealedSnapshotReadersDoNotFindAnotherTenantsSnapshot`: tenant B asking for tenant A's snapshot, with its own login and with its login wired under tenant A's name: each reader answers with its own sentinel and its fixed text, identical to the answer for an ID nobody has.
  - `TestDBTheTwoSealedSnapshotReadersNameTheTenantInTheirOwnQueries`: with the cross-tenant login, only the queries' own tenant predicates stand between the tenants (snapshot, items, intent).
  - `TestEvidenceDBTheLaterOfTwoIntentsOfOneCorrelationIsAttributed`: two intents of one correlation with different nodes give the later (reader and adapter; the trust profile is asked for the later node only), around an intent of another correlation and tenant B's longer chain for the same correlation ID.
  - `TestEvidenceDBTheLatestIntentIsTheHighestVersionNotTheLatestTime`, `TestEvidenceDBACorrelationWithNoIntentIsNotAttributedToAnotherOnes`.
- Regression cases added: none (no defect; these are locks).
- Debt entries: none new. DEBT-52 (one shared reader) stays open; when it closes, these tests stay as the proof that the shared reader gives what both gave.
- Conflicts raised: none.

### Silences resolved by the safer reading (A332)
1. The Evidence reader does not read `payload_ref` or `sealed_at`, and the Reconciler's `Store` does not read the item's `correlation_id` or the intent. So "the same payload ref and sealed_at" cannot be asserted across the two readers; each is asserted against the seeded row for the reader that has it. A drift in those columns of one reader is caught, a difference between the readers on them cannot exist. Nothing was added to a reader to make them comparable.
2. The Evidence reader leaves the item order to the database (its adapter sorts); the Reconciler sorts bytewise. The agreement is on the set (both put in ID order); the Reconciler's own order is asserted separately.
3. Another tenant's snapshot: the "text semantics" each reader promises is its sentinel with its fixed text (`ErrNotFound`, `ErrSnapshotNotFound`) and the same text as for an unknown ID; no new text was invented.
4. The Reconciler's snapshot query carries a second lock in code (row tenant and ID compared with the ones asked). Dropping only the query's tenant predicate is therefore not visible (mutant R8, equivalent); both removed together is killed (R8b).

### Mutants (hand-made on the two production readers, one at a time, then restored; each killed by a named test run, none a compile error)
Run: `NOEMA_TEST_DATABASE_URL=... go test -count=1 -run 'TestDBTheTwo|TestDBEach' ./orchestrator/portadapters/trust/reconciler/` and `-run 'TestEvidenceDBThe|TestEvidenceDBACorrelation' ./orchestrator/portadapters/trust/`.

| Mutant | Killed by |
|---|---|
| R1 Reconciler items query swaps payload_ref and payload_hash | AgreeOnOneSnapshot, ReturnsItsOwnColumnsAsSealed |
| R2 swaps read_at and effective_at | AgreeOnOneSnapshot |
| R3 freshness limit + 1 | AgreeOnOneSnapshot |
| R4 items not sorted | AgreeOnOneSnapshot, ReturnsItsOwnColumnsAsSealed |
| R5 items sorted case-insensitively | AgreeOnOneSnapshot, ReturnsItsOwnColumnsAsSealed |
| R6 item times not converted to UTC | ReturnsItsOwnColumnsAsSealed |
| R7 read_at truncated to the millisecond | AgreeOnOneSnapshot |
| R8 snapshot query drops the tenant predicate (the second lock kept) | survives: equivalent (silence 4) |
| R8b the same, and the second lock removed | NameTheTenantInTheirOwnQueries |
| R9 items query drops the tenant predicate | NameTheTenantInTheirOwnQueries |
| R10 sealed_at truncated to the second | ReturnsItsOwnColumnsAsSealed |
| R11 swaps source and fact_status | AgreeOnOneSnapshot |
| R12 snapshot query reads reconciliation_id as the set hash | AgreeOnOneSnapshot |
| R13 no rows reported as a store error | DoNotFindAnotherTenantsSnapshot, NameTheTenantInTheirOwnQueries |
| E1 Evidence intent ordered by version ASC | TheLaterOfTwoIntents..., TheLatestIntentIsTheHighestVersion... |
| E2 ordered by created_at DESC | TheLatestIntentIsTheHighestVersionNotTheLatestTime |
| E3 intent query drops the correlation predicate | ACorrelationWithNoIntent... |
| E4 intent query drops the tenant predicate | NameTheTenantInTheirOwnQueries |
| E5 items query swaps read_at and effective_at | AgreeOnOneSnapshot |
| E6 swaps source and fact_status | AgreeOnOneSnapshot, TheLaterOfTwoIntents..., ACorrelationWithNoIntent... |
| E7 items query drops the tenant predicate | NameTheTenantInTheirOwnQueries |
| E8 snapshot query drops the tenant predicate | NameTheTenantInTheirOwnQueries |
| E9 snapshot query reads reconciliation_id as the set hash | AgreeOnOneSnapshot and two intent tests |
| E10 freshness limit doubled | AgreeOnOneSnapshot |
| E11 read_at truncated to the second | AgreeOnOneSnapshot |
| E12 intent query swaps intent_id and intent_node | ReturnsItsOwnColumnsAsSealed and three more |
| E13 intent version read as 1 | TheLaterOfTwoIntents..., TheLatestIntentIsTheHighestVersion... |
| E14 no rows reported as a store error | DoNotFindAnotherTenantsSnapshot, NameTheTenantInTheirOwnQueries |

28 of 29 killed; the one survivor is equivalent and its double mutant is killed.

## Pre-work for the next agent
- Exact next step: DEBT-52 itself, if the Architect wants one shared reader (a refactor PR of its own, with these tests unchanged and green). Candidates may read the same snapshot; look before sharing.
- Open questions (Architect): whether the shared reader returns a union struct (payload_ref and sealed_at too) or the Evidence reader keeps reading fewer columns on purpose (it never needed the payload reference).
- Files to read first: `reconciler/store.go`, `evidence_sealed.go`, the two new test files, `migrations/0008`, `0020`, `0026`.
- Do not touch: the migrations; the readers' queries or error text without these tests.
