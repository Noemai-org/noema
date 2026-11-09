# Status · EVD-01 (Core Engineer 2) · the FactStatus function

Updated 2026-11-04. Plan task EVD-01 (owner C2+AG; REQ-CTX-001, REQ-CTX-002). Task card: docs#109 (draft, awaiting the Founder).

## Session 2026-11-04 · EVD-01 slice 1

### Session summary
- What changed: `evidence/layer/status.go` (`AssignStatus`, `Read`, `Source`, `ReadClass`, `Authority`, `Absence`) and `status_test.go`. Standard library only.
- Why: REQ-CTX-002, INV-19, INV-23; the Founder's order of 4 Nov 2026 to start EVD-01 slice 1.
- Risks: the function is new and unused; nothing calls it yet. Decisions D1 to D3 (corroborating-only, customer claim, read from the future are UNKNOWN) are the card's defaults, D1 is OPEN for the Architect.
- Tests added: the PERD §21.1 rows, absence never negative (EC-S11, EC-S19; the AUTHORITATIVE_NEGATIVE exception is not built, slice 1b), totality, determinism and no multi-item status over 50,000 seeded generated reads, and a Go fuzz target. Written first; red against a stub; mutation checks on every rule (the one survivor was a redundant clause, removed).
- Regression cases added: none (new code).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: slice 2, the `evidence_item` and `evidence_payload` tables with the immutability trigger (a migration number after `0007`) and the item builder, with real adapter faults and `EVIDENCE_READ` events.
- Open questions: D1 (Architect); the evidence-source contract has no `absence_semantics` field (slice 1 takes it as an input, no contract change).
- Files to read first: `services/core/evidence/layer/status.go`, PERD §21.1 and §21.6.
- Do not touch: `contracts/` (read-only).

# Session 2026-11-05 09:30 WAT · EVD-01 (slice 2a)

