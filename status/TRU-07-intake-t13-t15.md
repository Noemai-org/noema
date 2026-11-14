# Status · TRU-07 intake T13 to T15 (helper session for Core Engineer 1) · confirmation, plan, the recorded decision

Updated 2026-11-14. The Trust intake's last three steps (the G1 card of noema#436; rulings A204, A205, A186, A197). This is unreviewed input for C1: C1 reviews, changes and pushes it.

## State: built on branch `feat/TRU-07-intake-t13-t15` in a local worktree; not pushed, no PR

## Session 2026-11-14 · TRU-07 intake T13 to T15

### Session summary
- What changed (PR links): none yet. `trust/intake/{ports_t13_t15.go,gates_t13_t15.go,t13_t15_test.go,late_ports_test.go}` new; `ports.go` (three ports in `Ports`), `intake.go` (`Result.DecisionID`, `Result.GrantIDs`, `evaluation.run`), `gates.go` (the three steps wired, `basis` and `late` on the evaluation, T3 keeps `needsWrite`), `gates_test.go` (spy gets the three late ports), README and doc.go.
- Why: T13 to T15 are the part of the intake that decides CONFIRM, plans, and writes the Decision Record before an ALLOW is returned.
- Risks, and how they are contained: it adds the only allow path of the intake. ALLOW is built only at T15, with all three layers PASS and the basis of the builder, and stands only after `Recorder.Record` returned a decision ID and one grant per write step; any error, panic, malformed result or ended context is UNKNOWN. T7 to T12 are not built, so no full evaluation allows yet.
- Tests added (REQ-TRU-002, REQ-TRU-008, REQ-TRU-009, REQ-PLN-002, REQ-NFR-002, INV-16, INV-21, INV-25, EC-E16): `t13_t15_test.go`; one test in `gates_test.go`. Red first at `686c277d`.
- Regression cases added: the above.
- Debt entries: none.
- Conflicts raised: none. Gaps listed below.

### Open points for C1 and EL
- `trust/decide` has no CONFIRM reason code, so the T13 CONFIRM carries no reason (decide accepts it); T13 needs none in PERD 23.3.
- The confirmation binding (`confirm.BindingHash`) binds the plan hash and the params hash, which T14 produces after T13. The `Confirmations` adapter must work out the binding itself (it may ask the Planner); the intake passes only the admitted intent.
- `recorder.Request` also needs `PolicyID`, the evidence set hash, `TargetVersions` and a `reconcile.Permission` per step. The intake carries none of them: the composition's `Recorder` adapter must take them from the stores T9 to T11 and T10 used. A CONFIRM (T11 or T13) and a UNKNOWN PLAN_INVALID are not recorded by this intake: T15 records only an ALLOW candidate.
- Other helpers adding T7 to T12 also need the builder: `evaluation.basis` is set in `Evaluate`.

### Pre-work for the next agent
- Exact next step: merge with the T7 to T12 branches (conflicts expected in `gates.go` `steps()` and the `evaluation` struct), then the EL adapter for `Recorder` over `recorder.Writer`.
- Open questions (owner): the confirmation store and its binding at T13 (EL, C1).
- Files to read first: `trust/intake/gates_t13_t15.go`, `ports_t13_t15.go`, `trust/recorder/recorder.go`.
- Do not touch: `trust/decide`, `trust/recorder`, `trust/grants`, `trust/confirm`.
