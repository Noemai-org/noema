# Status · REC-03 (Core Engineer 1) · reconstruction endpoint v1

Updated 2026-11-01. Plan task REC-03: owner C1+AG; REQ-AUD-003; needs REC-01; done when "US-01 and US-06 reconstruct from the Record alone".

## State: in review, not Done

- **What is built** (this pull request, branch `claude/new-session-3mbmkr`):
  - migration `0003_record_read.sql`, with the role `record_read` and the function `record_read_chain`;
  - `record/chain.Verify` reads through that function;
  - `record/reconstruct`: `Build`, the projection, the handler, and the default-refuse authoriser `DenyAll`;
  - golden reconstructions of US-01 and US-06, validated in Python.

  The route is **not** wired into `cmd/core`. EDG-01 does that, with the session check and the ops/audit role check.
- **Done-when.** US-01 and US-06 reconstruct from event sequences appended through the real appender and database. This is the approved basis for review (Architect decision 4 of 1 Nov 2026). The task stays **In review, not Done**, until Understanding, Trust and Outcome write those events themselves. On `main` only the execution ledger writes Record events.
- **Payload schemas.** REC-04 authors them in `/contracts` (decision 4). Until then the projection table in `record/reconstruct/project.go` is the one place that names payload keys.

## Session 2026-11-01 15:45 WAT · REC-03

### Session summary
- What changed (PR links): the REC-03 pull request (draft) on `claude/new-session-3mbmkr`.
- Why: REQ-AUD-003 (US-12, POC-FR-12, INV-10, INV-11); G1's critical path (BEN-05 needs REC-03).
- Risks, and how they are contained:
  - a new database role, contained by giving it EXECUTE on one STABLE function and nothing else;
  - the endpoint is unauthenticated until EDG-01, contained by not wiring it and by `DenyAll`;
  - the whole tenant chain is verified per request: measured P95 38.5 ms for 200 events in a 400-event chain on PostgreSQL 17.11.
- Tests added: REQ-AUD-003, INV-10, INV-11, INV-12, INV-03 (grant_state), REQ-AUD-001 (role refusals); `platform/db/record_read_test.go`, `record/reconstruct/*_test.go`, `tools/contracts/test_reconstruction_golden.py`.
- Regression cases added: a receipt whose `logical_action` is not a pack ID is refused (the ledger writes `ESCALATE_CASE` today).
- Debt entries: none filed. Proposed (Repo Steward): the access-log line does not pass through the redactor, because `platform/redaction` is empty. It carries IDs and codes only.
- Conflicts raised: REQ-AUD-003 says "Ops and audit role only"; the PERD §39.4 table says "audit". The narrower reading is applied.

### Pre-work for the next agent
- Exact next step: Codex review; then the founder's approval (the Founder approves PRs from `noemai-systems`).
- Open questions (owner):
  - Founders: ops as well as audit?
  - Founders: does the reconstruction envelope keep `schema_version` "1.0" (as the approved v3 fixtures do)?
- Files to read first: `services/core/record/reconstruct/README.md`, `services/core/migrations/0003_record_read.sql`.
- Do not touch: noema#94 and noema#90 (held, DEBT-01).

## Messages

### 2026-11-01 (time not recorded) accepted · Architect (through the Technical Co-Founder's session) → Core Engineer 1
- Purpose:
  - order: CON-03, then REC-03 to REC-05;
  - E1 approved: `0003`, `record_read`, one read function, a refusal test for every other role, both migration orders;
  - E2 approved: library, handler and default-refuse authoriser, not wired; EDG-01 owns authentication and the ops/audit role check;
  - E3 approved: in review on appended sequences;
  - REC-04 authors the payload schemas;
  - `rapid` approved as a test-only dependency;
  - confirm the Postgres major in CI.
- Receiver used it for: this pull request (E1 to E3). The other items are reported in `status/roles/core-engineer-1.md`.
- Evidence (PR or commit): this pull request.
