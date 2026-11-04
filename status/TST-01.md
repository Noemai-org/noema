# Status · TST-01 (Core Engineer 1) · property tests of the Execution invariants

Updated 2026-11-03. Plan task TST-01: owner C1+AG; REQ-TST-002; Needs EXE-02; done when "All green on every merge".

## State: in review for INV-03, INV-07 and INV-08; INV-05 and INV-18 wait for EXE-01 and PLN-01 (they need the registry and the planner)

TST-01 is **not** Done until INV-05 and INV-18 are proved too. They ship with EXE-01 and PLN-01, which start when CON-03 (noema#120) merges.

## Session 2026-11-03 · TST-01

### Session summary
- What changed (PR links): the TST-01 pull request on `test/TST-01-execution-invariants`.
- Why: REQ-TST-002; G1 needs the property tests green.
- Risks, and how they are contained: a new dependency (`pgregory.net/rapid` v1.3.0, MPL-2.0), the same line as noema#122 and recorded in Noemai-org/docs#94; test-only. No production change.
- Tests added (INV-03, INV-07, INV-08): three property tests in `execution/controller/inv_property_test.go`. Mutation-checked four ways.
- Regression cases added: none (no defect found in the controller).
- Debt entries: none.
- Conflicts raised: none. One reading recorded: INV-03's "target … match the call" is enforced by the grant's signature, not by comparison with the plan, because a plan step has no target.

### Pre-work for the next agent
- Exact next step: EXE-01 (registry loader, static scan, INV-05) and PLN-01 (planner, plan hash, INV-18) once noema#120 is on `main`.
- Do not touch: noema#94, noema#90 (held, DEBT-01).

## Session 2026-11-04 · TST-01 · Codex review 5763774986
- **Finding:** deleting the five `rapid` replay files removes durable regression evidence. **Checked, and not accepted as stated:** the INV-03 file replays the defect "a target that does not match, validly signed", which my first draft had wrong (the controller correctly dispatches such a grant; I removed the defect, so the file's draw no longer means that case); the INV-07 and INV-08 files came from my own deliberate mutation runs (a 5xx classified as failed, a different key). None was a counterexample against the real code, and replaying them would mislead.
- **What I did with the substance:** the evidence should be durable and seed-free. Each property body is now a check function; `rapid` draws it, and three new tests run it exhaustively (`TestINV03EveryDefectUnderEveryFault`: 14 defects × 7 faults × 1 and 3 presentations; `TestINV07EveryFaultAndEveryCrashPoint`; `TestINV08EveryCrashPointUnderEveryFault`). The four mutations (signature check skipped, expiry skipped, key differs, 5xx as terminal failure) are each caught by the exhaustive tests alone (run and reverted).
