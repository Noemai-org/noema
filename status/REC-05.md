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

