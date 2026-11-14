# Status · TRU-06 intake T10 to T12 (Core Engineer 1, drafted by a helper session) · reconciliation, policy, clocks

Updated 2026-11-14. The second of four PRs of the Trust assembly (the G1 card of noema#436; rulings A205 and A188). Builds T10 (reconciliation and the freeze), T11 (policy) and T12 (clocks) of `trust/intake` behind consumer-defined ports. T7 to T9 and T13 to T15 still return `ErrNotBuilt`, so an evaluation still ends in UNKNOWN INTERNAL_ERROR at T7 and nothing can be allowed.

## State: drafted by a helper session for C1's review; not pushed, no PR

## Session 2026-11-14 · TRU-06 intake T10 to T12

### Session summary
- What changed (PR links): none yet. Branch `feat/TRU-06-intake-t10-t12`, two commits: the failing tests first, then `trust/intake/{ports_t10_t12.go,steps_t10_t12.go,steps_t10_t12_test.go}`, small edits to `ports.go` (three ports, `Request.EvidenceSnapshotID`), `intake.go` (`newEvaluation`: one clock reading and one basis builder per evaluation), `gates.go` (steps wired, T4 keeps the capability), `doc.go`, `README.md`, and `gates_test.go` (its spy supplies the three new ports).
- Why: T10 to T12 exist and have no production caller (`reconcile.FreezeGate`, `policy.Set.Evaluate`, `clocks.Evaluate`); the intake is where they are called.
- Risks, and how they are contained: it adds no allow path. Every port error, panic, empty or malformed answer is UNKNOWN INTERNAL_ERROR (`decide` runner). T10 and T12 may stop only with ESCALATE and only with a pack reason code the port gave; T11 uses the rule's effect and defaults to REJECT `NO_PERMITTING_RULE`. No number is in code: the at-risk fraction, the wait time and the freeze list come from the ports.
- Tests added (REQ-TRU-004, REQ-TRU-006, REQ-TRU-007, REQ-RES-001, REQ-RES-002, REQ-NFR-002, INV-03, INV-09, INV-16, INV-21): `steps_t10_t12_test.go`: every stop outcome with its reason code and authority layers, default deny (four forms), the freeze (four cases), WAIT time, a clock not started, AT_RISK (fraction from the port), BREACHED, invalid clock inputs (five), error and panic at every port method, RecordPolicy once with the deciding rules, the basis of a CONFIRM, nothing recorded by T12, determinism, wiring, configuration.
- Regression cases added: the above. Mutation check: 15 mutants of the new code, all caught.
- Debt entries: none.
- Conflicts raised: none. Decisions no rule covers are listed in the hand-over report (authority layers per outcome, a frozen target with no pack mapping, an unresolved intent at T10 to T12, `Request.EvidenceSnapshotID`).

### Pre-work for the next agent
- Exact next step: C1 reviews; PR 3 (T7 to T9: T9 seals the snapshot and calls `RecordEvidence`, then `Request.EvidenceSnapshotID` goes); PR 4 (T13 to T15, the Decision Record, `Trust.Decide` adapter by EL).
- Open questions (owner): the Q1 pack data (at-risk 25 percent remaining, A205) has no field in `clock.v1.json`; the `Clocks` adapter must supply the ratio from provisional pack data (founders). The pack mapping of CONTRADICTORY_STATE to an ESCALATE reason code must exist, or a frozen target with a blocked capability is UNKNOWN (PCK owner). The `Policies` adapter must read `admitted_at` itself for AT_ADMISSION rules and compute `ReevaluateAt` from `wait_seconds` (C1).
- Files to read first: `trust/intake/{ports_t10_t12.go,steps_t10_t12.go}`.
- Do not touch: `trust/decide`, `trust/reconcile`, `trust/policy`, `trust/clocks`.

## Messages
- 2026-11-14 · C1 → helper session (task statement): build T10 to T12 tests first; the helper's output is unreviewed input. Receiver used it for: this branch. Evidence: the two commits of `feat/TRU-06-intake-t10-t12`.

## Session 2026-11-14 · TRU-06 · the Security Reviewer's blocking finding on noema#570 (T10)
### Session summary
- What changed (PR links): noema#570. T10 no longer lets a classification go on just because the pack mapped nothing: with no pack mapping, only the settled classifications (SUCCESS_CONFIRMED, REJECTED_CONFIRMED, REVERSED_CONFIRMED, DEBIT_POSTED_CREDIT_UNCONFIRMED, SETTLEMENT_PENDING, REVERSAL_PENDING, PARTIAL) go on; CONTRADICTORY_STATE, EXECUTION_OUTCOME_UNKNOWN, POSSIBLE_DUPLICATE, EVENT_ORDER_UNCERTAIN, INSUFFICIENT_EVIDENCE and any value that is not a classification fail closed (UNKNOWN INTERNAL_ERROR). A frozen target goes on only with a capability the pack allows under the freeze; an intent that names none now fails closed. A test reads the declared classifications out of `reconcile/types.go` and requires each to be in exactly one of the two sets, so a thirteenth cannot go on by default.
- Why: INV-16 and "unknown means refuse or hand to the team"; the pack may add escalations but must not be the only thing between a contradiction and an ALLOW.
- Risks, and how they are contained: **the split of the twelve classifications into the two sets is a provisional safer reading (AGENT-20), not a ruling.** If the Architect puts a classification in the other set, the table is one function and one test list. Red first at `1af4cadc` (three tests failed), then the implementation.
- Not done here: the plan's capabilities are checked against the freeze at T14 (noema#572 and the integration after noema#567), because the plan does not exist at T10. The Security Reviewer's notes 3 to 6 (policy versions on the Record, the clock routing README, the snapshot's scope) are carried to that integration.
- Tests added (REQ-RES-001, REQ-RES-002, INV-16): `TestT10GoesOnWhenThePackMapsNothingToASettledClassification`, `TestT10AClassificationThatMustNotGoOnFailsClosedWhenThePackMapsNothing`, `TestT10EveryDeclaredClassificationIsEitherSettledOrStopsWhenUnmapped`, `TestT10AFrozenTargetWithNoNamedCapabilityAndNoPackMappingFailsClosed`; replaced (not weakened): `TestT10GoesOnWhenThePackMapsNothing` (it asserted POSSIBLE_DUPLICATE goes on) and the "frozen, the intent names no capability" row of the freeze test.
- Debt entries: none. Conflicts raised: none; an Architect question is on the PR.
### Pre-work for the next agent
- Exact next step: after noema#567 merges, merge `main` in and check the plan's capabilities against the freeze at T14.
- Do not touch: the Reconciliation port's field names (they are the adapter's contract).
