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
