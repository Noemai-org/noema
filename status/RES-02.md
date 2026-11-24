# RES-02 · the contradiction freeze row

Task state: freeze-row facts and effective answer built (green written); the Record side and the US-14 end to end are C1's and EL's (see below). Owner: C2 (A640). Instruction: A642, interim, relayed, not yet committed.

## Session 2026-11-24 · RES-02

### Session summary
- What was already on main (RES-01 and the first RES-02 work): rules R1 to R7 as pack data, `FreezeGate`/`Permission`, TARGET_FROZEN at T10 and T14, `Frozen` derived from the sealed classification.
- What changed: `trust/reconcile/freeze_row.go`: `NewFreezeSet` (the facts of CONTRADICTION_FREEZE_SET, only for CONTRADICTORY_STATE, sorted, deterministic), `NewFreezeCleared` (the facts of CONTRADICTION_FREEZE_CLEARED, only with a recorded RESOLUTION or ESCALATION decision id: a changed classification is not one), `EffectiveFrozen(derived, recorded)` (a recorded freeze outlives the classification until cleared; an unknown recorder answer is frozen; a cleared freeze does not mask a new contradiction), and the consumer-side ports `FreezeRecorder` and `FreezeReader`.
- Safer reading taken (AGENT-20, A642): a recorded freeze is lifted only by a Trust-recorded decision, never because the evidence converged.
- Tests: `trust/reconcile/freeze_row_test.go` (REQ-RES-002, INV-20, US-14 in part). Hand mutants: 6 run, 6 killed.

### What is NOT done, and why
1. **Appending the events.** `CONTRADICTION_FREEZE_SET` and `_CLEARED` are Record event types, but no code writes them (and no code writes STATE_RECONCILED). The Recorder is C1's (`trust/recorder`, `trust/intake`): it must implement `FreezeRecorder` in the same transaction as the decision that causes the freeze, and `FreezeReader` from the Record (the latest of SET and CLEARED for the target). A migration is not expected (the event types exist); if C1 needs one, 0041 is next free (0040 is C1's key).
2. **The allow-list.** `AllowedUnderFreeze` comes from `trust_limits.json` (#910, A544, A567, A579, A596), which has not merged: the production accessor does not exist, and EL's consumer must fail closed when the entry is missing. Not built here.
3. **US-14 end to end** ("the freeze row exists; any other write to the frozen target is refused by Trust"): it needs 1 and 2 and the reconciler adapter passing `EffectiveFrozen` in place of the derived flag. The Trust refusal itself (TARGET_FROZEN at T10 and T14) is already merged and tested.
- Open entries: none new. The earlier question stands: REQ-RES-002's text says "descriptors" while A544/A567 put the list in `trust_limits.json` (A567: the conflicts register; the Steward drafts the CR).

## Pre-work for the next agent
- C1: implement `FreezeRecorder` and `FreezeReader` over the Record; call `NewFreezeSet` when T10 sees CONTRADICTORY_STATE, and `NewFreezeCleared` from the resolution or escalation decision.
- EL: the reconciler adapter (`orchestrator/portadapters/trust/reconciler`) passes `EffectiveFrozen(derived, rec)` as `Reconciliation.Frozen`.
- Do not touch: `trust/intake`, `trust/recorder`.
