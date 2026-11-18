# Status · REC-06 (Core Engineer 2) · chain verifier in CI and the nightly job

Updated 2026-11-17. Plan task REC-06: REQ-AUD-001; Needs REC-01 and PLT-02; done when "A tampered row and a broken prior hash each fail the CI job; the nightly job runs the same verifier and alerts on a mismatch". Taken from Core Engineer 1 by the Founder's order of 17 Nov 2026.

## State: draft pull request noema#687; red tests pushed, implementation follows after the red CI run is read.

## Session 2026-11-17 · REC-06

### Session summary
- What changed (PR links): noema#687. New `services/core/cmd/record-verify` (the command, its tests, README); one added step in `.github/workflows/core.yml` (shard 0); new `.github/workflows/record-verify-nightly.yml`. No change to `record/verify`, `record/chain`, a migration or a contract.
- Why: REQ-AUD-001 and OD-06 (DEC-2026-10-17): nothing ran the REC-05 verifier, so a tampered row in the CI database would pass every merge.
- Risks, and how they are contained: the command can turn a tenant's kill switch on (REC-05 behaviour, kept); in CI and the nightly run that is a throwaway synthetic database. It refuses to run as any login that can write the Record. It prints no stored content, error text or connection string.
- Tests added (REQ-AUD-001, REQ-AUD-005, INV-11, AGENT-13): `job_test.go` (no database) and `ci_test.go` (a SYNTHETIC Record, the command run as a process).
- Regression cases added: none (new code). Mutants: 26 hand mutants of `job.go` run against the tests that run locally; 25 killed, 1 equivalent (see the PR hand-off).
- Debt entries: none filed. Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: read the red CI run of the tests-only commit, then push the implementation; read the green run (the tests that append events could not run locally: pgsodium is missing here).
- Open questions (owner): see the PR hand-off: (1) a tenant with no events and no head row is reported as `HEAD_MISMATCH` and stopped (C1/REC-05, Architect); (2) which database the nightly verifies once one exists (Ops Lead); (3) the alert channel, S1 incident and who is notified (Ops Lead); (4) CODEOWNERS for `cmd/record-verify` (founders); (5) a `record/verify/README.md` line that still says REC-06 will schedule `Run`.
- Files to read first: `services/core/cmd/record-verify/README.md`, `services/core/record/verify/README.md`.
- Do not touch: `record/verify` and `record/chain` (REC-05, REC-01) without their own task.
