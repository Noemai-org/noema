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

# Session 2026-11-05 17:30 WAT · EVD-01 (slice 2c)

## Session summary
- What changed: `evidence/layer/classify.go` (`ClassifyError`: adapter read error to read class; unknown failures are MALFORMED, i.e. UNKNOWN) with a table test; `execution/e2e/evidence_faults_test.go`: evidence built from reads that the REAL mock-case faults on (HTTP_5XX 503, HTTP_429 with Retry-After, TIMEOUT_BEFORE_COMMIT beyond the read deadline, and a real 404), through `Builder.Record` on the tenant's evidence login; `execution/e2e/mockcase_test.go` exposes the bench address and token. STACKED on noema#172 (builder) and noema#167 (tables).
- Result: every faulted or empty read gives UNAVAILABLE or UNKNOWN, none KNOWN; one `EVIDENCE_READ` per read; the Record chain verifies.
- Order of work, disclosed: `classify.go` and its table test were written in the same step, so the table test was not seen red. Mutants that make both tests fail: unauthorized to OK, 429 without Retry-After, timeout to OK, not found to OK. The e2e test was green on its first run against the real faults.
- Not done: MALFORMED_RESPONSE and SCHEMA_DRIFT are not mock-case effects (they come from the Tushe/proxy tracks); the malformed mapping is covered by the table test and by the `corrupted_recordings` test of the case client. A healthy KNOWN read through the real target is slice 3.
- Debt: `execution/e2e/evidence_faults_test.go` copies the bench client of `adapters/outcome_e2e/bench_test.go` (third copy of the mock-case harness pieces); to be filed in the docs debt register.

## Pre-work for the next agent
- Exact next step: slice 3, the G1 story tests in `execution/e2e` (a healthy read KNOWN, a stale read STALE, with the real target).

# Session 2026-11-05 18:30 WAT · EVD-01 (slice 3)

## Session summary
- What changed: `execution/e2e/evidence_faults_test.go` gains `TestEvidenceFromAHealthyRealReadIsKnownThenStale`: a case is created through the real adapter on the real mock-case and read back; the evidence is KNOWN with its hash on a hash-only `EVIDENCE_READ` event, and the same read judged one second after its 300 s window is STALE (INV-23), on the injected clock. STACKED on noema#174.
- Mutants caught: the builder judging against the read's own time (never stale); the payload placed in the event.
- First version of the hash assertion was wrong (two reads of the same content share a hash); fixed in the test, not in the code.
- With slices 1 to 3 the card's "Done when" (fuzzed payloads give total, correct statuses) is met on the status function; the item path to the Record is exercised against faulted and healthy real reads. Not done: slice 1b (AUTHORITATIVE_NEGATIVE), blocked on a contract change request (Q-EVD01-1).

## Pre-work for the next agent
- Exact next step: after #167, #172, #174 and this PR merge, move the board; slice 1b waits for the Architect.