## Session summary
- What changed: migration `0008_evidence_tables.sql` (`evidence_payload`, `evidence_item`, immutability triggers, CHECKs for the eight FactStatus values, the sha256 hash shape, the `read_by` set and a positive freshness window, composite tenant FK, grants), and `evidence/layer/tables_test.go`. The item builder and `EVIDENCE_READ` events are slice 2b and need `AssignStatus` (noema#160).
- Why: REQ-CTX-001 (immutable evidence), INV-10, INV-19, INV-11.
- Risks: a new Postgres table family. Contained by insert-only triggers, no CASCADE, `evidence_rw` as the only writer, tenant on every row.
- Tests added: five, written first and seen failing (relation does not exist). Four mutants caught (item trigger removed, single-column FK, trust may write, extra status); all reverted.
- Decision: `evidence_payload` is written by `evidence_rw` (PERD 38.2 names the writer "Adapter"; adapters run inside the core and Evidence Layer persists their reads). Trust and Outcome read; Execution does not (no card needs it). Reversible.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: slice 2b, the item builder using `AssignStatus` and `EVIDENCE_READ` events, after #160 merges; real adapter faults from the Tushe FaultPlans.
- Files to read first: `services/core/migrations/0008_evidence_tables.sql`, `status/EVD-01.md`.
- Do not touch: contracts (no change here).

## Addendum 2026-11-05 · answering the reviewer's NOT READY on noema#167 at `5d18fec`
- Finding: `evidence_rw` could write evidence rows for any tenant (a foreign key proves only that the tenant exists). Fixed in the unmerged `0008`: `evidence_row_tenant_guard()` (SECURITY DEFINER) as a BEFORE INSERT trigger on both tables refuses a row unless `session_user` is mapped in `tenant_login` to the power `evidence` and to the row's tenant. A login with no mapping (the administrator) cannot write.
- Tests first, seen failing: `TestAnEvidenceLoginWritesOnlyItsOwnTenant` (cross-tenant payload and item, unmapped login). `TestAnotherPowersLoginIsRefusedEvenWithAMistakenGrant` was added after a mutant (power check removed) survived; it fails against that mutant. Mutants caught: item guard trigger removed; tenant check removed; power check removed.
- All tests now write on `testpg.TenantLogin(..., "evidence_rw", tenant)`.
- Merged `main` (the slice 1 status file conflicted; both kept) and corrected one stale phrase in the slice 1 notes (the AUTHORITATIVE_NEGATIVE exception is not built).

## Addendum 2026-11-05 · answering Codex's P1 on noema#167 at `5469c01` (reads were not tenant-scoped)
- Finding: table-wide SELECT let a tenant A trust, outcome or evidence login read tenant B's evidence. Verified with a test first (`TestEvidenceReadsAreScopedToTheLoginsTenant`, red: a tenant A login saw 1 foreign row in each table, and an unmapped trust session saw both items).
- Fix in the unmerged `0008`: row-level security on both tables with a policy on `evidence_login_tenant()` (SECURITY DEFINER, `session_user` through `tenant_login`) for `evidence_rw`, `trust_rw`, `outcome_rw`, for reads and writes. A session with no mapping sees no row. Mutants caught: RLS not enabled; read policy `USING (true)`; policy for the evidence role only.
- Not covered: the other tenant-scoped tables (`decision_grant` and the rest) are still selected by `tenant_id` in the caller's SQL (Tier 1, `status/DEBT-04.md`).

## Addendum 2026-11-05 · answering the reviewer's NOT READY on noema#167 at `0148d21` (definer functions and the search path)
- Finding: the SECURITY DEFINER functions resolved `tenant_login` unqualified with `public` in their search path. Verified as a real bypass, by a different route than the reviewer named: a login's own temporary table `tenant_login` (pg_temp is searched first for relations) mapping itself to another tenant let a tenant A evidence login write and read tenant B's evidence. `TestATemporaryShadowOfTheTenantMappingChangesNothing` was red first (write allowed; 2 rows read).
- Fix in the unmerged `0008`: both functions name `public.tenant_login` and set `search_path = pg_catalog, pg_temp`. Mutant: unqualified name again, caught (9 failures).
- Checked, not changed: the merged Record functions of `0003` and `0004` have the same unqualified pattern, but they are owned by `record_append`, which has no privilege on a caller's temporary table, so the same shadow is refused with `permission denied for table tenant_login` (checked with a scratch test, removed). Fail-closed by accident, not by design: hardening to a schema-qualified name with `pg_temp` last is recommended in a migration of its own (not done here; outside this task's allowlist).

## Addendum 2026-11-05 · answering the Security Reviewer's review of noema#167 at `360e562`
- Gap: row-level security was enabled but not forced, so the table owner (the migration role) bypassed the policy. Fixed in the unmerged `0008`: `FORCE ROW LEVEL SECURITY` on both tables. `TestRowSecurityIsEnabledAndForcedOnBothEvidenceTables` was red first (`relforcerowsecurity` false on both); the mutant without FORCE is caught. The suite (evidence/layer, platform/db, execution/e2e) is green with FORCE on, including the foreign-key checks and the builder paths.
# Session 2026-11-05 16:00 WAT · EVD-01 (slice 2b)

