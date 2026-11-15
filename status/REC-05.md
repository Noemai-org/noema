# Status · REC-05 (Core Engineer 1) · chain verification

Updated 2026-11-03. Plan task REC-05: owner C1; REQ-AUD-005; Needs REC-01; done when "Tampered row detected within one run".

## State: in review (draft until checks are green at the final head and Codex has reviewed). Slice 1 of 3.

**Needs check.** REC-01 shows "In progress" on the board: its property test is in noema#122 (draft). This slice uses only `chain.Verify`, which is on `main`.

## Session 2026-11-03 · REC-05

### Session summary
- What changed (PR links): the REC-05 pull request on `feat/REC-05-chain-verification`.
- Why: REQ-AUD-005; "tampered row detected within one run"; a break triggers the alert and the kill switch.
- Risks, and how they are contained: the job can stop a tenant's writes, so a false break would be an outage. It reads one tenant in one snapshot; a failed read is ERROR and stops nothing. A head check was added that `chain.Verify` cannot make. The job needs a database role that does not exist yet (below).
- Tests added (REQ-AUD-005, INV-11): `record/verify/verify_test.go`, 7 tests over real PostgreSQL.
- Regression cases added: none (new code). Mutations run and reverted: head check off; kill switch not set; alert error swallowed; stop at the first break; an unreadable tenant reported clean (found a gap in my first test, then closed it).
- Debt entries: none filed. Open: the verifier role migration.
- Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: slice 2, the `record_verify` login role (a migration after noema#107's `0003`) and tests that the role cannot write the Record; slice 3, the endpoint handler on the reconstruct pattern (after noema#107 merges), wired by EDG-01 only after DEBT-04 option A.
- Open questions (owner): where the S1 incident and the alert go in the POC (an `Alerts` implementation; Ops Lead); whether the nightly run is a Temporal schedule or a CI workflow (REC-06 and the orchestrator).
- Files to read first: `services/core/record/verify/README.md`.
- Do not touch: noema#94, noema#90 (held, DEBT-01).

## Session 2026-11-04 · REC-05
- Merged `main` (noema#145 is on `main`) and adapted the verify test world to per-tenant `execution_rw` logins (`testpg.TenantLogin`); no production code change. The Verifier still runs as the administrator in these tests; its own `record_verify` login (`noema_verify`) is slice 2.

## Session 2026-11-05 · REC-05 slice 2
- The Verifier tests now run it as the verification login (`testpg.VerifierLogin`: a member of `record_verify` only, no tenant mapping; `noema_verify` in Compose), not as the administrator: every tamper case, the kill switch, the head check and the failed-read case pass under that login, and it still cannot write the Record (`platform/db/provision_test.go`). The privileges are migration `0006` (noema#145); no production code or migration changed in this slice.
- Mutation: revoking `UPDATE (kill_switch)` from `record_verify` makes the tamper cases fail (4 tests fail; checked on the test cluster, reverted).
- Next: slice 3, the on-demand endpoint (ops and audit only), wired by EDG-01.

## Update 2026-11-07 (Founder decision D5: set-only kill switch, noema#265)
- Added migration `0017_kill_switch_set_only.sql` and `platform/db/kill_switch_test.go` (committed red first at fe06dbb). The verification login may set any tenant's kill switch (it is all-tenant by design, a named exception, SR F1) and cannot clear it; the administrator can.
- Not decided here: which login a person clears the switch with in production (the control plane, EXE-06, route names per D14), and an audit event for a clear.
- Pre-work: when EXE-06 exists, add a test that its ops login clears the switch and that the clear is recorded.


## Session 2026-11-08 · REC-05 review F1 on noema#265: deny every non-superuser, not only record_verify
### Session summary
- What changed (PR links): noema#265: migration `0017_kill_switch_set_only.sql` (edited in place: unmerged) and `kill_switch_test.go`; the same trigger is carried in noema#261.
- Why: the reviewer's F1 at `b0a6393`: the trigger refused a clear only for members of `record_verify`, so any other role given the column by a later grant could clear the switch. Today no other role holds the grant (migration 0006 gave it to `record_verify` only), so it was not exploitable, but the rule was written against one role instead of denying by default.
- Tests added (REQ-AUD-005, INV-11, INV-12): `TestNoRuntimeLoginCanClearTheKillSwitchEvenWithTheGrant`: for each of six runtime groups, grant the column to the group (the mistaken grant), set the switch, and require the guard's own refusal (42501, "kill switch"), not a missing privilege. Failing first at `6c5d347` ([CI run](https://github.com/Noemai-org/noema/actions/runs/42475905418/job/128717109142), `core` red on all six).
- Not done, and why: the reviewer also asked to allow clears "only through the person-authenticated C-ADMIN path" and to test it. That path is EXE-06 (ruling A9: the C-ADMIN credential, the ops role, a SECURITY_EVENT naming the actor) and does not exist yet; the database cannot tell a person from a process. Until EXE-06 the person's path is the administrator (a superuser, break-glass). The rule is now deny-by-default, so EXE-06 only adds its one login.
- Debt entries: none new (EXE-06 is the planned owner of the person path). Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: EXE-06 adds the ops login to `tenant_kill_switch_guard` and writes the SECURITY_EVENT on every clear.
- Do not touch: migration 0017 after it merges (forward-only).

## Session 2026-11-08 · REC-05 second re-review of noema#265
### Session summary
- What changed (PR links): noema#265 (and noema#261, which carries the same file): a test, comments and README text; no behaviour change.
- Why: the re-review at `8203292` accepted F1 and left two gates. (1) The authority: D5 is committed in `DEC-2026-11-07-founder-decisions-block-d` and the clear rule is ruling A71 of `DEC-2026-11-08-rulings-a41-onward` (docs#140, merged); the migration, README and test now cite those records instead of 'the Founders' instruction'. (2) The superuser escape hatch: the database cannot show that a superuser credential is held by a person. What can be shown is that no service login is a superuser: `TestNoServiceLoginIsCreatedAsASuperuser` reads `deploy/compose/postgres/init.sh` and requires every `CREATE ROLE ... LOGIN` to say `NOSUPERUSER`. It passes today (a guard on an existing property, so there is no red run); with `SUPERUSER` put on one login it fails (checked and reverted).
- Not done: the person-authenticated C-ADMIN path (EXE-06, ruling A71) and the SECURITY_EVENT on a clear. That the superuser is held by a person only is an operating rule for a runbook; I did not write runbook commands for code that does not exist.
- Debt entries: none new. Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: EXE-06 adds the ops login to `tenant_kill_switch_guard` and writes the SECURITY_EVENT naming the actor on every clear.
