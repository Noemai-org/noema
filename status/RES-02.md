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

# Session 2026-11-27 · RES-02 · A642, A677 (C1, interim, relayed): the Record side of the freeze row

## Session summary
- What changed: `orchestrator/portadapters/trust.FreezeRow` implements `reconcile.FreezeRecorder` and `reconcile.FreezeReader` over the Record. `RecordFreezeSet` and `RecordFreezeCleared` append CONTRADICTION_FREEZE_SET and CONTRADICTION_FREEZE_CLEARED as `core-trust`, each in a transaction of its own; `RecordFreezeCleared` also needs a recorded freeze for the target and a decision of the tenant that lifts it (an ESCALATE for an escalation, an ALLOW for a resolution). `RecordedFreeze` reads the tenant's chain through `record_read_chain_t` (as the tenant's own read login) and returns the latest row for the target; any failure is an error, which `reconcile.ReadFreeze` turns into FreezeUnknown. `reconcile.FreezeSet` gained a `CorrelationID` the caller sets (the Record's events carry one).
- Why: A642 and A677: the port was on `main` (#958) with no implementation.
- Risks (listed, not decided): (1) The append is not in the transaction of the decision that caused it (the port passes no transaction). Both losses fail closed: a lost SET leaves the derived freeze, a lost CLEARED leaves the target frozen. (2) The reader reads the whole tenant chain per question (the only read path into the Record is `record_read_chain_t`): a dedicated read function by target is a candidate, DEBT-60 proposed. (3) That the clearing decision names the target is NOT checked: the Record holds no link from a decision to a target. Needs a ruling (Architect). (4) The emitter `core-trust` is my reading of "the Recorder's side" (PERD 32.1); the Record's events table does not say which emitter writes these two types.
- Tests added (REQ-RES-002; INV-20): `freeze_row_test.go` (none, set, cleared only by a recorded escalation, nothing to clear, a new contradiction after a clearing, a reader that cannot answer). Regression cases: the red at `46cf56090`. Debt entries: DEBT-60 proposed for the Steward (the whole-chain read). Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the composition builds `NewFreezeRow` and gives it to the reconcile adapter; the caller of `NewFreezeSet` sets `CorrelationID`.
- Do not touch: `trust/reconcile` rules.

# Session 2026-11-27 · RES-02 · A756 (C1, interim, relayed): the target reference in both events, verified

- Both CONTRADICTION_FREEZE_SET and CONTRADICTION_FREEZE_CLEARED carry `target_ref` in their payload. The reader checks it against the row's own object reference: a row that names another target, or none, is a defect and the answer is an error (FreezeUnknown, frozen), which also stops `RecordFreezeCleared`. This is the verification against the freeze row; the decision-to-target link of the first session is now carried by the event, not the Decision Record (which has no target member). `core-trust` is confirmed as the emitter (A756). DEBT-60 (the by-target read function, instead of reading the whole tenant chain) is the Steward's to file; I propose it. Red at `45bf17f88`.

# Session 2026-11-28 · RES-02 · SR on #1035 (C1)

- Finding 1 (a decision of the right outcome but of another investigation could clear a freeze): `RecordFreezeCleared` requires the named decision to be recorded after the SET (`decisionFollows`) and to carry the SET's correlation; an ALLOW or ESCALATE of another investigation, or one recorded before the freeze, is `ErrClearingDecision`. The Decision Record has no target member, so the link is the investigation and the order on the chain; a `target_ref` member on the Decision Record is a contracts PR for both Founders (AGENT-15): LISTED, not done here.
- Finding 2 (the SET is outside the decision's transaction): `RecordFreezeSet` returns an error whenever the append fails, and `ErrFreezeCorrelation` when the SET names no investigation, so the caller treats it as a refusal (no grant, no write, retry). Giving `reconcile.FreezeRecorder` a transaction parameter, as A724 did for the confirmation store, changes `trust/reconcile` (pure) and is LISTED for a ruling.
- Condition 3 (race): the clearing takes a transaction-scoped advisory lock per tenant and target and reads the state after the lock, so concurrent clearings write one CLEARED. Condition 5: DEBT-60 (the by-target read) stays proposed.
- Red at `b9ec82404`. The green on the branch is `199480637`; a second green written in this session (`9bafbc7b1`, never pushed) was dropped because that one already passed the same tests (checked: 10 freeze tests pass).
