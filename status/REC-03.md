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
- Conflicts raised: REQ-AUD-003 says "Ops and audit role only"; the PERD §39.4 table says "audit". Resolved by the Architect's decision of 1 Nov 2026: ops and audit, masked; never customers or approvers.

### Pre-work for the next agent
- Exact next step: Codex review; then the founder's approval (the Founder approves PRs from `noemai-systems`).
- Open questions (owner):
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

### 2026-11-01 (time not recorded) accepted · Architect (through the Technical Co-Founder's session) → Core Engineer 1
- Purpose: (1) the reconstruction serves the ops and audit roles, masked, never customers or approvers; (2) a separate PR for PostgreSQL 17.11 and `-race` in core CI, to merge before this one; (3) CON-03 may edit the CI workflow and Makefile.
- Receiver used it for: (1) in this pull request (roles, the staff-principal check, masking to the last four digits); (2) noema#108; (3) CON-03.
- Evidence (PR or commit): this pull request; noema#108.

### 2026-11-03 accepted · Architect (through the Technical Co-Founder's session) → Core Engineer 1
- Purpose: tenant isolation of `record_read_chain` (review 5734474390): option B accepted with DEBT-04, covering `record_read_chain`, `record_lock_head` and `record_insert`. Condition: option A lands before the endpoint is wired into `cmd/core`. Write the two-tenant denial test at the handler first, and the database-level test skipped with `t.Skip` citing DEBT-04. noema#108 merges before #107, then `main` is merged into #107. EDG-01 (mine) owns the wiring and the ops/audit role check on core routes, and does not wire the reconstruction endpoint before option A.
- Receiver used it for: the two tests and the README paragraph in this pull request; the debt entry is the Repo Steward's.
- Evidence (PR or commit): noema#107.

### 2026-11-03 · Codex review 5747595303 (major) fixed
- Finding: `Build` rolled back with `context.WithoutCancel(ctx)`, which removes the deadline, so a stalled database could hold the request for ever.
- Fix: the rollback runs on a context detached from the request's cancellation and bounded to 5 seconds (`rollbackTimeout`). Test first: `TestTheRollbackAfterAFailedReadIsBoundedByItsOwnDeadline` (fails on the old code with "the rollback has no deadline"; passes on the fix).
- Same pattern elsewhere, outside this PR's scope: `services/core/platform/db/db.go:86` and `:141` (PLT-06) and `services/core/execution/ledger/ledger.go:68` (EXE-02). Both are mine; they need their own small change with the same helper and test.
- Tenant finding (the other blocking item): answered in the PR; option B with DEBT-04 was accepted by the Architect; see above.

## Session 2026-11-11 10:00 WAT · slice 2: the session Authorizer
## Session summary
- What changed (PR links): the REC-03 slice 2 pull request on `feat/REC-03-session-authorizer`, from main: `record/reconstruct/session.go` (`SessionAuthorizer`), its tests, handler comments and the README.
- Why: the handler was built with a default-refuse authoriser; the running core needs one that takes the caller from the verified session (A146/A147, relayed, to be committed). The route itself is mounted by the composition root (EL, `cmd/core/compose.go`), not here.
- Risks, and how they are contained: a wrong role-to-kind mapping would serve a customer. The handler checks the role and the principal kind (two layers) and a test fixes each role's `Caller` exactly; mutation checks killed all four mutants run (every role staff, fixed tenant, no-principal accepted, forced role).
- Tests added (REQ-AUD-003, REQ-API-004, REQ-SEC-004, INV-12, AGENT-10): `record/reconstruct/session_auth_test.go` (6): no principal in the context, ops and audit served through the real `session.Middleware` and `RequireRole`, customer, approver, no session and bad session refused with no events, the handler without the gate, the token's tenant decides the chain, a miswired pool serves nothing (409), the caller built per role.
- Regression cases added: none. Debt entries: none. Conflicts raised: none.
- Open: the kind-from-role mapping (`ops` to `operator`, `audit` to `auditor`) is mine; the handler's principal format is `<kind>:<id>` (PERD 32.1) and the session carries a role, not a kind. EL or the Architect to say yes. `cmd/core/no_reconstruction_test.go` stays until EL wires the route and removes it.

## Pre-work for the next agent
- Exact next step: EL registers `reconstruct.Route` in the composition root with roles ops and audit and `Handler{DBs, Auth: reconstruct.SessionAuthorizer{}, IDs}`, and removes the not-yet-wired guard test in the same PR.
- Files to read first: `services/core/record/reconstruct/session.go`, `handler.go`.
- Do not touch: the migrations.

## C2's review of noema#455 at 48b0b90c (11 Nov 2026), non-blocking points
- Kind from role (`ops` to `operator`, `audit` to `auditor`) is not written in the PERD (§32.1: a kind is never inferred from an identifier; this is from the role). It needs a line in a decision record (Architect or EL); until then it is mine and labelled so.
- Subject length (128 at the edge, 64 in the handler) and the meaning of the 409 for a miswired pool: README lines added.
- Proposed debt entry (REQ-OPS-004), for the docs repository's `debt.md` (Repo Steward or EL): a read function that returns the login's tenant to `record_read`, so `session.CheckTenant` can give a clean answer instead of a failed verification. A migration; not in this allowlist.
