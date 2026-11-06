# Status · RCV-01 (Core Engineer 1) · recovery evaluation through Trust

Updated 2026-11-05. Plan task RCV-01: owner C1; REQ-RCV-001; Needs OUT-01 (merged in noema#136); done when "Property: every dispatch with attempt > 1 follows a non-ACHIEVED verdict and a new ALLOW".

## State: slice 1 in review (draft until CI is green at the final head and Codex is READY). RCV-01 is not Done.

## Session 2026-11-05 · RCV-01

### Session summary
- What changed (PR links): the RCV-01 pull request on `feat/RCV-01-recovery`: `execution/recovery` (`Handler`, the `Trust` and `Alerts` ports, `CheckRetryOrder`), README, tests.
- Why: REQ-RCV-001; recovery may only go through Trust (INV-15); a Trust failure is UNKNOWN, which escalates with an ops alert (PERD 30.2).
- Risks, and how they are contained: recovery sits between a failed write and a retry. It starts nothing: it has no dispatch method and imports neither Trust nor Outcome. Every failure path ends in ESCALATE, never in a retry. The ordering checker treats anything unreadable as a violation.
- Tests added (REQ-RCV-001, INV-15): `recovery_test.go` (6 tests) and `order_test.go` (12 tests, one `rapid` property over Record sequences). Failing first: the package had no API; two checker tests (an unreadable verdict, an ACHIEVED verdict cancelling a failed one) failed against my first version and the code was changed.
- Mutations run and reverted: WAIT also retries (1 fails); a Trust error retries (2); panic not caught (1); ALLOW with no decision accepted (1); no-new-ALLOW rule off (4); no-verdict rule off (5); verdict pair reusable across dispatches (2).
- Regression cases added: none (new code). Debt entries: none. Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: the Temporal recovery workflow that calls `Handle` with the production Trust activity (OUT-03 owns Temporal); RCV-02 (budget, 2 dispatches per logical key) on top of `CheckRetryOrder`; the real `Trust` adapter reads the recovery table from policy data.
- Open questions (owner): where the ops alert goes in the POC (Ops Lead); whether `COMPENSATED` counts as a verdict that authorises a retry (Architect; today it does, literally "non-ACHIEVED"); whether a Trust `CLARIFY` or `CONFIRM` on a recovery should ever be passed to a person instead of escalating (Architect; today it escalates).
- Files to read first: `services/core/execution/recovery/README.md`.
- Do not touch: noema#94, noema#90 (held, DEBT-01).
