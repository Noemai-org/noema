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
