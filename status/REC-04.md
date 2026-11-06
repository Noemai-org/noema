# Status · REC-04 (Core Engineer 1) · completeness per consequential decision

Updated 2026-11-05. Plan task REC-04: owner C1+AG; REQ-AUD-002; Needs REC-03 (merged); done when "Removing any required event in a test makes the check fail".

## State: slice 1 in review (draft until CI is green at the final head and Codex is READY).

## Session 2026-11-05 · REC-04

### Session summary
- What changed (PR links): the REC-04 pull request on `feat/REC-04-completeness`: package `record/completeness` (the pure check, the metric hook, README, tests).
- Why: REQ-AUD-002; SoT §38.2 "consequential decisions with a complete causal record: 100%".
- Risks, and how they are contained: a false "incomplete" raises an alert; a false "complete" hides a gap. The second is worse, so unknown is treated as consequential (an unreadable `DECISION_MADE` counts as a governed write) and the explanation rule accepts only the two named events. The check reads no database and changes nothing.
- Tests added (REQ-AUD-002, INV-10): `record/completeness/completeness_test.go` (10 tests including one `rapid` property). Failing first: the package did not exist; one test (an unreadable decision) failed first against my first version, then the code was changed.
- Mutations run and reverted: governed-set check off (4 fail); explanation rule off (3); an ALLOW decision not counted as consequential (3); ALLOW not detected (2); metric not counted (1).
- Regression cases added: none (new code).
- Debt entries: none.
- Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: slice 2, run the check in the verification job after `chain.Verify` and in the benchmark audit with each case's `required_audit_events`; slice 3, the `RECORD_ACCESSED`-style report on the reconstruction endpoint if the Architect wants it there.
- Open questions (owner): whether a CONFIRM decision that never reaches an ALLOW is a governed write (Architect; today it is not); where `noema_audit_incomplete_total` is exported (telemetry is an empty boundary).
- Files to read first: `services/core/record/completeness/README.md`.
- Do not touch: noema#94, noema#90 (held, DEBT-01).
