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
