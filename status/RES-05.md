# RES-05 · convergence windows in the Reconciler

Task state: green written; CI to be read. Owner: C2 (A640: C2 keeps the Reconciler and the clocks and phase routing). Instruction: A642, interim, relayed by the Founder, not yet committed to `docs`.

## Session 2026-11-24 · RES-05

### Session summary
- What changed: `trust/reconcile` reads an optional `convergence_window_seconds` on a matrix pair (`incompatible`) and on a `single_valued` entry. A disagreement is expected lag while `now` is before the pair's deadline, the earlier fact's `effective_at` plus the window. The result then carries `Wait{Until, Labels}`, its classification is neither the freeze nor a verdict (a rule that would give a terminal classification falls back to INSUFFICIENT_EVIDENCE), its `ValidUntil` never passes the deadline, and `FreezeGate.PermitResult` refuses every capability with `ErrWaiting`, the freeze allow-list included. At the deadline and after it the disagreement is a contradiction again (CONTRADICTORY_STATE: the freeze and the escalation of the pack's routing). A pair with no window is a contradiction at once (fail closed, as before).
- Why: REQ-RES-005 and A642.
- Needs: RES-01 Done. PCK-05 (a window for every compared pair, a missing window fails packcheck) is Backlog: A642 is taken as the waiver in writing.
- Reading taken (safer, AGENT-20): the window starts at the EARLIER of the two facts (the shorter wait); the deadline itself is already the contradiction (`now < deadline` waits).
- Tests: `trust/reconcile/window_test.go` (REQ-RES-005, REQ-RES-002, INV-20, INV-21).
- Not done: no window value is declared in `packs/fintech/recon_rules/rules.v1.json`. The PERD and the SoT state none, so every pair stays contradictory at once until the Founders decide (AGENT-16). Trust's mapping of `Result.Wait` to the WAIT outcome with `reevaluate_at` is C1's (`trust/intake`); the durable timer is the orchestrator's.
- OPEN entry text for the Steward: OD-RES05-1, the convergence window of each compared fact pair (owner: Founders). Safest default: no window (fail closed). Evidence to invalidate: settlement and reversal lag observed in the benchmark world.

## Pre-work for the next agent
- Exact next step: C1 maps `Result.Wait` to a WAIT decision with `reevaluate_at = Wait.Until` and the Record event; PCK-05 declares the windows once decided and adds the packcheck rule that a compared pair without a window is reported.
- Files to read first: `trust/reconcile/reconcile.go` (`splitByWindow`), `freeze.go` (`PermitResult`), `window_test.go`.
- Do not touch: `trust/intake`, `trust/recorder`.

## Session 2026-11-25 · RES-05 · A659 (interim, relayed)
- A future-dated `effective_at` means NO WINDOW: the pair is contradictory at once (CONTRADICTORY_STATE, the freeze and the pack's escalation), never a wait, and the instant is never clamped to now: `deadlineOf` returns 0 when either fact is dated after `now` (a fact dated exactly now still opens its window).
- A waiting answer is now always INSUFFICIENT_EVIDENCE (the fallback), not only when a rule would give a terminal classification: a pending settlement, set against an in-window disagreement, would otherwise give SETTLEMENT_PENDING, which the Trust intake lets go on. The intake never lets INSUFFICIENT_EVIDENCE go on.
- Callers of `FreezeGate.Permit` and `PermitTarget` (every `.go` in the module): one production caller in the Trust intake (T10, `freezeBlocks`: it takes the sealed classification, so a waiting answer, sealed as INSUFFICIENT_EVIDENCE, is never let on), `cmd/grantdrill` (a synthetic target, nothing reconciled), and the gate itself; all the others are test fixtures that build a permission for a synthetic world. `permit_callers_test.go` walks every non-test file and fails on a new caller: it must use `PermitResult`, which refuses a waiting result.
- The window member is `convergence_window_seconds`; `rules.v1.json` is not changed here, and #963 (CAP-02, descriptor v2 contracts, per its title and its file list) does not touch it.

## Session 2026-11-25 · RES-05 · Codex's P2 on #949
- Finding (real, confirmed in the code): `Result.Contradictions` held every pair, including pairs still inside their window, while `Contributing` held the evidence of the expired pairs only. A freeze row built from the result (#958) would carry a waiting pair's label with none of its evidence.
- Fix: `Result.Contradictions` is the freezing set only (pairs outside their window or with none); the new `Result.InWindow` holds the pairs still inside their window, each with its deadline. When every pair is inside its window, `Contradictions` is empty and `Wait` is set; in a mixed result `Wait` is nil and the result is contradictory.
- A second defect the new test found: with one pair expired and another inside its window, the rules no longer saw the waiting pair, so a rule that guards against it (the reversal rule) gave REVERSED_CONFIRMED, a verdict while a contradiction stood. The rules now see every disagreement as soon as one is outside its window.
- #958 consumes `Result.Contradictions` and `Result.Contributing` only (`NewFreezeSet`), so it needs no change to use the fix; it gets it when it merges main. A mixed-pair test for the freeze row can be added to #958 after #949 merges.

# Session 2026-11-26 · RES-05 · A708 (C1, interim, relayed)

## Session summary
- What changed: `recon-rule-set.v1.json` allows `convergence_window_seconds` 0 (minimum 0); the 0 fixture moved from invalid to valid; packcheck docstring and the contracts README carry the note; generated Go and Pydantic types regenerated. The loader already accepted 0 (a negative is refused): a new test pins a version 2 set with every window 0 (CONTRADICTORY_STATE at once, no wait).
- Why: A708, REQ-RES-005 (0 is a window with no tolerance, fail closed).
- Risks: loosening within v1 (A698); no safety threshold is changed (AGENT-16): no pack declares a window by this change.
- Tests added: `tools/contracts/test_recon_rule_set.py`, `tools/packcheck/test_packcheck.py`, `trust/reconcile/window_test.go` (A708).
- Regression cases added: the valid fixture `fin_version_2_window_zero.json` (red at `f4eb552ab`).
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: both Founders on the PR. PCK-05 (EL) declares real windows once decided.
- Do not touch: `rules.v1.json` values.
