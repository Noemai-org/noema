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
