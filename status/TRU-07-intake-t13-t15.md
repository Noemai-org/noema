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

## Session 2026-11-15 · TRU-07 · the carried notes of ruling A226 and the Partial classification of A225
- What changed: **T1** refuses a request whose correlation ID is not the stored intent's, or is empty (REJECT INTENT_INVALID; the note of the Security Reviewer on noema#567). **T9** refuses a snapshot that names another intent or version, or none (UNKNOWN: a new `SnapshotEvidence.IntentID` and `IntentVersion` that the Evidence adapter sets from the snapshot's own record; the note on noema#571). A negative evidence age was already refused by the basis builder (`RecordEvidence`); the test stays as a regression case. **T10**: a Partial classification is unsettled and fails closed when the pack maps nothing (A225, third bullet; the drift test and the lists follow).
- Rulings: A225 and A226 are interim (docs#182, not merged); the Steward's reading is that A226's notes go into this PR because noema#567 and #571 have merged.
- **Not done here, and why:** A225 first bullet (reads continue on a frozen target; only writes are refused, as a typed refusal) and second bullet (T12 escalates by itself when the pack's routing omits a breached clock). Both need a reason code for an ESCALATE or REJECT that no pack supplied, and the core reason enum is closed (17 codes, `contracts/state-registry.md`) with none that fits. The question and my default are on this PR.
- Adapter contract (EL, C2): the Evidence adapter sets `SnapshotEvidence.IntentID` and `IntentVersion` from the snapshot's record (never from the request); the intent adapter's `AdmittedIntent.CorrelationID` is the correlation the intent was admitted under.

# Session 2026-11-16 · TRU-07 intake: A230, the confirmation is bound to the plan (noema#572)

## Session summary
- What changed (PR links): noema#572. `Confirmation.PlanHash` (the plan hash a Given confirmation was given for) and `RecordRequest.ConfirmationPlanHash`. T13: a Given answer without a well-formed plan hash is UNKNOWN (fail closed). T14: if the plan it builds has another hash than the confirmation's, UNKNOWN `PLAN_INVALID`, nothing recorded. T15 hands the confirmation's plan hash to the Recorder; the writer (noema#603) refuses the same mismatch as the last line of defence and spends the confirmation once.
- Why: A230 (interim, relayed): the writer refuses an ALLOW whose confirmation's plan hash differs from the plan T14 built; single use is noema#603. This is the Security Reviewer's blocking 1 on this PR, in the shape the Architect ruled.
- A correction of mine: my red push expected CONFIRM at T14 for a confirmation given for another plan (the Q-B default). PERD 23.3 gives T14 an empty stop set, so the decision engine allows only UNKNOWN there; the test and the code say UNKNOWN `PLAN_INVALID`. CONFIRM-again on a changed plan would need the check at T13, before the plan exists, and is not ruled.
- Risks, and how they are contained: the confirmation store's adapter (EL) must return the plan hash the request was created for; without it every Given answer is UNKNOWN (safe). A230 is interim until docs#185 merges.
- Tests added (REQ-TRU-009, INV-17, INV-25): `TestT14AConfirmationGivenForAnotherPlanIsPlanInvalidAndNothingIsRecorded`, `TestT13AGivenConfirmationWithoutAWellFormedPlanHashIsUnknown`, and the Recorder request carries the plan hash; seen failing first (local, then CI red at `61e0ea59`).
- Not done: the role check per plan step, the `Dispatches.Count` step after T14 and the A225 bullets 1 and 2 (the reason-code question is not answered), and the Security Reviewer's notes 3 to 7 of A226 (still carried).

## Pre-work for the next agent
- Exact next step: EL's confirmation adapter returns `PlanHash`; the Recorder adapter passes `ConfirmationPlanHash` and `ConfirmationEventID` to `recorder.Request`, and maps `recorder.ErrConfirmationSpent`, `ErrConfirmationPlanMismatch` and `ErrDecisionConflict` to UNKNOWN (never ALLOW).


# Session 2026-11-17 · TRU-07 intake, the second round of noema#572 (A224, A225, A254, A255; the Security Reviewer's notes 1, 5 and 7)
- What changed: T10 (a frozen target: reads go on, a blocked write is ESCALATE `TARGET_FROZEN`); T12 (an unrouted breached clock is ESCALATE `CLOCK_BREACHED_UNROUTED`; then the `Dispatches` port: spent budget is ESCALATE `DISPATCH_BUDGET_EXHAUSTED`); T14 (`Capabilities.Check(ctx, id, role)`, a step the role may not use is PLAN_INVALID); T15 (a sealed copy of the basis builder, `decide.BasisBuilder.Sealed`). Test fixtures use `cfe_` confirmation IDs (note 1). The determinism test cannot pass on a request that ignores its inputs: `TestT13ToT15TheRecordRequestFollowsTheInputsAndOnlyTheInputs` (note 7).
- **For the Architect:** the three reason codes can only be ESCALATE at T10 and T12 (PERD 23.3 stop sets: T10 {Escalate}, T12 {Escalate}, T14 {}), so the frozen refusal is an ESCALATE and the dispatch budget is asked at T12 for the capability and target the intent names, not "after T14" as relayed. A plan step on another capability is covered by the runner's backstop. If T14 should be able to escalate, that is a PERD 23.3 change.
- Not done: nothing of A225, A226 or the notes is left except note 3 and 4 (TRU-14, the end-to-end ALLOW test, both on the debt list).
- Tests (REQ-TRU-002, REQ-TRU-006, REQ-RES-002; INV-03, INV-16, INV-21): ten new in `a255_round_test.go`, one in `decide/basis_sealed_test.go`; red first at `6fa6dae0` (seven failed). Mutant: a Record request with a constant plan hash fails the new determinism test.
- Adapters (EL): `Capabilities.Check` fills `RoleAllowed`; `Dispatches.Count` answers from the ledger's dispatch events (the limit is the ledger's).

# Session 2026-11-18 · TRU-07 · the third round of noema#572 (Codex P1 and P2, C2's note)

## Session summary
- What changed (noema#572): `Capabilities.Authorized` (pack data, read once per evaluation, with the intent's own capability); T12 asks the dispatch budget for every capability of the set; T14 refuses a step outside it; T15 refuses a repeated grant ID.
- Why: Codex P1 (a planner could substitute another valid, role-allowed capability on the same target and get a grant) and P2 (the budget was asked only for the intent's capability, so a plan's other capability could receive an ALLOW grant with its budget spent); C2's note (repeated grant IDs passed the shape check).
- Risks, and how they are contained: the authorized set is a proposed default (the capabilities of the pack action graph the intent's capability roots) and is not yet confirmed by the Architect; the port has no production adapter yet, so nothing runs with it; an unreadable set fails closed; the budget check is conservative (may escalate for a capability the plan would not use).
- Tests added (REQ-TRU-003, REQ-TRU-004, REQ-CAP-002; INV-03, INV-17, INV-18): `plan_authorization_test.go` (seven red at `13137944`, one guard, one shared-read test added green). Mutants killed: no authorization check, budget asks nobody, duplicate grant IDs accepted, set read every time, budget off by one.
- Regression cases added: the same. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the Architect confirms the authorized-set definition (or gives another) on noema#572; the production adapter of `Capabilities.Authorized` reads the pack action graph rooted at the intent's capability.
- Do not touch: the stop sets of PERD 23.3.
