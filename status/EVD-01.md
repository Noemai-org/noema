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
