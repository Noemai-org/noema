# Status · PLT-06 (Core Engineer 1) · migrations, roles, and the database the tests run on

Updated 2026-11-01. Plan task PLT-06 (owner C1+AG; REQ-DATA-001, REQ-SEC-004).

## Session 2026-11-01 17:10 WAT · PLT-06

### Session summary
- What changed (PR links): the `core` CI job now tests against a PostgreSQL 17.11 service container, pinned by the same digest as `deploy/compose`. It sets `NOEMA_TEST_DATABASE_URL`, refuses to run if the server is not 17.11, and runs `go test -race`. Branch `chore/PLT-06-core-ci-pg17`.
- Why: until now `internal/testpg` used the `ubuntu-24.04` runner's own PostgreSQL 16, so migrations, roles and the Record were never tested in CI on the major the stack runs. Approved by the Architect on 1 Nov 2026, to merge before noema#107.
- Risks, and how they are contained: trust authentication on a job-local container holding synthetic data only, so no secret is added. `-race` lengthens the job (about 30 s locally).
- Tests added: none. The existing suite now runs on 17.11 with the race detector.
- Regression cases added: the version guard fails the job if the server is not 17.11. Shown locally: with `EXPECTED_POSTGRES=16.9` the step exits 1.
- Debt entries: none.
- Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: the Founder's approval and merge; then merge `main` into noema#107 so its CI runs on 17.11.
- Open questions (owner): none.
- Files to read first: `.github/workflows/core.yml`, `services/core/internal/testpg/testpg.go`.
- Do not touch: noema#94, noema#90 (held, DEBT-01).

## Messages

### 2026-11-01 (time not recorded) accepted · Architect (through the Technical Co-Founder's session) → Core Engineer 1
- Purpose: approval for a separate small PR adding a PostgreSQL 17.11 service container and `-race` to `.github/workflows/core.yml`, to merge before noema#107.
- Receiver used it for: this pull request.
- Evidence (PR or commit): this pull request.
