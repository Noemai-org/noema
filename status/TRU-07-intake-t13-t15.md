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

## Session 2026-11-15 · TRU-07 · integration after noema#567, #570 and #571 landed
- What changed: `main` is merged into this branch. The three siblings conflicted in the shared files; resolution: the Ports struct and the nil-port check hold all fourteen ports (T1 to T6, Candidates, Ownership, Evidence, Reconciler, Policies, Clocks, Confirmations, Planner, Recorder); the basis builder is `bb` everywhere (this PR's `basis` renamed); the `resolved()` of #571 is kept (it also checks the intent's version) and this PR's copy and its `errAnswer` duplicate are removed; the steps table has T1 to T15 built, so `notBuilt` is gone. This PR's test helpers are prefixed (`f13`, `t13Tenant`, ...) so the three test files build in one package.
- **New test, the integration the Security Reviewer asked for (the full T1 to T15 path):** `TestAReadThatPassesEveryStepIsAllowedAfterTheDecisionRecordIsWritten`: with every port answering well a read is ALLOW with the recorded decision ID, all fifteen steps in the trace, and the Recorder called last; and `TestAPlannerThatRefusesEndsUnknownAtT14AndNothingIsWritten`. They replace the two tests that said the evaluation ends at T7 or T13. The port fakes are still doubles: the real adapters are EL's.
- Verified: `gofmt`, `go vet`, `go test -race ./trust/... ./platform/ceilings/` and the lint pass on the merged tree. No step's behaviour changed.
- Still open and not done here: the two blocking findings of the Security Reviewer on this PR (the confirmation bound to the plan and spent once; the plan checked against T4, T8 and T10) and notes 3 to 7; the plan with the questions is on the PR. Red first, after the questions have an answer or the defaults are accepted.

## Session 2026-11-15 · TRU-07 · the Security Reviewer's blocking finding 2: the plan is checked against T4, T8 and T10
- What changed: after the Planner answers, T14 checks every step of the plan (`planAgrees`): its capability is registered and not prohibited (a new `Capabilities.Check`, asked of the registry for each step, not only for the capability the intent names, T4); its target is the one the stored intent names (a new `AdmittedIntent.TargetRef`, resolved by the intent store's adapter), which T8 checked the principal owns, and a write whose intent names no target has nothing owned to act on (T8); and on a target frozen for a contradiction its capability is one the pack allows under the freeze (T10 now keeps the freeze for T14). A plan that disagrees ends UNKNOWN PLAN_INVALID with nothing recorded; a registry that cannot answer ends UNKNOWN INTERNAL_ERROR; one bad step among good ones grants nothing (the Recorder is not called). This also closes the second half of the Security Reviewer's blocking finding 2 on noema#570 (the freeze against the plan's capabilities).
- Red first at `a8fb2f37`: at that head a plan with an unregistered or prohibited capability, another target, or a capability not allowed under the freeze was ALLOW with its grants recorded. Nine mutants of the check, each killed.
- Contract for the adapters (EL, C2): the intent store's adapter sets `AdmittedIntent.TargetRef` from the stored intent (never from the request); the registry adapter answers `Capabilities.Check`; `Ownership.OwnsTarget` checks that same target.
- Not done here: blocking finding 1 (the confirmation bound to the plan and spent once) waits for the answers to the three questions on the PR; notes 3 to 7.