## Session summary
- What changed: `evidence/layer/builder.go` (`Builder.Record`: assigns the status with `AssignStatus`, canonicalises and hashes the payload, stores payload and item and appends an `EVIDENCE_READ` Record event in one transaction on the tenant's evidence login) and `builder_test.go`. STACKED on noema#167 (the tables).
- Tests (written first; red because `Builder` did not exist): payload, item and Record event persisted, event carries the hash only (REQ-CTX-001, REQ-AUD-001); status per read class, no negative fact (REQ-CTX-002, INV-19); a failed append stores nothing (EC-E16); another tenant's observation refused by the database (INV-11); no freshness window refused.
- Mutants caught: status always KNOWN; payload in the event; commit instead of rollback on failure. One equivalent mutant (the Go check `sla < 1` relaxed): the table's CHECK still refuses, so the Go check is an early refusal only.
- Not done: real adapter faults. The builder takes a read class; the mapping from a real adapter error (Tushe FaultPlans: TIMEOUT, 5xx, malformed, 429) to a class, and its test against the real mock servers, is slice 2c, then the G1 story tests in `execution/e2e` (slice 3).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: slice 2c, `ClassifyError` (adapter result to `ReadClass`) with tests against the real hostility proxy and mock-case FaultPlans; then slice 3.
- Do not touch: contracts.

## Addendum 2026-11-06 · answering Codex's two P2 findings on noema#172 at `d435440`
- Findings: (1) a normalised response with an empty `RawHash` was stored with the hash of the empty body, a false checksum; (2) a `ReadOK` read with no response became KNOWN with a placeholder payload. Both verified in a test first (`TestBuilderRefusesAPresentResponseWithoutItsRawHashAndAnOKReadWithoutAResponse`, red: the OK read without a response was stored as KNOWN; the response without a raw hash left an `EVIDENCE_READ` event).
- Fix: `checkResponse` refuses an OK read without a response and a response without a valid raw hash; only a read that returned nothing (error, timeout, not found) gets the empty-body hash. Mutant: OK read without response allowed, caught. One equivalent mutant: the Go raw-hash check removed; the table's CHECK on `raw_hash` still refuses the row, so the Go check is the early, clear refusal.
- The healthy-read story test of the stacked branch supplies the real raw bytes' hash.

## Addendum 2026-11-06 · Codex's second round on noema#172 (three P2 findings at `9ba18e3`)
- (1) A malformed 2xx body keeps the hash of the bytes received: a supplied raw hash is kept for a read without a response; the empty-body hash is made only when none is supplied; a malformed supplied hash is refused. (2) A normalised payload must be a JSON object (`evidence-payload.v1`): scalars and arrays are refused before hashing. (3) The source's authority and the item's authority class must agree (AUTHORITATIVE with `AUTHORITATIVE_FOR:...`, CORROBORATING with `CORROBORATING_FOR:...`, NONE with `NONE`).
- Tests first, red: `TestBuilderKeepsTheRawHashOfAMalformedBody`, `TestBuilderRefusesAPayloadThatIsNotAnObject`, `TestBuilderRefusesAuthorityMetadataThatDisagrees`. Mutants, each caught: object check off, authority check off, supplied hash discarded.
- Note on method: an earlier mutant that left an unused variable would not compile and would show "0 failures" in a naive count. The harness now checks for a build error before counting failures.

## Addendum 2026-11-06 · the reviewer's P1 on noema#172 at `9e7397e` (rollback with no deadline)
- Finding: the builder rolled back on a context detached from the caller (`WithoutCancel`) with no deadline of its own, so a stalled database could hold the connection. Fixed: `rollbackWith` detaches the context and bounds it at 5 s (the same bound as `platform/db`'s cleanup), and joins the rollback's own error to the original one instead of swallowing it.
- Test: `TestEveryDetachedContextInTheLayerHasItsOwnDeadline` (a source guard; red first: one detached context with no deadline) and `TestAFailedAppendReleasesTheConnectionAndKeepsTheOriginalError`. A stalled-database test is not practical here, so the unboundedness is guarded by the source check, not exercised; mutant: a detached context with `WithCancel` instead of `WithTimeout`, caught.
- The same unbounded pattern exists in merged code: `outcome/store/store.go` (mine; fix in its own PR) and `execution/ledger/ledger.go` (Execution's owner).

## Addendum 2026-11-06 · the synthetic-source label (AGENT-25)
- `Observation.DataSource` and `Observation.Synthetic`: when `DataSource` is set, the `EVIDENCE_READ` event carries `data_source` and `synthetic`; the evidence item itself has no field for them (the contract has none, so nothing changed there). Test: `TestBuilderLabelsEvidenceFromASyntheticSourceOnTheRecord` (red as a compile failure first; mutant that drops the label caught). Needed by the stand-in adapter (noema#205), which owns its own mapping.

## Session 2026-11-07 · error-body provenance (Codex finding on #172, after IL's noema#262)
A failed read with no normalised response now carries exactly one of `RawHash` (the hash of the bytes that arrived, an empty body included) and `NoBytes` (explicit: a timeout, reset, local refusal or open circuit). SHA-256(empty) is no longer invented for a body that was there, and the `EVIDENCE_READ` event carries `body_received`. I did not make the rule class-based as first planned: `ReadServerError` also covers resets with no response and `ReadMalformed` also covers a local refusal, so a class rule would have refused legitimate no-bytes reads. Caller wiring (`ReadError.RawHash` into the observation) is in the next PR of the chain (#174, where `ClassifyError` lives). Red commit first; four mutants killed.
# Session 2026-11-05 17:30 WAT · EVD-01 (slice 2c)

## Session summary
- What changed: `evidence/layer/classify.go` (`ClassifyError`: adapter read error to read class; unknown failures are MALFORMED, i.e. UNKNOWN) with a table test; `execution/e2e/evidence_faults_test.go`: evidence built from reads that the REAL mock-case faults on (HTTP_5XX 503, HTTP_429 with Retry-After, TIMEOUT_BEFORE_COMMIT beyond the read deadline, and a real 404), through `Builder.Record` on the tenant's evidence login; `execution/e2e/mockcase_test.go` exposes the bench address and token. STACKED on noema#172 (builder) and noema#167 (tables).
- Result: every faulted or empty read gives UNAVAILABLE or UNKNOWN, none KNOWN; one `EVIDENCE_READ` per read; the Record chain verifies.
- Order of work, disclosed: `classify.go` and its table test were written in the same step, so the table test was not seen red. Mutants that make both tests fail: unauthorized to OK, 429 without Retry-After, timeout to OK, not found to OK. The e2e test was green on its first run against the real faults.
- Not done: MALFORMED_RESPONSE and SCHEMA_DRIFT are not mock-case effects (they come from the Tushe/proxy tracks); the malformed mapping is covered by the table test and by the `corrupted_recordings` test of the case client. A healthy KNOWN read through the real target is slice 3.
- Debt: `execution/e2e/evidence_faults_test.go` copies the bench client of `adapters/outcome_e2e/bench_test.go` (third copy of the mock-case harness pieces); to be filed in the docs debt register.

## Pre-work for the next agent
- Exact next step: slice 3, the G1 story tests in `execution/e2e` (a healthy read KNOWN, a stale read STALE, with the real target).

## Addendum 2026-11-06 · the reviewer's P2 on noema#174 (ClassifyError not seen failing first)
- Done properly this time: tests-only commit `da75530` (a bare, a wrapped and a network-timeout error must be TIMEOUT) pushed alone; its `core` run is red ([run](https://github.com/Noemai-org/noema/actions/runs/42199062358/job/127829537492)). The fix commit follows: a deadline that expires in a call, or a `net.Error` that reports a timeout, is `ReadTimeout` (UNAVAILABLE) rather than MALFORMED (UNKNOWN). The original table test of the classifier remains written together with its code; the new rows are the failing-first evidence for the classifier's behaviour.

### Caller wiring (#174)
`layer.BodyOf(err)` returns the adapter error's `RawHash` or an explicit no-bytes (a nil error has neither). The real-fault e2e test now passes both through and asserts, on the real mock-case faults, that the event's `body_received` agrees with the adapter, that a body that arrived is stored under the adapter's hash and not the hash of nothing (503, 429 and a real 404 carry bodies; the timeout does not). Stated: `BodyOf` and its unit test were written together, not failing first; a mutation that ignores the hash is killed by the unit test and by the e2e test. The e2e test itself went red when the builder rule landed in #172, then green.
# Session 2026-11-05 18:30 WAT · EVD-01 (slice 3)

## Session summary
- What changed: `execution/e2e/evidence_faults_test.go` gains `TestEvidenceFromAHealthyRealReadIsKnownThenStale`: a case is created through the real adapter on the real mock-case and read back; the evidence is KNOWN with its hash on a hash-only `EVIDENCE_READ` event, and the same read judged one second after its 300 s window is STALE (INV-23), on the injected clock. STACKED on noema#174.
- Mutants caught: the builder judging against the read's own time (never stale); the payload placed in the event.
- First version of the hash assertion was wrong (two reads of the same content share a hash); fixed in the test, not in the code.
- With slices 1 to 3 the card's "Done when" (fuzzed payloads give total, correct statuses) is met on the status function; the item path to the Record is exercised against faulted and healthy real reads. Not done: slice 1b (AUTHORITATIVE_NEGATIVE), blocked on a contract change request (Q-EVD01-1).

## Pre-work for the next agent
- Exact next step: after #167, #172, #174 and this PR merge, move the board; slice 1b waits for the Architect.

## Addendum 2026-11-06 · for the adapter owners (from Codex's finding on noema#172)
- `layer.Observation.RawHash` must be the SHA-256 of the bytes the adapter actually received. The case adapter returns the decoded record only, so the story test of this branch hashes the decoded record as a stand-in. Adapters (ENV-03, ENV-05, ENV-08, the stand-in adapter) need to expose the wire-bytes hash next to the decoded answer; without it the builder refuses a present response.

## EVD-03 slice 1 · the evidence_set_hash (stacked on #176)
`services/core/evidence/snapshot/hash.go`: `SetHash` in Go, tested against the CON-04 golden vectors (positive and negative) and a 50-permutation property; six mutants killed. Failing-first: tests-only commit "EVD-03: failing-first tests for the evidence_set_hash". Not yet built: the `context_snapshot` table, the sealer (refuses an empty set; writes `CONTEXT_SNAPSHOT_SEALED`), and the nightly recompute job. The table is a Trust-written, tenant-bound table (migration, roles and RLS per ruling A6), so it needs the EL/C1 decision on which login seals (Trust's) before I write the migration: blocked on that one line, noted below.
# Session 2026-11-08 · EVD-01 slice 2b · EL's point 3 on noema#172

## Session summary
- What changed: `evidence/layer/builder.go` `checkResponse` refuses a payload on any read whose class is not OK (the body hash or an explicit no-bytes is all a failed read keeps), so an OUT_OF_SCOPE or error body is never persisted (INV-19, INV-22). Founder decision of 8 Nov: do it in #172, the root of the chain, before the Codex sweep.
- Tests added: `TestBuilderRefusesAPayloadOnAReadThatDidNotSucceed` (seven failed classes refused, nothing stored, an OK read still accepted). Red first (commit 62a6995).
- Not done: EL's points 1 and 2 (the no-bytes marker and the persisted effective limit) are one forward migration built with EVD-02 (Architect decision A4); no migration is added to this PR.
- Debt entries: none. Conflicts raised: none.

# Session 2026-11-09 12:30 WAT · EVD-01

## Session summary
- What changed (noema#174, noema#176): (1) #174: a typed nil `*adapters.ReadError` made `ClassifyError` and `BodyOf` panic (`errors.As` is true for it). Failing test first (`c5d7b20`, red locally: nil pointer dereference), fix `51fc945`: it is MALFORMED and no-bytes. This is the failing-first evidence Codex asked for on the classifier; the original mapping table was written with the code, as its test says, and is still covered by the eleven-mutant check. (2) #176: the healthy-read story test hashed the re-marshalled record as the "raw" body. It is renamed `TestBuilderOnlyAHealthyRealReadWithASyntheticRawHashIsKnownThenStale` and says it proves freshness and Record behaviour only. The successful-read path is not claimed as provenance.
- Why: Codex P2 on noema#174 (no red run for the classifier) and P2 on noema#176 (decoded JSON stood in for response bytes; INV-19, REQ-CTX-001).
- Risks, and how they are contained: (2) is a claim correction, not new behaviour; nothing is weakened. The adapter is outside EVD-01's allowlist, so it is not changed here; the ask is noema#390.
- Tests added (REQ-CTX-002, INV-19, AGENT-11): `TestATypedNilReadErrorIsAnUnforeseenFailureAndNeverAPanic`.
- Regression cases added: the same test.
- Debt entries: none new; the gap is tracked in noema#390 (adapter exposes the response-byte hash of a successful read).
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: when noema#390 lands, pass the adapter's success-read hash through the builder in the G1 story test and assert it against the bytes the real mock-case sent; then rename the test back.
- Open questions (owner): the adapter owner (IL) on the field name and the success-read hash (noema#390).
- Files to read first: `services/core/evidence/layer/classify.go`, `services/core/execution/e2e/evidence_faults_test.go`.
- Do not touch: `services/core/adapters/` from EVD-01.
