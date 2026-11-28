# RES-02 · the contradiction freeze row

Task state: freeze-row facts and effective answer built (green written); the Record side and the US-14 end to end are C1's and EL's (see below). Owner: C2 (A640). Instruction: A642 (`DEC-2026-11-24-architect-rulings-a624-a643`) and the Founder's decision D1 (`DEC-2026-11-24-founder-decisions-t10-wait-and-921`), both committed on docs main.

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

## Session 2026-11-26 · RES-02 · review findings on #958 (A677)
- Merged main (#949 RES-05 is in): `Result.Contradictions` is the freezing set only and `Result.InWindow` holds the pairs still waiting.
- `NewFreezeSet` takes its labels from the freezing set alone and its evidence from the union of both ends of each freezing pair and `Contributing`, so the row always names what disagreed, even when the matched rule `uses` nothing (EL 3). A waiting pair is in neither `Labels` nor `Evidence` (A677, EL 2; test with one expired and one in-window pair).
- A freeze cleared by an ESCALATION, then a second contradiction inside a window: not frozen while it waits, frozen at the deadline by a new freeze row, never by a clearing (C1 2, EL 5).
- `ReadFreeze(ctx, reader, tenant, target)` returns `FreezeUnknown` on any error (even if the reader returned a value with it), on a missing reader and on an out-of-range value (SR 2). The adapter calls only this, never the port.
- `NewFreezeCleared` checks the shape of the decision id: the `dec_` prefix, something after it, no control character or space (SR 1, EL 1; cheap, in this package).
- The freeze allow-list now has its source: #910 merged (`trust_limits.json`). EL owns the consumer, which must fail closed on a missing entry. Not built here.

### Acceptance list for the Recorder (C1; not built here)
`FreezeRecorder.RecordFreezeCleared` must, in the same transaction as the append and against the Record, refuse a clearing unless the decision (SR 1, EL 1):
1. exists and belongs to the same tenant;
2. is a RESOLUTION or ESCALATE outcome (kind), not any other decision;
3. names this target (not another intent's decision in the same tenant: the gap Codex found on #733);
4. has a `dec_`-prefixed id with no control character (the shape check here is the cheap first line, not the proof).
Each with its own test. Also (C1 note 1): the freeze row is appended in the decision's transaction before the grant is issued, so a freeze that cannot be recorded stops the write (PERD 32.4, EC-E16), with the failure case tested; `FreezeReader` answers from the latest of SET and CLEARED for the target and the adapter reads it only through `ReadFreeze`.
